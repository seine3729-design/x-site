package resourcecheck

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

const (
	// MaxRows 是列表最多返回多少条（docs/03 第 9.4 节「最多展示 200 条」）。
	MaxRows = 200
	// defaultRows 是不传条数时的默认值。
	defaultRows = 200
	// maxQueryRunes 是搜索词上限。
	maxQueryRunes = 60
	// probeConcurrency 是一部剧内部同时探测几个资源。
	//
	// 限并发是为了不压垮源站（docs/03 第 9.4 节）。River 没有「按存储源分组限并发」的能力，
	// 所以这里按任务内限并发 + 队列并发度限制同时跑几部剧，两者相乘就是对源站的实际压力。
	probeConcurrency = 4
)

// Repo 读写体检数据。
type Repo interface {
	ListChecks(ctx context.Context, f Filter) ([]Row, error)
	CountChecks(ctx context.Context, f Filter) (int64, error)
	Summary(ctx context.Context) (Summary, error)
	UpsertCheck(ctx context.Context, o Outcome) error
	// ClearChecks 清空结果，dramaID 为 0 时清全部。只清体检表，不动源文件。
	ClearChecks(ctx context.Context, dramaID int64) (int64, error)
	// DramaAssets 取一部剧的全部待检资源。
	DramaAssets(ctx context.Context, dramaID int64) (DramaAssets, error)
	// DramaIDsForCheck 按范围取待体检的剧编号。
	DramaIDsForCheck(ctx context.Context, includeUnpublished bool) ([]int64, error)
}

// Lines 按资源类型取线路（domain/storage.Repo）。
type Lines interface {
	ListLines(ctx context.Context, q storage.LineQuery) ([]storage.Line, error)
}

// Signer 把对象键转成可访问地址（domain/storage.SignService）。
type Signer interface {
	SignFor(ctx context.Context, line storage.Line, target storage.SignTarget, ttl time.Duration, now time.Time) (storage.SignedURL, error)
}

// Prober 探测一个地址能不能打开（实现在 store/blob/s3probe，domain 不碰 HTTP 细节）。
type Prober interface {
	ProbeAsset(ctx context.Context, req ProbeRequest) ProbeResult
}

// Enqueuer 把一部剧的体检任务入队（实现在 internal/jobs）。
type Enqueuer interface {
	EnqueueResourceCheck(ctx context.Context, dramaID int64) error
}

// Service 是资源体检。
type Service struct {
	repo   Repo
	lines  Lines
	signer Signer
	prober Prober
	queue  Enqueuer
	now    func() time.Time
}

func NewService(repo Repo, lines Lines, signer Signer, prober Prober, queue Enqueuer) *Service {
	return &Service{repo: repo, lines: lines, signer: signer, prober: prober, queue: queue, now: time.Now}
}

// Overview 是列表接口的完整响应。
type Overview struct {
	List    []Row        `json:"list"`
	Total   int64        `json:"total"`
	Shown   int          `json:"shown"`
	Limit   int          `json:"limit"`
	Summary Summary      `json:"summary"`
	Results []ResultInfo `json:"results"`
	// LineWarning 非空时说明有一类资源没有可用线路、体检会跳过它们。
	LineWarning string `json:"line_warning"`
}

// List 返回体检结果。
//
// 刻意不做分页：体检是「扫一遍找出坏的」的工具，运营的用法是筛「只看打不开」然后逐条去修，
// 翻页到第 7 页看第 140 条没有意义。超过上限时用 total 告诉运营还有多少没显示，让他缩小范围。
func (s *Service) List(ctx context.Context, f Filter) (Overview, error) {
	clean, err := cleanFilter(f)
	if err != nil {
		return Overview{}, err
	}
	list, err := s.repo.ListChecks(ctx, clean)
	if err != nil {
		return Overview{}, err
	}
	total, err := s.repo.CountChecks(ctx, clean)
	if err != nil {
		return Overview{}, err
	}
	sum, err := s.repo.Summary(ctx)
	if err != nil {
		return Overview{}, err
	}
	for i := range list {
		list[i].FieldTitle = FieldTitle(list[i].Field)
		list[i].ResultTitle = ResultTitle(list[i].Result)
	}
	warn, err := s.lineWarning(ctx)
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		List: list, Total: total, Shown: len(list), Limit: clean.Limit,
		Summary: sum, Results: Results(), LineWarning: warn,
	}, nil
}

