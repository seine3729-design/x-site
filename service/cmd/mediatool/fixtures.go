package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/transcode"
)

// 测试素材库（docs/11 X1-1）：10 部样例剧的源视频，覆盖不同比例、时长、码率、帧率、音轨与编码，
// 供转封装（S1-5）与播放（W1-3、M1-3）测试。画面是 FFmpeg 的 testsrc2 测试图（带计时器），声音是正弦波。
type fixture struct {
	Name    string // 文件名（不含扩展名）
	Ext     string
	Title   string
	Notes   string
	Width   int
	Height  int
	Rate    string // 帧率
	Seconds int
	Kbps    int    // 视频码率
	Codec   string // h264 / hevc
	Audio   string // stereo48 / mono44 / 空 = 没有音轨
	Noise   bool   // 叠加噪点，让画面更难压缩
	Rotate  int    // 旋转元数据（手机竖拍）
}

var fixtures = []fixture{
	{"f01-portrait-1080p30", "mp4", "竖屏 1080p30", "主流竖屏短剧，出 360/540/720/1080 四档", 1080, 1920, "30", 60, 6000, "h264", "stereo48", true, 0},
	{"f02-portrait-720p25-mono", "mp4", "竖屏 720p25 单声道", "源只有 720p：不放大，出三档；单声道保持单声道", 720, 1280, "25", 30, 2500, "h264", "mono44", false, 0},
	{"f03-landscape-1080p24", "mp4", "横屏 1080p24", "16:9 横屏，档位按短边计算", 1920, 1080, "24", 45, 8000, "h264", "stereo48", true, 0},
	{"f04-square-1080p30", "mp4", "方屏 1080", "1:1", 1080, 1080, "30", 20, 4000, "h264", "stereo48", false, 0},
	{"f05-portrait-3x4", "mp4", "竖屏 3:4", "810x1080，出三档", 810, 1080, "30", 15, 3000, "h264", "stereo48", false, 0},
	{"f06-portrait-1080p60", "mp4", "竖屏 1080p60", "高帧率源：输出降到 30fps", 1080, 1920, "60", 10, 10000, "h264", "stereo48", false, 0},
	{"f07-portrait-270x480", "mp4", "竖屏 270x480 低清", "比起播档还小：按源尺寸只出一档", 270, 480, "30", 12, 600, "h264", "mono44", false, 0},
	{"f08-portrait-720p-noaudio", "mp4", "竖屏 720p 无音轨", "没有音轨：CODECS 不含音频", 720, 1280, "30", 8, 2000, "h264", "", false, 0},
	{"f09-rotated-phone", "mov", "手机竖拍（旋转元数据）", "按 1920x1080 编码、旋转 90 度显示为竖屏", 1920, 1080, "30", 10, 6000, "h264", "stereo48", false, 90},
	{"f10-portrait-hevc-180s", "mp4", "竖屏 HEVC 3 分钟", "长时长、HEVC（hvc1）源", 1080, 1920, "30", 180, 3000, "hevc", "stereo48", false, 0},
}

// manifestItem 是 manifest.json 中的一项；expected_renditions 为按默认码率阶梯应出的档位，验收时对照。
type manifestItem struct {
	Key                string   `json:"key"`
	Title              string   `json:"title"`
	Notes              string   `json:"notes"`
	Width              int      `json:"width"`
	Height             int      `json:"height"`
	DisplayWidth       int      `json:"display_width"`
	DisplayHeight      int      `json:"display_height"`
	FPS                float64  `json:"fps"`
	DurationSec        float64  `json:"duration_sec"`
	VideoCodec         string   `json:"video_codec"`
	AudioChannels      int      `json:"audio_channels"`
	BitRate            int64    `json:"bit_rate"`
	SizeBytes          int64    `json:"size_bytes"`
	SHA256             string   `json:"sha256"`
	ExpectedRenditions []string `json:"expected_renditions"`
}

type manifest struct {
	Version     int            `json:"version"`
	GeneratedAt time.Time      `json:"generated_at"`
	FFmpeg      string         `json:"ffmpeg"`
	Items       []manifestItem `json:"items"`
}

