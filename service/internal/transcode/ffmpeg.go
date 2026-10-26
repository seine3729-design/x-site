package transcode

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 编码参数（docs/08 第 2.1 节）。
const (
	SegmentSeconds = 4      // 切片时长；关键帧每 4 秒强制一次，各档对齐
	audioBitrate   = 96_000 // AAC-LC
	videoCRF       = 23     // 质量目标，码率受各档上限约束（capped CRF）
	posterQuality  = 80     // WebP 质量
)

// transcodeArgs 生成一次解码、多路编码的命令：每档一个独立的 MPEG-TS HLS 输出，写到 outDir/<档位名>/。
// 输出不加密，加密与按内容哈希命名由 Go 完成：FFmpeg 不接触内容密钥。
func transcodeArgs(input, outDir string, p Plan, preset string) []string {
	args := []string{"-hide_banner", "-nostdin", "-y", "-v", "error", "-progress", "pipe:1", "-nostats"}
	args = append(args, inputGuard...)
	args = append(args, "-i", input)
	if p.GenerateSilentAudio {
		// 对没有音轨的源补本地生成的静音 AAC，保证新产物统一为 H.264 + AAC。
		args = append(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
	}

	var g strings.Builder
	g.WriteString("[0:v:0]")
	if p.FPSNum > 0 {
		fmt.Fprintf(&g, "fps=%d/%d,", p.FPSNum, p.FPSDen)
	}
	n := len(p.Renditions)
	if n > 1 {
		fmt.Fprintf(&g, "split=%d", n)
		for i := range n {
			fmt.Fprintf(&g, "[s%d]", i)
		}
		g.WriteString(";")
	}
	for i, r := range p.Renditions {
		if n > 1 {
			fmt.Fprintf(&g, "[s%d]", i)
		}
		fmt.Fprintf(&g, "scale=%d:%d:flags=lanczos,setsar=1,format=yuv420p[o%d]", r.Width, r.Height, i)
		if i < n-1 {
			g.WriteString(";")
		}
	}
	args = append(args, "-filter_complex", g.String())

	audioInput := "0:a:0"
	if p.GenerateSilentAudio {
		audioInput = "1:a:0"
	}
	keyframes := fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentSeconds)
	for i, r := range p.Renditions {
		dir := filepath.Join(outDir, r.Name)
		args = append(args, "-map", fmt.Sprintf("[o%d]", i))
		if p.AudioChannels > 0 {
			args = append(args, "-map", audioInput)
		}
		args = append(args,
			"-map_metadata", "-1", "-map_chapters", "-1", // 去掉源文件元数据（拍摄设备、位置等）
			"-c:v", "libx264", "-preset", preset, "-profile:v", "high",
			// VBV 缓冲 1 秒：限制开头的码率突发，4 秒起播切片仍可控（docs/08 第 2.3 节）。
			"-crf", strconv.Itoa(videoCRF), "-maxrate", strconv.FormatInt(r.MaxRate, 10), "-bufsize", strconv.FormatInt(r.MaxRate, 10),
			"-force_key_frames", keyframes, "-sc_threshold", "0", "-flags", "+cgop", "-x264-params", "repeat-headers=1",
		)
		if p.AudioChannels > 0 {
			args = append(args, "-c:a", "aac", "-profile:a", "aac_low", "-b:a", strconv.Itoa(audioBitrate), "-ac", strconv.Itoa(p.AudioChannels))
		}
		if p.GenerateSilentAudio {
			args = append(args, "-shortest")
		}
		args = append(args,
			"-f", "hls", "-hls_time", strconv.Itoa(SegmentSeconds), "-hls_playlist_type", "vod",
			"-hls_segment_type", "mpegts",
			"-hls_segment_filename", filepath.Join(dir, "seg_%05d.ts"),
			filepath.Join(dir, "index.m3u8"),
		)
	}
	return args
}

// posterArgs 生成首帧图（WebP）的命令。
func posterArgs(input, output string, r Rendition) []string {
	args := []string{"-hide_banner", "-nostdin", "-y", "-v", "error"}
	args = append(args, inputGuard...)
	return append(args, "-i", input,
		"-map", "0:v:0", "-frames:v", "1", "-map_metadata", "-1",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", r.Width, r.Height),
		"-c:v", "libwebp", "-quality", strconv.Itoa(posterQuality), "-f", "webp", output,
	)
}

