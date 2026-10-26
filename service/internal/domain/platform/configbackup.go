package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// BackupFormat 是备份文件的格式版本。改动 BackupData 的字段含义时 +1，并在导入侧保留旧版本的读取分支。
// 版本号一并参与签名，所以单独改版本号会让签名校验失败。
const BackupFormat = 1

// Backup 是导出的备份文件。
//
// Signature 是对「格式版本 + Data 的规范 JSON」算的 HMAC（见 crypto/backupsig）：
// 文件是明文 JSON、可以人工检视，但改过之后导入会被拒绝。没有配置签名密钥时为空。
type Backup struct {
	Format     int        `json:"format"`
	ExportedAt time.Time  `json:"exported_at"`
	Site       string     `json:"site"`      // 导出时的站点版本，便于分辨备份来自哪个环境
	Signature  string     `json:"signature"` // 没配签名密钥时为空
	Data       BackupData `json:"data"`
}

// BackupData 是备份正文（docs/06 第 29 节）。
//
// 菜单、静态页、区块三类的表与接口还不存在（A3-1 / A3-2 / S3-1 / S3-2），
// 这里先把字段留出来、导出为空数组：等它们实现后填上内容，不必升格式版本。
type BackupData struct {
	Menus       []json.RawMessage `json:"menus"`
	StaticPages []json.RawMessage `json:"static_pages"`
	Blocks      []json.RawMessage `json:"blocks"`
	Taxonomies  []BackupTaxonomy  `json:"taxonomies"`
	Ads         []BackupAd        `json:"ads"`
}

// BackupTaxonomy 是一个筛选维度及其取值。不含数据库主键：导入按 slug 匹配，
// 这样备份可以在不同环境之间搬（两边的自增编号本来就不一样）。
type BackupTaxonomy struct {
	Name          string       `json:"name"`
	Slug          string       `json:"slug"`
	MultiSelect   bool         `json:"multi_select"`
	ShowInExplore bool         `json:"show_in_explore"`
	Status        string       `json:"status"`
	Sort          int32        `json:"sort"`
	Terms         []BackupTerm `json:"terms"`
}

// BackupTerm 是维度下的一个取值。同样不含主键，导入按「维度 slug + 取值 slug」匹配。
type BackupTerm struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	IsChannel bool   `json:"is_channel"`
	Code      string `json:"code"`
	Sort      int32  `json:"sort"`
}

// BackupAd 是一条广告素材。不含 imp_count / click_count：那是运行期统计，
// 由 worker 从 Valkey 累加落库，写进备份再导回去等于把统计回滚到导出那一刻。
type BackupAd struct {
	Name         string     `json:"name"`
	Slot         string     `json:"slot"`
	CreativeType string     `json:"creative_type"`
	Image        string     `json:"image"`
	VideoURL     string     `json:"video_url"`
	Text         string     `json:"text"`
	HTML         string     `json:"html"`
	Alt          string     `json:"alt"`
	ClickURL     string     `json:"click_url"`
	Device       string     `json:"device"`
	Locales      []string   `json:"locales"`
	Weight       int        `json:"weight"`
	EndAt        *time.Time `json:"end_at"`
	FreqCap      int        `json:"freq_cap"`
	Status       string     `json:"status"`
}

// Counts 是各类数据的条数（页面上「各显数量」）。
type Counts struct {
	Menus       int `json:"menus"`
	StaticPages int `json:"static_pages"`
	Blocks      int `json:"blocks"`
	Taxonomies  int `json:"taxonomies"`
	Terms       int `json:"terms"`
	Ads         int `json:"ads"`
}

// Counts 统计备份正文里各类的条数。
func (d BackupData) Counts() Counts {
	c := Counts{
		Menus: len(d.Menus), StaticPages: len(d.StaticPages), Blocks: len(d.Blocks),
		Taxonomies: len(d.Taxonomies), Ads: len(d.Ads),
	}
	for _, t := range d.Taxonomies {
		c.Terms += len(t.Terms)
	}
	return c
}

// ImportResult 是一次导入的结果。
type ImportResult struct {
	Format   int    `json:"format"`
	Signed   bool   `json:"signed"`    // 导入的文件是否带有效签名
	BackupID int64  `json:"backup_id"` // 导入前自动存档的备份编号，用于回滚
	Applied  Counts `json:"applied"`   // 实际写入的条数

	// TaxonomiesKept / TermsKept 是库里有、备份里没有、因此**保留未删**的维度与取值数量。
	//
	// 维度导入刻意不做全表替换：taxonomy_term 被 drama_term 以 ON DELETE CASCADE 引用，
	// 删一个取值会连带删掉所有剧集对它的归属，而且不会报错。所以这里只新增与更新，
	// 多出来的项列在 KeptNames 里交给运维确认后在维度页手动删。
	TaxonomiesKept int      `json:"taxonomies_kept"`
	TermsKept      int      `json:"terms_kept"`
	KeptNames      []string `json:"kept_names"`

	// AdsReplaced 为真表示广告做了整表替换（ad 没有外键引用，可以安全重建），
	// 此时原有广告的曝光与点击计数归零。
	AdsReplaced bool `json:"ads_replaced"`
}