func runFixtures(args []string) error {
	fs := flag.NewFlagSet("fixtures", flag.ContinueOnError)
	out := fs.String("out", "", "生成目录，留空用临时目录并在结束后删除")
	upload := fs.Bool("upload", true, "上传到 S3_BUCKET_ASSETS")
	prefix := fs.String("prefix", "fixtures/", "桶内前缀")
	only := fs.String("only", "", "只生成这些（逗号分隔的编号，如 f01,f07）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	version, err := transcode.CheckTools(ctx, "ffmpeg", "ffprobe")
	if err != nil {
		return err
	}

	dir := *out
	if dir == "" {
		if dir, err = os.MkdirTemp("", "fixtures-"); err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var bucket *blob.Bucket
	if *upload {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		m := cfg.Media
		c, err := blob.New(blob.Config{Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle})
		if err != nil {
			return err
		}
		if m.BucketAssets == "" {
			return fmt.Errorf("S3_BUCKET_ASSETS 未设置")
		}
		bucket = c.Bucket(m.BucketAssets)
	}

	man := manifest{Version: 1, GeneratedAt: time.Now().UTC().Truncate(time.Second), FFmpeg: version}
	for _, f := range fixtures {
		if *only != "" && !strings.Contains(","+*only+",", ","+f.Name[:3]+",") {
			continue
		}
		file := filepath.Join(dir, f.Name+"."+f.Ext)
		start := time.Now()
		if err := generate(ctx, f, file); err != nil {
			return fmt.Errorf("生成 %s: %w", f.Name, err)
		}
		item, err := describe(ctx, f, file, path.Join(*prefix, f.Name+"."+f.Ext))
		if err != nil {
			return fmt.Errorf("探测 %s: %w", f.Name, err)
		}
		fmt.Printf("%-28s %4dx%-4d → 显示 %4dx%-4d %5.1ffps %6.1fs %6.0f kbps %7.1f MB  档位 %s（%s）\n",
			f.Name, item.Width, item.Height, item.DisplayWidth, item.DisplayHeight, item.FPS, item.DurationSec,
			float64(item.BitRate)/1000, float64(item.SizeBytes)/1e6, strings.Join(item.ExpectedRenditions, "/"), time.Since(start).Round(time.Second))
		if bucket != nil {
			ctype := "video/mp4"
			if f.Ext == "mov" {
				ctype = "video/quicktime"
			}
			if err := bucket.PutFile(ctx, item.Key, file, blob.PutOptions{ContentType: ctype, CacheControl: "no-cache"}); err != nil {
				return err
			}
		}
		man.Items = append(man.Items, item)
	}
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	if bucket != nil {
		key := path.Join(*prefix, "manifest.json")
		if err := bucket.PutBytes(ctx, key, data, blob.PutOptions{ContentType: "application/json", CacheControl: "no-cache"}); err != nil {
			return err
		}
		fmt.Printf("已上传 %d 个素材与 %s 到 %s\n", len(man.Items), key, bucket.Name())
	} else {
		fmt.Printf("已生成到 %s（未上传）\n", dir)
	}
	return nil
}

// generate 用 FFmpeg 的测试图与正弦波生成一个源文件。
func generate(ctx context.Context, f fixture, file string) error {
	video := fmt.Sprintf("testsrc2=size=%dx%d:rate=%s", f.Width, f.Height, f.Rate)
	args := []string{"-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i", video}
	switch f.Audio {
	case "stereo48":
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
	case "mono44":
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=44100")
	}
	args = append(args, "-t", strconv.Itoa(f.Seconds), "-map", "0:v")
	if f.Noise {
		args = append(args, "-vf", "noise=alls=12:allf=t")
	}
	rate := strconv.Itoa(f.Kbps) + "k"
	switch f.Codec {
	case "hevc":
		args = append(args, "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=error", "-tag:v", "hvc1")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "veryfast")
	}
	args = append(args, "-b:v", rate, "-maxrate", rate, "-bufsize", strconv.Itoa(2*f.Kbps)+"k", "-pix_fmt", "yuv420p")
	switch f.Audio {
	case "stereo48":
		args = append(args, "-map", "1:a", "-c:a", "aac", "-b:a", "128k", "-ac", "2")
	case "mono44":
		args = append(args, "-map", "1:a", "-c:a", "aac", "-b:a", "64k", "-ac", "1")
	}
	target := file
	if f.Rotate != 0 {
		target = file + ".tmp." + f.Ext
	}
	if err := ffmpeg(ctx, append(args, "-movflags", "+faststart", target)...); err != nil {
		return err
	}
	if f.Rotate == 0 {
		return nil
	}
	// 旋转元数据（手机竖拍的方式）：FFmpeg 5.1 只在流复制时把 rotate 标签写成 tkhd 显示矩阵
	defer os.Remove(target)
	return ffmpeg(ctx, "-hide_banner", "-v", "error", "-y", "-i", target, "-map", "0", "-c", "copy",
		"-metadata:s:v:0", "rotate="+strconv.Itoa(f.Rotate), "-movflags", "+faststart", file)
}

func ffmpeg(ctx context.Context, args ...string) error {
	if b, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w：%s", err, b)
	}
	return nil
}

func describe(ctx context.Context, f fixture, file, key string) (manifestItem, error) {
	info, err := transcode.Probe(ctx, "ffprobe", file)
	if err != nil {
		return manifestItem{}, err
	}
	plan, err := transcode.PlanFor(info, transcode.DefaultLadder)
	if err != nil {
		return manifestItem{}, err
	}
	sum, size, err := fileHash(file)
	if err != nil {
		return manifestItem{}, err
	}
	item := manifestItem{
		Key: key, Title: f.Title, Notes: f.Notes, Width: f.Width, Height: f.Height,
		DisplayWidth: info.Width, DisplayHeight: info.Height, FPS: info.FPS(), DurationSec: info.Duration,
		VideoCodec: info.VideoCodec, AudioChannels: info.AudioChannels, BitRate: info.BitRate, SizeBytes: size, SHA256: sum,
	}
	for _, r := range plan.Renditions {
		item.ExpectedRenditions = append(item.ExpectedRenditions, r.Name)
	}
	return item, nil
}

func fileHash(file string) (string, int64, error) {
	fh, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
