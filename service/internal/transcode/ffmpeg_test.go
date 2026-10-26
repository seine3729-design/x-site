package transcode

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// hasSeq 判断 args 中是否有连续的 seq。
func hasSeq(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestTranscodeArgs(t *testing.T) {
	p, _ := PlanFor(SourceInfo{Width: 1080, Height: 1920, FPSNum: 60, FPSDen: 1, HasAudio: true, AudioChannels: 2}, DefaultLadder)
	args := transcodeArgs("/w/source", "/w/out", p, "veryfast")

	for _, seq := range [][]string{
		{"-protocol_whitelist", "file", "-format_whitelist", allowedFormats, "-i", "/w/source"}, // 输入只允许本地文件与常见容器
		{"-progress", "pipe:1"},
		{"-filter_complex", "[0:v:0]fps=60/2,split=4[s0][s1][s2][s3];" +
			"[s0]scale=360:640:flags=lanczos,setsar=1,format=yuv420p[o0];[s1]scale=480:854:flags=lanczos,setsar=1,format=yuv420p[o1];" +
			"[s2]scale=720:1280:flags=lanczos,setsar=1,format=yuv420p[o2];[s3]scale=1080:1920:flags=lanczos,setsar=1,format=yuv420p[o3]"},
		{"-map", "[o0]", "-map", "0:a:0", "-map_metadata", "-1"},
		{"-maxrate", "400000", "-bufsize", "400000"},
		{"-maxrate", "850000", "-bufsize", "850000"},
		{"-maxrate", "1600000", "-bufsize", "1600000"},
		{"-maxrate", "3000000", "-bufsize", "3000000"},
		{"-force_key_frames", "expr:gte(t,n_forced*4)", "-sc_threshold", "0", "-flags", "+cgop", "-x264-params", "repeat-headers=1"},
		{"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "96000", "-ac", "2"},
		{"-hls_time", "4", "-hls_playlist_type", "vod", "-hls_segment_type", "mpegts"},
		{"-hls_segment_filename", "/w/out/v720/seg_%05d.ts", "/w/out/v720/index.m3u8"},
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("命令中缺少 %q\n%q", seq, args)
		}
	}
	if n := strings.Count(strings.Join(args, " "), "-f hls"); n != 4 {
		t.Errorf("每档一个 HLS 输出，得到 %d 个", n)
	}
	if slices.Contains(args, "-hls_key_info_file") || slices.Contains(args, "-hls_enc") {
		t.Error("FFmpeg 不应接触内容密钥，加密在 Go 中完成")
	}
	if slices.Contains(args, "-hls_fmp4_init_filename") || slices.Contains(args, "fmp4") {
		t.Errorf("MPEG-TS profile 不应混入 fMP4 参数：%q", args)
	}

	// 没有音轨时生成本地静音 AAC，仍输出标准 H.264 + AAC HLS。
	small, _ := PlanFor(SourceInfo{Width: 270, Height: 480, FPSNum: 30, FPSDen: 1}, DefaultLadder)
	args = transcodeArgs("/w/source", "/w/out", small, "medium")
	if !hasSeq(args, "-filter_complex", "[0:v:0]scale=270:480:flags=lanczos,setsar=1,format=yuv420p[o0]") {
		t.Errorf("单档不应 split 或改帧率：%q", args)
	}
	if !hasSeq(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000") ||
		!hasSeq(args, "-map", "1:a:0") || !hasSeq(args, "-shortest") {
		t.Errorf("无音轨源应补本地静音 AAC：%q", args)
	}
	if !hasSeq(args, "-preset", "medium") {
		t.Error("应使用配置的 x264 速度档")
	}
}

func TestPosterArgs(t *testing.T) {
	args := posterArgs("/w/source", "/w/poster.webp", Rendition{Width: 720, Height: 1280})
	for _, seq := range [][]string{
		{"-protocol_whitelist", "file", "-format_whitelist", allowedFormats, "-i", "/w/source"},
		{"-frames:v", "1", "-map_metadata", "-1"},
		{"-vf", "scale=720:1280:flags=lanczos,setsar=1"},
		{"-c:v", "libwebp", "-quality", "80", "-f", "webp", "/w/poster.webp"},
	} {
		if !hasSeq(args, seq...) {
			t.Errorf("命令中缺少 %q\n%q", seq, args)
		}
	}
}

func TestProgressParser(t *testing.T) {
	var got []float64
	parse := progressParser(10, func(f float64) { got = append(got, f) })
	for _, line := range []string{"frame=1", "out_time_us=N/A", "out_time_us=2500000", "out_time_us=12000000", "progress=end"} {
		parse(line)
	}
	if !slices.Equal(got, []float64{0.25, 1}) {
		t.Fatalf("得到 %v", got)
	}
}

func TestProcessErrorAndTail(t *testing.T) {
	tb := &tailBuffer{max: 8}
	_, _ = tb.Write([]byte("0123456789"))
	_, _ = tb.Write([]byte("ab"))
	if tb.String() != "456789ab" {
		t.Fatalf("只保留末尾 8 字节，得到 %q", tb.String())
	}

	e := &ProcessError{Tool: "ffmpeg", ExitCode: 1, Stderr: "第一行\nsource: Invalid data found when processing input\n"}
	if e.Error() != "ffmpeg 退出码 1：source: Invalid data found when processing input" {
		t.Fatalf("只显示最后一行：%q", e.Error())
	}
	if got := scrub("/tmp/x/transcode-1/source: bad", "/tmp/x/transcode-1"); got != "source: bad" {
		t.Fatalf("应去掉临时目录：%q", got)
	}
}

func TestRun_ReportsExitCodeAndScrubsWorkDir(t *testing.T) {
	dir := t.TempDir()
	err := run(context.Background(), "sh", []string{"-c", "echo progress=1; echo " + dir + "/source: broken >&2; exit 3"}, dir, nil)
	var pe *ProcessError
	if !errors.As(err, &pe) || pe.ExitCode != 3 || pe.Error() != "sh 退出码 3：source: broken" {
		t.Fatalf("得到 %v", err)
	}

	var lines []string
	if err := run(context.Background(), "sh", []string{"-c", "echo a; echo b"}, "", func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lines, []string{"a", "b"}) {
		t.Fatalf("应逐行回调标准输出：%v", lines)
	}

	// 子进程拿不到 worker 的环境变量（数据库与存储凭据）
	t.Setenv("S3_SECRET_KEY", "must-not-leak")
	var env []string
	_ = run(context.Background(), "sh", []string{"-c", "env"}, "", func(l string) { env = append(env, l) })
	if slices.ContainsFunc(env, func(l string) bool { return strings.Contains(l, "must-not-leak") }) {
		t.Fatal("FFmpeg 子进程不应继承凭据类环境变量")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, "sleep", []string{"5"}, "", nil); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx 取消后应中止，得到 %v", err)
	}
}
