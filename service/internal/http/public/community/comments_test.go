package community

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"uuid"

	"xsite/service/internal/crypto/authtoken"
	"xsite/service/internal/domain/identity"
	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

type fakeResolver struct {
	access identity.Access
	err    error
}

func (f fakeResolver) ResolveAccess(string) (identity.Access, error) { return f.access, f.err }

type fakeComments struct {
	created   dom.CreateCommentInput
	deleted   int64
	delAuth   dom.Author
	likedBy   int64
	likedID   int64
	createErr error
}

func (f *fakeComments) Create(_ context.Context, in dom.CreateCommentInput) (dom.Comment, error) {
	f.created = in
	if f.createErr != nil {
		return dom.Comment{}, f.createErr
	}
	return dom.Comment{ID: 1, EpisodeID: in.EpisodeID, Content: in.Content, Status: dom.StatusVisible}, nil
}
func (f *fakeComments) List(context.Context, int64, string, string, int) (dom.CommentPage, error) {
	return dom.CommentPage{List: []dom.CommentView{{ID: 9, Content: "你好"}}}, nil
}
func (f *fakeComments) Delete(_ context.Context, a dom.Author, id int64) error {
	f.delAuth, f.deleted = a, id
	return nil
}
func (f *fakeComments) Like(_ context.Context, userID, commentID int64) error {
	f.likedBy, f.likedID = userID, commentID
	return nil
}

func userAccess() identity.Access { return identity.Access{Subject: "7", Role: authtoken.RoleUser} }
func guestAccess() identity.Access {
	return identity.Access{Subject: uuid.NewV7().String(), Role: authtoken.RoleGuest}
}

func do(t *testing.T, h *Handlers, resolver fakeResolver, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Resolver = resolver
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListComments_Public(t *testing.T) {
	h := &Handlers{Comments: &fakeComments{}}
	w := do(t, h, fakeResolver{}, http.MethodGet, "/api/v1/episodes/5/comments?sort=hot", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("公开读应 200，得到 %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data dom.CommentPage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.List) != 1 {
		t.Fatalf("列表数据：%s", w.Body.String())
	}
}

func TestCreateComment_UserAndGuestAndAnon(t *testing.T) {
	// 注册用户
	fc := &fakeComments{}
	h := &Handlers{Comments: fc}
	w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/episodes/5/comments", `{"content":"好剧","parent_id":3}`, "tok")
	if w.Code != http.StatusOK || fc.created.Author.UserID != 7 || fc.created.EpisodeID != 5 || fc.created.ParentID != 3 {
		t.Fatalf("注册用户发表：%d %+v", w.Code, fc.created)
	}
	// 游客
	fc2 := &fakeComments{}
	h2 := &Handlers{Comments: fc2}
	if w := do(t, h2, fakeResolver{access: guestAccess()}, http.MethodPost, "/api/v1/episodes/5/comments", `{"content":"路过"}`, "tok"); w.Code != http.StatusOK || !fc2.created.Author.IsGuest() {
		t.Fatalf("游客发表应成功且作者为游客：%d %+v", w.Code, fc2.created)
	}
	// 匿名（无令牌）→ 401
	h3 := &Handlers{Comments: &fakeComments{}}
	if w := do(t, h3, fakeResolver{}, http.MethodPost, "/api/v1/episodes/5/comments", `{"content":"x"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("匿名发表应 401，得到 %d", w.Code)
	}
}

func TestCreateComment_DomainError(t *testing.T) {
	h := &Handlers{Comments: &fakeComments{createErr: errs.ErrBadRequest.WithMessage("内容包含不允许发布的词")}}
	w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/episodes/5/comments", `{"content":"坏"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("领域 400 应透传，得到 %d", w.Code)
	}
}

func TestDeleteComment(t *testing.T) {
	fc := &fakeComments{}
	h := &Handlers{Comments: fc}
	if w := do(t, h, fakeResolver{access: userAccess()}, http.MethodDelete, "/api/v1/comments/9", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("删除应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if fc.deleted != 9 || fc.delAuth.UserID != 7 {
		t.Fatalf("删除入参：id=%d author=%+v", fc.deleted, fc.delAuth)
	}
}

func TestLikeComment(t *testing.T) {
	fc := &fakeComments{}
	h := &Handlers{Comments: fc}
	if w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/comments/9/like", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("点赞应 204，得到 %d", w.Code)
	}
	if fc.likedBy != 7 || fc.likedID != 9 {
		t.Fatalf("点赞入参：by=%d id=%d", fc.likedBy, fc.likedID)
	}
	// 游客点赞 → 401
	if w := do(t, &Handlers{Comments: &fakeComments{}}, fakeResolver{access: guestAccess()}, http.MethodPost, "/api/v1/comments/9/like", "", "tok"); w.Code != http.StatusUnauthorized {
		t.Fatalf("游客点赞应 401，得到 %d", w.Code)
	}
}
