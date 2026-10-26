package contentimport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// ---------- 替身 ----------

type fakeRepo struct {
	dramas  []ExistingDrama
	tags    []ExistingTag
	applied []ApplyPlan
	logs    []Log
	total   int64

	findErr  error
	applyErr error
	// nextIDs 是 Apply 返回的新建剧编号
	nextIDs     []int64
	tagsCreated int
}

func (f *fakeRepo) FindDramas(_ context.Context, titles []string) ([]ExistingDrama, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	var out []ExistingDrama
	want := map[string]bool{}
	for _, t := range titles {
		want[t] = true
	}
	for _, d := range f.dramas {
		if want[strings.ToLower(d.Title)] {
			out = append(out, d)
			continue
		}
		for _, a := range d.Aliases {
			if want[strings.ToLower(a)] {
				out = append(out, d)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) FindTags(_ context.Context, names []string) ([]ExistingTag, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []ExistingTag
	for _, t := range f.tags {
		if want[strings.ToLower(t.Name)] {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeRepo) Apply(_ context.Context, plan ApplyPlan) (ApplyOutcome, error) {
	if f.applyErr != nil {
		return ApplyOutcome{}, f.applyErr
	}
	f.applied = append(f.applied, plan)
	ids := make([]int64, len(plan.Creates))
	for i := range ids {
		if i < len(f.nextIDs) {
			ids[i] = f.nextIDs[i]
		} else {
			ids[i] = int64(1000 + i)
		}
	}
	created := f.tagsCreated
	if created == 0 {
		created = len(plan.NewTags)
	}
	return ApplyOutcome{CreatedIDs: ids, TagsCreated: created, LogID: 77}, nil
}

func (f *fakeRepo) ListLogs(_ context.Context, _ LogFilter) ([]Log, int64, error) {
	return f.logs, f.total, nil
}

// fakeWords 命中列表里的任意子串就算 block。
type fakeWords struct{ words []string }

func (f *fakeWords) HasBlock(text string) bool {
	low := strings.ToLower(text)
	for _, w := range f.words {
		if w != "" && strings.Contains(low, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

func newSvc(repo *fakeRepo, words Blockwords) *Service {
	return NewService(repo, words)
}

func csvOf(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// ---------- CSV 解析 ----------

func TestParseCSVBasic(t *testing.T) {
	rows, err := ParseCSV(csvOf(
		"剧名,别名,简介,标签",
		"霸道总裁,总裁来了|霸总,一个关于总裁的故事,都市、言情",
		"重生之我是赘婿,,,古装",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("想要 2 行，得到 %d", len(rows))
	}
	r := rows[0]
	if r.Line != 2 {
		t.Errorf("行号该从表头之后算，得到 %d", r.Line)
	}
	if r.Title != "霸道总裁" {
		t.Errorf("剧名错了：%q", r.Title)
	}
	if len(r.Aliases) != 2 || r.Aliases[0] != "总裁来了" || r.Aliases[1] != "霸总" {
		t.Errorf("别名该按竖线切开，得到 %+v", r.Aliases)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "都市" || r.Tags[1] != "言情" {
		t.Errorf("标签该按顿号切开，得到 %+v", r.Tags)
	}
	if rows[1].Summary != "" || len(rows[1].Aliases) != 0 {
		t.Errorf("空单元格该是空值，得到 %+v", rows[1])
	}
}

func TestParseCSVHeaderAliases(t *testing.T) {
	cases := [][]string{
		{"剧名,别名,简介,标签", "甲,乙,丙,丁"},
		{"title,aliases,summary,tags", "甲,乙,丙,丁"},
		{"名称,又名,描述,tag", "甲,乙,丙,丁"},
		{"  剧 名 , 别_名 , 简介 , 标签 ", "甲,乙,丙,丁"},     // 空白与下划线都该忽略
		{"Title,ALIASES,Summary,Tags", "甲,乙,丙,丁"}, // 大小写不敏感
	}
	for _, c := range cases {
		t.Run(c[0], func(t *testing.T) {
			rows, err := ParseCSV(csvOf(c...))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Title != "甲" {
				t.Fatalf("表头该被认出来，得到 %+v", rows)
			}
			if rows[0].Summary != "丙" || len(rows[0].Tags) != 1 {
				t.Errorf("其余列也该认出来：%+v", rows[0])
			}
		})
	}
}

func TestParseCSVStripsBOM(t *testing.T) {
	// Excel 存 UTF-8 CSV 会加 BOM，不去掉第一个列名就认不出来
	data := append([]byte{0xEF, 0xBB, 0xBF}, csvOf("剧名,简介", "甲,乙")...)
	rows, err := ParseCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "甲" {
		t.Fatalf("BOM 该被去掉，得到 %+v", rows)
	}
}

func TestParseCSVSeparators(t *testing.T) {
	// 别名与标签的分隔符：运营用什么输入法打出来的都认
	rows, err := ParseCSV(csvOf("剧名,别名,标签", `甲,"乙，丙、丁|戊;己/庚","A，B"`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rows[0].Aliases); n != 6 {
		t.Fatalf("六种分隔符都该认，得到 %d 个：%+v", n, rows[0].Aliases)
	}
	if len(rows[0].Tags) != 2 {
		t.Errorf("标签也该切开，得到 %+v", rows[0].Tags)
	}
}

func TestParseCSVDropsTitleFromAliases(t *testing.T) {
	// 别名里出现剧名本身是常见的，别名字段的意义是「除了剧名还叫什么」
	rows, err := ParseCSV(csvOf("剧名,别名", "霸道总裁,霸道总裁|霸总"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Aliases) != 1 || rows[0].Aliases[0] != "霸总" {
		t.Fatalf("剧名该从别名里去掉，得到 %+v", rows[0].Aliases)
	}
}

func TestParseCSVDedupesList(t *testing.T) {
	rows, err := ParseCSV(csvOf("剧名,标签", "甲,都市|都市|都市|言情"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Tags) != 2 {
		t.Fatalf("标签该去重，得到 %+v", rows[0].Tags)
	}
}

func TestParseCSVTruncatesLongText(t *testing.T) {
	long := strings.Repeat("长", maxTitleRunes+50)
	rows, err := ParseCSV(csvOf("剧名", long))
	if err != nil {
		t.Fatal(err)
	}
	// 超长截断而不是报错：为一行多几个字就让整个文件导不进去，运营只能自己去数字数
	if n := len([]rune(rows[0].Title)); n != maxTitleRunes {
		t.Fatalf("剧名该截断到 %d 字，得到 %d", maxTitleRunes, n)
	}
}

func TestParseCSVErrors(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"空文件", nil, "空的"},
		{"只有表头", csvOf("剧名,简介"), "除了表头没有数据行"},
		{"没有剧名列", csvOf("编号,简介", "1,甲"), "找不到「剧名」列"},
		{"不是 UTF-8", []byte{0xC0, 0x80, 0x0A}, "UTF-8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCSV(c.data)
			if !errors.Is(err, errs.ErrBadRequest) {
				t.Fatalf("想要 400，得到 %v", err)
			}
			if !strings.Contains(errs.From(err).Message, c.want) {
				t.Errorf("提示里该含 %q，得到 %q", c.want, errs.From(err).Message)
			}
		})
	}
}

func TestParseCSVRowLimit(t *testing.T) {
	lines := []string{"剧名"}
	for i := 0; i <= MaxRows; i++ {
		lines = append(lines, "剧"+strings.Repeat("x", i%5))
	}
	_, err := ParseCSV(csvOf(lines...))
	if !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("超过行数上限该报 400，得到 %v", err)
	}
	if !strings.Contains(errs.From(err).Message, "拆开") {
		t.Errorf("该提示拆分文件，得到 %q", errs.From(err).Message)
	}
}

func TestParseCSVSizeLimit(t *testing.T) {
	big := make([]byte, MaxBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := ParseCSV(big); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("超过大小上限该报 400，得到 %v", err)
	}
}

func TestParseCSVHandlesQuotedCommas(t *testing.T) {
	rows, err := ParseCSV(csvOf("剧名,简介", `甲,"他说，这很好"`))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Summary != "他说，这很好" {
		t.Fatalf("引号里的逗号该保留，得到 %q", rows[0].Summary)
	}
}

func TestParseCSVToleratesShortRows(t *testing.T) {
	// 末尾少几个逗号是常见情况，按表头对齐即可，不该整个文件失败
	rows, err := ParseCSV(csvOf("剧名,别名,简介,标签", "甲,乙", "丙"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].Title != "丙" {
		t.Fatalf("短行该被容忍，得到 %+v", rows)
	}
}

// ---------- 预览与导入 ----------

func TestImportPreviewDoesNotWrite(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,标签", "甲,都市"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !out.DryRun {
		t.Error("结果该标明是预览")
	}
	if len(repo.applied) != 0 {
		t.Fatal("预览不该写库")
	}
	if out.LogID != 0 {
		t.Error("预览不该记导入记录")
	}
	if out.Stats.DramasCreated != 1 {
		t.Errorf("预览也该给出会新增几部，得到 %d", out.Stats.DramasCreated)
	}
	if out.Note == "" || len(out.Actions) == 0 {
		t.Error("该带上说明与图例")
	}
}

func TestImportCreatesAndUpdates(t *testing.T) {
	repo := &fakeRepo{
		dramas:  []ExistingDrama{{ID: 9, Title: "已有的剧"}},
		nextIDs: []int64{101},
	}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV:    csvOf("剧名,简介", "新剧,简介A", "已有的剧,简介B"),
		Source: "batch-1",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.DramasCreated != 1 || out.Stats.DramasUpdated != 1 {
		t.Fatalf("想要 1 新增 1 更新，得到 %+v", out.Stats)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("该落地一次，得到 %d", len(repo.applied))
	}
	plan := repo.applied[0]
	if len(plan.Creates) != 1 || plan.Creates[0].Title != "新剧" {
		t.Errorf("新建的该是「新剧」，得到 %+v", plan.Creates)
	}
	if plan.Creates[0].Source != "batch-1" {
		t.Errorf("该带上来源标识，得到 %q", plan.Creates[0].Source)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].ID != 9 {
		t.Errorf("更新的该是 9 号剧，得到 %+v", plan.Updates)
	}
	if plan.Log.AdminID != 42 {
		t.Errorf("该记下操作人，得到 %d", plan.Log.AdminID)
	}
	// 新建的编号要回填到明细，让运营点得进去
	for _, r := range out.Rows {
		if r.Action == ActionCreate && r.DramaID != 101 {
			t.Errorf("新建的编号该回填，得到 %d", r.DramaID)
		}
		if r.Action == ActionUpdate && r.MatchedBy != "剧名" {
			t.Errorf("该说明按什么匹配上的，得到 %q", r.MatchedBy)
		}
	}
	if out.LogID != 77 {
		t.Errorf("该返回记录编号，得到 %d", out.LogID)
	}
}

func TestImportMatchesByAlias(t *testing.T) {
	// 库里的剧有别名「霸总」，CSV 的剧名正是它 —— 同一部剧换了名字采集回来，不该重建
	repo := &fakeRepo{dramas: []ExistingDrama{{ID: 5, Title: "霸道总裁", Aliases: []string{"霸总"}}}}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "霸总"), DryRun: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.DramasUpdated != 1 || out.Stats.DramasCreated != 0 {
		t.Fatalf("该按别名匹配上，得到 %+v", out.Stats)
	}
	if out.Rows[0].MatchedBy != "别名" || out.Rows[0].DramaID != 5 {
		t.Errorf("匹配信息不对：%+v", out.Rows[0])
	}
}

func TestImportMatchesCSVAliasAgainstExistingTitle(t *testing.T) {
	// 反方向：CSV 的别名命中库里的剧名
	repo := &fakeRepo{dramas: []ExistingDrama{{ID: 7, Title: "霸道总裁"}}}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,别名", "总裁驾到,霸道总裁"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.DramasUpdated != 1 {
		t.Fatalf("CSV 的别名该参与查重，得到 %+v", out.Stats)
	}
	if out.Rows[0].DramaID != 7 {
		t.Errorf("该匹配到 7 号剧，得到 %d", out.Rows[0].DramaID)
	}
}

func TestImportTrashedDramaCountsAsExisting(t *testing.T) {
	// 回收站里的剧也算已存在：否则导入会建一部同名的新剧，把运营刚删掉的东西又带回来
	repo := &fakeRepo{dramas: []ExistingDrama{{ID: 3, Title: "删掉的剧", InTrash: true}}}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "删掉的剧"), DryRun: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.DramasCreated != 0 || out.Stats.DramasUpdated != 1 {
		t.Fatalf("不该重建，得到 %+v", out.Stats)
	}
	if !out.Rows[0].InTrash {
		t.Error("该标明匹配到的剧在回收站里")
	}
}

func TestImportSkipsEmptyTitle(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,简介", ",只有简介", "正常的剧,x"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Skipped != 1 || out.Stats.DramasCreated != 1 {
		t.Fatalf("没填剧名该跳过，得到 %+v", out.Stats)
	}
	if out.Rows[0].Action != ActionSkip || !strings.Contains(out.Rows[0].Reason, "剧名") {
		t.Errorf("该说明原因，得到 %+v", out.Rows[0])
	}
}

func TestImportSkipsDuplicateRows(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名", "甲", "甲", "乙"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 文件里重复的只处理第一次出现的那行
	if out.Stats.DramasCreated != 2 || out.Stats.Skipped != 1 {
		t.Fatalf("想要 2 新增 1 跳过，得到 %+v", out.Stats)
	}
	if !strings.Contains(out.Rows[1].Reason, "重复") {
		t.Errorf("该说明是文件内重复，得到 %q", out.Rows[1].Reason)
	}
}

func TestImportSecondOccurrenceOfSameTitleNotCreatedTwice(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "甲", "甲")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 落地时只能建一部，不然同一批就造出两部同名的剧
	if n := len(repo.applied[0].Creates); n != 1 {
		t.Fatalf("同名只该建一部，得到 %d", n)
	}
	_ = out
}

// ---------- 屏蔽词 ----------

func TestImportRejectsBlockedTitle(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, &fakeWords{words: []string{"违禁"}})
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名", "违禁内容", "正常的剧"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Rejected != 1 || out.Stats.DramasCreated != 1 {
		t.Fatalf("命中屏蔽词该被拒，得到 %+v", out.Stats)
	}
	if out.Rows[0].Action != ActionReject || !strings.Contains(out.Rows[0].Reason, "屏蔽词") {
		t.Errorf("该说明原因，得到 %+v", out.Rows[0])
	}
}

func TestImportRejectsBlockedSummaryAndAlias(t *testing.T) {
	// 采集来的简介里塞引流联系方式是常见手法，只查剧名挡不住
	cases := []struct {
		name string
		csv  []byte
	}{
		{"简介命中", csvOf("剧名,简介", "正常剧名,内容里有违禁词")},
		{"别名命中", csvOf("剧名,别名", "正常剧名,违禁别名")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := newSvc(repo, &fakeWords{words: []string{"违禁"}})
			out, err := svc.Import(context.Background(), Input{CSV: c.csv, DryRun: true}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if out.Stats.Rejected != 1 {
				t.Fatalf("该被拒，得到 %+v", out.Stats)
			}
		})
	}
}

func TestImportRejectedRowNotWritten(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, &fakeWords{words: []string{"违禁"}})
	if _, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "违禁内容")}, 1); err != nil {
		t.Fatal(err)
	}
	if len(repo.applied[0].Creates) != 0 || len(repo.applied[0].Updates) != 0 {
		t.Fatal("被拒的行不该写库")
	}
}

