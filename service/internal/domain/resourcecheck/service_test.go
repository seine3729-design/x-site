package resourcecheck

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// ---------- 替身 ----------

type fakeRepo struct {
	mu       sync.Mutex
	assets   map[int64]DramaAssets
	ids      []int64
	written  []Outcome
	rows     []Row
	total    int64
	summary  Summary
	cleared  int64
	err      error
	assetErr error
}

func (f *fakeRepo) ListChecks(context.Context, Filter) ([]Row, error) { return f.rows, f.err }
func (f *fakeRepo) CountChecks(context.Context, Filter) (int64, error) {
	return f.total, f.err
}
func (f *fakeRepo) Summary(context.Context) (Summary, error) { return f.summary, f.err }
func (f *fakeRepo) UpsertCheck(_ context.Context, o Outcome) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, o)
	return f.err
}
func (f *fakeRepo) ClearChecks(_ context.Context, dramaID int64) (int64, error) {
	f.cleared = dramaID
	return 3, f.err
}
func (f *fakeRepo) DramaAssets(_ context.Context, id int64) (DramaAssets, error) {
	if f.assetErr != nil {
		return DramaAssets{}, f.assetErr
	}
	a, ok := f.assets[id]
	if !ok {
		return DramaAssets{}, errs.ErrNotFound.WithMessage("剧集不存在")
	}
	return a, nil
}
func (f *fakeRepo) DramaIDsForCheck(_ context.Context, includeUnpublished bool) ([]int64, error) {
	if includeUnpublished {
		return f.ids, f.err
	}
	// 只取已上架的：替身里约定编号 >= 100 的是未上架
	var out []int64
	for _, id := range f.ids {
		if id < 100 {
			out = append(out, id)
		}
	}
	return out, f.err
}

type fakeLines struct {
	lines []storage.Line
	err   error
}

func (f *fakeLines) ListLines(_ context.Context, q storage.LineQuery) ([]storage.Line, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []storage.Line
	for _, l := range f.lines {
		if q.Kind == "" || l.Kind == q.Kind {
			out = append(out, l)
		}
	}
	return out, nil
}

type fakeSigner struct {
	err    error
	called []storage.SignTarget
	mu     sync.Mutex
}

func (f *fakeSigner) SignFor(_ context.Context, line storage.Line, t storage.SignTarget, _ time.Duration, _ time.Time) (storage.SignedURL, error) {
	f.mu.Lock()
	f.called = append(f.called, t)
	f.mu.Unlock()
	if f.err != nil {
		return storage.SignedURL{}, f.err
	}
	// 模拟带签名的地址：查询串里是签名材料，入库时必须被去掉
	return storage.SignedURL{URL: line.BaseURL + "/" + t.Key + "?exp=123&s=SIGNATURE"}, nil
}

type fakeProber struct {
	mu      sync.Mutex
	byURL   map[string]ProbeResult
	visited []string
	deflt   ProbeResult
}

func (f *fakeProber) ProbeAsset(_ context.Context, req ProbeRequest) ProbeResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.visited = append(f.visited, req.URL)
	for frag, r := range f.byURL {
		if strings.Contains(req.URL, frag) {
			return r
		}
	}
	return f.deflt
}

type fakeQueue struct {
	ids []int64
	err error
}

func (f *fakeQueue) EnqueueResourceCheck(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.ids = append(f.ids, id)
	return nil
}

func lineOfKind(id int64, kind storage.LineKind, priority int32, status storage.LineStatus, baseURL string) storage.Line {
	return storage.Line{ID: id, Kind: kind, Priority: priority, Status: status, BaseURL: baseURL, Name: "线路" + string(kind)}
}

func newSvc(repo *fakeRepo, lines *fakeLines, signer *fakeSigner, prober *fakeProber, queue *fakeQueue) *Service {
	s := NewService(repo, lines, signer, prober, queue)
	s.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return s
}

// ---------- 结果映射 ----------

