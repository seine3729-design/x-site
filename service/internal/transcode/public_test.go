package transcode

import (
	"context"
	"strings"
	"testing"
)

func TestWriteMediaPlaylistWithoutKeyIsPublicHLS(t *testing.T) {
	out := string(writeMediaPlaylist("", "", []outSegment{{Duration: 4, URI: "s/a.ts"}}))
	if strings.Contains(out, "#EXT-X-KEY") {
		t.Fatalf("公开 HLS 不应写入内容密钥：%s", out)
	}
	if strings.Contains(out, "#EXT-X-MAP") || !strings.Contains(out, "\ns/a.ts\n") || !strings.Contains(out, "#EXT-X-VERSION:4") {
		t.Fatalf("公开 MPEG-TS HLS 格式不正确：%s", out)
	}
}

func TestOutputPrefixesUseVideoNamespace(t *testing.T) {
	if got := OutputPrefix(12, "job"); got != "video/episodes/12/job" {
		t.Fatalf("分集输出目录 = %q", got)
	}
	if got := AssetOutputPrefix("asset"); got != "video/assets/asset" {
		t.Fatalf("广告输出目录 = %q", got)
	}
}

func TestPipeline_PublicVideoWritesPlayableHLS(t *testing.T) {
	requireFFmpeg(t)
	source := makeSource(t, "-f", "lavfi", "-i", "testsrc2=size=360x640:rate=25", "-t", "3", "-c:v", "libx264", "-preset", "ultrafast")
	f := newFixture(t, source)
	ctx := context.Background()
	ws, err := f.p.Fetch(ctx, "ad-upload", "fixtures/t.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	res, err := f.p.Package(ctx, ws, Request{JobID: "ad-upload", Prefix: AssetOutputPrefix("ad-upload"), Public: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefix != "video/assets/ad-upload" || res.Poster != "" {
		t.Fatalf("公开视频产物不符合预期：%+v", res)
	}
	master := string(f.media.get(t, res.Prefix+"/master.m3u8"))
	if strings.Contains(master, "#EXT-X-KEY") || len(res.Renditions) == 0 {
		t.Fatalf("公开主清单不应含密钥且必须有档位：%s", master)
	}
	playlist := string(f.media.get(t, res.Prefix+"/"+res.Renditions[0].Name+".m3u8"))
	if strings.Contains(playlist, "#EXT-X-KEY") || strings.Contains(playlist, "#EXT-X-MAP") || !strings.Contains(playlist, ".ts") {
		t.Fatalf("公开媒体清单应为无密钥 MPEG-TS：%s", playlist)
	}
	if frames := decodeHLS(t, f, res, 1, testKey, res.Renditions[0].Name+".m3u8"); frames != 75 {
		t.Fatalf("公开 HLS 解码帧数 = %d，want 75", frames)
	}
}
