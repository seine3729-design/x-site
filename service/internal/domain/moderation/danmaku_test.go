package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

type fakeDanmakuRepo struct {
	created NewDanmaku
	window  []DanmakuView
}

func (r *fakeDanmakuRepo) Create(_ context.Context, in NewDanmaku) (Danmaku, error) {
	r.created = in
	return Danmaku{ID: 1, EpisodeID: in.EpisodeID, TimeMS: in.TimeMS, Content: in.Content, Color: in.Color, Status: in.Status}, nil
}

func (r *fakeDanmakuRepo) ListWindow(_ context.Context, _ int64, _, _, _ int) ([]DanmakuView, error) {
	return r.window, nil
}

func newDanmakuSvc(repo *fakeDanmakuRepo, eval Evaluator, feat Features, usr Users) *DanmakuService {
	if eval == nil {
		eval = fakeEval{}
	}
	if feat == nil {
		feat = fakeFeatures{}
	}
	if usr == nil {
		usr = fakeUsers{}
	}
	return NewDanmakuService(repo, eval, feat, usr)
}

func TestDanmaku_SendVisible(t *testing.T) {
	repo := &fakeDanmakuRepo{}
	svc := newDanmakuSvc(repo, nil, nil, nil)
	d, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, TimeMS: 1200, Content: " 冲鸭\n"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != StatusVisible || repo.created.Content != "冲鸭" || repo.created.Color != defaultDanmakuColor {
		t.Fatalf("应清洗后可见、默认白：%+v", repo.created)
	}
}

func TestDanmaku_ColorWhitelist(t *testing.T) {
	// 合法颜色（去 # 大写归一）
	repo := &fakeDanmakuRepo{}
	svc := newDanmakuSvc(repo, nil, nil, nil)
	if _, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "a", Color: "#ff0000"}); err != nil {
		t.Fatal(err)
	}
	if repo.created.Color != "FF0000" {
		t.Fatalf("颜色应归一为 FF0000，得到 %s", repo.created.Color)
	}
	// 非白名单颜色
	svc2 := newDanmakuSvc(&fakeDanmakuRepo{}, nil, nil, nil)
	if _, err := svc2.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "a", Color: "123456"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("非白名单颜色应 400，得到 %v", err)
	}
}

func TestDanmaku_LengthAndEmpty(t *testing.T) {
	svc := newDanmakuSvc(&fakeDanmakuRepo{}, nil, nil, nil)
	if _, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "  "}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空内容应 400")
	}
	long := strings.Repeat("字", maxDanmakuRunes+1)
	if _, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: long}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("超长应 400")
	}
}

func TestDanmaku_BlockAndReview(t *testing.T) {
	// 命中 block → 400
	svc := newDanmakuSvc(&fakeDanmakuRepo{}, fakeEval{d: Decision{Blocked: true, Text: "x"}}, nil, nil)
	if _, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "坏话"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("命中 block 应 400，得到 %v", err)
	}
	// 命中 review → pending
	repo := &fakeDanmakuRepo{}
	svc2 := newDanmakuSvc(repo, fakeEval{d: Decision{Review: true, Text: "y"}}, nil, nil)
	if d, _ := svc2.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "y"}); d.Status != StatusPending {
		t.Fatalf("命中 review 应 pending，得到 %s", d.Status)
	}
}

func TestDanmaku_GateMutedAndFeature(t *testing.T) {
	// 禁言 → 403
	svc := newDanmakuSvc(&fakeDanmakuRepo{}, nil, nil, fakeUsers{muted: true})
	if _, err := svc.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("禁言应 403，得到 %v", err)
	}
	// 弹幕功能关闭 → 403
	svc2 := newDanmakuSvc(&fakeDanmakuRepo{}, nil, fakeFeatures{off: map[string]bool{FlagDanmaku: true}}, nil)
	if _, err := svc2.Send(context.Background(), SendDanmakuInput{Author: userAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("弹幕关闭应 403，得到 %v", err)
	}
	// 游客互动关闭 → 401
	svc3 := newDanmakuSvc(&fakeDanmakuRepo{}, nil, fakeFeatures{off: map[string]bool{FlagGuestInteraction: true}}, nil)
	if _, err := svc3.Send(context.Background(), SendDanmakuInput{Author: guestAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("游客互动关闭应 401，得到 %v", err)
	}
}

func TestDanmaku_Window(t *testing.T) {
	repo := &fakeDanmakuRepo{window: []DanmakuView{{ID: 1, TimeMS: 100, Content: "hi", Color: "FFFFFF"}}}
	svc := newDanmakuSvc(repo, nil, nil, nil)
	got, err := svc.Window(context.Background(), 5, 0, 60000)
	if err != nil || len(got) != 1 {
		t.Fatalf("窗口：%+v %v", got, err)
	}
	// 分集 ID 无效
	if _, err := svc.Window(context.Background(), 0, 0, 1000); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("分集 ID 无效应 400")
	}
	// 空结果归一为非 nil 空切片
	empty := newDanmakuSvc(&fakeDanmakuRepo{}, nil, nil, nil)
	if got, _ := empty.Window(context.Background(), 5, 0, 1000); got == nil {
		t.Fatal("空结果应为非 nil 空切片")
	}
}