// ConfigBackupRepo 读写备份涉及的业务数据与备份存档。
type ConfigBackupRepo interface {
	// ExportConfig 读出当前的维度与广告。
	ExportConfig(ctx context.Context) ([]BackupTaxonomy, []BackupAd, error)
	// ImportConfig 在一个事务内导入：维度按 slug upsert（不删多出的），广告整表替换。
	ImportConfig(ctx context.Context, data BackupData) (ImportResult, error)
	// SaveBackup 存一份备份存档，返回编号。
	SaveBackup(ctx context.Context, b BackupRecord) (int64, error)
	// ListBackups 按时间倒序列出备份存档（不含正文）。
	ListBackups(ctx context.Context, limit int) ([]BackupSummary, error)
	// GetBackup 读出一份备份存档的正文。
	GetBackup(ctx context.Context, id int64) (BackupRecord, error)
}

// BackupRecord 是一条备份存档。
type BackupRecord struct {
	ID        int64
	Format    int
	Reason    BackupReason
	AdminID   int64
	Note      string
	Payload   BackupData
	Signature string
	CreatedAt time.Time
}

// BackupSummary 是备份存档的列表项（不含正文，只给各类数量）。
type BackupSummary struct {
	ID        int64        `json:"id"`
	Format    int          `json:"format"`
	Reason    BackupReason `json:"reason"`
	AdminID   int64        `json:"admin_id"`
	Note      string       `json:"note"`
	Counts    Counts       `json:"counts"`
	Signed    bool         `json:"signed"`
	CreatedAt time.Time    `json:"created_at"`
}

// BackupReason 是存档的来源。
type BackupReason string

const (
	// ReasonManual 是运维主动导出时留的存档。
	ReasonManual BackupReason = "manual"
	// ReasonBeforeImport 是导入前自动存的那一份（docs/06 第 29 节「导入前先备份」）。
	ReasonBeforeImport BackupReason = "before_import"
)

// BackupSigner 给备份签名并校验（实现见 crypto/backupsig）。
type BackupSigner interface {
	Enabled() bool
	Sign(format int, payload []byte) (string, error)
	Verify(format int, payload []byte, signature string) error
}

// ConfigService 是配置导出导入（docs/06 第 29 节，S4-4）。
type ConfigService struct {
	repo   ConfigBackupRepo
	signer BackupSigner
	site   SiteConfigManager
	now    func() time.Time
}

func NewConfigService(repo ConfigBackupRepo, signer BackupSigner, site SiteConfigManager) *ConfigService {
	return &ConfigService{repo: repo, signer: signer, site: site, now: time.Now}
}

// maxBackupList 是备份存档列表的上限。
const maxBackupList = 50

// Export 导出当前配置。store 为真时同时在库里存一份存档（导出的人、时间与正文都留痕）。
func (s *ConfigService) Export(ctx context.Context, adminID int64, store bool) (Backup, error) {
	taxonomies, ads, err := s.repo.ExportConfig(ctx)
	if err != nil {
		return Backup{}, err
	}
	data := BackupData{
		// 用空切片而不是 nil：导出的 JSON 里是 []，不是 null，三端解析都省一次判空
		Menus: []json.RawMessage{}, StaticPages: []json.RawMessage{}, Blocks: []json.RawMessage{},
		Taxonomies: taxonomies, Ads: ads,
	}
	out := Backup{Format: BackupFormat, ExportedAt: s.now().UTC(), Data: data}
	if cfg, err := s.site.Get(ctx); err == nil {
		out.Site = fmt.Sprintf("v%d", cfg.Version)
	}
	if out.Signature, err = s.sign(data); err != nil {
		return Backup{}, err
	}
	if store {
		if _, err := s.repo.SaveBackup(ctx, BackupRecord{
			Format: BackupFormat, Reason: ReasonManual, AdminID: adminID,
			Payload: data, Signature: out.Signature,
		}); err != nil {
			return Backup{}, err
		}
	}
	return out, nil
}