func TestImportWithoutBlockwordsWorks(t *testing.T) {
	// 没装屏蔽词引擎时不该崩（单测与某些部署下可能没有）
	svc := newSvc(&fakeRepo{}, nil)
	out, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "甲"), DryRun: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Rejected != 0 {
		t.Fatalf("没有词表时不该拒任何行，得到 %+v", out.Stats)
	}
}

// ---------- 标签 ----------

func TestImportPlansNewTags(t *testing.T) {
	repo := &fakeRepo{tags: []ExistingTag{{ID: 1, Name: "都市"}}}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,标签", "甲,都市|言情", "乙,言情|古装"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 「都市」已有；「言情」「古装」要新建，且只算一次
	if out.Stats.TagsCreated != 2 {
		t.Fatalf("该新建 2 个标签，得到 %d", out.Stats.TagsCreated)
	}
	if len(out.Rows[0].NewTags) != 1 || out.Rows[0].NewTags[0] != "言情" {
		t.Errorf("第一行只该报「言情」是新的，得到 %+v", out.Rows[0].NewTags)
	}
	if len(out.Rows[1].NewTags) != 1 || out.Rows[1].NewTags[0] != "古装" {
		t.Errorf("第二行只该报「古装」是新的（言情已在上一行算过），得到 %+v", out.Rows[1].NewTags)
	}
}

