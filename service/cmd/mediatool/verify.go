package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/transcode"
)

// runVerify 校验一集当前的转封装产物：下载主清单、各档清单与切片，检查切片按内容哈希命名、每个切片都有 EXT-X-KEY，
// 用 media_key 中的内容密钥（KEK 解开）让 FFmpeg 解密并完整解码每一档，再确认首帧图存在。密钥只写进临时目录，结束即删除。
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	episodeID := fs.Int64("episode", 0, "分集 ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *episodeID <= 0 {
		return errors.New(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	cur, err := pg.NewMediaRepo(pool, nil).CurrentMedia(ctx, *episodeID)
	if err != nil {
		return fmt.Errorf("第 %d 集没有可用的产物: %w", *episodeID, err)
	}
	keys, err := mediakey.ParseKeyring(cfg.Media.KEKs)
	if err != nil {
		return fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	key, err := keys.Unwrap(cur.EpisodeID, cur.KeyVer, cur.WrappedKey)
	if err != nil {
		return fmt.Errorf("解开内容密钥: %w", err)
	}
	m := cfg.Media
	client, err := blob.New(blob.Config{Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle})
	if err != nil {
		return err
	}
	media := client.Bucket(m.BucketMedia)

	dir, err := os.MkdirTemp("", "verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	objs, err := media.List(ctx, cur.ObjectKey+"/")
	if err != nil {
		return err
	}
	var total int64
	for _, o := range objs {
		rel := strings.TrimPrefix(o.Key, cur.ObjectKey+"/")
		data, err := download(ctx, media, o.Key)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if strings.HasPrefix(rel, "s/") {
			sum := sha256.Sum256(data)
			if name := strings.TrimSuffix(path.Base(rel), path.Ext(rel)); name != hex.EncodeToString(sum[:])[:32] {
				return fmt.Errorf("%s 没有按内容哈希命名", rel)
			}
		}
		if strings.HasSuffix(rel, ".m3u8") {
			data = bytes.ReplaceAll(data, []byte(transcode.KeyURI(cur.KeyVer)), []byte("key.bin"))
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "key.bin"), key, 0o600); err != nil {
		return err
	}
	clear(key)

	master, err := os.ReadFile(filepath.Join(dir, "master.m3u8"))
	if err != nil {
		return fmt.Errorf("缺少主清单: %w", err)
	}
	fmt.Printf("第 %d 集  状态 %s  密钥版本 %d  产物 %s（%d 个文件，%.1f MB）\n",
		cur.EpisodeID, cur.MediaStatus, cur.KeyVer, cur.ObjectKey, len(objs), float64(total)/1e6)
	var inf string
	sc := bufio.NewScanner(bytes.NewReader(master))
	variants := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			inf = strings.TrimPrefix(line, "#EXT-X-STREAM-INF:")
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		variants++
		text, err := os.ReadFile(filepath.Join(dir, line))
		if err != nil {
			return fmt.Errorf("缺少 %s: %w", line, err)
		}
		segs, keysN := strings.Count(string(text), "#EXTINF:"), strings.Count(string(text), "#EXT-X-KEY:METHOD=AES-128")
		if segs == 0 || segs != keysN {
			return fmt.Errorf("%s 有 %d 个切片、%d 个 EXT-X-KEY", line, segs, keysN)
		}
		frames, dur, err := decode(ctx, filepath.Join(dir, line))
		if err != nil {
			return fmt.Errorf("解码 %s: %w", line, err)
		}
		fmt.Printf("  %-11s %3d 片  解码 %5d 帧 %6.2fs  %s\n", line, segs, frames, dur, inf)
	}
	if variants == 0 {
		return errors.New("主清单没有任何一档")
	}
	poster, err := client.Bucket(m.BucketAssets).Head(ctx, cur.Poster)
	if err != nil {
		return fmt.Errorf("首帧图 %s: %w", cur.Poster, err)
	}
	fmt.Printf("  首帧图 %s（%s，%d 字节）\n验证通过\n", cur.Poster, poster.ContentType, poster.Size)
	return nil
}

func download(ctx context.Context, b *blob.Bucket, key string) ([]byte, error) {
	body, _, err := b.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

// decode 让 FFmpeg 读取本地化后的加密 HLS，返回视频帧数与时长。
func decode(ctx context.Context, playlist string) (int, float64, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-allowed_extensions", "ALL", "-protocol_whitelist", "file,crypto,data",
		"-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames:format=duration", "-of", "default=nw=1:nk=1",
		playlist).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("%w：%s", err, out)
	}
	var frames int
	var dur float64
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("ffprobe 输出不完整：%q", out)
	}
	if _, err := fmt.Sscan(fields[0], &frames); err != nil {
		return 0, 0, fmt.Errorf("读不到帧数：%q", out)
	}
	_, _ = fmt.Sscan(fields[len(fields)-1], &dur)
	return frames, dur, nil
}