func TestResultOf(t *testing.T) {
	cases := []struct {
		name string
		in   ProbeResult
		want Result
	}{
		{"200 正常", ProbeResult{HTTPStatus: 200}, ResultOK},
		{"206 分段响应也是正常", ProbeResult{HTTPStatus: 206}, ResultOK},
		{"301 重定向算能打开", ProbeResult{HTTPStatus: 301}, ResultOK},
		{"399 边界仍算正常", ProbeResult{HTTPStatus: 399}, ResultOK},
		{"404 不存在", ProbeResult{HTTPStatus: 404}, ResultNotFound},
		{"410 与 404 同义", ProbeResult{HTTPStatus: 410}, ResultNotFound},
		{"401 拒绝访问", ProbeResult{HTTPStatus: 401}, ResultForbidden},
		{"403 拒绝访问", ProbeResult{HTTPStatus: 403}, ResultForbidden},
		{"500 服务器错", ProbeResult{HTTPStatus: 500}, ResultServerError},
		{"503 服务器错", ProbeResult{HTTPStatus: 503}, ResultServerError},
		{"400 打不开", ProbeResult{HTTPStatus: 400}, ResultUnopenable},
		{"405 打不开", ProbeResult{HTTPStatus: 405}, ResultUnopenable},
		{"416 打不开", ProbeResult{HTTPStatus: 416}, ResultUnopenable},
		{"429 打不开", ProbeResult{HTTPStatus: 429}, ResultUnopenable},
		{"地址无效（没发出请求）算打不开", ProbeResult{HTTPStatus: 0}, ResultUnopenable},
		{"传输层失败算连不上", ProbeResult{Unreachable: true}, ResultUnreachable},
		{"连不上优先于状态码", ProbeResult{Unreachable: true, HTTPStatus: 200}, ResultUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResultOf(c.in); got != c.want {
				t.Fatalf("ResultOf(%+v) = %s，想要 %s", c.in, got, c.want)
			}
		})
	}
}

func TestResultsAllDescribed(t *testing.T) {
	all := []Result{ResultOK, ResultNotFound, ResultForbidden, ResultServerError, ResultUnopenable, ResultUnreachable}
	infos := Results()
	if len(infos) != len(all) {
		t.Fatalf("结果图例有 %d 条，枚举有 %d 个", len(infos), len(all))
	}
	for _, r := range all {
		found := false
		for _, i := range infos {
			if i.Result == r {
				found = true
				if i.Title == "" || i.Desc == "" {
					t.Errorf("%s 缺中文名或说明", r)
				}
			}
		}
		if !found {
			t.Errorf("结果 %s 没有出现在图例里", r)
		}
		if ResultTitle(r) == string(r) {
			t.Errorf("结果 %s 没有中文名", r)
		}
	}
}

func TestFieldTitles(t *testing.T) {
	for _, f := range []Field{FieldCoverPortrait, FieldCoverLandscape, FieldEpisodeCover, FieldEpisodePoster, FieldVideo} {
		if FieldTitle(f) == string(f) {
			t.Errorf("字段 %s 没有中文名", f)
		}
	}
	if got := FieldTitle("unknown"); got != "unknown" {
		t.Fatalf("未知字段应原样返回，得到 %q", got)
	}
}

// ---------- 封面值归一 ----------

func TestNormalizeAsset(t *testing.T) {
	cases := []struct {
		name, in, wantKey, wantAbs string
	}{
		{"相对路径", "covers/a.webp", "covers/a.webp", ""},
		{"带前导斜杠", "/covers/a.webp", "covers/a.webp", ""},
		{"前后空格", "  covers/a.webp  ", "covers/a.webp", ""},
		{"完整 https 网址", "https://cdn.example.com/a.webp", "", "https://cdn.example.com/a.webp"},
		{"完整 http 网址", "http://cdn.example.com/a.webp", "", "http://cdn.example.com/a.webp"},
		{"省略协议的网址", "//cdn.example.com/a.webp", "", ""},
		{"data URI 当没填", "data:image/png;base64,AAAA", "", ""},
		{"ftp 协议当没填", "ftp://host/a.webp", "", ""},
		{"空", "", "", ""},
		{"只有空格", "   ", "", ""},
		{"带查询串的网址原样保留", "https://cdn.example.com/a.webp?v=2", "", "https://cdn.example.com/a.webp?v=2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, abs := normalizeAsset(c.in)
			if key != c.wantKey || abs != c.wantAbs {
				t.Fatalf("normalizeAsset(%q) = (%q, %q)，想要 (%q, %q)", c.in, key, abs, c.wantKey, c.wantAbs)
			}
		})
	}
}

