// Package contentimport 是 CSV 批量导入（A4-2 / S4-6，docs/06 第 8 节）：
// 从 CSV 批量建剧，先预览（不写库）再正式导入。
//
// 几处与文档的对照说明：
//
//   - **CSV 只带剧级信息**（剧名、别名、简介、标签）。docs/06 第 8 节首句写「导入剧与分集」，
//     但同节的列定义里没有任何分集字段，也没有任何地方定义过 CSV 的分集列格式或集号来源。
//     所以分集两项统计在 CSV 场景恒为 0；保留这两个字段是为了让 FTP 扫描与开放平台推送
//     （S4-6 的另两种来源）共用同一套统计口径。
//   - **预览与正式导入是同一条代码路径**，只有「写库」那几步被 dryRun 跳过（照 storage.CleanupService
//     的做法）。这样「预览说会新增 3 部」与「导入真的新增 3 部」不可能对不上 —— 两套逻辑必然漂移。
//   - **屏蔽词只认 block**（docs/02 第 5.7 节「采集只认 block」）：mask 与 review 在采集场景不做任何事，
//     命中 block 的整行被拒、不写库。
package contentimport

import "time"

// MaxRows 是一次导入的行数上限。
//
// 有上限才能同步处理完：预览必须当场出结果，而预览与正式导入共用一条路径，
// 拆成「预览同步、导入异步」两套会让统计口径漂移。超过上限让运营拆分文件。
const MaxRows = 2000

// MaxBytes 是 CSV 的大小上限。2000 行剧级信息远到不了 1 MiB，
// 这个限制主要是挡住误传的视频或数据库备份。
const MaxBytes = 1 << 20

// Row 是 CSV 里的一行（已清洗）。
type Row struct {
	// Line 是在文件里的行号（含表头，从 1 开始），报错时指给运营看。
	Line    int      `json:"line"`
	Title   string   `json:"title"`
	Aliases []string `json:"aliases"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// Action 是一行的处置结果。
type Action string

const (
	// ActionCreate 是会新建一部剧（新剧为草稿）。
	ActionCreate Action = "create"
	// ActionUpdate 是剧已存在，会补充别名与简介（不动封面、评分、状态等 CSV 没带的字段）。
	ActionUpdate Action = "update"
	// ActionSkip 是这一行用不了（没填剧名）或在文件里重复了。
	ActionSkip Action = "skip"
	// ActionReject 是命中屏蔽词，不导入。
	ActionReject Action = "reject"
)

// actionTitles 是处置结果的中文名。
var actionTitles = map[Action]string{
	ActionCreate: "新增", ActionUpdate: "更新", ActionSkip: "跳过", ActionReject: "被拒",
}

// ActionTitle 返回处置结果的中文名。
func ActionTitle(a Action) string {
	if t, ok := actionTitles[a]; ok {
		return t
	}
	return string(a)
}

// ActionInfo 描述一种处置结果，页面做图例用。
type ActionInfo struct {
	Action Action `json:"action"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
}

var actions = []ActionInfo{
	{ActionCreate, "新增", "库里没有同名或同别名的剧，会新建一部。新剧是草稿，要自己去上架。"},
	{ActionUpdate, "更新", "剧已存在。只补充别名与简介，封面、评分、状态、标签这些都不动（标签只追加不删）。"},
	{ActionSkip, "跳过", "这一行用不了：没填剧名，或者剧名在这个文件里重复了（只处理第一次出现的那行）。"},
	{ActionReject, "被拒", "剧名、别名或简介命中了屏蔽词。整行不导入 —— 采集场景只认「直接拦下」那一类词。"},
}

// Actions 返回全部处置结果及说明。
func Actions() []ActionInfo {
	out := make([]ActionInfo, len(actions))
	copy(out, actions)
	return out
}

