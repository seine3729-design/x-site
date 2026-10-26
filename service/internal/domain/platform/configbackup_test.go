package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------- 测试替身 ----------

type fakeConfigRepo struct {
	taxonomies []BackupTaxonomy
	ads        []BackupAd
	imported   *BackupData // 最后一次导入的内容
	saved      []BackupRecord
	result     ImportResult
	exportErr  error
	importErr  error
	saveErr    error
}

func (r *fakeConfigRepo) ExportConfig(context.Context) ([]BackupTaxonomy, []BackupAd, error) {
	return r.taxonomies, r.ads, r.exportErr
}

func (r *fakeConfigRepo) ImportConfig(_ context.Context, d BackupData) (ImportResult, error) {
	if r.importErr != nil {
		return ImportResult{}, r.importErr
	}
	copied := d
	r.imported = &copied
	return r.result, nil
}

func (r *fakeConfigRepo) SaveBackup(_ context.Context, b BackupRecord) (int64, error) {
	if r.saveErr != nil {
		return 0, r.saveErr
	}
	r.saved = append(r.saved, b)
	return int64(len(r.saved)), nil
}

func (r *fakeConfigRepo) ListBackups(context.Context, int) ([]BackupSummary, error) {
	return []BackupSummary{}, nil
}

func (r *fakeConfigRepo) GetBackup(context.Context, int64) (BackupRecord, error) {
	return BackupRecord{Format: BackupFormat, Payload: BackupData{Taxonomies: r.taxonomies}}, nil
}

// fakeSigner 用「正文的长度 + 版本」当签名，足够验证「内容变了签名就不匹配」。
type fakeSigner struct{ on bool }

func (s fakeSigner) Enabled() bool { return s.on }

func (s fakeSigner) Sign(format int, payload []byte) (string, error) {
	if !s.on {
		return "", errors.New("未启用")
	}
	return "fake:" + string(rune('0'+format)) + ":" + itoaLen(payload), nil
}

func (s fakeSigner) Verify(format int, payload []byte, signature string) error {
	want, _ := s.Sign(format, payload)
	if want != signature {
		return errors.New("备份签名校验失败：内容与签名不一致")
	}
	return nil
}

func itoaLen(b []byte) string {
	n := len(b)
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

func newConfigSvc(repo *fakeConfigRepo, signer BackupSigner, site *fakeSite) *ConfigService {
	s := NewConfigService(repo, signer, site)
	s.now = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
	return s
}

func sampleData() BackupData {
	return BackupData{
		Menus: []json.RawMessage{}, StaticPages: []json.RawMessage{}, Blocks: []json.RawMessage{},
		Taxonomies: []BackupTaxonomy{{
			Name: "题材", Slug: "genre", Status: "active", Sort: 10,
			Terms: []BackupTerm{{Name: "都市", Slug: "urban", Status: "active", IsChannel: true, Code: "urban"}},
		}},
		Ads: []BackupAd{{
			Name: "首页横幅", Slot: "home_top", CreativeType: "image", Image: "a.png",
			Device: "all", Locales: []string{}, Weight: 10, Status: "active",
		}},
	}
}

// ---------- 导出 ----------

func TestExport(t *testing.T) {
	d := sampleData()
	repo := &fakeConfigRepo{taxonomies: d.Taxonomies, ads: d.Ads}
	svc := newConfigSvc(repo, fakeSigner{on: true}, &fakeSite{version: 7})

	out, err := svc.Export(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if out.Format != BackupFormat {
		t.Errorf("格式版本 = %d，期望 %d", out.Format, BackupFormat)
	}
	if out.Signature == "" {
		t.Error("配了签名密钥时导出应当带签名")
	}
	if out.Site != "v7" {
		t.Errorf("站点版本 = %q，期望 v7", out.Site)
	}
	c := out.Data.Counts()
	if c.Taxonomies != 1 || c.Terms != 1 || c.Ads != 1 {
		t.Errorf("各类数量不对: %+v", c)
	}
	// 未实现的三类导出为空数组而不是 null，三端解析省一次判空
	raw, _ := json.Marshal(out.Data)
	for _, key := range []string{`"menus":[]`, `"static_pages":[]`, `"blocks":[]`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("导出的 JSON 缺少 %s：%s", key, raw)
		}
	}
	// store=true 要留存档
	if len(repo.saved) != 1 || repo.saved[0].Reason != ReasonManual {
		t.Errorf("导出应当留一条 manual 存档，得到 %+v", repo.saved)
	}
}

func TestExport_NoSignerLeavesSignatureEmpty(t *testing.T) {
	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{})
	out, err := svc.Export(context.Background(), 1, false)
	if err != nil {
		t.Fatalf("没配密钥时导出也应当可用: %v", err)
	}
	if out.Signature != "" {
		t.Errorf("没配密钥时签名应为空，得到 %q", out.Signature)
	}
	if len(repo.saved) != 0 {
		t.Error("store=false 时不该留存档")
	}
}

