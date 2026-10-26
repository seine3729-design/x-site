package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"xsite/service/internal/crypto/mediakey"
)

// Media 是对象存储（S0-9）与媒体转封装（S1-5，docs/specs/S1-5.md）的配置。目前只有 worker 与本地工具使用；
// S1-6a 起存储源改在后台配置，这里的对象存储是本地开发与转封装产物使用的默认存储。
type Media struct {
	// S3Endpoint 为空时使用 AWS 默认地址；SeaweedFS、R2 等 S3 兼容服务填写服务地址。
	S3Endpoint   string `env:"S3_ENDPOINT"`
	S3Region     string `env:"S3_REGION" envDefault:"us-east-1"`
	S3PathStyle  bool   `env:"S3_FORCE_PATH_STYLE"`
	S3AccessKey  string `env:"S3_ACCESS_KEY"`
	S3SecretKey  string `env:"S3_SECRET_KEY"`
	BucketMedia  string `env:"S3_BUCKET_MEDIA"`  // HLS 产物（经视频线路分发）
	BucketAssets string `env:"S3_BUCKET_ASSETS"` // 图片等公开资源（首帧图、封面）

	// TranscodeConcurrency 是本进程同时转封装的任务数；0 表示不消费 media 队列（没有 FFmpeg 的通用 worker）。
	TranscodeConcurrency int           `env:"MEDIA_TRANSCODE_CONCURRENCY" envDefault:"0"`
	TranscodeTimeout     time.Duration `env:"MEDIA_TRANSCODE_TIMEOUT" envDefault:"1h"`
	X264Preset           string        `env:"MEDIA_X264_PRESET" envDefault:"veryfast"`
	// WorkDir 是源文件与中间产物的临时目录，为空时用系统临时目录。
	WorkDir string `env:"MEDIA_WORK_DIR"`
	// SourceBucket 是源文件所在的桶：线上必须是私有的入库桶，不能是公开读的图片桶。
	SourceBucket string `env:"MEDIA_SOURCE_BUCKET"`
	// KEKs 是内容密钥的包装密钥 `kid:secret[,kid2:secret2]`（S2-8 接入 KMS 前使用）。
	KEKs        string `env:"MEDIA_KEKS"`
	FFmpegPath  string `env:"FFMPEG_PATH" envDefault:"ffmpeg"`
	FFprobePath string `env:"FFPROBE_PATH" envDefault:"ffprobe"`
}

// 单个 worker 进程的并发上限：每路转封装同时跑多档 x264 编码，再多只会互相抢 CPU。
const maxTranscodeConcurrency = 16

var x264Presets = []string{"ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow"}

// TranscodeEnabled 表示本进程是否执行转封装任务。
func (m Media) TranscodeEnabled() bool { return m.TranscodeConcurrency > 0 }

// ValidateTranscode 校验转封装需要的配置，worker 在启用转封装时调用；问题一次全部列出。
func (c Config) ValidateTranscode() error {
	m := c.Media
	var problems []string
	if m.TranscodeConcurrency < 0 || m.TranscodeConcurrency > maxTranscodeConcurrency {
		problems = append(problems, fmt.Sprintf("MEDIA_TRANSCODE_CONCURRENCY 应在 0–%d 之间", maxTranscodeConcurrency))
	}
	if !m.TranscodeEnabled() {
		return joinProblems(problems)
	}
	if m.S3Region == "" || m.S3AccessKey == "" || m.S3SecretKey == "" {
		problems = append(problems, "转封装需要对象存储：S3_REGION、S3_ACCESS_KEY、S3_SECRET_KEY 不能为空")
	}
	if m.S3Endpoint != "" {
		if u, err := url.Parse(m.S3Endpoint); err != nil || u.Host == "" || (u.Scheme != "https" && (u.Scheme != "http" || c.AppEnv == "production")) {
			problems = append(problems, "S3_ENDPOINT 应为 http(s)://host[:port]（production 只允许 https）")
		}
	}
	for _, b := range []struct{ name, value string }{
		{"S3_BUCKET_MEDIA", m.BucketMedia}, {"S3_BUCKET_ASSETS", m.BucketAssets}, {"MEDIA_SOURCE_BUCKET", m.SourceBucket},
	} {
		if b.value == "" {
			problems = append(problems, b.name+" 不能为空")
		}
	}
	// 图片桶对外公开读：源文件（未加密的原片）放进去就等于公开
	if c.AppEnv == "production" && m.SourceBucket != "" && m.SourceBucket == m.BucketAssets {
		problems = append(problems, "production 环境 MEDIA_SOURCE_BUCKET 不能是公开读的图片桶 S3_BUCKET_ASSETS")
	}
	if m.KEKs == "" {
		problems = append(problems, "MEDIA_KEKS 不能为空（内容密钥的包装密钥 kid:secret）")
	} else if _, err := mediakey.ParseKeyring(m.KEKs); err != nil {
		problems = append(problems, "MEDIA_KEKS 无效："+err.Error())
	}
	if !slices.Contains(x264Presets, m.X264Preset) {
		problems = append(problems, "MEDIA_X264_PRESET 应为 "+strings.Join(x264Presets, "、")+" 之一")
	}
	if m.TranscodeTimeout < time.Minute || m.TranscodeTimeout > 24*time.Hour {
		problems = append(problems, "MEDIA_TRANSCODE_TIMEOUT 应在 1m–24h 之间")
	}
	if m.WorkDir != "" && !filepath.IsAbs(m.WorkDir) {
		problems = append(problems, "MEDIA_WORK_DIR 应为绝对路径")
	}
	if m.FFmpegPath == "" || m.FFprobePath == "" {
		problems = append(problems, "FFMPEG_PATH、FFPROBE_PATH 不能为空")
	}
	return joinProblems(problems)
}

func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New("转封装配置校验失败: " + strings.Join(problems, "；"))
}