func TestStripQuery(t *testing.T) {
	cases := [][2]string{
		{"https://cdn/a.m3u8?exp=1&s=SIG", "https://cdn/a.m3u8"},
		{"https://cdn/a.m3u8#frag", "https://cdn/a.m3u8"},
		{"https://cdn/a.m3u8", "https://cdn/a.m3u8"},
		{"https://cdn/a.m3u8?X-Amz-Signature=abc", "https://cdn/a.m3u8"},
	}
	for _, c := range cases {
		if got := stripQuery(c[0]); got != c[1] {
			t.Errorf("stripQuery(%q) = %q，想要 %q", c[0], got, c[1])
		}
	}
}

// ---------- 待检清单 ----------

func TestTargets(t *testing.T) {
	a := DramaAssets{
		DramaID: 7, Title: "测试剧", CoverPortrait: "covers/p.webp", CoverLandscape: "",
		Episodes: []EpisodeAssets{
			{ID: 71, Number: 1, Cover: "covers/e1.webp", Poster: "posters/71/a.webp", ObjectKey: "hls/71/job1", ObjectKind: "hls_dir"},
			{ID: 72, Number: 2, Cover: "", Poster: "", ObjectKey: "", ObjectKind: "", MediaStatus: "pending"},
			{ID: 73, Number: 3, Cover: "", Poster: "", ObjectKey: "files/73.mp4", ObjectKind: "file"},
		},
	}
	got := Targets(a)

	// 竖封面 + 第1集的封面/首帧/视频 + 第3集的视频 = 5；横封面与第2集全空都该跳过
	if len(got) != 5 {
		t.Fatalf("待检清单有 %d 项，想要 5：%+v", len(got), got)
	}
	byField := map[Field]Target{}
	for _, tg := range got {
		key := tg.Field
		if tg.Field == FieldVideo {
			// 视频按分集区分
			if tg.EpisodeID == 73 {
				key = "video73"
			}
		}
		byField[key] = tg
	}
	if _, ok := byField[FieldCoverLandscape]; ok {
		t.Error("横封面是空的，不该进待检清单")
	}
	for _, tg := range got {
		if tg.EpisodeID == 72 {
			t.Error("第 2 集什么资源都没有，不该进待检清单")
		}
		if tg.DramaID != 7 {
			t.Errorf("剧编号错了：%+v", tg)
		}
	}
	// hls_dir 探主清单，并把目录作为签名前缀
	v := byField[FieldVideo]
	if v.Key != "hls/71/job1/master.m3u8" {
		t.Errorf("hls_dir 该探 master.m3u8，得到 %q", v.Key)
	}
	if v.Prefix != "hls/71/job1/" {
		t.Errorf("hls_dir 的签名前缀该是目录，得到 %q", v.Prefix)
	}
	if v.FileType != TypeVideo {
		t.Errorf("视频的大类错了：%s", v.FileType)
	}
	// file 直接探对象本身，没有前缀
	f := byField["video73"]
	if f.Key != "files/73.mp4" || f.Prefix != "" {
		t.Errorf("file 该直接探对象：key=%q prefix=%q", f.Key, f.Prefix)
	}
	// 封面归到 cover 大类
	if byField[FieldCoverPortrait].FileType != TypeCover || byField[FieldEpisodePoster].FileType != TypeCover {
		t.Error("封面与首帧图都该是 cover 大类")
	}
	// 剧级资源没有分集
	if byField[FieldCoverPortrait].EpisodeID != 0 {
		t.Error("剧封面不该带分集编号")
	}
}

func TestTargetsHandlesTrailingSlashInObjectKey(t *testing.T) {
	got := Targets(DramaAssets{
		DramaID:  1,
		Episodes: []EpisodeAssets{{ID: 11, ObjectKey: "hls/11/job/", ObjectKind: "hls_dir"}},
	})
	if len(got) != 1 {
		t.Fatalf("想要 1 项，得到 %d", len(got))
	}
	if got[0].Key != "hls/11/job/master.m3u8" {
		t.Errorf("目录末尾的斜杠该被吃掉，得到 %q", got[0].Key)
	}
}