// ---------- 导入：签名 ----------

func TestImport_VerifiesSignature(t *testing.T) {
	signer := fakeSigner{on: true}
	data := sampleData()
	payload, _ := json.Marshal(data)
	sig, _ := signer.Sign(BackupFormat, payload)

	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, signer, &fakeSite{version: 1})
	res, err := svc.Import(context.Background(), 9, Backup{Format: BackupFormat, Signature: sig, Data: data})
	if err != nil {
		t.Fatalf("合法签名应当导入成功: %v", err)
	}
	if !res.Signed {
		t.Error("结果应当标明已验签")
	}
}

// 改过内容的备份必须被拒——这是「勿手改备份」的实际防线。
func TestImport_RejectsTamperedBackup(t *testing.T) {
	signer := fakeSigner{on: true}
	data := sampleData()
	payload, _ := json.Marshal(data)
	sig, _ := signer.Sign(BackupFormat, payload)

	tampered := sampleData()
	tampered.Ads[0].ClickURL = "https://evil.example.com"

	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, signer, &fakeSite{})
	_, err := svc.Import(context.Background(), 9, Backup{Format: BackupFormat, Signature: sig, Data: tampered})
	if err == nil {
		t.Fatal("改过的备份应当被拒绝")
	}
	if repo.imported != nil {
		t.Error("校验失败时不该写库")
	}
	if len(repo.saved) != 0 {
		t.Error("校验失败时不该留存档")
	}
}

// 配了密钥但文件没签名：不能放行，否则签名形同虚设。
func TestImport_RejectsUnsignedWhenSignerEnabled(t *testing.T) {
	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, fakeSigner{on: true}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: sampleData()})
	if err == nil {
		t.Fatal("配了密钥时没签名的备份应当被拒绝")
	}
	if !strings.Contains(err.Error(), "没有签名") {
		t.Errorf("报错应当说明原因：%v", err)
	}
}

// 文件带签名但本环境没配密钥：无法校验，也不能假装通过。
func TestImport_RejectsSignedWhenNoKeys(t *testing.T) {
	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Signature: "fake:1:5", Data: sampleData()})
	if err == nil {
		t.Fatal("带签名但无密钥时应当拒绝")
	}
	if !strings.Contains(err.Error(), "CONFIG_BACKUP_KEYS") {
		t.Errorf("报错应当指出要配什么：%v", err)
	}
}

// 本地开发：两边都没有签名时允许导入。
func TestImport_AllowsUnsignedWhenNoKeys(t *testing.T) {
	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{version: 1})
	res, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: sampleData()})
	if err != nil {
		t.Fatalf("本地开发（都没签名）应当允许导入: %v", err)
	}
	if res.Signed {
		t.Error("没验签时不该标记为已验签")
	}
}

// ---------- 导入：格式版本 ----------

func TestImport_RejectsWrongFormat(t *testing.T) {
	repo := &fakeConfigRepo{}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{})
	for _, format := range []int{0, BackupFormat + 1, 99} {
		_, err := svc.Import(context.Background(), 1, Backup{Format: format, Data: sampleData()})
		if err == nil {
			t.Errorf("格式版本 %d 应当被拒绝", format)
		}
	}
	if repo.imported != nil {
		t.Error("格式版本不对时不该写库")
	}
}

