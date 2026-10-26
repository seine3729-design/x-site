package contentimport

import (
	"context"
	"fmt"
	"strings"

	"xsite/service/internal/errs"
)

// importNote 说明 CSV 能带什么、不能带什么。
//
// 写在后端：这是导入行为本身的事实，前端照抄一份必然漂移。
const importNote = "CSV 只带剧级信息（剧名、别名、简介、标签），不含分集。" +
	"新建的剧是草稿，要自己去上架；分集靠资源机扫描或开放平台推送带入，所以这里的分集统计恒为 0。"

const (
	maxSourceRunes   = 60
	maxFileNameRunes = 200
	defaultLogPage   = 20
	maxLogPageSize   = 100
	// tagSlugPrefix 与 content 域给标签自动生成 slug 的前缀一致（docs/specs/S1-3.md）。
	tagSlugPrefix = "t-"
)

// Repo 读写导入需要的数据。
type Repo interface {
	// FindDramas 按剧名或别名精确查重（titles 已转小写）。
	FindDramas(ctx context.Context, titles []string) ([]ExistingDrama, error)
	// FindTags 按名称精确找标签（names 已转小写）。
	FindTags(ctx context.Context, names []string) ([]ExistingTag, error)
	// Apply 在单事务内落地一批写入：建剧、更新剧、建标签、挂标签，并记一条导入记录。
	Apply(ctx context.Context, plan ApplyPlan) (ApplyOutcome, error)
	ListLogs(ctx context.Context, f LogFilter) ([]Log, int64, error)
}

// Blockwords 判断文本是否命中屏蔽词（domain/moderation.Service）。
type Blockwords interface {
	// HasBlock 只报告 block 类命中：采集场景只认这一类（docs/02 第 5.7 节）。
	HasBlock(text string) bool
}

// ApplyPlan 是要落地的全部写入。
type ApplyPlan struct {
	// NewTags 是要新建的标签名（已去重）。
	NewTags []string
	// Creates 是要新建的剧，顺序与 CreateLines 一一对应。
	Creates []DramaWrite
	// CreateLines 是每个 Creates 对应的 CSV 行号，用于把新建出来的编号回填到明细里。
	CreateLines []int
	// Updates 是要更新的剧。
	Updates []DramaUpdate
	// Log 是这一批的导入记录。
	Log LogWrite
}

// DramaUpdate 是对一部已有剧的补充。
type DramaUpdate struct {
	ID      int64
	Aliases []string
	Summary string
	Source  string
	// TagNames 是这一行的标签名；落地时解析成编号后追加。
	TagNames []string
}

// LogWrite 是导入记录的字段。
type LogWrite struct {
	Source   string
	FileName string
	Stats    Stats
	AdminID  int64
}

// ApplyOutcome 是落地结果。
type ApplyOutcome struct {
	// CreatedIDs 按 CreateLines 的顺序给出新建的剧编号。
	CreatedIDs []int64
	// TagsCreated 是实际新建的标签数。
	TagsCreated int
	LogID       int64
}

// Service 是 CSV 批量导入。
type Service struct {
	repo  Repo
	words Blockwords
}

func NewService(repo Repo, words Blockwords) *Service {
	return &Service{repo: repo, words: words}
}

