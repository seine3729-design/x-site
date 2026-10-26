// Package transcode 把一集的源视频转成加密的 MPEG-TS HLS（S1-5b，docs/specs/S1-5b-MPEG-TS-HLS.md、docs/03 第 9.6 节）：
// 下载源文件并计算哈希 → ffprobe 探测 → 选码率阶梯 → 一次 FFmpeg 解码、多路 H.264/AAC 编码 → 首帧 WebP →
// 逐片 AES-128 加密并按内容哈希命名 → 上传 → 生成各档清单与主清单。
//
// 本包只负责产物；任务状态、密钥入库、回写分集由 internal/jobs 与 internal/domain/media 完成。
package transcode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"xsite/service/internal/store/blob"
)

// ObjectStore 是产物写入的存储（blob.Bucket 实现）。
type ObjectStore interface {
	PutBytes(ctx context.Context, key string, data []byte, opt blob.PutOptions) error
	PutFile(ctx context.Context, key, path string, opt blob.PutOptions) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// SourceStore 是源文件所在的存储（blob.Bucket 实现）。
type SourceStore interface {
	Head(ctx context.Context, key string) (blob.ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
}

// 产物的响应头：MPEG-TS 切片按内容哈希命名，可以永久缓存；清单是 S1-8 改写前的模板，不缓存。
const immutable = "public, max-age=31536000, immutable"

var (
	optSegment  = blob.PutOptions{ContentType: "video/mp2t", CacheControl: immutable}
	optPlaylist = blob.PutOptions{ContentType: "application/vnd.apple.mpegurl", CacheControl: "no-cache"}
	optPoster   = blob.PutOptions{ContentType: "image/webp", CacheControl: immutable}
)

const (
	uploadLimit  = 8  // 并发上传数
	hashNameSize = 32 // 按内容命名的文件名长度（十六进制）
)

// Pipeline 是转封装流水线。字段在创建后只读，可被多个任务并发使用。
type Pipeline struct {
	FFmpeg, FFprobe string
	Preset          string // x264 速度档
	WorkDir         string // 临时目录的上级目录，为空用系统临时目录
	Source          SourceStore
	Media           ObjectStore // HLS 产物
	Images          ObjectStore // 首帧图
	Ladder          []Rung      // 为空用 DefaultLadder
}

// OutputPrefix 是一集任务的受保护 HLS 产物目录（媒体桶内，不带结尾的 /）。目录按任务区分：
// 重新转封装写新目录，旧产物在切换前一直可播，切片地址也不会与旧内容混在一起。
func OutputPrefix(episodeID int64, jobID string) string {
	return fmt.Sprintf("video/episodes/%d/%s", episodeID, jobID)
}

// AssetOutputPrefix 是非分集视频（如广告）的公开 HLS 目录。它与分集 HLS 使用不同前缀，
// 不会进入带内容密钥的播放接口。
func AssetOutputPrefix(id string) string {
	return fmt.Sprintf("video/assets/%s", id)
}

// Workspace 是一次尝试的临时目录与已下载的源文件；用完调用 Close 删除。
type Workspace struct {
	dir        string
	SourcePath string
	SHA256     string // 源文件 SHA-256（十六进制），转封装幂等键
	Size       int64
}

// Close 删除临时目录。
func (w *Workspace) Close() error { return os.RemoveAll(w.dir) }

// Fetch 建临时目录并下载源文件，同时计算 SHA-256。源文件不存在或超过大小上限时返回 Permanent 错误。
func (p *Pipeline) Fetch(ctx context.Context, jobID, sourceKey string) (*Workspace, error) {
	info, err := p.Source.Head(ctx, sourceKey)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, Permanent(fmt.Errorf("源文件 %s 不存在", sourceKey))
	}
	if err != nil {
		return nil, fmt.Errorf("查询源文件: %w", err)
	}
	if info.Size > maxSourceBytes {
		return nil, Permanent(fmt.Errorf("源文件 %d 字节，超过上限 %d 字节", info.Size, maxSourceBytes))
	}
	if info.Size == 0 {
		return nil, Permanent(fmt.Errorf("源文件 %s 为空", sourceKey))
	}

	root := p.WorkDir
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("创建临时目录: %w", err)
	}
	dir, err := os.MkdirTemp(root, "transcode-"+jobID+"-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录: %w", err)
	}
	ws := &Workspace{dir: dir, SourcePath: filepath.Join(dir, "source")}
	if err := ws.download(ctx, p.Source, sourceKey); err != nil {
		_ = ws.Close()
		return nil, err
	}
	return ws, nil
}