// ---------- 导入：自动备份 ----------

func TestImport_SavesBackupBeforeApplying(t *testing.T) {
	before := sampleData()
	before.Ads[0].Name = "导入前的广告"
	repo := &fakeConfigRepo{taxonomies: before.Taxonomies, ads: before.Ads}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{version: 1})

	incoming := sampleData()
	incoming.Ads[0].Name = "导入后的广告"
	res, err := svc.Import(context.Background(), 42, Backup{Format: BackupFormat, Data: incoming})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("导入前应当自动存档一次，得到 %d 次", len(repo.saved))
	}
	rec := repo.saved[0]
	if rec.Reason != ReasonBeforeImport {
		t.Errorf("存档来源 = %q，期望 %q", rec.Reason, ReasonBeforeImport)
	}
	if rec.AdminID != 42 {
		t.Errorf("存档应当记下操作人，得到 %d", rec.AdminID)
	}
	// 存的是导入前的内容，不是即将导入的内容
	if len(rec.Payload.Ads) != 1 || rec.Payload.Ads[0].Name != "导入前的广告" {
		t.Errorf("存档存成了导入后的内容: %+v", rec.Payload.Ads)
	}
	if res.BackupID != 1 {
		t.Errorf("结果应当返回存档编号，得到 %d", res.BackupID)
	}
}

// 自动备份失败就中止导入：没有回滚手段时不该动数据。
func TestImport_AbortsWhenBackupFails(t *testing.T) {
	repo := &fakeConfigRepo{saveErr: errors.New("磁盘满了")}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: sampleData()})
	if err == nil {
		t.Fatal("自动备份失败时应当报错")
	}
	if !strings.Contains(err.Error(), "已中止导入") {
		t.Errorf("报错应当说明导入已中止：%v", err)
	}
	if repo.imported != nil {
		t.Error("自动备份失败后不该继续写库")
	}
}

// ---------- 导入：站点版本 ----------

func TestImport_BumpsSiteVersion(t *testing.T) {
	site := &fakeSite{version: 5}
	svc := newConfigSvc(&fakeConfigRepo{}, fakeSigner{on: false}, site)
	if _, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: sampleData()}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	// 维度与广告都在前台的读穿缓存里，版本 +1 才能让各端立即拿到新配置
	if site.bumped != 1 {
		t.Errorf("导入后应当让站点版本 +1，调用了 %d 次", site.bumped)
	}
}

// ---------- 导入：校验 ----------

func TestImport_ValidatesData(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*BackupData)
		want   string
	}{
		{"维度标识为空", func(d *BackupData) { d.Taxonomies[0].Slug = "" }, "标识不能为空"},
		{"维度标识非法", func(d *BackupData) { d.Taxonomies[0].Slug = "Bad_Slug" }, "只能用小写字母"},
		{"维度名称为空", func(d *BackupData) { d.Taxonomies[0].Name = "  " }, "名称不能为空"},
		{"维度状态非法", func(d *BackupData) { d.Taxonomies[0].Status = "deleted" }, "状态"},
		{"取值标识重复", func(d *BackupData) {
			d.Taxonomies[0].Terms = append(d.Taxonomies[0].Terms, BackupTerm{Name: "重复", Slug: "urban", Status: "active"})
		}, "同一个标识"},
		{"广告位非法", func(d *BackupData) { d.Ads[0].Slot = "nowhere" }, "广告位"},
		{"素材类型非法", func(d *BackupData) { d.Ads[0].CreativeType = "gif" }, "素材类型"},
		{"图片素材没有图", func(d *BackupData) { d.Ads[0].Image = "" }, "没有图片地址"},
		{"投放端非法", func(d *BackupData) { d.Ads[0].Device = "tv" }, "投放端"},
		{"权重为负", func(d *BackupData) { d.Ads[0].Weight = -1 }, "权重"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := sampleData()
			c.mutate(&d)
			repo := &fakeConfigRepo{}
			svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{})
			_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: d})
			if err == nil {
				t.Fatalf("%s 应当被拒绝", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("报错里应当包含 %q，得到 %v", c.want, err)
			}
			if repo.imported != nil {
				t.Error("校验失败时不该写库")
			}
		})
	}
}