// Clear 清空结果。只清体检表，源文件一个都不会动。
func (s *Service) Clear(ctx context.Context, dramaID int64) (int64, error) {
	if dramaID < 0 {
		return 0, badRequest("剧编号不正确")
	}
	return s.repo.ClearChecks(ctx, dramaID)
}

// Run 发起体检：按范围取剧，逐部入队。
//
// 不等结果就返回：一部剧几十集、一集好几个资源，全站体检是分钟级的事情，
// 让后台页面轮询结果比把 HTTP 请求挂在那里等更合适（docs/03 第 9.4 节「按剧分片」）。
func (s *Service) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.DramaID < 0 {
		return RunResult{}, badRequest("剧编号不正确")
	}
	var ids []int64
	if req.DramaID > 0 {
		// 确认这部剧存在，否则运营填错编号会得到「已入队 1 部」的假成功
		if _, err := s.repo.DramaAssets(ctx, req.DramaID); err != nil {
			return RunResult{}, err
		}
		ids = []int64{req.DramaID}
	} else {
		got, err := s.repo.DramaIDsForCheck(ctx, req.IncludeUnpublished)
		if err != nil {
			return RunResult{}, err
		}
		ids = got
	}
	if len(ids) == 0 {
		return RunResult{}, badRequest("没有符合范围的剧集：当前没有已上架的剧，可以勾选「含未上架」再试")
	}
	queued := 0
	for _, id := range ids {
		if err := s.queue.EnqueueResourceCheck(ctx, id); err != nil {
			return RunResult{}, fmt.Errorf("入队资源体检（剧 %d）: %w", id, err)
		}
		queued++
	}
	return RunResult{Dramas: len(ids), Queued: queued}, nil
}

