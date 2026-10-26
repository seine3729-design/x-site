package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"uuid"

	"xsite/service/internal/crypto/authtoken"
	"xsite/service/internal/errs"
)

// 身份服务（S1-10）的单元测试：游客签发、令牌轮换与复用检测、登出、Access Token 解析。
// 这里用一个忠实复刻会话族语义的内存仓储，真实 SQL 的轮换与复用在 store/pg 的集成测试里验证。

const testSecret = "0123456789abcdef0123456789abcdef"

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// memSession 是内存里的一行会话。
type memSession struct {
	id, family, device uuid.UUID
	userID             int64
	platform           string
	expiresAt          time.Time
	revoked            bool
	reason             string
}

// memRepo 忠实复刻 pg.SessionRepo 的轮换 / 复用语义，供服务层单测。
type memRepo struct {
	devices  map[uuid.UUID]GuestDevice
	byHash   map[string]*memSession // hex(refresh_hash) → 会话
	guestN   int
	sessionN int
}

func newMemRepo() *memRepo {
	return &memRepo{devices: map[uuid.UUID]GuestDevice{}, byHash: map[string]*memSession{}}
}

func (r *memRepo) CreateGuest(_ context.Context, d GuestDevice) error {
	r.guestN++
	r.devices[d.ID] = d
	return nil
}

func (r *memRepo) CreateSession(_ context.Context, s NewSession) error {
	r.sessionN++
	r.byHash[hex.EncodeToString(s.RefreshHash)] = &memSession{
		id: s.ID, family: s.FamilyID, device: s.GuestDeviceID, userID: s.UserID,
		platform: s.Platform, expiresAt: s.ExpiresAt,
	}
	return nil
}

func (r *memRepo) revokeFamily(family uuid.UUID, reason string) {
	for _, s := range r.byHash {
		if s.family == family && !s.revoked {
			s.revoked, s.reason = true, reason
		}
	}
}

func (r *memRepo) Rotate(_ context.Context, oldHash []byte, in RotateInput, now time.Time) (RotateResult, error) {
	old, ok := r.byHash[hex.EncodeToString(oldHash)]
	if !ok {
		return RotateResult{Status: RotateNotFound}, nil
	}
	if old.revoked {
		r.revokeFamily(old.family, "reuse_detected")
		return RotateResult{Status: RotateReuse}, nil
	}
	if !old.expiresAt.After(now) {
		return RotateResult{Status: RotateExpired}, nil
	}
	old.revoked, old.reason = true, "rotated"
	exp := in.GuestExpiresAt
	if old.userID > 0 {
		exp = in.UserExpiresAt
	}
	r.byHash[hex.EncodeToString(in.NewRefreshHash)] = &memSession{
		id: in.NewSessionID, family: old.family, device: old.device, userID: old.userID,
		platform: old.platform, expiresAt: exp,
	}
	return RotateResult{Status: RotateOK, GuestDeviceID: old.device, UserID: old.userID, Platform: old.platform}, nil
}

func (r *memRepo) RevokeFamilyByRefresh(_ context.Context, hash []byte, reason string) error {
	if s, ok := r.byHash[hex.EncodeToString(hash)]; ok {
		r.revokeFamily(s.family, reason)
	}
	return nil
}

func (r *memRepo) ListUserSessions(_ context.Context, userID int64, now time.Time) ([]SessionInfo, error) {
	var out []SessionInfo
	for _, s := range r.byHash {
		if s.userID == userID && !s.revoked && s.expiresAt.After(now) {
			out = append(out, SessionInfo{ID: s.id, Platform: s.platform})
		}
	}
	return out, nil
}

func (r *memRepo) RevokeSessionByID(_ context.Context, userID int64, sessionID uuid.UUID, reason string) (int64, error) {
	for _, s := range r.byHash {
		if s.id == sessionID && s.userID == userID && !s.revoked {
			s.revoked, s.reason = true, reason
			return 1, nil
		}
	}
	return 0, nil
}

func (r *memRepo) RevokeAllForUser(_ context.Context, userID int64, reason string) error {
	for _, s := range r.byHash {
		if s.userID == userID && !s.revoked {
			s.revoked, s.reason = true, reason
		}
	}
	return nil
}

func newService(t *testing.T) (*Service, *memRepo, *authtoken.Keyring) {
	t.Helper()
	keys, err := authtoken.ParseKeyring("t1:" + testSecret)
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemRepo()
	svc := NewService(repo, keys)
	svc.now = func() time.Time { return testNow }
	return svc, repo, keys
}

