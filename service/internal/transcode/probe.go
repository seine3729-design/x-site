package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SourceInfo 是 ffprobe 探测出的源文件信息。Width、Height 是显示尺寸（已按旋转与非方形像素换算）。
type SourceInfo struct {
	Width, Height int
	// FPSNum / FPSDen 是平均帧率；为 0 表示未知（如部分可变帧率文件），转码时保持源时间戳。
	FPSNum, FPSDen int
	Duration       float64 // 秒
	VideoCodec     string
	Rotation       int // 0 / 90 / 180 / 270
	HasAudio       bool
	AudioChannels  int
	BitRate        int64 // 整体码率（bps），未知为 0
}

// FPS 返回平均帧率，未知时为 0。
func (s SourceInfo) FPS() float64 {
	if s.FPSNum <= 0 || s.FPSDen <= 0 {
		return 0
	}
	return float64(s.FPSNum) / float64(s.FPSDen)
}

// 源文件限制：超出时任务直接失败（不重试）。
const (
	maxSourceBytes    = 4 << 30 // 4 GiB
	maxSourceDuration = 4 * time.Hour
	minSourceSide     = 64
	maxSourceSide     = 7680
	probeTimeout      = time.Minute
)

// 只接受常见的视频容器（ffmpeg 的 demuxer 名）。播放列表、拼接列表、图片序列等能引用其他文件或地址的格式一律拒绝，
// 配合只允许 file 协议，防止精心构造的源文件读取 worker 上的其他文件或发起网络请求。
const allowedFormats = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,avi,flv,mpegts,mpeg,asf"

// inputGuard 是 ffprobe / ffmpeg 读取源文件时的输入限制。
var inputGuard = []string{"-protocol_whitelist", "file", "-format_whitelist", allowedFormats}

// Probe 用 ffprobe 读取源文件信息。文件不是可识别的视频时返回 Permanent 错误。
func Probe(ctx context.Context, ffprobe, path string) (SourceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	args := append([]string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams"}, inputGuard...)
	args = append(args, "-i", path)
	out, err := runCapture(ctx, ffprobe, args, filepath.Dir(path))
	if err != nil {
		if ctx.Err() != nil {
			return SourceInfo{}, fmt.Errorf("ffprobe 超时: %w", ctx.Err())
		}
		return SourceInfo{}, Permanent(fmt.Errorf("源文件不是可识别的视频：%w", err))
	}
	info, err := parseProbe(out)
	if err != nil {
		return SourceInfo{}, Permanent(err)
	}
	return info, nil
}

type probeJSON struct {
	Streams []struct {
		CodecName         string `json:"codec_name"`
		CodecType         string `json:"codec_type"`
		Width             int    `json:"width"`
		Height            int    `json:"height"`
		SampleAspectRatio string `json:"sample_aspect_ratio"`
		AvgFrameRate      string `json:"avg_frame_rate"`
		RFrameRate        string `json:"r_frame_rate"`
		Duration          string `json:"duration"`
		Channels          int    `json:"channels"`
		Disposition       struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		Tags         map[string]string `json:"tags"`
		SideDataList []struct {
			SideDataType string  `json:"side_data_type"`
			Rotation     float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

// parseProbe 解析 ffprobe 的 JSON 输出，并校验时长与尺寸限制。
func parseProbe(data []byte) (SourceInfo, error) {
	var p probeJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return SourceInfo{}, fmt.Errorf("无法解析 ffprobe 输出: %w", err)
	}
	var info SourceInfo
	found := false
	for _, s := range p.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && !found:
			found = true
			info.VideoCodec = s.CodecName
			info.Width, info.Height = s.Width, s.Height
			if num, den, ok := parseRatio(s.SampleAspectRatio, ":"); ok && num != den {
				info.Width = int(math.Round(float64(s.Width) * float64(num) / float64(den)))
			}
			info.Rotation = rotationOf(s.Tags["rotate"], s.SideDataList)
			if info.Rotation == 90 || info.Rotation == 270 {
				info.Width, info.Height = info.Height, info.Width
			}
			info.FPSNum, info.FPSDen = frameRate(s.AvgFrameRate, s.RFrameRate)
			if info.Duration == 0 {
				info.Duration, _ = strconv.ParseFloat(s.Duration, 64)
			}
		case s.CodecType == "audio" && !info.HasAudio:
			info.HasAudio = true
			info.AudioChannels = s.Channels
		}
	}
	if !found {
		return SourceInfo{}, fmt.Errorf("源文件没有视频轨")
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil && d > 0 {
		info.Duration = d
	}
	info.BitRate, _ = strconv.ParseInt(p.Format.BitRate, 10, 64)

	switch {
	case info.Duration <= 0 || math.IsNaN(info.Duration):
		return SourceInfo{}, fmt.Errorf("无法读取源文件时长")
	case info.Duration > maxSourceDuration.Seconds():
		return SourceInfo{}, fmt.Errorf("源文件时长 %.0f 秒，超过上限 %.0f 秒", info.Duration, maxSourceDuration.Seconds())
	case min(info.Width, info.Height) < minSourceSide || max(info.Width, info.Height) > maxSourceSide:
		return SourceInfo{}, fmt.Errorf("源文件分辨率 %dx%d 不在支持范围（短边 ≥ %d，长边 ≤ %d）", info.Width, info.Height, minSourceSide, maxSourceSide)
	}
	return info, nil
}

// rotationOf 取显示旋转角度（0/90/180/270）。ffprobe 在显示矩阵中给出逆时针角度（如 -90），旧文件用 rotate 标签（顺时针）；
// 只有 90 / 270 会交换宽高，方向由 FFmpeg 自动旋转时处理。显示矩阵的类型名在 5.x 为 DisplayMatrix，新版为 Display Matrix。
func rotationOf(tag string, side []struct {
	SideDataType string  `json:"side_data_type"`
	Rotation     float64 `json:"rotation"`
}) int {
	deg := 0
	for _, s := range side {
		if strings.EqualFold(strings.ReplaceAll(s.SideDataType, " ", ""), "DisplayMatrix") {
			deg = -int(math.Round(s.Rotation))
			return ((deg % 360) + 360) % 360
		}
	}
	if v, err := strconv.Atoi(strings.TrimSpace(tag)); err == nil {
		deg = v
	}
	return ((deg % 360) + 360) % 360
}

// frameRate 优先用平均帧率，异常（0、超过 240）时退回 r_frame_rate，都不可用时返回 0/0。
func frameRate(avg, r string) (int, int) {
	for _, v := range []string{avg, r} {
		if num, den, ok := parseRatio(v, "/"); ok && num > 0 {
			if fps := float64(num) / float64(den); fps > 0 && fps <= 240 {
				return num, den
			}
		}
	}
	return 0, 0
}

func parseRatio(v, sep string) (int, int, bool) {
	a, b, ok := strings.Cut(v, sep)
	if !ok {
		return 0, 0, false
	}
	num, err1 := strconv.Atoi(a)
	den, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}
