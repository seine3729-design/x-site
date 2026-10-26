package transcode

import (
	"fmt"
	"math"
)

// Rung 是码率阶梯的一档：按短边定义，竖屏、横屏、方屏通用（docs/08 第 2.1 节）。
type Rung struct {
	Short   int   // 短边像素
	MaxRate int64 // 视频码率上限（bps）
}

// DefaultLadder 是默认码率阶梯：360p 起播、480p 标清、720p 高清、1080p 超清。
// 播放器从第一档起播，网络好时由 ABR 自动升到 1080p，用户也可以手选（docs/08 第 2.1 节）。
var DefaultLadder = []Rung{
	{Short: 360, MaxRate: 400_000},
	{Short: 480, MaxRate: 850_000},
	{Short: 720, MaxRate: 1_600_000},
	{Short: 1080, MaxRate: 3_000_000},
}

// 输出帧率上限：短剧不需要高帧率，超过时减半（60 → 30、50 → 25）。
const maxOutputFPS = 30.5

// 首帧图最长边对应的档位：不超过高清档，兼顾清晰度与体积。
const posterMaxShort = 720

// Rendition 是计划输出的一档。
type Rendition struct {
	Name          string // 清单与目录名：v + 短边，如 v720
	Width, Height int
	MaxRate       int64
}

// Plan 是一次转封装的输出计划。
type Plan struct {
	Renditions []Rendition // 码率从低到高
	// FPSNum / FPSDen 不为 0 时输出固定为该帧率（源帧率过高时减半）；为 0 时保持源时间戳。
	FPSNum, FPSDen int
	// AudioChannels 是输出 AAC 声道数。无音轨的源会补双声道静音 AAC，保证所有新产物都有 H.264 + AAC。
	AudioChannels int
	// GenerateSilentAudio 表示第二个 FFmpeg 输入是本地生成的静音音轨，不读取外部资源。
	GenerateSilentAudio bool
	Poster              Rendition
}

// FPS 返回输出帧率；保持源时间戳时返回源帧率（可能为 0）。
func (p Plan) FPS(src SourceInfo) float64 {
	if p.FPSNum > 0 && p.FPSDen > 0 {
		return float64(p.FPSNum) / float64(p.FPSDen)
	}
	return src.FPS()
}

// PlanFor 按源文件选择档位：只出不超过源短边的档位，源比最低档还小时按源尺寸出一档；不放大。
func PlanFor(src SourceInfo, ladder []Rung) (Plan, error) {
	if len(ladder) == 0 {
		return Plan{}, fmt.Errorf("码率阶梯为空")
	}
	if src.Width <= 0 || src.Height <= 0 {
		return Plan{}, fmt.Errorf("源尺寸无效 %dx%d", src.Width, src.Height)
	}
	short, long := min(src.Width, src.Height), max(src.Width, src.Height)
	var p Plan
	for _, r := range ladder {
		if r.Short <= short {
			p.Renditions = append(p.Renditions, rendition(src, r.Short, long*r.Short, short, r.MaxRate))
		}
	}
	if len(p.Renditions) == 0 {
		// 比起播档还小：按源尺寸（取偶数）出一档，码率上限用最低档
		p.Renditions = append(p.Renditions, rendition(src, short, long, 1, ladder[0].MaxRate))
	}

	p.Poster = p.Renditions[0]
	for _, r := range p.Renditions {
		if min(r.Width, r.Height) <= posterMaxShort {
			p.Poster = r
		}
	}
	if fps := src.FPS(); fps > maxOutputFPS {
		num, den := src.FPSNum, src.FPSDen
		for float64(num)/float64(den) > maxOutputFPS {
			den *= 2
		}
		p.FPSNum, p.FPSDen = num, den
	}
	if src.HasAudio {
		p.AudioChannels = 2
		if src.AudioChannels == 1 {
			p.AudioChannels = 1
		}
	} else {
		p.AudioChannels = 2
		p.GenerateSilentAudio = true
	}
	return p, nil
}

// rendition 计算一档的输出尺寸：短边为 shortSide，长边按源比例（longNum / longDen）取最接近的偶数。
// 最终再向下夹紧到源显示尺寸，保证 H.264 的偶数要求不会把奇数源放大。
func rendition(src SourceInfo, shortSide, longNum, longDen int, maxRate int64) Rendition {
	longSide := evenOf(float64(longNum) / float64(longDen))
	shortSide = evenOf(float64(shortSide))
	r := Rendition{MaxRate: maxRate, Width: longSide, Height: shortSide}
	if src.Height >= src.Width { // 竖屏与方屏：宽是短边
		r.Width, r.Height = shortSide, longSide
	}
	r.Width = min(r.Width, evenAtMost(src.Width))
	r.Height = min(r.Height, evenAtMost(src.Height))
	r.Name = fmt.Sprintf("v%d", min(r.Width, r.Height))
	return r
}

// evenAtMost 返回不大于输入值的最大偶数；Probe 已保证源尺寸不小于 64，因此至少为 2。
func evenAtMost(v int) int {
	return max(v/2*2, 2)
}

// evenOf 取最接近的偶数（H.264 4:2:0 要求宽高为偶数），至少为 2。直接从小数取整，避免两次舍入（1920.67 → 1920）。
func evenOf(v float64) int {
	return max(int(math.Round(v/2))*2, 2)
}