func TestTargetsEmptyDrama(t *testing.T) {
	if got := Targets(DramaAssets{DramaID: 1}); len(got) != 0 {
		t.Fatalf("什么都没填的剧不该有待检项，得到 %+v", got)
	}
}

// ---------- 选线 ----------

func TestPickLine(t *testing.T) {
	svc := newSvc(&fakeRepo{}, &fakeLines{lines: []storage.Line{
		lineOfKind(1, storage.LineVideo, 30, storage.LineActive, "https://v3.example.com"),
		lineOfKind(2, storage.LineVideo, 10, storage.LineActive, "https://v1.example.com"),
		lineOfKind(3, storage.LineVideo, 5, storage.LineDisabled, "https://off.example.com"),
		lineOfKind(4, storage.LineVideo, 1, storage.LineActive, ""), // 没配域名
		lineOfKind(5, storage.LineImage, 20, storage.LineWatch, "https://img.example.com"),
	}}, &fakeSigner{}, &fakeProber{}, &fakeQueue{})

	video, err := svc.pickLine(context.Background(), storage.LineVideo)
	if err != nil {
		t.Fatal(err)
	}
	if !video.usable || video.line.ID != 2 {
		t.Fatalf("该选 priority 最小且可用的 2 号线路，得到 %+v", video)
	}

	// 观察中的线路仍可用于体检（它还在对外服务，只是排在后面）
	image, err := svc.pickLine(context.Background(), storage.LineImage)
	if err != nil {
		t.Fatal(err)
	}
	if !image.usable || image.line.ID != 5 {
		t.Fatalf("观察中的线路该能用于体检，得到 %+v", image)
	}
}

