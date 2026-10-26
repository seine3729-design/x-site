package transcode

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// HLS 清单（RFC 8216）。FFmpeg 写出的各档清单只用来读取切片顺序与时长；交付用的清单由下面的函数重新生成。

// KeyURI 是清单中内容密钥地址的占位符，带密钥版本；S1-8 返回清单时改写为带播放令牌的密钥接口地址。
func KeyURI(keyVer int32) string { return "xsite-key:" + strconv.FormatInt(int64(keyVer), 10) }

type segment struct {
	Duration float64
	URI      string
}

type mediaPlaylist struct {
	// InitURI 只用于兼容旧 fMP4 清单；新的 MPEG-TS 清单没有 EXT-X-MAP。
	InitURI  string
	Segments []segment
}

// parseMediaPlaylist 解析 FFmpeg 写出的 MPEG-TS 或旧 fMP4 媒体清单。只接受同目录下的文件名（不含 / 与 ..）。
func parseMediaPlaylist(data []byte) (mediaPlaylist, error) {
	var p mediaPlaylist
	sc := bufio.NewScanner(bytes.NewReader(data))
	first, ended := true, false
	pending := -1.0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			if line != "#EXTM3U" {
				return p, fmt.Errorf("清单缺少 #EXTM3U")
			}
			first = false
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			uri := attr(strings.TrimPrefix(line, "#EXT-X-MAP:"), "URI")
			if !plainName(uri) {
				return p, fmt.Errorf("init 段地址 %q 不是同目录文件名", uri)
			}
			p.InitURI = uri
		case strings.HasPrefix(line, "#EXTINF:"):
			if pending >= 0 {
				return p, fmt.Errorf("连续两个 EXTINF 之间缺少切片")
			}
			v, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			d, err := strconv.ParseFloat(v, 64)
			if err != nil || d <= 0 || math.IsInf(d, 0) || math.IsNaN(d) {
				return p, fmt.Errorf("切片时长 %q 无效", v)
			}
			pending = d
		case line == "#EXT-X-ENDLIST":
			ended = true
		case strings.HasPrefix(line, "#"):
			// 其他标签由我们重新生成。
		default:
			if pending < 0 {
				return p, fmt.Errorf("切片 %q 前缺少 #EXTINF", line)
			}
			if !plainName(line) {
				return p, fmt.Errorf("切片地址 %q 不是同目录文件名", line)
			}
			p.Segments = append(p.Segments, segment{Duration: pending, URI: line})
			pending = -1
		}
	}
	if err := sc.Err(); err != nil {
		return p, err
	}
	switch {
	case first:
		return p, fmt.Errorf("清单为空")
	case pending >= 0:
		return p, fmt.Errorf("最后一个 EXTINF 后缺少切片")
	case len(p.Segments) == 0:
		return p, fmt.Errorf("清单没有切片")
	case !ended:
		return p, fmt.Errorf("清单缺少 #EXT-X-ENDLIST（转码未完成）")
	}
	return p, nil
}

// attr 从属性列表中取一个属性值（去掉引号）。只用于读取 FFmpeg 的输出，值中不含逗号。
func attr(list, name string) string {
	for part := range strings.SplitSeq(list, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k == name {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\"`) && !strings.HasPrefix(s, "#")
}

// outSegment 是交付清单中的一个切片。
type outSegment struct {
	Duration float64
	URI      string
	IV       [16]byte
}

// writeMediaPlaylist 生成交付用的媒体清单。新 MPEG-TS 产物不含 init 段；传入 initURI 仅用于兼容历史 fMP4 夹具。
// keyURI 非空时切片使用 AES-128；空时输出公开 HLS，供广告等非分集资产使用。
func writeMediaPlaylist(initURI, keyURI string, segs []outSegment) []byte {
	var b bytes.Buffer
	if initURI == "" {
		// MPEG-TS + AES-128 在协议版本 4 即可完整表达，兼顾旧设备与 AVERAGE-BANDWIDTH 主清单。
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:4\n")
	} else {
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	}
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", targetDuration(segs))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	if initURI != "" {
		b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
		fmt.Fprintf(&b, "#EXT-X-MAP:URI=%q\n", initURI)
	}
	for _, s := range segs {
		if keyURI != "" {
			fmt.Fprintf(&b, "#EXT-X-KEY:METHOD=AES-128,URI=%q,IV=0x%s\n", keyURI, hex.EncodeToString(s.IV[:]))
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", s.Duration, s.URI)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.Bytes()
}

// targetDuration 是四舍五入后最长的切片时长（RFC 8216 第 4.3.3.1 节），至少 1。
func targetDuration(segs []outSegment) int {
	t := 1
	for _, s := range segs {
		t = max(t, int(math.Round(s.Duration)))
	}
	return t
}

// variant 是主清单中的一档。
type variant struct {
	URI              string
	Bandwidth        int64 // 峰值码率
	AverageBandwidth int64
	Width, Height    int
	FrameRate        float64 // 0 表示未知，不写
	Codecs           string
}

// writeMasterPlaylist 生成主清单。各档按传入顺序（码率从低到高）：播放器默认从第一档起播。
func writeMasterPlaylist(vs []variant) []byte {
	var b bytes.Buffer
	// 新产物是 MPEG-TS HLS；不写仅为 fMP4 设计的 EXT-X-INDEPENDENT-SEGMENTS，实际 GOP 仍由转码器强制对齐。
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:4\n")
	for _, v := range vs {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,CODECS=%q,RESOLUTION=%dx%d",
			v.Bandwidth, v.AverageBandwidth, v.Codecs, v.Width, v.Height)
		if v.FrameRate > 0 {
			fmt.Fprintf(&b, ",FRAME-RATE=%.3f", v.FrameRate)
		}
		fmt.Fprintf(&b, "\n%s\n", v.URI)
	}
	return b.Bytes()
}

// peakBitrate 按 RFC 8216 第 4.3.4.2 节计算峰值码率：总时长在目标时长 0.5–1.5 倍之间的任意连续切片的最大码率。
// 结尾很短的切片不会单独计算（避免一个关键帧把峰值抬高数倍）；没有满足条件的窗口时退回平均码率。
func peakBitrate(durs []float64, sizes []int64, target float64) int64 {
	var peak float64
	for i := range durs {
		var d float64
		var bytes int64
		for j := i; j < len(durs); j++ {
			d += durs[j]
			bytes += sizes[j]
			if d > 1.5*target {
				break
			}
			if d >= 0.5*target {
				peak = max(peak, float64(bytes)*8/d)
			}
		}
	}
	if peak == 0 {
		return averageBitrate(durs, sizes)
	}
	return int64(math.Ceil(peak))
}

// averageBitrate 是全部切片的平均码率（bps）。
func averageBitrate(durs []float64, sizes []int64) int64 {
	var d float64
	var bytes int64
	for i := range durs {
		d += durs[i]
		bytes += sizes[i]
	}
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(bytes) * 8 / d))
}
