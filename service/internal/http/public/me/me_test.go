package me

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
	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// 「我的」接口（S2-1）HTTP 层测试：身份中间件鉴权边界、current 标记、改密与下线的错误映射。

type fakeResolver struct {
	access identity.Access
	err    error
}

func (f fakeResolver) ResolveAccess(string) (identity.Access, error) { return f.access, f.err }

type fakeAccounts struct {
	err    error
	gotUID int64
	gotIn  user.ChangePasswordInput
}

func (f *fakeAccounts) ChangePassword(_ context.Context, uid int64, in user.ChangePasswordInput) error {
	f.gotUID, f.gotIn = uid, in
	return f.err
}

type fakeSessions struct {
	list      []identity.SessionInfo
	listErr   error
	revokeErr error
	gotUID    int64
	gotSID    uuid.UUID
}

func (f *fakeSessions) ListSessions(_ context.Context, uid int64) ([]identity.SessionInfo, error) {
	f.gotUID = uid
	return f.list, f.listErr
}

func (f *fakeSessions) RevokeSession(_ context.Context, uid int64, sid uuid.UUID) error {
	f.gotUID, f.gotSID = uid, sid
	return f.revokeErr
}

func userAccess(sid string) identity.Access {
	return identity.Access{Subject: "7", SessionID: sid, Role: authtoken.RoleUser}
}

func do(t *testing.T, h *Handlers, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Body.Len() == 0 {
		return ""
	}
	var b struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return b.Code
}

// 鉴权边界：无令牌、游客令牌、无效令牌都不能进 /me/*。
func TestMe_RequireUser(t *testing.T) {
	for _, tt := range []struct {
		name     string
		resolver fakeResolver
		token    string
		status   int
		code     string
	}{
		{"无令牌", fakeResolver{}, "", 401, "NEED_LOGIN"},
		{"游客令牌", fakeResolver{access: identity.Access{Subject: "dev", Role: authtoken.RoleGuest}}, "tok", 401, "NEED_LOGIN"},
		{"令牌过期", fakeResolver{err: errs.ErrTokenExpired}, "tok", 401, "TOKEN_EXPIRED"},
		{"令牌无效", fakeResolver{err: errs.ErrNeedLogin}, "tok", 401, "NEED_LOGIN"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handlers{Accounts: &fakeAccounts{}, Sessions: &fakeSessions{}, Resolver: tt.resolver}
			w := do(t, h, http.MethodGet, "/api/v1/me/sessions", "", tt.token)
			if w.Code != tt.status || codeOf(t, w) != tt.code {
				t.Fatalf("期望 %d %s，得到 %d %s", tt.status, tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestListSessions(t *testing.T) {
	sid1, sid2 := uuid.NewV7(), uuid.NewV7()
	sess := &fakeSessions{list: []identity.SessionInfo{
		{ID: sid1, Platform: "web"}, {ID: sid2, Platform: "ios"},
	}}
	h := &Handlers{Accounts: &fakeAccounts{}, Sessions: sess, Resolver: fakeResolver{access: userAccess(sid1.String())}}
	w := do(t, h, http.MethodGet, "/api/v1/me/sessions", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if sess.gotUID != 7 {
		t.Fatalf("应按当前用户 7 查询，得到 %d", sess.gotUID)
	}
	var body struct {
		Data struct {
			Sessions []struct {
				ID       string `json:"id"`
				Platform string `json:"platform"`
				Current  bool   `json:"current"`
			} `json:"sessions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	s := body.Data.Sessions
	if len(s) != 2 || s[0].ID != sid1.String() || !s[0].Current || s[1].Current {
		t.Fatalf("会话列表或 current 标记不对：%s", w.Body.String())
	}
}

func TestChangePassword(t *testing.T) {
	// 成功 204，入参按当前用户传入
	acc := &fakeAccounts{}
	h := &Handlers{Accounts: acc, Sessions: &fakeSessions{}, Resolver: fakeResolver{access: userAccess("s")}}
	w := do(t, h, http.MethodPost, "/api/v1/me/password", `{"old_password":"old-pass-1","new_password":"new-pass-9"}`, "tok")
	if w.Code != http.StatusNoContent {
		t.Fatalf("期望 204，得到 %d %s", w.Code, w.Body.String())
	}
	if acc.gotUID != 7 || acc.gotIn.NewPassword != "new-pass-9" {
		t.Fatalf("改密入参不对：uid=%d in=%+v", acc.gotUID, acc.gotIn)
	}

	// 空请求体 → 400
	h2 := &Handlers{Accounts: &fakeAccounts{}, Sessions: &fakeSessions{}, Resolver: fakeResolver{access: userAccess("s")}}
	if w := do(t, h2, http.MethodPost, "/api/v1/me/password", "", "tok"); w.Code != http.StatusBadRequest {
		t.Fatalf("空请求体应 400，得到 %d", w.Code)
	}

	// 领域错误（旧密码错）→ 400
	h3 := &Handlers{Accounts: &fakeAccounts{err: errs.ErrBadRequest.WithMessage("当前密码不正确")}, Sessions: &fakeSessions{}, Resolver: fakeResolver{access: userAccess("s")}}
	if w := do(t, h3, http.MethodPost, "/api/v1/me/password", `{"old_password":"x","new_password":"new-pass-9"}`, "tok"); w.Code != http.StatusBadRequest || codeOf(t, w) != "BAD_REQUEST" {
		t.Fatalf("旧密码错应 400，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestRevokeSession(t *testing.T) {
	// 合法 uuid → 204，入参正确
	sess := &fakeSessions{}
	sid := uuid.NewV7()
	h := &Handlers{Accounts: &fakeAccounts{}, Sessions: sess, Resolver: fakeResolver{access: userAccess("s")}}
	if w := do(t, h, http.MethodDelete, "/api/v1/me/sessions/"+sid.String(), "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("期望 204，得到 %d %s", w.Code, w.Body.String())
	}
	if sess.gotUID != 7 || sess.gotSID != sid {
		t.Fatalf("下线入参不对：uid=%d sid=%v", sess.gotUID, sess.gotSID)
	}

	// 非法 uuid → 404
	h2 := &Handlers{Accounts: &fakeAccounts{}, Sessions: &fakeSessions{}, Resolver: fakeResolver{access: userAccess("s")}}
	if w := do(t, h2, http.MethodDelete, "/api/v1/me/sessions/not-a-uuid", "", "tok"); w.Code != http.StatusNotFound {
		t.Fatalf("非法 uuid 应 404，得到 %d", w.Code)
	}

	// 会话不属于本人（领域返回 NotFound）→ 404
	h3 := &Handlers{Accounts: &fakeAccounts{}, Sessions: &fakeSessions{revokeErr: errs.ErrNotFound}, Resolver: fakeResolver{access: userAccess("s")}}
	if w := do(t, h3, http.MethodDelete, "/api/v1/me/sessions/"+uuid.NewV7().String(), "", "tok"); w.Code != http.StatusNotFound || codeOf(t, w) != "NOT_FOUND" {
		t.Fatalf("不属于本人应 404，得到 %d %s", w.Code, w.Body.String())
	}
}