func TestPickLineNoneUsable(t *testing.T) {
	svc := newSvc(&fakeRepo{}, &fakeLines{lines: []storage.Line{
		lineOfKind(1, storage.LineVideo, 1, storage.LineDisabled, "https://a.example.com"),
	}}, &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	got, err := svc.pickLine(context.Background(), storage.LineVideo)
	if err != nil {
		t.Fatal(err)
	}
	if got.usable {
		t.Fatal("全部停用时不该有可用线路")
	}
}

func TestPickLineSamePriorityPicksSmallerID(t *testing.T) {
	svc := newSvc(&fakeRepo{}, &fakeLines{lines: []storage.Line{
		lineOfKind(9, storage.LineVideo, 10, storage.LineActive, "https://b.example.com"),
		lineOfKind(4, storage.LineVideo, 10, storage.LineActive, "https://a.example.com"),
	}}, &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	got, _ := svc.pickLine(context.Background(), storage.LineVideo)
	// 同优先级取编号小的：体检要可重复，不能每次随机换线路
	if got.line.ID != 4 {
		t.Fatalf("同优先级该取编号小的，得到 %d", got.line.ID)
	}
}

func TestLineWarning(t *testing.T) {
	cases := []struct {
		name  string
		lines []storage.Line
		want  string
	}{
		{"两类都有", []storage.Line{
			lineOfKind(1, storage.LineVideo, 1, storage.LineActive, "https://v"),
			lineOfKind(2, storage.LineImage, 1, storage.LineActive, "https://i"),
		}, ""},
		{"缺视频线路", []storage.Line{
			lineOfKind(2, storage.LineImage, 1, storage.LineActive, "https://i"),
		}, "没有启用中的视频线路"},
		{"缺图片线路", []storage.Line{
			lineOfKind(1, storage.LineVideo, 1, storage.LineActive, "https://v"),
		}, "没有启用中的图片线路"},
		{"一条线路都没有", nil, "还没有配置任何启用中的线路"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := newSvc(&fakeRepo{}, &fakeLines{lines: c.lines}, &fakeSigner{}, &fakeProber{}, &fakeQueue{})
			got, err := svc.lineWarning(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" && got != "" {
				t.Fatalf("不该有提示，得到 %q", got)
			}
			if c.want != "" && !strings.Contains(got, c.want) {
				t.Fatalf("提示里该含 %q，得到 %q", c.want, got)
			}
		})
	}
}

// ---------- 体检一部剧 ----------

func fullLines() *fakeLines {
	return &fakeLines{lines: []storage.Line{
		lineOfKind(1, storage.LineVideo, 1, storage.LineActive, "https://v.example.com"),
		lineOfKind(2, storage.LineImage, 1, storage.LineActive, "https://i.example.com"),
	}}
}

func TestCheckDrama(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{
		7: {
			DramaID: 7, CoverPortrait: "covers/p.webp",
			Episodes: []EpisodeAssets{
				{ID: 71, Number: 1, Cover: "covers/e1.webp", ObjectKey: "hls/71/j", ObjectKind: "hls_dir"},
			},
		},
	}}
	prober := &fakeProber{
		byURL: map[string]ProbeResult{
			"covers/p.webp":  {HTTPStatus: 200, ElapsedMs: 12, Message: "正常（HTTP 200）"},
			"covers/e1.webp": {HTTPStatus: 404, ElapsedMs: 8, Message: "源站上没有这个文件（HTTP 404）"},
			"master.m3u8":    {Unreachable: true, ElapsedMs: 10000, Message: "连接超时，请检查端点地址与网络"},
		},
	}
	signer := &fakeSigner{}
	svc := newSvc(repo, fullLines(), signer, prober, &fakeQueue{})

	n, err := svc.CheckDrama(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("该写 3 条结果，得到 %d", n)
	}

	got := map[Field]Outcome{}
	for _, o := range repo.written {
		got[o.Field] = o
	}
	if got[FieldCoverPortrait].Result != ResultOK {
		t.Errorf("竖封面该是正常，得到 %s", got[FieldCoverPortrait].Result)
	}
	if got[FieldEpisodeCover].Result != ResultNotFound || got[FieldEpisodeCover].StatusCode != 404 {
		t.Errorf("本集封面该是 404，得到 %+v", got[FieldEpisodeCover])
	}
	if got[FieldVideo].Result != ResultUnreachable {
		t.Errorf("视频该是连不上，得到 %s", got[FieldVideo].Result)
	}
	// 签名材料不入库
	for _, o := range repo.written {
		if strings.Contains(o.URL, "?") || strings.Contains(o.URL, "SIGNATURE") {
			t.Errorf("入库的地址不该带查询串与签名：%q", o.URL)
		}
	}
	// 用对了线路：视频走 video 线路、封面走 image 线路
	if got[FieldVideo].LineID != 1 {
		t.Errorf("视频该走 1 号线路，得到 %d", got[FieldVideo].LineID)
	}
	if got[FieldCoverPortrait].LineID != 2 {
		t.Errorf("封面该走 2 号线路，得到 %d", got[FieldCoverPortrait].LineID)
	}
	// 分集资源带分集编号、剧封面不带
	if got[FieldEpisodeCover].EpisodeID != 71 || got[FieldCoverPortrait].EpisodeID != 0 {
		t.Error("分集编号的归属不对")
	}
	// HLS 产物按目录前缀签名（和播放时一致）
	foundPrefix := false
	for _, c := range signer.called {
		if c.Prefix == "hls/71/j/" && c.Key == "hls/71/j/master.m3u8" {
			foundPrefix = true
		}
	}
	if !foundPrefix {
		t.Errorf("视频该按目录前缀签名，实际签名目标：%+v", signer.called)
	}
}

func TestCheckDramaSkipsWhenLineMissing(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{
		7: {
			DramaID: 7, CoverPortrait: "covers/p.webp",
			Episodes: []EpisodeAssets{{ID: 71, ObjectKey: "hls/71/j", ObjectKind: "hls_dir"}},
		},
	}}
	// 只有图片线路：视频该被跳过，不该被记成「连不上」
	lines := &fakeLines{lines: []storage.Line{
		lineOfKind(2, storage.LineImage, 1, storage.LineActive, "https://i.example.com"),
	}}
	svc := newSvc(repo, lines, &fakeSigner{}, &fakeProber{deflt: ProbeResult{HTTPStatus: 200}}, &fakeQueue{})

	n, err := svc.CheckDrama(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("只有封面该被检，得到 %d 条", n)
	}
	for _, o := range repo.written {
		if o.Field == FieldVideo {
			t.Error("没有视频线路时不该写视频的结果（会把「没配线路」误报成「资源丢了」）")
		}
	}
}

