package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// stubRepo 只实现用到的方法；嵌入接口让未实现的方法在被误用时直接 panic，暴露测试假设。
type stubRepo struct {
	Repo
	users      map[string]User
	byID       map[int64]User
	registered NewUser
	checkErr   error
	regErr     error
}

func newStubRepo() *stubRepo {
	return &stubRepo{users: map[string]User{}, byID: map[int64]User{}}
}

func (r *stubRepo) add(u User) {
	r.users[strings.ToLower(u.Handle)] = u
	r.byID[u.ID] = u
}

func (r *stubRepo) Register(_ context.Context, in NewUser, check func(Invite) error) (User, error) {
	r.registered = in
	if err := check(Invite{MaxUses: 1, UsedCount: 0}); err != nil {
		r.checkErr = err
		return User{}, err
	}
	if r.regErr != nil {
		return User{}, r.regErr
	}
	u := User{ID: 1, Handle: in.Handle, Nickname: in.Nickname, Status: UserActive,
		SessionVersion: 1, PasswordHash: in.PasswordHash}
	r.add(u)
	return u, nil
}

func (r *stubRepo) UserByHandle(_ context.Context, handle string) (User, error) {
	if u, ok := r.users[strings.ToLower(handle)]; ok {
		return u, nil
	}
	return User{}, errs.ErrNotFound
}

func (r *stubRepo) User(_ context.Context, id int64) (User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return User{}, errs.ErrNotFound
}

func (r *stubRepo) UpdateSecret(_ context.Context, id int64, pw string) (User, error) {
	u, ok := r.byID[id]
	if !ok {
		return User{}, errs.ErrNotFound
	}
	u.PasswordHash = pw
	u.SessionVersion++
	r.add(u)
	return u, nil
}

// fakeSessions 把会话存在内存里。
type fakeSessions struct {
	m map[string]Session
}

func newFakeSessions() *fakeSessions { return &fakeSessions{m: map[string]Session{}} }

func (s *fakeSessions) Create(_ context.Context, key string, sess Session, _ time.Duration) error {
	s.m[key] = sess
	return nil
}

func (s *fakeSessions) GetAndTouch(_ context.Context, key string, _ time.Duration) (Session, error) {
	sess, ok := s.m[key]
	if !ok {
		return Session{}, errs.ErrNotFound
	}
	return sess, nil
}

func (s *fakeSessions) Delete(_ context.Context, key string) error {
	delete(s.m, key)
	return nil
}

// fakeCounter 记录失败计数。
type fakeCounter struct {
	n        map[string]int64
	equalize int
}

func newFakeCounter() *fakeCounter { return &fakeCounter{n: map[string]int64{}} }

func (c *fakeCounter) Get(_ context.Context, key string) (int64, time.Duration, error) {
	return c.n[key], time.Minute, nil
}

func (c *fakeCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	c.n[key]++
	return c.n[key], nil
}

func (c *fakeCounter) Reset(_ context.Context, key string) error {
	delete(c.n, key)
	return nil
}

// fakeHasher 用可逆前缀模拟哈希，便于断言；Equalize 计数用于验证防枚举。
type fakeHasher struct{ equalized int }

func (h *fakeHasher) Hash(_ context.Context, plain string) (string, error) { return "h:" + plain, nil }

func (h *fakeHasher) Verify(_ context.Context, plain, encoded string) (bool, bool, error) {
	return encoded == "h:"+plain, false, nil
}

func (h *fakeHasher) Equalize(context.Context, string) { h.equalized++ }

func newIdentity(t *testing.T) (*IdentityService, *stubRepo, *fakeSessions, *fakeCounter, *fakeHasher) {
	t.Helper()
	repo, sess, cnt, hasher := newStubRepo(), newFakeSessions(), newFakeCounter(), &fakeHasher{}
	svc := NewIdentityService(repo, sess, cnt, hasher)
	svc.now = func() time.Time { return testNow }
	return svc, repo, sess, cnt, hasher
}

