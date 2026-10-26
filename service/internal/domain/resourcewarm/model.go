// Package resourcewarm 是资源预热（S4-5，docs/02 第 4.4 节、docs/03 第 9.5 节、docs/06 第 10 节）：
// 预先拉取清单与前 N 个切片，让边缘缓存住，减少真实观众的回源等待。
//
// 预热拉的必须是**播放器真正会请求的那些地址**，所以它不自己拼地址，而是复用播放链路
// （play.HLSService.Playlist）：自签一个播放令牌、在 worker 进程内直接调，拿到的就是改写后的清单，
// 里面是和观众拿到的同一套签名地址。自己拼地址一旦签名方式或路径有偏差，边缘缓存的就是另一个 key，预热白做。
//
// 两层缓存被热到的东西不一样，这点要分清：
//   - 清单：走加密接口、Cache-Control private, no-store（docs/08 第 46 行），**不在边缘缓存**。
//     预热清单热的是服务端 Valkey 里的清单原文读穿缓存（xsite:play:manifest:），省掉回源读清单那一跳。
//   - 切片：public, max-age=31536000, immutable、地址按内容哈希（docs/08 第 46 行），这才是边缘预热的正主。
//     前提是 CDN 把鉴权参数（hmac_token 的 p/exp/s、SigV4 的 X-Amz-*）排除在缓存键之外 ——
//     否则 exp 每轮换一次就是一个新 key，immutable 长缓存本身也就无从谈起。
package resourcewarm

import "time"

// Mode 是预热深度。
type Mode string

const (
	// ModePlaylistOnly 只拉清单。热的是服务端清单原文缓存，不碰切片，流量极小。
	ModePlaylistOnly Mode = "playlist_only"
	// ModeWithSegments 连前 N 个切片一起拉。真正的边缘预热，流量按片数 × 清晰度数 × 集数增长。
	ModeWithSegments Mode = "with_segments"
)

// Status 是一轮预热的状态。
type Status string

const (
	StatusRunning Status = "running"
	// StatusDone 是正常跑完（中途有拿不到的资源也算跑完，数量在 failed 里）。
	StatusDone Status = "done"
	// StatusStopped 是运营点了停止。已经热好的算数。
	StatusStopped Status = "stopped"
	// StatusInterrupted 是出错或 worker 失去响应。
	StatusInterrupted Status = "interrupted"
)

// StatusInfo 描述一种状态，页面直接展示。
type StatusInfo struct {
	Status Status `json:"status"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
}

var statuses = []StatusInfo{
	{StatusRunning, "预热中", "正在跑。可以关掉这个页面，回来还能看到进度。"},
	{StatusDone, "已完成", "整轮跑完了。中途有拿不到的资源不影响这个状态，数量在「有问题」里。"},
	{StatusStopped, "已停止", "运营点了停止。已经热好的那些仍然在缓存里，不会退回去。"},
	{StatusInterrupted, "已中断", "出错或预热进程失去响应。可以直接再开一轮，已经热好的会很快跳过。"},
}

// Statuses 返回全部状态及说明。
func Statuses() []StatusInfo {
	out := make([]StatusInfo, len(statuses))
	copy(out, statuses)
	return out
}

var statusTitles = map[Status]string{
	StatusRunning: "预热中", StatusDone: "已完成", StatusStopped: "已停止", StatusInterrupted: "已中断",
}

// StatusTitle 返回状态的中文名。
func StatusTitle(s Status) string {
	if t, ok := statusTitles[s]; ok {
		return t
	}
	return string(s)
}

var modeTitles = map[Mode]string{
	ModePlaylistOnly: "只拉清单", ModeWithSegments: "连切片一起拉",
}

// ModeTitle 返回模式的中文名。
func ModeTitle(m Mode) string {
	if t, ok := modeTitles[m]; ok {
		return t
	}
	return string(m)
}

// Input 是开一轮预热的入参。
type Input struct {
	// DramaID 为 0 表示全部剧。
	DramaID            int64
	Mode               Mode
	SegmentsPerQuality int
	Concurrency        int
	IncludeUnpublished bool
}

// Job 是一轮预热。
type Job struct {
	ID         int64  `json:"id"`
	DramaID    int64  `json:"drama_id"`
	DramaTitle string `json:"drama_title"`
	Mode       Mode   `json:"mode"`
	ModeTitle  string `json:"mode_title"`

	SegmentsPerQuality int  `json:"segments_per_quality"`
	Concurrency        int  `json:"concurrency"`
	IncludeUnpublished bool `json:"include_unpublished"`

	Status      Status `json:"status"`
	StatusTitle string `json:"status_title"`

	EpisodesTotal int `json:"episodes_total"`
	EpisodesDone  int `json:"episodes_done"`
	// Warmed 是热好的资源数（清单与切片各算一个）。
	Warmed int64 `json:"warmed"`
	// Failed 是拿不到的资源数。它们会同时写进资源体检的结果里，可以在那边逐条排查。
	Failed       int64 `json:"failed"`
	Fetched      int64 `json:"fetched"`
	TrafficBytes int64 `json:"traffic_bytes"`
	// Current 是当前在热什么，给运营看进展。
	Current string `json:"current"`

	StopRequested bool   `json:"stop_requested"`
	Error         string `json:"error"`
	AdminID       int64  `json:"admin_id"`

	StartedAt   time.Time  `json:"started_at"`
	HeartbeatAt time.Time  `json:"heartbeat_at"`
	FinishedAt  *time.Time `json:"finished_at"`

	// ElapsedMs 是已跑或总共跑了多久，由 StartedAt 与 FinishedAt（或当前时间）算出。
	ElapsedMs int64 `json:"elapsed_ms"`
	// Percent 是进度百分比（0–100），没有总数时为 0。
	Percent int `json:"percent"`
}

// Running 表示这一轮还在跑。
func (j Job) Running() bool { return j.Status == StatusRunning }

// Progress 是进度接口的响应。
type Progress struct {
	// Job 是在跑的那一轮；没有在跑的时候是最近一轮（让运营看到上次的结果）。为 nil 表示从来没跑过。
	Job *Job `json:"job"`
	// Running 表示 Job 是否正在跑。
	Running bool `json:"running"`
	// Statuses 是状态说明，页面做图例用。
	Statuses []StatusInfo `json:"statuses"`
	// Note 是对预热效果的说明（哪一层被热到了），页面上展示，避免运营误以为清单也进了 CDN。
	Note string `json:"note"`
}

// Episode 是一集待预热的内容。
type Episode struct {
	ID         int64
	DramaID    int64
	Number     int32
	DramaTitle string
}

// Plan 是一轮预热要做的事（worker 领到任务后自己算出来）。
type Plan struct {
	Job      Job
	Episodes []Episode
}

// FetchResult 是拉一个地址的结果。
type FetchResult struct {
	Bytes int64
	// HTTPStatus 为 0 表示没拿到响应。
	HTTPStatus int
	// Unreachable 为真表示传输层失败。
	Unreachable bool
	Message     string
}

// OK 表示这个地址拿到了。
func (r FetchResult) OK() bool {
	return !r.Unreachable && r.HTTPStatus >= 200 && r.HTTPStatus < 400
}

// Counters 是一轮预热的累计量。worker 内部用，定期同步到库。
type Counters struct {
	EpisodesDone int
	Warmed       int64
	Failed       int64
	Fetched      int64
	TrafficBytes int64
	Current      string
}