// CheckDrama 体检一部剧：组装待检清单、并发探测、写结果。由 worker 调用。
func (s *Service) CheckDrama(ctx context.Context, dramaID int64) (int, error) {
	assets, err := s.repo.DramaAssets(ctx, dramaID)
	if err != nil {
		return 0, err
	}
	video, err := s.pickLine(ctx, storage.LineVideo)
	if err != nil {
		return 0, err
	}
	image, err := s.pickLine(ctx, storage.LineImage)
	if err != nil {
		return 0, err
	}
	targets := Targets(assets)
	if len(targets) == 0 {
		return 0, nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(probeConcurrency)
	done := 0
	outcomes := make([]*Outcome, len(targets))
	for i, t := range targets {
		i, t := i, t
		g.Go(func() error {
			lo := image
			if t.FileType == TypeVideo {
				lo = video
			}
			o, skip := s.probeTarget(gctx, t, lo)
			if skip {
				return nil
			}
			outcomes[i] = &o
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return 0, err
	}
	// 串行写库：并发 UPSERT 同一张表只会互相等锁，没有收益
	for _, o := range outcomes {
		if o == nil {
			continue
		}
		if err := s.repo.UpsertCheck(ctx, *o); err != nil {
			return done, fmt.Errorf("写体检结果（剧 %d 字段 %s）: %w", dramaID, o.Field, err)
		}
		done++
	}
	return done, nil
}

// probeTarget 探测一个资源。skip 为真表示这个资源这次检不了（没有可用线路），不写结果。
func (s *Service) probeTarget(ctx context.Context, t Target, lo lineOf) (Outcome, bool) {
	target := t.AbsoluteURL
	var lineID int64
	if target == "" {
		if !lo.usable {
			// 没有对应 kind 的线路。不记成「连不上」：那会把「后台还没配线路」误报成「资源丢了」，
			// 运营会去改路径，而真正要做的是去配线路。缺线路在列表接口用 line_warning 单独提示。
			return Outcome{}, true
		}
		signed, err := s.signer.SignFor(ctx, lo.line, storage.SignTarget{Key: t.Key, Prefix: t.Prefix}, 0, s.now())
		if err != nil {
			return Outcome{
				Target: t, URL: "", LineID: lo.line.ID, Result: ResultUnopenable,
				Message: "生成访问地址失败：" + errs.From(err).Message,
			}, false
		}
		target = signed.URL
		lineID = lo.line.ID
	}
	res := s.prober.ProbeAsset(ctx, ProbeRequest{URL: target})
	return Outcome{
		Target:     t,
		URL:        stripQuery(target),
		LineID:     lineID,
		Result:     ResultOf(res),
		StatusCode: int32(res.HTTPStatus),
		ElapsedMs:  int32(res.ElapsedMs),
		Message:    res.Message,
	}, false
}

// pickLine 取某类资源的首选线路：启用中、配了访问域名、priority 最小的那条。
//
// 不走选线算法（domain/storage.Evaluate）：那是按用户地理位置给单个播放请求择优，
// 体检要的是一个稳定可重复的参照 —— 同一个资源每次体检都走同一条线路，结果才有可比性。
func (s *Service) pickLine(ctx context.Context, kind storage.LineKind) (lineOf, error) {
	lines, err := s.lines.ListLines(ctx, storage.LineQuery{Kind: kind})
	if err != nil {
		return lineOf{}, err
	}
	var best storage.Line
	found := false
	for _, l := range lines {
		if l.Status == storage.LineDisabled || l.BaseURL == "" {
			continue
		}
		if !found || l.Priority < best.Priority || (l.Priority == best.Priority && l.ID < best.ID) {
			best, found = l, true
		}
	}
	return lineOf{line: best, usable: found}, nil
}

// lineWarning 检查两类线路是否齐全，缺的在页面上提示。
func (s *Service) lineWarning(ctx context.Context) (string, error) {
	video, err := s.pickLine(ctx, storage.LineVideo)
	if err != nil {
		return "", err
	}
	image, err := s.pickLine(ctx, storage.LineImage)
	if err != nil {
		return "", err
	}
	switch {
	case !video.usable && !image.usable:
		return "还没有配置任何启用中的线路，体检没有可用的访问地址，视频与封面都会被跳过。请先到「线路」页添加。", nil
	case !video.usable:
		return "没有启用中的视频线路，视频产物会被跳过，只体检封面。", nil
	case !image.usable:
		return "没有启用中的图片线路，封面与首帧图会被跳过，只体检视频。", nil
	}
	return "", nil
}

// Targets 把一部剧的资源摊成待检清单。空字段跳过（没填不算故障）。
func Targets(a DramaAssets) []Target {
	var out []Target
	add := func(epID int64, ft FileType, f Field, raw string) {
		key, abs := normalizeAsset(raw)
		if key == "" && abs == "" {
			return
		}
		out = append(out, Target{
			DramaID: a.DramaID, EpisodeID: epID, FileType: ft, Field: f, Key: key, AbsoluteURL: abs,
		})
	}
	add(0, TypeCover, FieldCoverPortrait, a.CoverPortrait)
	add(0, TypeCover, FieldCoverLandscape, a.CoverLandscape)
	for _, e := range a.Episodes {
		add(e.ID, TypeCover, FieldEpisodeCover, e.Cover)
		add(e.ID, TypeCover, FieldEpisodePoster, e.Poster)
		if e.ObjectKey == "" {
			// 还没转封装或转失败：没有产物可检。这不是资源故障，分集本身的 media_status 已经说明了问题
			continue
		}
		out = append(out, videoTarget(a.DramaID, e))
	}
	return out
}

// videoTarget 构造一集视频产物的待检目标。
//
// hls_dir 的 key 是目录，探主清单 master.m3u8：它是播放的入口，拿不到就等于整集播不了；
// 同时把目录作为签名前缀，这样一个令牌覆盖整集（和播放时的签名方式一致，体检才检得准）。
func videoTarget(dramaID int64, e EpisodeAssets) Target {
	t := Target{
		DramaID: dramaID, EpisodeID: e.ID, FileType: TypeVideo, Field: FieldVideo, Key: e.ObjectKey,
	}
	if e.ObjectKind == "hls_dir" {
		dir := strings.TrimRight(e.ObjectKey, "/")
		t.Key = dir + "/master.m3u8"
		t.Prefix = dir + "/"
	}
	return t
}

// normalizeAsset 归一封面值。
//
// 封面是运营手填的文本（docs/02 第 2.1 节「资源机路径」），后端从来没校验过格式，
// 所以实际可能是 "covers/a.webp"、"/covers/a.webp" 或 "https://cdn.example.com/a.webp"。
// 返回 (对象键, 完整网址)，两者必有一个为空。
func normalizeAsset(raw string) (key, absolute string) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ""
	}
	if strings.HasPrefix(v, "//") {
		// 省略协议的网址。它不是对象键，但也无从判断该用 http 还是 https，当没填
		return "", ""
	}
	// 用 url.Parse 认协议，不要只找 "://"：data:、mailto: 这类没有双斜杠，
	// 漏掉就会把 "data:image/png;base64,..." 整串当成对象键拼到线路域名后面去探
	if u, err := url.Parse(v); err == nil && u.Scheme != "" {
		if (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return "", v
		}
		// 其他协议（data:、ftp:、file:…）探不了，当没填
		return "", ""
	}
	return strings.TrimPrefix(v, "/"), ""
}

// ResultOf 把一次探测映射成体检结果（docs/02 第 4.3 节的六个枚举值）。
func ResultOf(r ProbeResult) Result {
	if r.Unreachable {
		return ResultUnreachable
	}
	switch s := r.HTTPStatus; {
	case s >= 200 && s < 400:
		return ResultOK
	case s == 404 || s == 410:
		// 410 Gone 和 404 对运营是同一件事：这个地址上没有东西
		return ResultNotFound
	case s == 401 || s == 403:
		return ResultForbidden
	case s >= 500:
		return ResultServerError
	default:
		// 其他 4xx（400、405、416、429…）与 0：连上了但没拿到内容
		return ResultUnopenable
	}
}

// stripQuery 去掉查询串与片段。
//
// HMAC 令牌与 S3 预签名都把密钥派生值和过期时间放在查询串里，入库就等于把签名材料留在表里、
// 还会显示在后台页面上。只留地址本身，运营要看的「是哪个文件」也在地址里。
func stripQuery(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

func cleanFilter(f Filter) (Filter, error) {
	out := Filter{DramaID: f.DramaID, BadOnly: f.BadOnly}
	if f.DramaID < 0 {
		return Filter{}, badRequest("剧编号不正确")
	}
	word := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, f.Q))
	if utf8.RuneCountInString(word) > maxQueryRunes {
		return Filter{}, badRequest("搜索词不能超过 %d 个字", maxQueryRunes)
	}
	out.Q = word
	if word != "" {
		out.Pattern = "%" + likeEscaper.Replace(word) + "%"
	}
	switch n := f.Limit; {
	case n == 0:
		out.Limit = defaultRows
	case n < 1 || n > MaxRows:
		return Filter{}, badRequest("条数只能是 1–%d", MaxRows)
	default:
		out.Limit = n
	}
	return out, nil
}