func TestImportTagNamesCaseInsensitive(t *testing.T) {
	repo := &fakeRepo{tags: []ExistingTag{{ID: 1, Name: "Drama"}}}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,标签", "甲,drama"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.TagsCreated != 0 {
		t.Fatalf("大小写不同的同名标签不该重建，得到 %d", out.Stats.TagsCreated)
	}
}

func TestImportPassesTagNamesToPlan(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	if _, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名,标签", "甲,都市")}, 1); err != nil {
		t.Fatal(err)
	}
	// 标签传名字而不是编号：本批新建的标签在落地时才有编号
	if len(repo.applied[0].Creates[0].TagNames) != 1 {
		t.Fatalf("标签名该传下去，得到 %+v", repo.applied[0].Creates[0])
	}
}

// ---------- 校验与错误 ----------

func TestImportValidatesSource(t *testing.T) {
	svc := newSvc(&fakeRepo{}, nil)
	_, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名", "甲"), Source: strings.Repeat("长", maxSourceRunes+1),
	}, 1)
	if !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("来源标识过长该报 400，得到 %v", err)
	}
}

func TestImportRepoErrorsPropagate(t *testing.T) {
	boom := errors.New("库挂了")
	svc := newSvc(&fakeRepo{findErr: boom}, nil)
	if _, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "甲"), DryRun: true}, 1); !errors.Is(err, boom) {
		t.Fatalf("查重出错该透出去，得到 %v", err)
	}

	svc2 := newSvc(&fakeRepo{applyErr: boom}, nil)
	if _, err := svc2.Import(context.Background(), Input{CSV: csvOf("剧名", "甲")}, 1); !errors.Is(err, boom) {
		t.Fatalf("落地出错该透出去，得到 %v", err)
	}
}

