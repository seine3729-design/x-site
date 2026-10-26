package play

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 选源（S1-7）的领域层测试：观看权限判定、令牌内容与选线编排；SQL 在 store/pg 的集成测试里。

var testNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestDecide(t *testing.T) {
	type viewer struct {
		name          string
		loggedIn, vip bool
	}
	guest := viewer{"游客", false, false}
	member := viewer{"注册用户", true, false}
	vipUser := viewer{"会员", true, true}

	tests := []struct {
		access content.Access
		trial  int32
		viewer viewer
		want   int32 // 试看秒数，0 = 完整播放
		err    *errs.Error
	}{
		// 免费：任何人完整播放，试看秒数无意义
		{content.AccessFree, 0, guest, 0, nil},
		{content.AccessFree, 30, guest, 0, nil},
		// 登录可看：登录后完整播放；游客有试看就试看，否则登录墙
		{content.AccessLogin, 0, member, 0, nil},
		{content.AccessLogin, 30, member, 0, nil},
		{content.AccessLogin, 30, guest, 30, nil},
		{content.AccessLogin, 0, guest, 0, errs.ErrNeedLogin},
		// 会员可看：会员完整播放；非会员（含游客）有试看就试看，否则 VIP 墙
		{content.AccessVIP, 0, vipUser, 0, nil},
		{content.AccessVIP, 30, vipUser, 0, nil},
		{content.AccessVIP, 30, member, 30, nil},
		{content.AccessVIP, 30, guest, 30, nil},
		{content.AccessVIP, 0, member, 0, errs.ErrNeedVIP},
		{content.AccessVIP, 0, guest, 0, errs.ErrNeedVIP},
	}
	for _, tt := range tests {
		name := string(tt.access) + "/" + tt.viewer.name
		got, err := decide(tt.access, tt.trial, tt.viewer.loggedIn, tt.viewer.vip)
		if tt.err != nil {
			if !errors.Is(err, tt.err) {
				t.Errorf("%s（试看 %d）：期望 %v，得到 %v", name, tt.trial, tt.err.Code, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s（试看 %d）：期望试看 %d 秒，得到 %d / %v", name, tt.trial, tt.want, got, err)
		}
	}
	if _, err := decide("paid", 0, true, true); err == nil || errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("未知的观看权限应是内部错误，得到 %v", err)
	}
}

type fakeRepo struct {
	episodes map[int64]Episode
}

func (r fakeRepo) PlayableEpisode(_ context.Context, id int64) (Episode, error) {
	ep, ok := r.episodes[id]
	if !ok {
		return Episode{}, errs.ErrNotFound
	}
	return ep, nil
}

type fakeLines struct {
	sel   storage.PlaybackSelection
	err   error
	calls []storage.PlaybackInput
}

func (f *fakeLines) ForPlayback(_ context.Context, in storage.PlaybackInput) (storage.PlaybackSelection, error) {
	f.calls = append(f.calls, in)
	return f.sel, f.err
}

type fakeMembers struct {
	until map[int64]time.Time
	err   error
	calls []int64
}

func (f *fakeMembers) VIPUntil(_ context.Context, userID int64) (time.Time, error) {
	f.calls = append(f.calls, userID)
	return f.until[userID], f.err
}

type fixture struct {
	svc     *Service
	lines   *fakeLines
	members *fakeMembers
	keys    *playtoken.Keyring
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	keys, err := playtoken.ParseKeyring("k1:0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	repo := fakeRepo{episodes: map[int64]Episode{
		1: {ID: 1, DramaID: 9, Access: content.AccessFree, VideoObjectID: 501},
		2: {ID: 2, DramaID: 9, Access: content.AccessLogin, TrialSec: 45},
		3: {ID: 3, DramaID: 9, Access: content.AccessVIP, TrialSec: 30},
		4: {ID: 4, DramaID: 9, Access: content.AccessVIP},
		5: {ID: 5, DramaID: 9, Access: content.AccessLogin},
	}}
	lines := &fakeLines{sel: storage.PlaybackSelection{Region: "CN-广东-电信", Lines: []storage.PlaybackLine{
		{LineID: 12, Name: "线路 1", RegionTag: "本地区", Recommended: true},
		{LineID: 7, Name: "", RegionTag: "通用"},
	}}}
	members := &fakeMembers{until: map[int64]time.Time{
		100: testNow.Add(24 * time.Hour), // 会员
		101: testNow.Add(-time.Minute),   // 刚到期
	}}
	svc := NewService(repo, lines, keys, members)
	svc.now = func() time.Time { return testNow }
	return fixture{svc: svc, lines: lines, members: members, keys: keys}
}

// tokenOf 从清单地址里取出令牌并校验。
func (f fixture) tokenOf(t *testing.T, url string) playtoken.Claims {
	t.Helper()
	tok, ok := strings.CutPrefix(url, "/api/v1/hls/")
	if !ok {
		t.Fatalf("清单地址应在 /api/v1/hls/ 下：%s", url)
	}
	tok, ok = strings.CutSuffix(tok, "/index.m3u8")
	if !ok {
		t.Fatalf("清单地址应以 /index.m3u8 结尾：%s", url)
	}
	c, err := f.keys.Verify(tok, testNow)
	if err != nil {
		t.Fatalf("清单地址里的令牌应能校验：%v", err)
	}
	return c
}