// RowResult 是一行的处置明细，预览时逐行展示。
type RowResult struct {
	Row
	Action      Action `json:"action"`
	ActionTitle string `json:"action_title"`
	// DramaID 在 update 时是已存在的那部剧；create 时预览为 0、正式导入后是新建的编号。
	DramaID int64 `json:"drama_id"`
	// MatchedBy 说明是按什么判定为已存在的（剧名 / 别名）。
	MatchedBy string `json:"matched_by"`
	// InTrash 为真表示匹配到的剧在回收站里 —— 它仍算已存在，不会重建一部同名的。
	InTrash bool `json:"in_trash"`
	// NewTags 是这一行会自动新建的标签名。
	NewTags []string `json:"new_tags"`
	// Reason 是跳过或被拒的原因。
	Reason string `json:"reason"`
}

// Stats 是一次导入的统计（docs/06 第 8 节）。
type Stats struct {
	RowsTotal     int `json:"rows_total"`
	DramasCreated int `json:"dramas_created"`
	DramasUpdated int `json:"dramas_updated"`
	// EpisodesCreated / EpisodesUpdated 在 CSV 场景恒为 0：CSV 的列里没有分集信息。
	// 保留字段让 FTP 扫描与开放平台推送共用同一套统计。
	EpisodesCreated int `json:"episodes_created"`
	EpisodesUpdated int `json:"episodes_updated"`
	Skipped         int `json:"skipped"`
	Rejected        int `json:"rejected"`
	TagsCreated     int `json:"tags_created"`
}

// Input 是一次导入的入参。
type Input struct {
	// CSV 是文件内容。
	CSV []byte
	// FileName 只用于记录，让运营认出是哪一批。
	FileName string
	// Source 是来源标识（采集批次），写进 drama.source。
	Source string
	// DryRun 为真时只预览、不写库，也不记导入记录。
	DryRun bool
}

// Result 是导入结果。
type Result struct {
	DryRun bool  `json:"dry_run"`
	Stats  Stats `json:"stats"`
	// Rows 是逐行明细。
	Rows []RowResult `json:"rows"`
	// Actions 是处置结果的中文说明，页面做图例用。
	Actions []ActionInfo `json:"actions"`
	// LogID 是正式导入后的记录编号；预览时为 0。
	LogID int64 `json:"log_id"`
	// Note 说明 CSV 不含分集信息，页面上展示。
	Note string `json:"note"`
}

// Log 是一次正式导入的记录。
type Log struct {
	ID              int64     `json:"id"`
	Source          string    `json:"source"`
	FileName        string    `json:"file_name"`
	RowsTotal       int       `json:"rows_total"`
	DramasCreated   int       `json:"dramas_created"`
	DramasUpdated   int       `json:"dramas_updated"`
	EpisodesCreated int       `json:"episodes_created"`
	EpisodesUpdated int       `json:"episodes_updated"`
	Skipped         int       `json:"skipped"`
	Rejected        int       `json:"rejected"`
	TagsCreated     int       `json:"tags_created"`
	AdminID         int64     `json:"admin_id"`
	AdminName       string    `json:"admin_name"`
	CreatedAt       time.Time `json:"created_at"`
}

// LogFilter 是导入记录的筛选。
type LogFilter struct {
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// ExistingDrama 是查重命中的一部剧。
type ExistingDrama struct {
	ID      int64
	Title   string
	Aliases []string
	// InTrash 为真表示在回收站里。
	InTrash bool
}

// ExistingTag 是按名称查到的标签。
type ExistingTag struct {
	ID   int64
	Name string
	// MergedInto 非 0 表示这个标签已并入别的标签，导入时改指目标。
	MergedInto int64
}

// DramaWrite 是要写入的一部剧。
//
// 标签传名字而不是编号：本批要新建的标签在落地时才有编号，
// 在编排阶段就想拿到编号的话得先提交一次标签、再提交剧，那就不是一个事务了。
type DramaWrite struct {
	Title    string
	Aliases  []string
	Summary  string
	Source   string
	TagNames []string
}