func TestRegister(t *testing.T) {
	ctx := context.Background()
	valid := RegisterInput{Handle: "maomi_user", Password: "community-pass-2026", InviteCode: "abcd-efgh-ijkl"}

	t.Run("成功时返回令牌并建会话", func(t *testing.T) {
		svc, repo, sess, _, _ := newIdentity(t)
		res, err := svc.Register(ctx, valid)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if res.Token == "" {
			t.Fatal("注册必须返回令牌")
		}
		if len(sess.m) != 1 {
			t.Fatalf("应创建 1 个会话，实际 %d", len(sess.m))
		}
		// 邀请码只以哈希形式传给仓储
		if repo.registered.InviteHash == valid.InviteCode {
			t.Fatal("邀请码明文不能传给仓储")
		}
	})

	// 恢复码已删除（adr/0011）：注册不再发码，也不再有知情确认字段。
	t.Run("响应里不含任何找回凭据", func(t *testing.T) {
		svc, _, _, _, _ := newIdentity(t)
		res, err := svc.Register(ctx, valid)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		// AuthResult 的字段就是全部对外返回的内容，序列化后不应出现 recovery 字样
		blob, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("序列化失败：%v", err)
		}
		if strings.Contains(strings.ToLower(string(blob)), "recovery") {
			t.Fatalf("注册响应仍带找回凭据：%s", blob)
		}
	})

	t.Run("缺邀请码被拒", func(t *testing.T) {
		svc, _, _, _, _ := newIdentity(t)
		in := valid
		in.InviteCode = "  "
		if _, err := svc.Register(ctx, in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("邀请码已过期时整体失败且不建会话", func(t *testing.T) {
		expired := testNow.Add(-time.Hour)
		sess := newFakeSessions()
		svc := NewIdentityService(
			&expiringRepo{stubRepo: newStubRepo(), expiresAt: expired},
			sess, newFakeCounter(), &fakeHasher{},
		)
		svc.now = func() time.Time { return testNow }
		if _, err := svc.Register(ctx, valid); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("过期邀请码应被拒，得到 %v", err)
		}
		if len(sess.m) != 0 {
			t.Fatal("注册失败不应留下会话")
		}
	})
}

// expiringRepo 让 check 收到一个已过期的邀请码。
type expiringRepo struct {
	*stubRepo
	expiresAt time.Time
}

func (r *expiringRepo) Register(_ context.Context, in NewUser, check func(Invite) error) (User, error) {
	if err := check(Invite{MaxUses: 1, ExpiresAt: &r.expiresAt}); err != nil {
		return User{}, err
	}
	return User{ID: 2, Handle: in.Handle}, nil
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("用户名不存在与密码错误返回同一业务码", func(t *testing.T) {
		svc, repo, _, cnt, hasher := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1, PasswordHash: "h:right-password-x"})

		_, errUnknown := svc.Login(ctx, LoginInput{Handle: "nobody", Password: "right-password-x"})
		_, errWrong := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "wrong-password-x"})
		if errs.From(errUnknown).Code != errs.From(errWrong).Code {
			t.Fatalf("两种失败的业务码不同：%v vs %v", errUnknown, errWrong)
		}
		if errs.From(errUnknown).Code != errs.ErrNeedLogin.Code {
			t.Fatalf("期望 NEED_LOGIN，得到 %v", errUnknown)
		}
		// 账号不存在时也要消耗一次哈希时间
		if hasher.equalized != 1 {
			t.Fatalf("Equalize 调用次数 = %d，期望 1", hasher.equalized)
		}
		if cnt.n["nobody"] != 1 || cnt.n["maomi"] != 1 {
			t.Fatalf("两种失败都应计数：%+v", cnt.n)
		}
	})

	t.Run("成功后清零失败计数", func(t *testing.T) {
		svc, repo, _, cnt, _ := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1, PasswordHash: "h:right-password-x"})
		cnt.n["maomi"] = 3
		if _, err := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "right-password-x"}); err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if _, ok := cnt.n["maomi"]; ok {
			t.Fatal("成功登录应清零失败计数")
		}
	})

	t.Run("失败次数过多返回限流", func(t *testing.T) {
		svc, repo, _, cnt, _ := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1, PasswordHash: "h:right-password-x"})
		cnt.n["maomi"] = maxLoginFailures
		_, err := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "right-password-x"})
		if !errors.Is(err, errs.ErrRateLimited) {
			t.Fatalf("期望 429，得到 %v", err)
		}
		if errs.RetryAfter(err) <= 0 {
			t.Fatal("限流应带可重试时长")
		}
	})

	t.Run("封禁账号不能登录", func(t *testing.T) {
		svc, repo, _, _, _ := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserSuspended, SessionVersion: 1, PasswordHash: "h:right-password-x"})
		if _, err := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "right-password-x"}); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("期望 403，得到 %v", err)
		}
	})
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("会话版本变化后令牌失效", func(t *testing.T) {
		svc, repo, _, _, _ := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1, PasswordHash: "h:right-password-x"})
		res, err := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "right-password-x"})
		if err != nil {
			t.Fatalf("登录失败：%v", err)
		}
		if _, err := svc.Authenticate(ctx, res.Token); err != nil {
			t.Fatalf("刚登录的令牌应有效：%v", err)
		}
		// 模拟改密：会话版本 +1
		u := repo.byID[1]
		u.SessionVersion = 2
		repo.add(u)
		if _, err := svc.Authenticate(ctx, res.Token); !errors.Is(err, errs.ErrTokenExpired) {
			t.Fatalf("期望 TOKEN_EXPIRED，得到 %v", err)
		}
	})

	t.Run("空令牌与未知令牌需要登录", func(t *testing.T) {
		svc, _, _, _, _ := newIdentity(t)
		if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, errs.ErrNeedLogin) {
			t.Fatalf("期望 NEED_LOGIN，得到 %v", err)
		}
		if _, err := svc.Authenticate(ctx, "not-a-real-token"); !errors.Is(err, errs.ErrNeedLogin) {
			t.Fatalf("期望 NEED_LOGIN，得到 %v", err)
		}
	})

	t.Run("封禁后令牌立即失效", func(t *testing.T) {
		svc, repo, _, _, _ := newIdentity(t)
		repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1, PasswordHash: "h:right-password-x"})
		res, _ := svc.Login(ctx, LoginInput{Handle: "maomi", Password: "right-password-x"})
		u := repo.byID[1]
		u.Status = UserSuspended
		repo.add(u)
		if _, err := svc.Authenticate(ctx, res.Token); err == nil {
			t.Fatal("封禁账号的令牌不应继续有效")
		}
	})
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newIdentity(t)
	repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1,
		PasswordHash: "h:old-password-2026"})

	res, err := svc.ChangePassword(ctx, 1, ChangePasswordInput{OldPassword: "old-password-2026", NewPassword: "new-password-2026"})
	if err != nil {
		t.Fatalf("不该出错：%v", err)
	}
	if res.Token == "" {
		t.Fatal("改密后应换发令牌")
	}
	after := repo.byID[1]
	if after.PasswordHash != "h:new-password-2026" {
		t.Fatalf("密码未更新：%q", after.PasswordHash)
	}
	if after.SessionVersion != 2 {
		t.Fatalf("改密应让旧会话失效，session_version = %d", after.SessionVersion)
	}

	if _, err := svc.ChangePassword(ctx, 1, ChangePasswordInput{OldPassword: "wrong", NewPassword: "another-password-x"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("旧密码错误应被拒，得到 %v", err)
	}
}