func (w *Workspace) download(ctx context.Context, src SourceStore, key string) error {
	body, _, err := src.Get(ctx, key)
	if errors.Is(err, blob.ErrNotFound) {
		return Permanent(fmt.Errorf("源文件 %s 不存在", key))
	}
	if err != nil {
		return fmt.Errorf("下载源文件: %w", err)
	}
	defer body.Close()
	f, err := os.OpenFile(w.SourcePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("写入源文件: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, maxSourceBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("下载源文件: %w", err)
	}
	if n > maxSourceBytes {
		return Permanent(fmt.Errorf("源文件超过上限 %d 字节", maxSourceBytes))
	}
	w.SHA256, w.Size = hex.EncodeToString(h.Sum(nil)), n
	return nil
}

// Request 是一次打包的参数。分集视频由调用方生成内容密钥；Public 为 true 时生成不含内容密钥的公开 HLS。
// Prefix 留空时按 EpisodeID 与 JobID 生成分集目录，非分集视频显式传 AssetOutputPrefix。
type Request struct {
	JobID     string
	EpisodeID int64
	KeyVer    int32
	Key       []byte
	Prefix    string
	Public    bool
}

// RenditionInfo 是产出的一档（写入 episode.renditions）。
type RenditionInfo struct {
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bitrate     int64  `json:"bitrate"`      // 平均码率（bps，含音频）
	PeakBitrate int64  `json:"peak_bitrate"` // 峰值码率（主清单 BANDWIDTH）
	Codecs      string `json:"codecs"`
}

// Result 是打包结果。
type Result struct {
	Prefix       string // 媒体桶中的产物目录（media_object.key）
	MasterSHA256 string
	SizeBytes    int64 // 上传的产物总字节数
	Renditions   []RenditionInfo
	Poster       string  // 图片桶中的首帧图 key
	Duration     float64 // 源时长（秒）
}

// Package 探测、转码并上传，progress 接收 0–99 的进度（可能从多个 goroutine 调用）。
// 开始前先清空本任务的产物目录：重试不会留下上一次失败的半成品。
func (p *Pipeline) Package(ctx context.Context, ws *Workspace, req Request, progress func(int)) (Result, error) {
	if progress == nil {
		progress = func(int) {}
	}
	info, err := Probe(ctx, p.FFprobe, ws.SourcePath)
	if err != nil {
		return Result{}, err
	}
	ladder := p.Ladder
	if len(ladder) == 0 {
		ladder = DefaultLadder
	}
	plan, err := PlanFor(info, ladder)
	if err != nil {
		return Result{}, Permanent(err)
	}

	posterFile := filepath.Join(ws.dir, "poster.webp")
	if !req.Public {
		if err := run(ctx, p.FFmpeg, posterArgs(ws.SourcePath, posterFile, plan.Poster), ws.dir, nil); err != nil {
			return Result{}, fmt.Errorf("生成首帧图: %w", err)
		}
	}
	outDir := filepath.Join(ws.dir, "out")
	for _, r := range plan.Renditions {
		if err := os.MkdirAll(filepath.Join(outDir, r.Name), 0o700); err != nil {
			return Result{}, err
		}
	}
	// 转码占进度的 0–80，加密上传占 80–99
	onLine := progressParser(info.Duration, func(f float64) { progress(int(f * 80)) })
	if err := run(ctx, p.FFmpeg, transcodeArgs(ws.SourcePath, outDir, plan, p.Preset), ws.dir, onLine); err != nil {
		return Result{}, fmt.Errorf("转码: %w", err)
	}
	progress(80)

	prefix := req.Prefix
	if prefix == "" {
		prefix = OutputPrefix(req.EpisodeID, req.JobID)
	}
	if _, err := p.Media.DeletePrefix(ctx, prefix+"/"); err != nil {
		return Result{}, fmt.Errorf("清理上次的产物: %w", err)
	}
	up := &uploader{store: p.Media, prefix: prefix, key: req.Key, encrypt: !req.Public}
	keyURI := ""
	if !req.Public {
		keyURI = KeyURI(req.KeyVer)
	}
	playlists := make([]mediaPlaylist, len(plan.Renditions))
	for i, r := range plan.Renditions {
		data, err := os.ReadFile(filepath.Join(outDir, r.Name, "index.m3u8"))
		if err != nil {
			return Result{}, fmt.Errorf("读取 %s 清单: %w", r.Name, err)
		}
		if playlists[i], err = parseMediaPlaylist(data); err != nil {
			return Result{}, fmt.Errorf("%s 清单: %w", r.Name, err)
		}
		up.total += len(playlists[i].Segments)
	}
	up.progress = func(done int) { progress(80 + done*19/max(up.total, 1)) }

	variants := make([]variant, len(plan.Renditions))
	renditions := make([]RenditionInfo, len(plan.Renditions))
	for i, r := range plan.Renditions {
		v, err := up.rendition(ctx, filepath.Join(outDir, r.Name), r, playlists[i], keyURI, plan.AudioChannels > 0)
		if err != nil {
			return Result{}, fmt.Errorf("上传 %s: %w", r.Name, err)
		}
		v.FrameRate = plan.FPS(info)
		variants[i] = v
		renditions[i] = RenditionInfo{
			Name: r.Name, Width: r.Width, Height: r.Height, Bitrate: v.AverageBandwidth, PeakBitrate: v.Bandwidth, Codecs: v.Codecs,
		}
	}
	master := writeMasterPlaylist(variants)
	if err := up.put(ctx, "master.m3u8", master, optPlaylist); err != nil {
		return Result{}, err
	}

	posterKey := ""
	if !req.Public {
		posterData, err := os.ReadFile(posterFile)
		if err != nil {
			return Result{}, fmt.Errorf("读取首帧图: %w", err)
		}
		posterKey = fmt.Sprintf("img/posters/%s.webp", hashName(posterData))
		if err := p.Images.PutBytes(ctx, posterKey, posterData, optPoster); err != nil {
			return Result{}, fmt.Errorf("上传首帧图: %w", err)
		}
	}
	sum := sha256.Sum256(master)
	return Result{
		Prefix: prefix, MasterSHA256: hex.EncodeToString(sum[:]), SizeBytes: up.bytes.Load(),
		Renditions: renditions, Poster: posterKey, Duration: info.Duration,
	}, nil
}

// Discard 删除一个分集任务已上传的产物（失败后尽力清理）。
func (p *Pipeline) Discard(ctx context.Context, episodeID int64, jobID string) error {
	return p.DiscardPrefix(ctx, OutputPrefix(episodeID, jobID))
}

// DiscardPrefix 删除任意视频产物目录；调用方只能传服务端生成的前缀。
func (p *Pipeline) DiscardPrefix(ctx context.Context, prefix string) error {
	_, err := p.Media.DeletePrefix(ctx, prefix+"/")
	return err
}

// PublicError 是写进任务记录、显示在后台的错误信息：去掉临时目录路径。
func (p *Pipeline) PublicError(err error) string {
	msg := err.Error()
	if p.WorkDir != "" {
		msg = strings.ReplaceAll(msg, p.WorkDir, "")
	}
	return truncate(strings.ReplaceAll(msg, os.TempDir(), ""), 500)
}

// uploader 加密并上传一档的切片，统计字节数与进度。
type uploader struct {
	store    ObjectStore
	prefix   string
	key      []byte
	encrypt  bool
	total    int
	done     atomic.Int64
	bytes    atomic.Int64
	progress func(done int)
}

func (u *uploader) put(ctx context.Context, name string, data []byte, opt blob.PutOptions) error {
	if err := u.store.PutBytes(ctx, path.Join(u.prefix, name), data, opt); err != nil {
		return err
	}
	u.bytes.Add(int64(len(data)))
	return nil
}

// rendition 加密并上传一档 MPEG-TS 切片，写出该档的交付清单，返回主清单中的一档。
func (u *uploader) rendition(ctx context.Context, dir string, r Rendition, pl mediaPlaylist, keyURI string, hasAudio bool) (variant, error) {
	if len(pl.Segments) == 0 {
		return variant{}, fmt.Errorf("MPEG-TS 清单没有切片")
	}
	// TS 没有 fMP4 init 段。强制每个关键帧重复 SPS/PPS 后，从首片提取实际的 H.264 profile/level，
	// 避免把 x264 在不同 preset 下的真实 CODECS 写错。
	first, err := os.ReadFile(filepath.Join(dir, pl.Segments[0].URI))
	if err != nil {
		return variant{}, err
	}
	codecs, err := codecsFromMPEGTS(first, hasAudio)
	if err != nil {
		return variant{}, fmt.Errorf("读取 MPEG-TS 编码信息: %w", err)
	}

	segs := make([]outSegment, len(pl.Segments))
	sizes := make([]int64, len(pl.Segments))
	durs := make([]float64, len(pl.Segments))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(uploadLimit)
	for i, s := range pl.Segments {
		g.Go(func() error {
			plain, err := os.ReadFile(filepath.Join(dir, s.URI))
			if err != nil {
				return err
			}
			data := plain
			var iv [16]byte
			if u.encrypt {
				if _, err := rand.Read(iv[:]); err != nil {
					return err
				}
				data, err = encryptSegment(u.key, iv, plain)
				if err != nil {
					return err
				}
			}
			uri := "s/" + hashName(data) + ".ts"
			if err := u.put(gctx, uri, data, optSegment); err != nil {
				return err
			}
			// 每个 goroutine 只写自己的下标，不需要加锁。
			segs[i] = outSegment{Duration: s.Duration, URI: uri, IV: iv}
			sizes[i], durs[i] = int64(len(data)), s.Duration
			u.progress(int(u.done.Add(1)))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return variant{}, err
	}
	if err := u.put(ctx, r.Name+".m3u8", writeMediaPlaylist("", keyURI, segs), optPlaylist); err != nil {
		return variant{}, err
	}
	return variant{
		URI: r.Name + ".m3u8", Width: r.Width, Height: r.Height, Codecs: codecs,
		Bandwidth: peakBitrate(durs, sizes, SegmentSeconds), AverageBandwidth: averageBitrate(durs, sizes),
	}, nil
}

// hashName 是按内容命名的文件名：SHA-256 的前 32 位十六进制（128 位，不可推测）。
func hashName(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:hashNameSize]
}

// permanentError 标记不可恢复的错误（源文件不存在、不是视频、超出限制），任务不再重试。
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent 把错误标记为不可恢复。
func Permanent(err error) error { return permanentError{err} }

// IsPermanent 表示错误不可恢复，重试没有意义。
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}
