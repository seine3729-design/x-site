package config

import (
	"strings"
	"testing"
	"time"
)

func validMedia() Media {
	return Media{
		S3Endpoint: "http://seaweedfs:8333", S3Region: "us-east-1", S3PathStyle: true,
		S3AccessKey: "ak", S3SecretKey: "sk", BucketMedia: "xsite-media", BucketAssets: "xsite-assets",
		TranscodeConcurrency: 1, TranscodeTimeout: time.Hour, X264Preset: "veryfast", SourceBucket: "xsite-assets",
		KEKs: "dev1:" + token32, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
	}
}

func TestValidateTranscode(t *testing.T) {
	if err := (Config{AppEnv: "development", Media: validMedia()}).ValidateTranscode(); err != nil {
		t.Fatalf("合法配置不应报错：%v", err)
	}
	// 关闭转封装时不要求对象存储与 KEK（没有 FFmpeg 的通用 worker）
	if err := (Config{AppEnv: "production", Media: Media{}}).ValidateTranscode(); err != nil {
		t.Fatalf("未启用转封装时不应校验：%v", err)
	}

	tests := []struct {
		name, env, want string
		edit            func(*Media)
	}{
		{"缺少 KEK", "development", "MEDIA_KEKS", func(m *Media) { m.KEKs = "" }},
		{"KEK 太短", "development", "MEDIA_KEKS", func(m *Media) { m.KEKs = "dev1:short" }},
		{"缺少凭据", "development", "S3_ACCESS_KEY", func(m *Media) { m.S3SecretKey = "" }},
		{"缺少媒体桶", "development", "S3_BUCKET_MEDIA", func(m *Media) { m.BucketMedia = "" }},
		{"缺少源文件桶", "development", "MEDIA_SOURCE_BUCKET", func(m *Media) { m.SourceBucket = "" }},
		{"production 源文件放在公开图片桶", "production", "MEDIA_SOURCE_BUCKET", func(m *Media) { m.S3Endpoint = "https://s3.example.com" }},
		{"production 不允许 http 存储地址", "production", "S3_ENDPOINT", func(m *Media) { m.SourceBucket = "xsite-ingest" }},
		{"未知的 x264 速度档", "development", "MEDIA_X264_PRESET", func(m *Media) { m.X264Preset = "turbo" }},
		{"超时太短", "development", "MEDIA_TRANSCODE_TIMEOUT", func(m *Media) { m.TranscodeTimeout = time.Second }},
		{"并发过大", "development", "MEDIA_TRANSCODE_CONCURRENCY", func(m *Media) { m.TranscodeConcurrency = 99 }},
		{"临时目录不是绝对路径", "development", "MEDIA_WORK_DIR", func(m *Media) { m.WorkDir = "tmp" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMedia()
			tt.edit(&m)
			err := (Config{AppEnv: tt.env, Media: m}).ValidateTranscode()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("应指出 %s，得到 %v", tt.want, err)
			}
		})
	}
}