func TestIssueGuest(t *testing.T) {
	svc, repo, keys := newService(t)
	tok, err := svc.IssueGuest(context.Background(), IssueInput{Platform: "ios", SourceChannel: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.guestN != 1 || repo.sessionN != 1 {
		t.Fatalf("应建一台游客设备与一个会话，实际 %d / %d", repo.guestN, repo.sessionN)
	}
	if tok.Role != authtoken.RoleGuest || tok.ExpiresIn != int64(AccessTTL/time.Second) || tok.RefreshToken == "" || tok.DeviceID == "" {
		t.Fatalf("返回值不对：%+v", tok)
	}
	// Access Token 可校验，sub=设备、sid 非空、role=guest
	c, err := keys.Verify(tok.AccessToken, testNow)
	if err != nil {
		t.Fatalf("Access Token 应可校验：%v", err)
	}
	if c.Subject != tok.DeviceID || c.Role != authtoken.RoleGuest || c.SessionID == "" {
		t.Fatalf("令牌载荷不对：%+v", c)
	}
	if _, err := uuid.Parse(tok.DeviceID); err != nil {
		t.Fatalf("设备 ID 应是 UUID：%v", err)
	}
	// 平台非法时回落到 web（不报错）
	if _, err := svc.IssueGuest(context.Background(), IssueInput{Platform: "windows"}); err != nil {
		t.Fatalf("非法平台应回落而非报错：%v", err)
	}
	if got := repo.devices; len(got) != 2 {
		t.Fatalf("应有两台设备")
	}
}

// 令牌轮换 + 复用检测的端到端（服务层）：签发 → 刷新（旧失效）→ 旧令牌复用（整族吊销）→ 新令牌也随之失效。
func TestRefreshRotationAndReuse(t *testing.T) {
	svc, _, keys := newService(t)
	ctx := context.Background()

	issued, err := svc.IssueGuest(ctx, IssueInput{Platform: "web"})
	if err != nil {
		t.Fatal(err)
	}
	r1 := issued.RefreshToken

	// 第一次刷新：拿到新 Access/Refresh，设备不变
	rot, err := svc.Refresh(ctx, RefreshInput{RefreshToken: r1})
	if err != nil {
		t.Fatalf("首次刷新应成功：%v", err)
	}
	r2 := rot.RefreshToken
	if r2 == r1 {
		t.Fatal("刷新必须轮换 refresh token")
	}
	if rot.DeviceID != issued.DeviceID {
		t.Fatalf("刷新后设备应不变：%s vs %s", rot.DeviceID, issued.DeviceID)
	}
	c, _ := keys.Verify(rot.AccessToken, testNow)
	if c.Subject != issued.DeviceID || c.Role != authtoken.RoleGuest {
		t.Fatalf("刷新后的 Access 载荷不对：%+v", c)
	}

	// 复用：再用 r1（已轮换吊销）刷新 → 判为泄露，整族吊销，返回统一的 TOKEN_EXPIRED
	_, err = svc.Refresh(ctx, RefreshInput{RefreshToken: r1})
	if !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("复用旧令牌应 TOKEN_EXPIRED，得到 %v", err)
	}

	// 整族已吊销：新令牌 r2 也不能再用
	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: r2}); !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("复用检测后整族失效，r2 也应 TOKEN_EXPIRED，得到 %v", err)
	}
}

func TestRefresh_NotFoundExpiredEmpty(t *testing.T) {
	svc, repo, _ := newService(t)
	ctx := context.Background()

	for name, in := range map[string]RefreshInput{
		"空令牌":  {RefreshToken: ""},
		"未知令牌": {RefreshToken: "deadbeef"},
	} {
		if _, err := svc.Refresh(ctx, in); !errors.Is(err, errs.ErrTokenExpired) {
			t.Errorf("%s：应 TOKEN_EXPIRED，得到 %v", name, err)
		}
	}

	// 过期会话
	issued, _ := svc.IssueGuest(ctx, IssueInput{Platform: "web"})
	// 把该会话改成已过期
	for _, s := range repo.byHash {
		s.expiresAt = testNow.Add(-time.Second)
	}
	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: issued.RefreshToken}); !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("过期会话刷新应 TOKEN_EXPIRED，得到 %v", err)
	}
}

func TestLogoutRevokesFamily(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	issued, _ := svc.IssueGuest(ctx, IssueInput{Platform: "web"})

	if err := svc.Logout(ctx, issued.RefreshToken); err != nil {
		t.Fatal(err)
	}
	// 登出后该刷新令牌不能再刷新
	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: issued.RefreshToken}); !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("登出后刷新应失败，得到 %v", err)
	}
	// 空令牌登出是幂等成功
	if err := svc.Logout(ctx, ""); err != nil {
		t.Fatalf("空令牌登出应成功：%v", err)
	}
}

