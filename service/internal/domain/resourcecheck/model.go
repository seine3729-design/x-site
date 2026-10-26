// Package resourcecheck 是资源体检（S4-5，docs/02 第 4.3 节、docs/03 第 9.4 节、docs/06 第 9 节）：
// 批量探测剧集的视频产物与封面是否可达，把结果存起来供运营后台查看。
//
// 它和「选线预览与健康」是两件事：那边查的是**线路**好不好（worker 周期探测 probe_path + 客户端上报），
// 这边查的是**资源**在不在（一个一个对象去 HEAD/GET）。线路整条挂了这里会全红，
// 但那种情况该去看线路健康页，不是在这里逐条排查。
package resourcecheck

import (
	"time"

	"xsite/service/internal/domain/storage"
)

// FileType 是资源大类（docs/02 第 4.3 节）。
type FileType string

const (
	TypeVideo FileType = "video"
	TypeCover FileType = "cover"
)

// Field 是具体哪个字段。
//
// 只有 FileType 分不出是哪个：一部剧有竖横两张封面，一集有封面、首帧图与视频，
// 运营看到「封面打不开」得知道该去改哪一个。
type Field string

const (
	FieldCoverPortrait  Field = "cover_portrait"
	FieldCoverLandscape Field = "cover_landscape"
	FieldEpisodeCover   Field = "episode_cover"
	FieldEpisodePoster  Field = "episode_poster"
	FieldVideo          Field = "video"
)

// fieldTitles 是字段的中文名，页面直接用。
var fieldTitles = map[Field]string{
	FieldCoverPortrait:  "竖封面",
	FieldCoverLandscape: "横封面",
	FieldEpisodeCover:   "本集封面",
	FieldEpisodePoster:  "首帧图",
	FieldVideo:          "视频",
}

// FieldTitle 返回字段的中文名。
func FieldTitle(f Field) string {
	if s, ok := fieldTitles[f]; ok {
		return s
	}
	return string(f)
}

// Result 是体检结果（docs/02 第 4.3 节的六个枚举值）。
type Result string

const (
	// ResultOK 是能打开（2xx 或 3xx）。
	ResultOK Result = "ok"
	// ResultNotFound 是资源不存在（404 / 410）—— 最常见的真问题，通常是路径填错或文件没上传。
	ResultNotFound Result = "not_found"
	// ResultForbidden 是拒绝访问（401 / 403）—— 多半是桶权限或签名配置的问题，不是文件丢了。
	ResultForbidden Result = "forbidden"
	// ResultServerError 是源站出错（5xx）—— 通常是暂时的，重新体检可能就好了。
	ResultServerError Result = "server_error"
	// ResultUnopenable 是能连上但打不开（其他 4xx、重定向成环等）。
	ResultUnopenable Result = "unopenable"
	// ResultUnreachable 是连不上（DNS 解析失败、拒绝连接、超时、被出站策略拦截）。
	ResultUnreachable Result = "unreachable"
)

// resultTitles 是结果的中文名。
var resultTitles = map[Result]string{
	ResultOK:          "正常",
	ResultNotFound:    "404",
	ResultForbidden:   "拒绝访问",
	ResultServerError: "服务器错",
	ResultUnopenable:  "打不开",
	ResultUnreachable: "连不上",
}

// ResultTitle 返回结果的中文名。
func ResultTitle(r Result) string {
	if s, ok := resultTitles[r]; ok {
		return s
	}
	return string(r)
}