func TestCheckDramaAbsoluteURLSkipsLine(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{
		7: {DramaID: 7, CoverPortrait: "https://cdn.other.com/p.webp"},
	}}
	signer := &fakeSigner{}
	prober := &fakeProber{deflt: ProbeResult{HTTPStatus: 200}}
	svc := newSvc(repo, fullLines(), signer, prober, &fakeQueue{})

	if _, err := svc.CheckDrama(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(signer.called) != 0 {
		t.Error("填的是完整网址，不该去生成签名地址")
	}
	if len(prober.visited) != 1 || prober.visited[0] != "https://cdn.other.com/p.webp" {
		t.Fatalf("该直接探运营填的网址，实际探了 %+v", prober.visited)
	}
	if repo.written[0].LineID != 0 {
		t.Errorf("没经线路时 line_id 该为空，得到 %d", repo.written[0].LineID)
	}
}

func TestCheckDramaSignFailureRecorded(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{
		7: {DramaID: 7, CoverPortrait: "covers/p.webp"},
	}}
	svc := newSvc(repo, fullLines(), &fakeSigner{err: errs.ErrBadRequest.WithMessage("线路缺少签名密钥")},
		&fakeProber{}, &fakeQueue{})

	if _, err := svc.CheckDrama(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(repo.written) != 1 {
		t.Fatalf("签名失败也该记一条，得到 %d", len(repo.written))
	}
	o := repo.written[0]
	if o.Result != ResultUnopenable {
		t.Errorf("签名失败该记成打不开，得到 %s", o.Result)
	}
	if !strings.Contains(o.Message, "线路缺少签名密钥") {
		t.Errorf("该带上原因，得到 %q", o.Message)
	}
}

func TestCheckDramaMissingDrama(t *testing.T) {
	svc := newSvc(&fakeRepo{assets: map[int64]DramaAssets{}}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	if _, err := svc.CheckDrama(context.Background(), 404); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("剧不存在该返回 NotFound，得到 %v", err)
	}
}

func TestCheckDramaNoAssets(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{7: {DramaID: 7}}}
	prober := &fakeProber{}
	svc := newSvc(repo, fullLines(), &fakeSigner{}, prober, &fakeQueue{})
	n, err := svc.CheckDrama(context.Background(), 7)
	if err != nil || n != 0 {
		t.Fatalf("没有资源该是 0 条无错，得到 (%d, %v)", n, err)
	}
	if len(prober.visited) != 0 {
		t.Error("没有待检资源时不该发请求")
	}
}

// ---------- 发起体检 ----------