// 两个维度用同一个频道标识：频道有独立前台地址，标识要跨维度唯一。
func TestImport_RejectsDuplicateChannelSlug(t *testing.T) {
	d := sampleData()
	d.Taxonomies = append(d.Taxonomies, BackupTaxonomy{
		Name: "地区", Slug: "region", Status: "active",
		Terms: []BackupTerm{{Name: "都市频道", Slug: "urban", Status: "active", IsChannel: true}},
	})
	svc := newConfigSvc(&fakeConfigRepo{}, fakeSigner{on: false}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: d})
	if err == nil || !strings.Contains(err.Error(), "频道标识") {
		t.Errorf("重复的频道标识应当被拒绝，得到 %v", err)
	}
}

// 两个取值用同一个编码：编码供区块与专题规则引用，要全站唯一。
func TestImport_RejectsDuplicateCode(t *testing.T) {
	d := sampleData()
	d.Taxonomies = append(d.Taxonomies, BackupTaxonomy{
		Name: "地区", Slug: "region", Status: "active",
		Terms: []BackupTerm{{Name: "别的", Slug: "other", Status: "active", Code: "urban"}},
	})
	svc := newConfigSvc(&fakeConfigRepo{}, fakeSigner{on: false}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: d})
	if err == nil || !strings.Contains(err.Error(), "编码") {
		t.Errorf("重复的编码应当被拒绝，得到 %v", err)
	}
}

func TestImport_RejectsOversizedBackup(t *testing.T) {
	d := sampleData()
	d.Ads = make([]BackupAd, maxBackupAds+1)
	svc := newConfigSvc(&fakeConfigRepo{}, fakeSigner{on: false}, &fakeSite{})
	_, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: d})
	if err == nil || !strings.Contains(err.Error(), "超过上限") {
		t.Errorf("超量的备份应当被拒绝，得到 %v", err)
	}
}

// ---------- 导入结果 ----------

// 维度不做全表替换：库里多出的项要报告出来而不是悄悄删掉。
func TestImport_ReportsKeptItems(t *testing.T) {
	repo := &fakeConfigRepo{result: ImportResult{
		Applied:        Counts{Taxonomies: 1, Terms: 1, Ads: 1},
		TaxonomiesKept: 2, TermsKept: 3,
		KeptNames: []string{"地区（region）", "genre / 古装（costume）"},
	}}
	svc := newConfigSvc(repo, fakeSigner{on: false}, &fakeSite{version: 1})
	res, err := svc.Import(context.Background(), 1, Backup{Format: BackupFormat, Data: sampleData()})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.TaxonomiesKept != 2 || res.TermsKept != 3 {
		t.Errorf("保留项数量不对: %+v", res)
	}
	if len(res.KeptNames) != 2 {
		t.Errorf("应当列出保留项的名称，得到 %v", res.KeptNames)
	}
	// 广告是整表替换，结果里要标明（提示运维计数会归零）
	if !res.AdsReplaced {
		t.Error("广告整表替换应当在结果里标明")
	}
}

func TestCounts(t *testing.T) {
	d := sampleData()
	d.Taxonomies[0].Terms = append(d.Taxonomies[0].Terms, BackupTerm{Name: "古装", Slug: "costume", Status: "active"})
	c := d.Counts()
	if c.Taxonomies != 1 || c.Terms != 2 || c.Ads != 1 {
		t.Errorf("计数不对: %+v", c)
	}
	if c.Menus != 0 || c.StaticPages != 0 || c.Blocks != 0 {
		t.Errorf("未实现的三类应当为 0: %+v", c)
	}
}