// ProcessError 是 FFmpeg / ffprobe 非零退出，Stderr 为输出末尾（已去掉临时目录路径）。
type ProcessError struct {
	Tool     string
	ExitCode int
	Stderr   string
}

func (e *ProcessError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[i+1:]) // 只取最后一行，通常就是原因
	}
	if msg == "" {
		return fmt.Sprintf("%s 退出码 %d", e.Tool, e.ExitCode)
	}
	return fmt.Sprintf("%s 退出码 %d：%s", e.Tool, e.ExitCode, truncate(msg, 300))
}

// stderr 只保留末尾这么多字节。
const stderrTail = 8 << 10

// run 执行 FFmpeg；onLine 逐行接收标准输出（-progress pipe:1）。ctx 取消时结束进程。
// 子进程只拿到最小的环境变量：worker 的环境里有数据库与存储凭据，处理不可信文件的进程不应看到它们。
func run(ctx context.Context, bin string, args []string, workDir string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
	cmd.Dir = workDir
	cmd.WaitDelay = 5 * time.Second
	tail := &tailBuffer{max: stderrTail}
	cmd.Stderr = tail
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s: %w", filepath.Base(bin), err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if onLine != nil {
			onLine(sc.Text())
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s 被中止: %w", filepath.Base(bin), ctx.Err())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ProcessError{Tool: filepath.Base(bin), ExitCode: exit.ExitCode(), Stderr: scrub(tail.String(), workDir)}
	}
	return fmt.Errorf("%s: %w", filepath.Base(bin), err)
}

// CheckTools 确认 FFmpeg 与 ffprobe 可用且带转封装需要的编码器，返回 FFmpeg 版本行。worker 启用转封装时在启动阶段调用：
// 镜像里缺 FFmpeg 或编码器时启动失败，而不是等任务来了才逐个失败。
func CheckTools(ctx context.Context, ffmpeg, ffprobe string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := runCapture(ctx, ffprobe, []string{"-hide_banner", "-version"}, ""); err != nil {
		return "", fmt.Errorf("ffprobe 不可用（FFPROBE_PATH=%s）: %w", ffprobe, err)
	}
	version, err := runCapture(ctx, ffmpeg, []string{"-hide_banner", "-version"}, "")
	if err != nil {
		return "", fmt.Errorf("ffmpeg 不可用（FFMPEG_PATH=%s）: %w", ffmpeg, err)
	}
	encoders, err := runCapture(ctx, ffmpeg, []string{"-hide_banner", "-encoders"}, "")
	if err != nil {
		return "", fmt.Errorf("读取 ffmpeg 编码器: %w", err)
	}
	for _, enc := range []string{"libx264", "libwebp", "aac"} {
		if !bytes.Contains(encoders, []byte(" "+enc+" ")) {
			return "", fmt.Errorf("ffmpeg 缺少编码器 %s", enc)
		}
	}
	first, _, _ := strings.Cut(string(version), "\n")
	return strings.TrimSpace(first), nil
}

// runCapture 执行命令并返回标准输出（ffprobe）。
func runCapture(ctx context.Context, bin string, args []string, workDir string) ([]byte, error) {
	var out bytes.Buffer
	err := run(ctx, bin, args, workDir, func(line string) {
		out.WriteString(line)
		out.WriteByte('\n')
	})
	return out.Bytes(), err
}

// progressParser 把 -progress 输出中的 out_time_us 换算成进度（0–1）。
func progressParser(total float64, report func(float64)) func(string) {
	return func(line string) {
		v, ok := strings.CutPrefix(line, "out_time_us=")
		if !ok || total <= 0 {
			return
		}
		us, err := strconv.ParseInt(v, 10, 64)
		if err != nil || us < 0 {
			return // 开头几行可能是 N/A
		}
		report(min(float64(us)/1e6/total, 1))
	}
}

// tailBuffer 只保留写入内容的最后 max 字节。
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// scrub 去掉错误信息中的临时目录路径（错误会显示在后台）。
func scrub(s, workDir string) string {
	if workDir == "" {
		return s
	}
	return strings.ReplaceAll(s, workDir+string(filepath.Separator), "")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