func TestRunSingleDrama(t *testing.T) {
	repo := &fakeRepo{assets: map[int64]DramaAssets{7: {DramaID: 7}}}
	queue := &fakeQueue{}
	svc := newSvc(repo, fullLines(), &fakeSigner{}, &fakeProber{}, queue)

	out, err := svc.Run(context.Background(), RunRequest{DramaID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if out.Dramas != 1 || out.Queued != 1 {
		t.Fatalf("想要 1 部 1 个任务，得到 %+v", out)
	}
	if len(queue.ids) != 1 || queue.ids[0] != 7 {
		t.Fatalf("入队的剧编号不对：%+v", queue.ids)
	}
}

func TestRunRejectsMissingDrama(t *testing.T) {
	svc := newSvc(&fakeRepo{assets: map[int64]DramaAssets{}}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	// 填错编号该当场报错，而不是回「已入队 1 部」的假成功
	if _, err := svc.Run(context.Background(), RunRequest{DramaID: 999}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("想要 NotFound，得到 %v", err)
	}
}

func TestRunScope(t *testing.T) {
	repo := &fakeRepo{ids: []int64{1, 2, 100, 101}} // 100+ 是未上架
	queue := &fakeQueue{}
	svc := newSvc(repo, fullLines(), &fakeSigner{}, &fakeProber{}, queue)

	out, err := svc.Run(context.Background(), RunRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Dramas != 2 {
		t.Fatalf("默认只检已上架，想要 2 部，得到 %d", out.Dramas)
	}

	queue.ids = nil
	out, err = svc.Run(context.Background(), RunRequest{IncludeUnpublished: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Dramas != 4 {
		t.Fatalf("勾了含未上架该有 4 部，得到 %d", out.Dramas)
	}
}

func TestRunEmptyScope(t *testing.T) {
	svc := newSvc(&fakeRepo{}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	_, err := svc.Run(context.Background(), RunRequest{})
	if !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("没有符合范围的剧该报 400，得到 %v", err)
	}
	if !strings.Contains(errs.From(err).Message, "含未上架") {
		t.Errorf("该提示可以勾选含未上架，得到 %q", errs.From(err).Message)
	}
}

func TestRunNegativeID(t *testing.T) {
	svc := newSvc(&fakeRepo{}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	if _, err := svc.Run(context.Background(), RunRequest{DramaID: -1}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("负编号该报 400，得到 %v", err)
	}
}

// ---------- 列表与清空 ----------

func TestListFillsTitles(t *testing.T) {
	repo := &fakeRepo{
		rows: []Row{
			{ID: 1, Field: FieldVideo, Result: ResultNotFound},
			{ID: 2, Field: FieldCoverPortrait, Result: ResultOK},
		},
		total:   2,
		summary: Summary{Total: 2, Bad: 1},
	}
	svc := newSvc(repo, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	out, err := svc.List(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if out.List[0].FieldTitle != "视频" || out.List[0].ResultTitle != "404" {
		t.Errorf("该补上中文名，得到 %+v", out.List[0])
	}
	if out.Limit != defaultRows {
		t.Errorf("默认条数该是 %d，得到 %d", defaultRows, out.Limit)
	}
	if out.Shown != 2 || out.Total != 2 || out.Summary.Bad != 1 {
		t.Errorf("统计不对：%+v", out)
	}
	if len(out.Results) == 0 {
		t.Error("该带上结果图例")
	}
}

func TestListFilterValidation(t *testing.T) {
	svc := newSvc(&fakeRepo{}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	cases := []struct {
		name string
		f    Filter
	}{
		{"负剧编号", Filter{DramaID: -1}},
		{"条数超上限", Filter{Limit: MaxRows + 1}},
		{"条数为负", Filter{Limit: -1}},
		{"搜索词过长", Filter{Q: strings.Repeat("长", maxQueryRunes+1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.List(context.Background(), c.f); !errors.Is(err, errs.ErrBadRequest) {
				t.Fatalf("想要 400，得到 %v", err)
			}
		})
	}
}

func TestCleanFilterEscapesLikeWildcards(t *testing.T) {
	got, err := cleanFilter(Filter{Q: "100%_a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Pattern != `%100\%\_a%` {
		t.Fatalf("通配符该被转义，得到 %q", got.Pattern)
	}
	// 原始词保留，供「按剧编号匹配」使用
	if got.Q != "100%_a" {
		t.Fatalf("原始搜索词该保留，得到 %q", got.Q)
	}
}

func TestCleanFilterStripsControlChars(t *testing.T) {
	got, err := cleanFilter(Filter{Q: "剧\x00名\t"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(got.Q, 0) {
		t.Fatalf("控制字符该被去掉，得到 %q", got.Q)
	}
}

func TestClear(t *testing.T) {
	repo := &fakeRepo{}
	svc := newSvc(repo, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})

	if _, err := svc.Clear(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if repo.cleared != 7 {
		t.Fatalf("该清 7 号剧，得到 %d", repo.cleared)
	}
	if _, err := svc.Clear(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if repo.cleared != 0 {
		t.Fatal("0 该表示清全部")
	}
	if _, err := svc.Clear(context.Background(), -1); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("负编号该报 400")
	}
}

func TestListRepoError(t *testing.T) {
	boom := errors.New("库挂了")
	svc := newSvc(&fakeRepo{err: boom}, fullLines(), &fakeSigner{}, &fakeProber{}, &fakeQueue{})
	if _, err := svc.List(context.Background(), Filter{}); !errors.Is(err, boom) {
		t.Fatalf("该把仓储错误透出去，得到 %v", err)
	}
}