// Import 校验并导入一份备份。
//
// 顺序是「先存档、再导入」而不是放在一个事务里：导入成功后那份存档才有价值（用来回滚），
// 所以它必须独立提交。导入本身在一个事务内完成，失败时整体回滚。
func (s *ConfigService) Import(ctx context.Context, adminID int64, b Backup) (ImportResult, error) {
	if b.Format != BackupFormat {
		return ImportResult{}, badRequest("备份格式版本是 %d，当前只能导入 %d 版（请用同版本的后台导出的备份）", b.Format, BackupFormat)
	}
	payload, err := json.Marshal(b.Data)
	if err != nil {
		return ImportResult{}, fmt.Errorf("序列化备份正文: %w", err)
	}
	signed := false
	switch {
	case s.signer.Enabled():
		// 配了签名密钥就必须验签，否则改过的备份能直接导进来
		if b.Signature == "" {
			return ImportResult{}, badRequest("这份备份没有签名，无法确认未被改动过；请用后台导出的备份文件")
		}
		if err := s.signer.Verify(BackupFormat, payload, b.Signature); err != nil {
			return ImportResult{}, badRequest("%s", err.Error())
		}
		signed = true
	case b.Signature != "":
		// 文件带签名但本环境没配密钥：无法校验，不能假装通过
		return ImportResult{}, badRequest("这份备份带有签名，但当前环境没有配置签名密钥（CONFIG_BACKUP_KEYS），无法校验")
	}

	if err := validateBackupData(b.Data); err != nil {
		return ImportResult{}, err
	}
	return s.apply(ctx, adminID, b.Data, signed, "导入前自动存档")
}

// apply 是导入与回滚共用的落地步骤：先存一份当前配置的存档，再在一个事务内写入，最后刷新站点版本。
func (s *ConfigService) apply(ctx context.Context, adminID int64, data BackupData, signed bool, note string) (ImportResult, error) {
	current, ads, err := s.repo.ExportConfig(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	before := BackupData{
		Menus: []json.RawMessage{}, StaticPages: []json.RawMessage{}, Blocks: []json.RawMessage{},
		Taxonomies: current, Ads: ads,
	}
	beforeSig, err := s.sign(before)
	if err != nil {
		return ImportResult{}, err
	}
	backupID, err := s.repo.SaveBackup(ctx, BackupRecord{
		Format: BackupFormat, Reason: ReasonBeforeImport, AdminID: adminID,
		Note: note, Payload: before, Signature: beforeSig,
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("导入前自动备份失败，已中止导入: %w", err)
	}

	res, err := s.repo.ImportConfig(ctx, data)
	if err != nil {
		return ImportResult{}, err
	}
	res.Format = BackupFormat
	res.Signed = signed
	res.BackupID = backupID
	res.AdsReplaced = true

	// 维度与广告都进了前台的读穿缓存，版本 +1 让各端立即拿到新配置（docs/06 第 29 节「成功后 5 秒生效」）
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return res, fmt.Errorf("导入已完成，但刷新站点版本失败（前台可能最多 60 秒后才生效）: %w", err)
	}
	return res, nil
}

// Backups 列出备份存档。
func (s *ConfigService) Backups(ctx context.Context) ([]BackupSummary, error) {
	return s.repo.ListBackups(ctx, maxBackupList)
}

// Restore 从库里的存档回滚。
//
// 与 Import 的区别是**不验签**：存档是系统自己生成、一直存在本库里的，可信度来自「它没离开过系统」，
// 而不是来自签名。要求它带签名会让「刚启用签名」或「轮换过密钥」之前的存档全部失去回滚价值 ——
// 恰恰是出事时最需要的那几份。下载下来再上传仍然走 Import 的验签（文件在外面可能被改过）。
//
// 格式版本与数据本身照样校验：存档可能是旧版本写的。
func (s *ConfigService) Restore(ctx context.Context, adminID int64, id int64) (ImportResult, error) {
	rec, err := s.repo.GetBackup(ctx, id)
	if err != nil {
		return ImportResult{}, err
	}
	if rec.Format != BackupFormat {
		return ImportResult{}, badRequest("这份存档是 %d 版格式，当前只能导入 %d 版", rec.Format, BackupFormat)
	}
	if err := validateBackupData(rec.Payload); err != nil {
		return ImportResult{}, err
	}
	return s.apply(ctx, adminID, rec.Payload, false, "回滚到存档 #"+strconv.FormatInt(id, 10))
}

// Backup 取出一份备份存档，还原成可下载的备份文件。
func (s *ConfigService) Backup(ctx context.Context, id int64) (Backup, error) {
	rec, err := s.repo.GetBackup(ctx, id)
	if err != nil {
		return Backup{}, err
	}
	return Backup{
		Format: rec.Format, ExportedAt: rec.CreatedAt.UTC(), Signature: rec.Signature, Data: rec.Payload,
	}, nil
}

// sign 给备份正文签名；没有配置密钥时返回空串（本地开发）。
func (s *ConfigService) sign(data BackupData) (string, error) {
	if !s.signer.Enabled() {
		return "", nil
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("序列化备份正文: %w", err)
	}
	sig, err := s.signer.Sign(BackupFormat, payload)
	if err != nil {
		return "", fmt.Errorf("备份签名: %w", err)
	}
	return sig, nil
}