// Import 执行一次导入。
//
// 预览与正式导入是**同一条路径**：解析、查重、屏蔽词判定、标签解析全都一样，
// 只有最后的写库与记录被 DryRun 跳过。这样「预览说会新增 3 部」与「导入真的新增 3 部」
// 不可能对不上 —— 两套逻辑必然漂移，而运营恰恰是看了预览才敢点导入的。
func (s *Service) Import(ctx context.Context, in Input, adminID int64) (Result, error) {
	source, err := cleanText("来源标识", in.Source, maxSourceRunes)
	if err != nil {
		return Result{}, err
	}
	fileName, err := cleanText("文件名", in.FileName, maxFileNameRunes)
	if err != nil {
		return Result{}, err
	}
	rows, err := ParseCSV(in.CSV)
	if err != nil {
		return Result{}, err
	}

	plan, results, stats, err := s.plan(ctx, rows, source)
	if err != nil {
		return Result{}, err
	}
	out := Result{
		DryRun: in.DryRun, Stats: stats, Rows: results,
		Actions: Actions(), Note: importNote,
	}
	if in.DryRun {
		return out, nil
	}

	plan.Log = LogWrite{Source: source, FileName: fileName, Stats: stats, AdminID: adminID}
	done, err := s.repo.Apply(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	// 把新建出来的编号回填到明细里，让运营点得进去
	byLine := make(map[int]int64, len(done.CreatedIDs))
	for i, id := range done.CreatedIDs {
		if i < len(plan.CreateLines) {
			byLine[plan.CreateLines[i]] = id
		}
	}
	for i := range out.Rows {
		if id, ok := byLine[out.Rows[i].Line]; ok {
			out.Rows[i].DramaID = id
		}
	}
	out.Stats.TagsCreated = done.TagsCreated
	out.LogID = done.LogID
	return out, nil
}

// plan 算出每一行的处置与要落地的写入。不碰数据库之外的任何状态。
func (s *Service) plan(ctx context.Context, rows []Row, source string) (ApplyPlan, []RowResult, Stats, error) {
	stats := Stats{RowsTotal: len(rows)}
	results := make([]RowResult, 0, len(rows))
	var plan ApplyPlan

	// 一次把本批所有剧名查完，不要每行查一次：2000 行就是 2000 次往返
	titles := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Title != "" {
			titles = append(titles, strings.ToLower(r.Title))
		}
		// 别名也参与查重：CSV 里的别名可能正是库里某部剧的剧名
		for _, a := range r.Aliases {
			titles = append(titles, strings.ToLower(a))
		}
	}
	existing, err := s.findExisting(ctx, titles)
	if err != nil {
		return plan, nil, stats, err
	}

	// 标签：先把本批用到的全部标签名查一遍，分出「已有」与「要新建」
	tagNames := map[string]string{} // 小写 → 原文（第一次出现的写法）
	for _, r := range rows {
		for _, t := range r.Tags {
			if k := strings.ToLower(t); tagNames[k] == "" {
				tagNames[k] = t
			}
		}
	}
	known, err := s.findTags(ctx, tagNames)
	if err != nil {
		return plan, nil, stats, err
	}
	newTags := map[string]bool{}

	// seen 记本批已经处理过的剧名（含别名），用于判定「文件内重复」
	seen := map[string]bool{}
	for _, r := range rows {
		res := RowResult{Row: r}
		switch {
		case r.Title == "":
			res.Action, res.Reason = ActionSkip, "没填剧名"
		case s.blocked(r):
			res.Action, res.Reason = ActionReject, "命中屏蔽词"
		default:
			key := strings.ToLower(r.Title)
			if seen[key] {
				res.Action, res.Reason = ActionSkip, "剧名在这个文件里重复了"
				break
			}
			seen[key] = true
			for _, a := range r.Aliases {
				seen[strings.ToLower(a)] = true
			}
			if hit, by := matchExisting(existing, r); hit != nil {
				res.Action, res.DramaID, res.MatchedBy, res.InTrash = ActionUpdate, hit.ID, by, hit.InTrash
				plan.Updates = append(plan.Updates, DramaUpdate{
					ID: hit.ID, Aliases: r.Aliases, Summary: r.Summary, Source: source, TagNames: r.Tags,
				})
			} else {
				res.Action = ActionCreate
				plan.Creates = append(plan.Creates, DramaWrite{
					Title: r.Title, Aliases: r.Aliases, Summary: r.Summary, Source: source, TagNames: r.Tags,
				})
				plan.CreateLines = append(plan.CreateLines, r.Line)
				// 新建的剧也算「已存在」：同一批里后面又出现同名时走更新而不是再建一部。
				// 这里只能按剧名记，编号要等落地后才有
				existing = append(existing, ExistingDrama{Title: r.Title, Aliases: r.Aliases})
			}
			// 这一行要新建哪些标签
			for _, t := range r.Tags {
				k := strings.ToLower(t)
				if _, ok := known[k]; ok || newTags[k] {
					continue
				}
				newTags[k] = true
				res.NewTags = append(res.NewTags, t)
			}
		}
		res.ActionTitle = ActionTitle(res.Action)
		// 契约里这三个字段声明的是数组，就不能返回 null：Go 的 nil slice 会序列化成 JSON null，
		// 前端拿到 null 再去 .includes() 就当场崩掉。契约该由产出方保证，不是让每个调用方都去判空
		res.Aliases, res.Tags, res.NewTags = nonNil(res.Aliases), nonNil(res.Tags), nonNil(res.NewTags)
		switch res.Action {
		case ActionCreate:
			stats.DramasCreated++
		case ActionUpdate:
			stats.DramasUpdated++
		case ActionSkip:
			stats.Skipped++
		case ActionReject:
			stats.Rejected++
		}
		results = append(results, res)
	}

	for k := range newTags {
		plan.NewTags = append(plan.NewTags, tagNames[k])
	}
	stats.TagsCreated = len(plan.NewTags)
	return plan, results, stats, nil
}

