package transcode

import (
	"strings"
	"testing"
)

// FFmpeg 写出的 MPEG-TS 媒体清单（4 秒切片）。
const ffmpegTSPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:4.000000,
seg_00000.ts
#EXTINF:4.000000,
seg_00001.ts
#EXTINF:1.000000,
seg_00002.ts
#EXT-X-ENDLIST
`

// 历史 fMP4 清单仍可由打包器解析，保证重试或旧工具链产生的临时产物不被误判。
const ffmpegFMP4Playlist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="init.mp4"
#EXTINF:2.002000,
seg_00000.m4s
#EXTINF:2.002000,
seg_00001.m4s
#EXTINF:1.001000,
seg_00002.m4s
#EXT-X-ENDLIST
`

func TestParseMediaPlaylist(t *testing.T) {
	p, err := parseMediaPlaylist([]byte(ffmpegTSPlaylist))
	if err != nil {
		t.Fatal(err)
	}
	if p.InitURI != "" || len(p.Segments) != 3 || p.Segments[0] != (segment{4, "seg_00000.ts"}) ||
		p.Segments[2] != (segment{1, "seg_00002.ts"}) {
		t.Fatalf("MPEG-TS 解析结果不符合预期：%+v", p)
	}

	legacy, err := parseMediaPlaylist([]byte(ffmpegFMP4Playlist))
	if err != nil || legacy.InitURI != "init.mp4" || len(legacy.Segments) != 3 {
		t.Fatalf("历史 fMP4 清单仍应可解析：%+v, %v", legacy, err)
	}

	bad := map[string]string{
		"空":              "",
		"缺少头":            strings.TrimPrefix(ffmpegTSPlaylist, "#EXTM3U\n"),
		"转码未完成":          strings.Replace(ffmpegTSPlaylist, "#EXT-X-ENDLIST\n", "", 1),
		"切片路径越界":         strings.Replace(ffmpegTSPlaylist, "seg_00001.ts", "../../etc/passwd", 1),
		"切片缺少时长":         strings.Replace(ffmpegTSPlaylist, "#EXTINF:4.000000,\nseg_00000.ts", "seg_00000.ts", 1),
		"时长无效":           strings.Replace(ffmpegTSPlaylist, "#EXTINF:4.000000,", "#EXTINF:abc,", 1),
		"最后时长没有切片":       strings.Replace(ffmpegTSPlaylist, "#EXT-X-ENDLIST", "#EXTINF:4.000000,\n#EXT-X-ENDLIST", 1),
		"历史 init 段是绝对路径": strings.Replace(ffmpegFMP4Playlist, `URI="init.mp4"`, `URI="/etc/passwd"`, 1),
	}
	for name, data := range bad {
		if _, err := parseMediaPlaylist([]byte(data)); err == nil {
			t.Errorf("%s：应解析失败", name)
		}
	}
}

func TestWriteMediaPlaylist(t *testing.T) {
	var iv1, iv2 [16]byte
	iv1[15], iv2[0] = 1, 0xab
	got := string(writeMediaPlaylist("s/init.mp4", KeyURI(3), []outSegment{
		{Duration: 2.002, URI: "s/a.m4s", IV: iv1},
		{Duration: 2.5, URI: "s/b.m4s", IV: iv2},
	}))
	want := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:3
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="s/init.mp4"
#EXT-X-KEY:METHOD=AES-128,URI="xsite-key:3",IV=0x00000000000000000000000000000001
#EXTINF:2.002000,
s/a.m4s
#EXT-X-KEY:METHOD=AES-128,URI="xsite-key:3",IV=0xab000000000000000000000000000000
#EXTINF:2.500000,
s/b.m4s
#EXT-X-ENDLIST
`
	if got != want {
		t.Fatalf("\n得到\n%s\n期望\n%s", got, want)
	}
}

func TestWriteMPEGTSMediaPlaylist(t *testing.T) {
	var iv [16]byte
	iv[15] = 1
	got := string(writeMediaPlaylist("", KeyURI(3), []outSegment{{Duration: 4, URI: "s/a.ts", IV: iv}}))
	want := `#EXTM3U
#EXT-X-VERSION:4
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-KEY:METHOD=AES-128,URI="xsite-key:3",IV=0x00000000000000000000000000000001
#EXTINF:4.000000,
s/a.ts
#EXT-X-ENDLIST
`
	if got != want {
		t.Fatalf("\n得到\n%s\n期望\n%s", got, want)
	}
	if strings.Contains(got, "#EXT-X-MAP") || strings.Contains(got, "#EXT-X-INDEPENDENT-SEGMENTS") {
		t.Fatalf("MPEG-TS 清单不应带 fMP4 init 标签：%s", got)
	}
	if td := targetDuration([]outSegment{{Duration: 0.3}}); td != 1 {
		t.Fatalf("目标时长至少 1，得到 %d", td)
	}
}

func TestWriteMasterPlaylist(t *testing.T) {
	got := string(writeMasterPlaylist([]variant{
		{URI: "v360.m3u8", Bandwidth: 520000, AverageBandwidth: 410000, Width: 360, Height: 640, FrameRate: 29.97, Codecs: "avc1.64001e,mp4a.40.2"},
		{URI: "v720.m3u8", Bandwidth: 1900000, AverageBandwidth: 1500000, Width: 720, Height: 1280, Codecs: "avc1.64001f"},
	}))
	want := `#EXTM3U
#EXT-X-VERSION:4
#EXT-X-STREAM-INF:BANDWIDTH=520000,AVERAGE-BANDWIDTH=410000,CODECS="avc1.64001e,mp4a.40.2",RESOLUTION=360x640,FRAME-RATE=29.970
v360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1900000,AVERAGE-BANDWIDTH=1500000,CODECS="avc1.64001f",RESOLUTION=720x1280
v720.m3u8
`
	if got != want {
		t.Fatalf("\n得到\n%s\n期望\n%s", got, want)
	}
}

func TestPeakBitrate(t *testing.T) {
	// 第二片最大；结尾很短的切片不单独抬高峰值。
	durs := []float64{2, 2, 2, 0.2}
	sizes := []int64{100_000, 150_000, 100_000, 30_000}
	if got := peakBitrate(durs, sizes, 2); got != 600_000 {
		t.Fatalf("峰值应为 600000，得到 %d", got)
	}
	if got := averageBitrate(durs, sizes); got != 490_323 { // 380000*8/6.2 向上取整
		t.Fatalf("平均码率应为 490323，得到 %d", got)
	}
	if got := peakBitrate([]float64{0.5}, []int64{10_000}, 2); got != 160_000 {
		t.Fatalf("应退回平均码率 160000，得到 %d", got)
	}
	if got := averageBitrate(nil, nil); got != 0 {
		t.Fatalf("没有切片时为 0，得到 %d", got)
	}
}
