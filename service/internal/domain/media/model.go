// Package media 是媒体域：分集的转封装任务、媒体对象与内容密钥（S1-5，docs/specs/S1-5.md）。
// 本包是规则与仓储接口；产物由 internal/transcode 生成，River 任务在 internal/jobs。
package media

import (
	"time"

	"uuid"
)

// JobStatus 是转封装任务状态。
type JobStatus string

const (
	JobQueued    JobStatus = "queued"    // 排队中，或本次尝试失败后等待重试
	JobRunning   JobStatus = "running"   // 转码中
	JobSucceeded JobStatus = "succeeded" // 已切换到新产物
	JobSkipped   JobStatus = "skipped"   // 源文件与当前产物相同，没有重新转码
	JobFailed    JobStatus = "failed"    // 不可恢复的错误或重试用尽
)

// Active 表示任务还在进行（占住这一集，不能再提交）。
func (s JobStatus) Active() bool { return s == JobQueued || s == JobRunning }

// 分集的媒体状态（episode.media_status）。
const (
	EpisodePending     = "pending"     // 没有视频
	EpisodeTranscoding = "transcoding" // 第一次转封装进行中
	EpisodeReady       = "ready"       // 有可播放的视频
	EpisodeFailed      = "failed"      // 转封装失败，没有可播放的视频
)

// Rendition 是产物中的一档（写入 episode.renditions 与任务记录）。
type Rendition struct {
	Name        string `json:"name"` // v + 短边，如 v720
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bitrate     int64  `json:"bitrate"`      // 平均码率（bps，含音频）
	PeakBitrate int64  `json:"peak_bitrate"` // 峰值码率（主清单 BANDWIDTH）
	Codecs      string `json:"codecs"`       // RFC 6381，如 avc1.64001f,mp4a.40.2
}

// Job 是一个转封装任务。成功后 KeyVer、Renditions、Poster、DurationSec 为本次产物的信息。
type Job struct {
	ID            uuid.UUID
	EpisodeID     int64
	DramaID       int64
	EpisodeNumber int32
	SourceKey     string
	Force         bool
	Status        JobStatus
	Progress      int32
	Attempts      int32
	Error         string
	SourceHash    string
	KeyVer        int32
	Renditions    []Rendition
	Poster        string
	DurationSec   int32
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

// EnqueueInput 是一次提交：SourceKey 为空时沿用该集上一个任务的源文件（重试）；Force 为 true 时源文件没变也重新转码。
type EnqueueInput struct {
	SourceKey string
	Force     bool
}

// Attempt 是 worker 开始一次尝试时读到的任务与分集当前的媒体状态。
type Attempt struct {
	Job
	EpisodeMediaStatus string
	EpisodeSourceHash  string
}

// ShouldSkip 表示源文件与分集当前可播放的产物相同，且没有要求强制重新转码（任务幂等）。
func (a Attempt) ShouldSkip(sourceHash string) bool {
	return !a.Force && a.EpisodeMediaStatus == EpisodeReady && a.EpisodeSourceHash != "" && a.EpisodeSourceHash == sourceHash
}

// Output 是一次成功转封装的产物，由 Complete 在一个事务内写入。
type Output struct {
	EpisodeID    int64
	DramaID      int64
	Prefix       string // 媒体桶中的产物目录（media_object.key）
	SizeBytes    int64
	MasterSHA256 string
	SourceHash   string
	KeyVer       int32
	WrappedKey   []byte // KEK 包装后的内容密钥
	Renditions   []Rendition
	Poster       string
	DurationSec  int32
	// ProviderID 是产物写入的存储源（S1-6f）。非 0 时在记录产物的同一事务内登记主存储副本并入队复制任务；
	// 为 0 表示写入的是 config.Media 的默认桶（没有配置主存储时的回退），不登记副本。
	ProviderID int64
}

// JobFilter 是任务列表的筛选；为 0 / 空表示不筛选。
type JobFilter struct {
	EpisodeID int64
	DramaID   int64
	Status    JobStatus
	Page      int
	PageSize  int
}

// JobQuery 是校验后交给仓储的查询。
type JobQuery struct {
	EpisodeID int64
	DramaID   int64
	Status    JobStatus
	Limit     int
	Offset    int
}

// JobPage 是一页任务。
type JobPage struct {
	List     []Job
	Total    int64
	Page     int
	PageSize int
}