// ---------- 导入记录 ----------

func TestLogsPagination(t *testing.T) {
	repo := &fakeRepo{logs: []Log{{ID: 1}}, total: 1}
	svc := newSvc(repo, nil)
	_, total, page, size, err := svc.Logs(context.Background(), LogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || page != 1 || size != defaultLogPage {
		t.Fatalf("默认分页不对：total=%d page=%d size=%d", total, page, size)
	}
}

func TestLogsValidation(t *testing.T) {
	svc := newSvc(&fakeRepo{}, nil)
	day := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	earlier := day.Add(-24 * time.Hour)
	cases := []struct {
		name string
		f    LogFilter
	}{
		{"结束早于开始", LogFilter{From: &day, To: &earlier}},
		{"页码为负", LogFilter{Page: -1}},
		{"每页条数超上限", LogFilter{PageSize: maxLogPageSize + 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, _, _, err := svc.Logs(context.Background(), c.f); !errors.Is(err, errs.ErrBadRequest) {
				t.Fatalf("想要 400，得到 %v", err)
			}
		})
	}
}

// ---------- 说明文案 ----------

func TestActionsAllDescribed(t *testing.T) {
	all := []Action{ActionCreate, ActionUpdate, ActionSkip, ActionReject}
	infos := Actions()
	if len(infos) != len(all) {
		t.Fatalf("图例 %d 条，枚举 %d 个", len(infos), len(all))
	}
	for _, a := range all {
		found := false
		for _, i := range infos {
			if i.Action == a {
				found = true
				if i.Title == "" || i.Desc == "" {
					t.Errorf("%s 缺中文名或说明", a)
				}
			}
		}
		if !found {
			t.Errorf("处置 %s 没出现在图例里", a)
		}
		if ActionTitle(a) == string(a) {
			t.Errorf("处置 %s 没有中文名", a)
		}
	}
}

func TestNoteExplainsNoEpisodes(t *testing.T) {
	// docs/06 第 8 节首句写「导入剧与分集」，但列定义里没有分集字段。
	// 这段说明就是为了让运营知道分集不会被导入
	if !strings.Contains(importNote, "分集") || !strings.Contains(importNote, "草稿") {
		t.Fatalf("说明该讲清楚分集与草稿，得到 %q", importNote)
	}
}

func TestEpisodeStatsAlwaysZero(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		CSV: csvOf("剧名,别名,简介,标签", "甲,乙,丙,丁"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// CSV 的列里没有分集信息，这两项恒为 0；字段保留是为了和另两种采集来源共用统计口径
	if out.Stats.EpisodesCreated != 0 || out.Stats.EpisodesUpdated != 0 {
		t.Fatalf("CSV 场景分集统计该恒为 0，得到 %+v", out.Stats)
	}
}

func TestImportRowSlicesNeverNull(t *testing.T) {
	// 契约里 aliases / tags / new_tags 声明的是数组。Go 的 nil slice 会序列化成 JSON null，
	// 前端拿到 null 去 .includes() 会当场崩掉 —— 这个坑在浏览器验证时真的踩到了
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{
		// 这一行没有别名、没有标签，三个切片天然都是 nil
		CSV: csvOf("剧名", "没有任何别名与标签的剧"), DryRun: true,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := out.Rows[0]
	if r.Aliases == nil {
		t.Error("aliases 不能是 nil（会变成 JSON null）")
	}
	if r.Tags == nil {
		t.Error("tags 不能是 nil")
	}
	if r.NewTags == nil {
		t.Error("new_tags 不能是 nil")
	}
}

func TestImportRowSlicesSerializeAsArrays(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, nil)
	out, err := svc.Import(context.Background(), Input{CSV: csvOf("剧名", "甲"), DryRun: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(out.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"aliases":[]`, `"tags":[]`, `"new_tags":[]`} {
		if !strings.Contains(string(b), field) {
			t.Errorf("序列化结果里该有 %s，实际：%s", field, b)
		}
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("不该出现 null：%s", b)
	}
}
