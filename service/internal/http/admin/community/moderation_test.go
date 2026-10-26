package community

import (
	"context"
	"strings"
	"testing"

	dom "xsite/service/internal/domain/moderation"
)

type fakeModeration struct {
	lastKind   string
	lastFilter dom.ModerationFilter
	actKind    string
	actAction  string
	actIDs     []int64
}

func (f *fakeModeration) List(_ context.Context, kind string, filter dom.ModerationFilter) (dom.Page[dom.ModerationItem], error) {
	f.lastKind, f.lastFilter = kind, filter
	return dom.Page[dom.ModerationItem]{
		List: []dom.ModerationItem{
			{ID: 1, Kind: kind, EpisodeID: 5, Content: "待审内容", Status: dom.StatusPending, HitWords: []string{"坏词"}, ReportCount: 1, Author: dom.AuthorView{IsGuest: true, Nickname: "游客"}},
		},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}
func (f *fakeModeration) Act(_ context.Context, kind, action string, ids []int64) (int64, error) {
	f.actKind, f.actAction, f.actIDs = kind, action, ids
	return int64(len(ids)), nil
}

type fakePolicy struct{ p dom.CommentPolicy }

func (f *fakePolicy) Get(context.Context) (dom.CommentPolicy, error) { return f.p, nil }
func (f *fakePolicy) Set(_ context.Context, p dom.CommentPolicy) (dom.CommentPolicy, error) {
	f.p = p
	return p, nil
}

func TestModerationPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/moderation?kind=comment", "", 200},
		{"viewer", "GET", "/api/admin/comment-policy", "", 200},
		{"viewer", "POST", "/api/admin/moderation/actions", `{"kind":"comment","action":"approve","ids":[1]}`, 403},
		{"viewer", "PUT", "/api/admin/comment-policy", `{"pre_moderation":true}`, 403},
		{"editor", "POST", "/api/admin/moderation/actions", `{"kind":"comment","action":"approve","ids":[1,2]}`, 200},
		{"editor", "PUT", "/api/admin/comment-policy", `{"pre_moderation":true}`, 200},
		{"nobody", "GET", "/api/admin/moderation", "", 403},
		{"", "GET", "/api/admin/moderation", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
}

func TestListModeration(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/moderation?kind=danmaku&status=pending&reported=true&blockword=true&q=%E5%9D%8F&page=2&page_size=10", ""), 200, "OK")
	if e.mod.lastKind != "danmaku" {
		t.Fatalf("kind 应为 danmaku，得到 %q", e.mod.lastKind)
	}
	f := e.mod.lastFilter
	if f.Status != "pending" || !f.Reported || !f.Blockword || f.Q != "坏" || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("筛选参数应交给服务：%+v", f)
	}
	if !strings.Contains(string(b.Data), `"hit_words"`) || !strings.Contains(string(b.Data), `"report_count":1`) {
		t.Fatalf("列表数据不对：%s", b.Data)
	}
	// 默认 kind=comment
	e.do("viewer", "GET", "/api/admin/moderation", "")
	if e.mod.lastKind != "comment" {
		t.Fatalf("kind 缺省应为 comment，得到 %q", e.mod.lastKind)
	}
}

func TestActModeration(t *testing.T) {
	e := newEnv()
	expect(t, e.do("editor", "POST", "/api/admin/moderation/actions", `{"kind":"danmaku","action":"delete","ids":[7,8]}`), 200, "OK")
	if e.mod.actKind != "danmaku" || e.mod.actAction != "delete" || len(e.mod.actIDs) != 2 {
		t.Fatalf("操作入参应交给服务：kind=%q action=%q ids=%v", e.mod.actKind, e.mod.actAction, e.mod.actIDs)
	}
	if a := e.audit.last(t); a.Action != "moderation.delete" || a.TargetType != "danmaku" {
		t.Fatalf("操作审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("editor", "POST", "/api/admin/moderation/actions", `{"kind":"comment","action":"approve","ids":[1],"x":1}`), 400, "BAD_REQUEST")
}

func TestCommentPolicy(t *testing.T) {
	e := newEnv()
	expect(t, e.do("editor", "PUT", "/api/admin/comment-policy", `{"pre_moderation":true}`), 200, "OK")
	if !e.pol.p.PreModeration {
		t.Fatal("应保存 pre_moderation=true")
	}
	if a := e.audit.last(t); a.Action != "comment_policy.update" || a.TargetType != "comment_policy" {
		t.Fatalf("策略审计：%+v", a)
	}
	b := expect(t, e.do("viewer", "GET", "/api/admin/comment-policy", ""), 200, "OK")
	if !strings.Contains(string(b.Data), `"pre_moderation":true`) {
		t.Fatalf("应读到已保存的策略：%s", b.Data)
	}
}
