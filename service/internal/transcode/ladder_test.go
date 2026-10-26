package transcode

import (
	"fmt"
	"slices"
	"testing"
)

func names(p Plan) []string {
	out := make([]string, len(p.Renditions))
	for i, r := range p.Renditions {
		out[i] = fmt.Sprintf("%s:%dx%d@%d", r.Name, r.Width, r.Height, r.MaxRate)
	}
	return out
}

func TestPlanFor_Ladder(t *testing.T) {
	tests := []struct {
		name string
		src  SourceInfo
		want []string
	}{
		{"竖屏 1080p：四档（docs/08 第 2.1 节）", SourceInfo{Width: 1080, Height: 1920},
			[]string{"v360:360x640@400000", "v480:480x854@850000", "v720:720x1280@1600000", "v1080:1080x1920@3000000"}},
		{"竖屏 720p：不放大，三档", SourceInfo{Width: 720, Height: 1280},
			[]string{"v360:360x640@400000", "v480:480x854@850000", "v720:720x1280@1600000"}},
		{"横屏 1080p：短边是高", SourceInfo{Width: 1920, Height: 1080},
			[]string{"v360:640x360@400000", "v480:854x480@850000", "v720:1280x720@1600000", "v1080:1920x1080@3000000"}},
		{"方屏", SourceInfo{Width: 1080, Height: 1080},
			[]string{"v360:360x360@400000", "v480:480x480@850000", "v720:720x720@1600000", "v1080:1080x1080@3000000"}},
		{"3:4", SourceInfo{Width: 810, Height: 1080},
			[]string{"v360:360x480@400000", "v480:480x640@850000", "v720:720x960@1600000"}},
		{"短边接近 720 仍不放大", SourceInfo{Width: 714, Height: 1270},
			[]string{"v360:360x640@400000", "v480:480x854@850000"}},
		{"比起播档还小：按源尺寸出一档", SourceInfo{Width: 270, Height: 480}, []string{"v270:270x480@400000"}},
		{"奇数尺寸向下取偶数且不放大", SourceInfo{Width: 201, Height: 355}, []string{"v200:200x354@400000"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := PlanFor(tt.src, DefaultLadder)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(p); !slices.Equal(got, tt.want) {
				t.Fatalf("\n得到 %v\n期望 %v", got, tt.want)
			}
			for _, r := range p.Renditions {
				if r.Width%2 != 0 || r.Height%2 != 0 {
					t.Fatalf("宽高必须是偶数：%+v", r)
				}
				if r.Width > tt.src.Width || r.Height > tt.src.Height {
					t.Fatalf("输出不能超过源显示尺寸 %dx%d：%+v", tt.src.Width, tt.src.Height, r)
				}
			}
		})
	}
}

func TestPlanFor_FPSAudioPoster(t *testing.T) {
	tests := []struct {
		name             string
		src              SourceInfo
		num, den, ch     int
		silent           bool
		poster           string
		wantOutFPSApprox float64
	}{
		{"60fps 减半到 30", SourceInfo{Width: 1080, Height: 1920, FPSNum: 60, FPSDen: 1, HasAudio: true, AudioChannels: 2}, 60, 2, 2, false, "v720", 30},
		{"59.94 减半到 29.97", SourceInfo{Width: 1080, Height: 1920, FPSNum: 60000, FPSDen: 1001}, 60000, 2002, 2, true, "v720", 29.97},
		{"120fps 减到 30", SourceInfo{Width: 720, Height: 1280, FPSNum: 120, FPSDen: 1}, 120, 4, 2, true, "v720", 30},
		{"30fps 保持源时间戳", SourceInfo{Width: 720, Height: 1280, FPSNum: 30, FPSDen: 1}, 0, 0, 2, true, "v720", 30},
		{"单声道保持单声道", SourceInfo{Width: 540, Height: 960, HasAudio: true, AudioChannels: 1}, 0, 0, 1, false, "v480", 0},
		{"5.1 声道下混为立体声", SourceInfo{Width: 270, Height: 480, HasAudio: true, AudioChannels: 6}, 0, 0, 2, false, "v270", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := PlanFor(tt.src, DefaultLadder)
			if err != nil {
				t.Fatal(err)
			}
			if p.FPSNum != tt.num || p.FPSDen != tt.den || p.AudioChannels != tt.ch || p.GenerateSilentAudio != tt.silent || p.Poster.Name != tt.poster {
				t.Fatalf("得到 fps=%d/%d 声道=%d 静音=%v 首帧图=%s", p.FPSNum, p.FPSDen, p.AudioChannels, p.GenerateSilentAudio, p.Poster.Name)
			}
			if got := p.FPS(tt.src); got < tt.wantOutFPSApprox-0.01 || got > tt.wantOutFPSApprox+0.01 {
				t.Fatalf("输出帧率 %.3f，期望约 %.2f", got, tt.wantOutFPSApprox)
			}
		})
	}
	if _, err := PlanFor(SourceInfo{}, DefaultLadder); err == nil {
		t.Fatal("尺寸无效应报错")
	}
	if _, err := PlanFor(SourceInfo{Width: 720, Height: 1280}, nil); err == nil {
		t.Fatal("阶梯为空应报错")
	}
}