func TestResolveAccess(t *testing.T) {
	svc, _, keys := newService(t)
	issued, _ := svc.IssueGuest(context.Background(), IssueInput{Platform: "web"})

	a, err := svc.ResolveAccess(issued.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if a.Subject != issued.DeviceID || a.Role != authtoken.RoleGuest || a.SessionID == "" {
		t.Fatalf("解析结果不对：%+v", a)
	}

	// 过期 → TOKEN_EXPIRED
	expired, _ := keys.Sign(authtoken.Claims{
		Subject: "x", SessionID: "y", Role: authtoken.RoleGuest,
		IssuedAt: testNow.Add(-time.Hour), ExpiresAt: testNow.Add(-time.Minute),
	})
	if _, err := svc.ResolveAccess(expired); !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("过期 Access 应 TOKEN_EXPIRED，得到 %v", err)
	}
	// 无效 → NEED_LOGIN
	for _, bad := range []string{"", "not-a-token", issued.AccessToken + "x"} {
		if _, err := svc.ResolveAccess(bad); !errors.Is(err, errs.ErrNeedLogin) {
			t.Errorf("无效 Access(%q) 应 NEED_LOGIN，得到 %v", bad, err)
		}
	}
}

func TestIssueForUser(t *testing.T) {
	svc, repo, keys := newService(t)
	tok, err := svc.IssueForUser(context.Background(), 42, IssueInput{Platform: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Role != authtoken.RoleUser || tok.DeviceID != "" || tok.RefreshToken == "" {
		t.Fatalf("注册用户令牌应 role=user、无 device_id：%+v", tok)
	}
	c, err := keys.Verify(tok.AccessToken, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "42" || c.Role != authtoken.RoleUser || c.SessionID == "" {
		t.Fatalf("载荷不对：%+v", c)
	}
	// Refresh 过期应为 30 天（UserRefreshTTL），不是游客的 180 天
	for _, s := range repo.byHash {
		if !s.expiresAt.Equal(testNow.Add(UserRefreshTTL)) {
			t.Fatalf("注册用户 refresh 应续 30 天：%v", s.expiresAt)
		}
	}
	// 无效用户 ID 报错
	if _, err := svc.IssueForUser(context.Background(), 0, IssueInput{}); err == nil {
		t.Fatal("用户 ID 为 0 应报错")
	}
}

// 注册用户刷新后仍用 30 天 TTL（而不是游客 180 天），且 role 保持 user、不带 device_id。
func TestRefresh_UserKeeps30Days(t *testing.T) {
	svc, repo, keys := newService(t)
	ctx := context.Background()
	issued, err := svc.IssueForUser(ctx, 7, IssueInput{Platform: "web"})
	if err != nil {
		t.Fatal(err)
	}
	rot, err := svc.Refresh(ctx, RefreshInput{RefreshToken: issued.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := keys.Verify(rot.AccessToken, testNow)
	if c.Subject != "7" || c.Role != authtoken.RoleUser {
		t.Fatalf("刷新后应保持 role=user、sub=7：%+v", c)
	}
	if rot.DeviceID != "" {
		t.Fatalf("注册用户刷新不应带 device_id：%q", rot.DeviceID)
	}
	var found bool
	for _, s := range repo.byHash {
		if s.userID == 7 && !s.revoked {
			found = true
			if !s.expiresAt.Equal(testNow.Add(UserRefreshTTL)) {
				t.Fatalf("注册用户刷新应续 30 天：%v", s.expiresAt)
			}
		}
	}
	if !found {
		t.Fatal("应有未吊销的用户会话")
	}
}

// 「我的设备」列表、设备下线（含防越权）、改密吊销全部会话。
func TestListAndRevokeSessions(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	// 用户 5 的两次登录（两个会话族）
	if _, err := svc.IssueForUser(ctx, 5, IssueInput{Platform: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueForUser(ctx, 5, IssueInput{Platform: "ios"}); err != nil {
		t.Fatal(err)
	}
	// 另一个用户 6
	if _, err := svc.IssueForUser(ctx, 6, IssueInput{Platform: "web"}); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListSessions(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("用户 5 应有 2 个会话，得到 %d", len(list))
	}

	// 下线其中一个
	if err := svc.RevokeSession(ctx, 5, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := svc.ListSessions(ctx, 5); len(l) != 1 {
		t.Fatalf("下线一个后应剩 1 个，得到 %d", len(l))
	}

	// 不能下线他人（用户 6）的会话：带 user_id 作用域，返回 ErrNotFound
	l6, _ := svc.ListSessions(ctx, 6)
	if err := svc.RevokeSession(ctx, 5, l6[0].ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("下线他人会话应 ErrNotFound，得到 %v", err)
	}
	if l, _ := svc.ListSessions(ctx, 6); len(l) != 1 {
		t.Fatal("用户 6 的会话不应被影响")
	}

	// 改密吊销全部
	if err := svc.RevokeAllSessions(ctx, 5, "password_changed"); err != nil {
		t.Fatal(err)
	}
	if l, _ := svc.ListSessions(ctx, 5); len(l) != 0 {
		t.Fatalf("改密吊销全部后应 0 个，得到 %d", len(l))
	}
}