// blocked 判断一行是否命中屏蔽词。
//
// 剧名、别名与简介都查：采集来的简介里塞引流联系方式是常见手法，只查剧名挡不住。
func (s *Service) blocked(r Row) bool {
	if s.words == nil {
		return false
	}
	if s.words.HasBlock(r.Title) || s.words.HasBlock(r.Summary) {
		return true
	}
	for _, a := range r.Aliases {
		if s.words.HasBlock(a) {
			return true
		}
	}
	return false
}

func (s *Service) findExisting(ctx context.Context, titles []string) ([]ExistingDrama, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	return s.repo.FindDramas(ctx, dedupe(titles))
}

func (s *Service) findTags(ctx context.Context, names map[string]string) (map[string]ExistingTag, error) {
	if len(names) == 0 {
		return map[string]ExistingTag{}, nil
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	tags, err := s.repo.FindTags(ctx, keys)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ExistingTag, len(tags))
	for _, t := range tags {
		// 同名标签可能有多条（tag.name 没有唯一约束），取第一条
		if _, dup := out[strings.ToLower(t.Name)]; !dup {
			out[strings.ToLower(t.Name)] = t
		}
	}
	return out, nil
}

// matchExisting 在已有的剧里找与这一行相同的，返回命中的剧与命中方式。
//
// 比对双向进行：CSV 的剧名对库里的剧名与别名、CSV 的别名对库里的剧名。
// 只比剧名会漏掉「同一部剧换了个名字采集回来」这种最常见的重复。
func matchExisting(existing []ExistingDrama, r Row) (*ExistingDrama, string) {
	title := strings.ToLower(r.Title)
	for i := range existing {
		e := &existing[i]
		if strings.ToLower(e.Title) == title {
			return e, "剧名"
		}
		for _, a := range e.Aliases {
			if strings.ToLower(a) == title {
				return e, "别名"
			}
		}
	}
	// CSV 的别名命中库里的剧名
	for _, alias := range r.Aliases {
		al := strings.ToLower(alias)
		for i := range existing {
			e := &existing[i]
			if strings.ToLower(e.Title) == al {
				return e, "别名"
			}
		}
	}
	return nil, ""
}

// Logs 返回导入记录（页码分页，可按日期区间筛）。
func (s *Service) Logs(ctx context.Context, f LogFilter) ([]Log, int64, int, int, error) {
	if f.From != nil && f.To != nil && f.To.Before(*f.From) {
		return nil, 0, 0, 0, badRequest("结束日期不能早于开始日期")
	}
	page, size := f.Page, f.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = defaultLogPage
	}
	if page < 1 || size < 1 || size > maxLogPageSize {
		return nil, 0, 0, 0, badRequest("分页参数错误：page 从 1 开始，page_size 为 1–%d", maxLogPageSize)
	}
	list, total, err := s.repo.ListLogs(ctx, LogFilter{From: f.From, To: f.To, Page: page, PageSize: size})
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return list, total, page, size, nil
}

// TagSlugPrefix 是自动建标签时 slug 的前缀（与 content 域一致，docs/specs/S1-3.md）。
// slug 本身在仓储层生成 —— 冲突要重试，那是数据库的事。
const TagSlugPrefix = tagSlugPrefix

// nonNil 把 nil 切片换成空切片，保证序列化成 [] 而不是 null。
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func dedupe(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func cleanText(label, s string, max int) (string, error) {
	v := strings.TrimSpace(s)
	if n := len([]rune(v)); n > max {
		return "", badRequest("%s不能超过 %d 个字", label, max)
	}
	return v, nil
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}