// likeEscaper 转义 LIKE 的通配符，否则搜「%」会匹配全部。
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

// LogSkipped 记录被跳过的资源数量，供 worker 打日志。
func LogSkipped(ctx context.Context, dramaID int64, total, written int) {
	if skipped := total - written; skipped > 0 {
		slog.InfoContext(ctx, "资源体检跳过了部分资源（缺对应线路）",
			"drama_id", dramaID, "total", total, "written", written, "skipped", skipped)
	}
}

// RecordWarmFailure 把资源预热拿不到的地址记进体检结果（resourcewarm.CheckRecorder）。
//
// 复用体检表而不是另立一张：对运营来说「这个地址拿不到」就是一件事，不该因为是预热发现的
// 还是体检发现的就跑两个页面去看。预热失败的记录与体检的结果在同一个唯一键上 UPSERT，
// 所以同一集的视频只会有一行，谁后发现就是谁的结论。
func (s *Service) RecordWarmFailure(ctx context.Context, dramaID, episodeID int64, url string, lineID int64, res WarmFailure) error {
	o := Outcome{
		Target: Target{
			DramaID: dramaID, EpisodeID: episodeID, FileType: TypeVideo, Field: FieldVideo,
		},
		URL: stripQuery(url), LineID: lineID,
		Result:     ResultOf(ProbeResult{HTTPStatus: res.HTTPStatus, Unreachable: res.Unreachable}),
		StatusCode: int32(res.HTTPStatus),
		Message:    res.Message,
	}
	return s.repo.UpsertCheck(ctx, o)
}

// WarmFailure 是预热失败的信息（与 resourcewarm.FetchResult 对应的子集，避免两个 domain 包互相导入）。
type WarmFailure struct {
	HTTPStatus  int
	Unreachable bool
	Message     string
}