// ResultInfo 描述一种结果，页面上做图例用。
type ResultInfo struct {
	Result Result `json:"result"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
}

// results 是全部结果及说明，按「问题由重到轻」排列。
var results = []ResultInfo{
	{ResultOK, "正常", "能打开，资源没问题。"},
	{ResultNotFound, "404", "资源不存在。多半是路径填错了，或者文件根本没上传上去。"},
	{ResultForbidden, "拒绝访问", "源站拒绝了请求。通常是桶的读权限或线路的签名方式配得不对，文件本身可能还在。"},
	{ResultServerError, "服务器错", "源站返回了 5xx。一般是暂时的，过一会儿重新体检看看。"},
	{ResultUnopenable, "打不开", "连上了但拿不到内容（异常的状态码、重定向成环等）。"},
	{ResultUnreachable, "连不上", "没能建立连接：域名解析不了、被拒绝、超时，或者地址落在不允许访问的网段。"},
}

// Results 返回全部结果及说明。
func Results() []ResultInfo {
	out := make([]ResultInfo, len(results))
	copy(out, results)
	return out
}

// Target 是一个待检资源。
type Target struct {
	DramaID   int64
	EpisodeID int64 // 0 表示剧级资源（封面）
	FileType  FileType
	Field     Field
	// Key 是对象在存储源内的相对路径。封面是运营手填的，可能带前导斜杠甚至是整个网址，取用前已归一。
	Key string
	// Prefix 非空时表示这是目录前缀签名的目标（一集 HLS 产物），一个令牌覆盖整集切片。
	Prefix string
	// AbsoluteURL 非空时说明运营把封面填成了完整网址，直接探它、不经线路。
	AbsoluteURL string
}

// Row 是一条体检结果（带剧名与集号，页面不用回查）。
type Row struct {
	ID            int64     `json:"id"`
	DramaID       int64     `json:"drama_id"`
	DramaTitle    string    `json:"drama_title"`
	EpisodeID     int64     `json:"episode_id"`
	EpisodeNumber int32     `json:"episode_number"`
	FileType      FileType  `json:"file_type"`
	Field         Field     `json:"field"`
	FieldTitle    string    `json:"field_title"`
	URL           string    `json:"url"`
	LineID        int64     `json:"line_id"`
	LineName      string    `json:"line_name"`
	Result        Result    `json:"result"`
	ResultTitle   string    `json:"result_title"`
	StatusCode    int32     `json:"status_code"`
	ElapsedMs     int32     `json:"elapsed_ms"`
	Message       string    `json:"message"`
	CheckedAt     time.Time `json:"checked_at"`
}

// Outcome 是一次探测的结论，准备写库。
type Outcome struct {
	Target
	// URL 是不含查询串的地址：HMAC 令牌与 S3 预签名都带密钥派生值与过期时间，落库等于把签名材料留在表里。
	URL        string
	LineID     int64
	Result     Result
	StatusCode int32
	ElapsedMs  int32
	Message    string
}

// Filter 是后台列表的筛选。Pattern 与 Limit 由 cleanFilter 填好，仓储直接用。
type Filter struct {
	DramaID int64
	BadOnly bool
	// Q 是原始搜索词。纯数字时还会当作剧编号匹配。
	Q string
	// Pattern 是转义后的 LIKE 模式（%词%），没有搜索词时为空。
	Pattern string
	Limit   int
}

// Summary 是概览。
type Summary struct {
	Total         int64      `json:"total"`
	Bad           int64      `json:"bad"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

// RunRequest 是一次体检的范围。
type RunRequest struct {
	// DramaID 为 0 表示全部剧。
	DramaID int64
	// IncludeUnpublished 为假时只检已上架的剧。
	IncludeUnpublished bool
}

// RunResult 是发起体检的结果。体检是批量长耗时任务，接口只负责入队。
type RunResult struct {
	Dramas int `json:"dramas"`
	Queued int `json:"queued"`
}

// DramaAssets 是一部剧的全部待检资源。
type DramaAssets struct {
	DramaID        int64
	Title          string
	Status         string
	CoverPortrait  string
	CoverLandscape string
	Episodes       []EpisodeAssets
}

// EpisodeAssets 是一集的待检资源。
type EpisodeAssets struct {
	ID          int64
	Number      int32
	Cover       string
	Poster      string
	MediaStatus string
	// ObjectKey 为空表示这一集还没有产物（没转封装或失败）。不算体检失败，跳过。
	ObjectKey  string
	ObjectKind string
}

// ProbeRequest 是对一个地址的探测请求。
type ProbeRequest struct {
	URL string
}

// ProbeResult 是探测结果。Err 非空表示没拿到 HTTP 响应（传输层失败）。
type ProbeResult struct {
	HTTPStatus int
	ElapsedMs  int64
	Message    string
	// Unreachable 为真表示连不上（传输层失败），此时 HTTPStatus 为 0。
	Unreachable bool
}

// lineOf 是按 kind 选中的线路与它的签名能力。
type lineOf struct {
	line   storage.Line
	usable bool
}