func TestSources_FreeEpisode(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.Sources(context.Background(), SourcesInput{
		EpisodeID: 1, Viewer: Viewer{DeviceID: "dev-1"}, IP: "203.0.113.9", LineID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.EpisodeID != 1 || got.Access != content.AccessFree || got.TrialSec != 0 || got.Region != "CN-广东-电信" {
		t.Fatalf("选源结果不对：%+v", got)
	}
	if len(got.Lines) != 2 || got.Lines[0].LineID != 12 || !got.Lines[0].Recommended || got.Lines[1].LineID != 7 {
		t.Fatalf("线路应按选线结果的顺序返回：%+v", got.Lines)
	}
	wantExp := testNow.Add(TokenTTL)
	for _, l := range got.Lines {
		c := f.tokenOf(t, l.URL)
		if c.EpisodeID != 1 || c.LineID != l.LineID || c.TrialSec != 0 || !c.BoundTo("dev-1") || c.BoundTo("dev-2") {
			t.Fatalf("令牌应绑定分集、本条线路与设备：%+v", c)
		}
		if !l.ExpiresAt.Equal(wantExp) || !c.ExpiresAt.Equal(wantExp) {
			t.Fatalf("过期时间应为签发后 10 分钟：%v / %v", l.ExpiresAt, c.ExpiresAt)
		}
	}
	// 选线拿到设备、手选线路、IP 与当前产物（按它的就绪副本挑存储源）
	in := f.lines.calls[0]
	if in.DeviceID != "dev-1" || in.Preferred != 7 || in.IP != "203.0.113.9" || in.ObjectID != 501 {
		t.Fatalf("选线输入不对：%+v", in)
	}
	if len(f.members.calls) != 0 {
		t.Fatal("免费分集不需要查会员")
	}
}

func TestSources_AccessScenarios(t *testing.T) {
	tests := []struct {
		name    string
		episode int64
		viewer  Viewer
		trial   int32
		err     *errs.Error
		members bool // 是否应查会员
	}{
		{"登录可看 · 游客试看", 2, Viewer{}, 45, nil, false},
		{"登录可看 · 已登录完整播放", 2, Viewer{UserID: 7}, 0, nil, false},
		{"登录可看 · 无试看的游客 401", 5, Viewer{}, 0, errs.ErrNeedLogin, false},
		{"会员可看 · 会员完整播放", 3, Viewer{UserID: 100}, 0, nil, true},
		{"会员可看 · 会员刚到期按试看", 3, Viewer{UserID: 101}, 30, nil, true},
		{"会员可看 · 游客试看（游客一律视为非会员）", 3, Viewer{}, 30, nil, false},
		{"会员可看 · 非会员无试看 402", 4, Viewer{UserID: 7}, 0, errs.ErrNeedVIP, true},
		{"会员可看 · 游客无试看 402", 4, Viewer{}, 0, errs.ErrNeedVIP, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			got, err := f.svc.Sources(context.Background(), SourcesInput{EpisodeID: tt.episode, Viewer: tt.viewer})
			if (len(f.members.calls) > 0) != tt.members {
				t.Fatalf("查会员 = %v，期望 %v", f.members.calls, tt.members)
			}
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("期望 %s，得到 %v", tt.err.Code, err)
				}
				if len(f.lines.calls) != 0 {
					t.Fatal("没有观看权限时不应选线、不应签发令牌")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.TrialSec != tt.trial {
				t.Fatalf("试看秒数 %d，期望 %d", got.TrialSec, tt.trial)
			}
			for _, l := range got.Lines {
				if c := f.tokenOf(t, l.URL); c.TrialSec != tt.trial {
					t.Fatalf("令牌里的试看秒数 %d，期望 %d（S1-8 据此裁剪清单）", c.TrialSec, tt.trial)
				}
			}
		})
	}
}

func TestSources_Errors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, id := range []int64{0, -1, 404} {
		if _, err := f.svc.Sources(ctx, SourcesInput{EpisodeID: id}); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("分集 %d 应 404，得到 %v", id, err)
		}
	}

	// 没有可用线路：503
	f.lines.sel = storage.PlaybackSelection{Region: "XX"}
	if _, err := f.svc.Sources(ctx, SourcesInput{EpisodeID: 1}); !errors.Is(err, errs.ErrUnavailable) {
		t.Errorf("没有线路应 503，得到 %v", err)
	}

	// 会员查询失败：不能当成非会员（会让已付费的用户看到 VIP 墙），按内部错误返回让客户端重试
	f = newFixture(t)
	f.members.err = errors.New("valkey down")
	_, err := f.svc.Sources(ctx, SourcesInput{EpisodeID: 3, Viewer: Viewer{UserID: 100}})
	if err == nil || errs.From(err) != errs.ErrInternal {
		t.Errorf("会员查询失败应是内部错误，得到 %v", err)
	}

	// 选线失败原样返回
	f = newFixture(t)
	f.lines.err = errs.ErrUnavailable
	if _, err := f.svc.Sources(ctx, SourcesInput{EpisodeID: 1}); !errors.Is(err, errs.ErrUnavailable) {
		t.Errorf("选线错误应原样返回，得到 %v", err)
	}
}

func TestSources_UnboundWithoutDevice(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.Sources(context.Background(), SourcesInput{EpisodeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if c := f.tokenOf(t, got.Lines[0].URL); c.Device != "" || !c.BoundTo("") {
		t.Fatalf("没有设备标识时令牌不绑定设备：%+v", c)
	}
}
