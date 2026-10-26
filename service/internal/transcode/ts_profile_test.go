package transcode

import (
	"slices"
	"testing"
)

// 新上传媒体统一使用 4 秒 MPEG-TS HLS，并仅生成 360p、480p、720p 三档。
// 此测试在旧 CMAF/fMP4 profile 上应失败，用于锁定迁移后的对外产物约定。
func TestDefaultHLSProfileIsTS(t *testing.T) {
	if SegmentSeconds != 4 {
		t.Errorf("切片时长 = %d，期望 4 秒", SegmentSeconds)
	}

	plan, err := PlanFor(SourceInfo{Width: 720, Height: 1280, HasAudio: true, AudioChannels: 2}, DefaultLadder)
	if err != nil {
		t.Fatal(err)
	}
	gotRenditions := make([]string, len(plan.Renditions))
	for i, rendition := range plan.Renditions {
		gotRenditions[i] = rendition.Name
	}
	if want := []string{"v360", "v480", "v720"}; !slices.Equal(gotRenditions, want) {
		t.Errorf("720p 源应出 360/480/720 三档（不放大），得到 %v，期望 %v", gotRenditions, want)
	}
	// 1080p 源要把四档都出来，前台清晰度菜单才有 360/480/720/1080 可选
	full, err := PlanFor(SourceInfo{Width: 1080, Height: 1920, HasAudio: true, AudioChannels: 2}, DefaultLadder)
	if err != nil {
		t.Fatal(err)
	}
	gotFull := make([]string, len(full.Renditions))
	for i, rendition := range full.Renditions {
		gotFull[i] = rendition.Name
	}
	if want := []string{"v360", "v480", "v720", "v1080"}; !slices.Equal(gotFull, want) {
		t.Errorf("1080p 源的清晰度阶梯 = %v，期望 %v", gotFull, want)
	}

	args := transcodeArgs("/work/source.mp4", "/work/out", plan, "veryfast")
	if !hasSeq(args, "-hls_time", "4", "-hls_playlist_type", "vod", "-hls_segment_type", "mpegts") {
		t.Errorf("FFmpeg 未配置 4 秒 MPEG-TS HLS：%q", args)
	}
	if !hasSeq(args, "-hls_segment_filename", "/work/out/v720/seg_%05d.ts", "/work/out/v720/index.m3u8") {
		t.Errorf("FFmpeg 未输出 .ts 分片：%q", args)
	}
	if slices.Contains(args, "-hls_fmp4_init_filename") {
		t.Errorf("MPEG-TS profile 不应输出 fMP4 init 段：%q", args)
	}
}