// 恢复码与「忘记密码」已彻底删除（adr/0011）。
//
// 这个测试守的是「没有后门」这件事：改密**必须**提供正确的当前密码，
// 没有任何绕过它的路径。原来的 TestRecover 连同 Recover 方法一起删掉了 ——
// 如果将来有人想加回找回功能，应该先改 ADR，而不是让这个测试悄悄失效。
func TestNoPasswordResetWithoutOldPassword(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newIdentity(t)
	repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1,
		PasswordHash: "h:old-password-2026"})

	for _, old := range []string{"", "wrong-password", "h:old-password-2026"} {
		_, err := svc.ChangePassword(ctx, 1, ChangePasswordInput{
			OldPassword: old, NewPassword: "brand-new-password",
		})
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("旧密码为 %q 时应被拒，得到 %v", old, err)
		}
	}
	if after := repo.byID[1]; after.PasswordHash != "h:old-password-2026" {
		t.Fatalf("密码不应被改动：%q", after.PasswordHash)
	}
}

func TestUpdateProfileLimits(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _, _ := newIdentity(t)
	repo.add(User{ID: 1, Handle: "maomi", Status: UserActive, SessionVersion: 1})

	// stubRepo 没有实现 UpdateProfile：校验必须在到达仓储前拦住，否则这里会 panic
	if _, err := svc.UpdateProfile(ctx, 1, ProfileInput{Nickname: strings.Repeat("长", 41)}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("昵称超长应被拒，得到 %v", err)
	}
	if _, err := svc.UpdateProfile(ctx, 1, ProfileInput{Bio: strings.Repeat("字", 301)}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("简介超长应被拒，得到 %v", err)
	}
}
