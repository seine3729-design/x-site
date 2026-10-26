package user

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/crypto/password"
	"xsite/service/internal/domain/identity"
	"xsite/service/internal/errs"
)

// 注册用户域（S2-1）单测：注册 / 登录 / 改密、输入校验、防枚举、改密吊销全部会话。
// 令牌签发用假 Issuer（真实签发在 identity 域已测）；密码用真实 Hasher（轻量参数）验证真实哈希往返。

type fakeRepo struct {
	byName map[string]*User
	byID   map[int64]*User
	seq    int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byName: map[string]*User{}, byID: map[int64]*User{}}
}

func (r *fakeRepo) Create(_ context.Context, u NewUser) (User, error) {
	if _, ok := r.byName[u.Username]; ok {
		return User{}, errs.ErrConflict
	}
	r.seq++
	rec := &User{ID: r.seq, Username: u.Username, Nickname: u.Nickname, PasswordHash: u.PasswordHash, Status: StatusNormal, CreatedAt: time.Now()}
	r.byName[u.Username] = rec
	r.byID[rec.ID] = rec
	return *rec, nil
}

func (r *fakeRepo) ByUsername(_ context.Context, username string) (User, error) {
	if rec, ok := r.byName[username]; ok {
		return *rec, nil
	}
	return User{}, errs.ErrNotFound
}

func (r *fakeRepo) ByID(_ context.Context, id int64) (User, error) {
	if rec, ok := r.byID[id]; ok {
		return *rec, nil
	}
	return User{}, errs.ErrNotFound
}

func (r *fakeRepo) UpdatePassword(_ context.Context, id int64, hash string) error {
	if rec, ok := r.byID[id]; ok {
		rec.PasswordHash = hash
		return nil
	}
	return errs.ErrNotFound
}

func (r *fakeRepo) GetProfile(_ context.Context, id int64) (Profile, error) {
	rec, ok := r.byID[id]
	if !ok {
		return Profile{}, errs.ErrNotFound
	}
	return Profile{
		Nickname: rec.Nickname, Avatar: rec.Avatar, Bio: rec.Bio,
		NicknameStatus: "ok", BioStatus: "ok", AvatarStatus: "ok", Locale: rec.Locale,
	}, nil
}

func (r *fakeRepo) UpdateProfile(ctx context.Context, id int64, f ProfileFields) (Profile, error) {
	rec, ok := r.byID[id]
	if !ok {
		return Profile{}, errs.ErrNotFound
	}
	if f.Nickname != nil {
		rec.Nickname = *f.Nickname
	}
	if f.Avatar != nil {
		rec.Avatar = *f.Avatar
	}
	if f.Bio != nil {
		rec.Bio = *f.Bio
	}
	return r.GetProfile(ctx, id)
}

type fakeIssuer struct {
	issued  []int64
	revoked []int64
}

func (f *fakeIssuer) IssueForUser(_ context.Context, userID int64, _ identity.IssueInput) (identity.Tokens, error) {
	f.issued = append(f.issued, userID)
	return identity.Tokens{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900}, nil
}

func (f *fakeIssuer) RevokeAllSessions(_ context.Context, userID int64, _ string) error {
	f.revoked = append(f.revoked, userID)
	return nil
}

type fakeFailures struct{ counts map[string]int64 }

func (f *fakeFailures) Get(_ context.Context, key string) (int64, time.Duration, error) {
	return f.counts[key], time.Minute, nil
}
func (f *fakeFailures) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	f.counts[key]++
	return f.counts[key], nil
}
func (f *fakeFailures) Reset(_ context.Context, key string) error {
	delete(f.counts, key)
	return nil
}

type fakeChallenge struct {
	accept bool
	issued int
}

func (f *fakeChallenge) Issue(_ context.Context) (Challenge, error) {
	f.issued++
	return Challenge{Nonce: "nonce", Difficulty: 4, ExpiresIn: 120}, nil
}
func (f *fakeChallenge) Verify(_ context.Context, _ ChallengeAnswer) (bool, error) {
	return f.accept, nil
}

func newSvc() (*Service, *fakeRepo, *fakeIssuer, *fakeFailures, *fakeChallenge) {
	repo := newFakeRepo()
	iss := &fakeIssuer{}
	// 轻量 Argon2id 参数：只为测真实哈希往返，不是强度测试
	h := password.NewHasher(password.Params{Memory: 16, Time: 1, Threads: 1, SaltLen: 8, KeyLen: 16}, 2)
	fails := &fakeFailures{counts: map[string]int64{}}
	chal := &fakeChallenge{}
	return NewService(repo, iss, h, fails, chal), repo, iss, fails, chal
}

const goodPassword = "s3cret-pass"

func TestRegister_RoundTrip(t *testing.T) {
	svc, repo, iss, _, _ := newSvc()
	res, err := svc.Register(context.Background(), RegisterInput{
		Username: "Alice_01", Password: goodPassword, Nickname: "小爱", AckNoRecovery: true,
	}, identity.IssueInput{Platform: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Username != "alice_01" {
		t.Fatalf("用户名应规范化为小写：%q", res.User.Username)
	}
	if res.User.Nickname != "小爱" || res.Tokens.AccessToken == "" {
		t.Fatalf("注册结果不对：%+v", res)
	}
	if len(iss.issued) != 1 || iss.issued[0] != res.User.ID {
		t.Fatalf("应为新用户签发令牌：%v", iss.issued)
	}
	// 库里存的是哈希，不是明文
	rec := repo.byName["alice_01"]
	if rec == nil || rec.PasswordHash == goodPassword || !strings.HasPrefix(rec.PasswordHash, "$argon2id$") {
		t.Fatalf("密码应以 Argon2id 哈希存储：%+v", rec)
	}
	// 用同样凭据能登录
	if _, err := svc.Login(context.Background(), LoginInput{Username: "alice_01", Password: goodPassword}, identity.IssueInput{}); err != nil {
		t.Fatalf("注册后应能登录：%v", err)
	}
}

func TestRegister_Validation(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   RegisterInput
	}{
		{"未确认无法找回", RegisterInput{Username: "bob123", Password: goodPassword}},
		{"用户名太短", RegisterInput{Username: "ab", Password: goodPassword, AckNoRecovery: true}},
		{"用户名非法字符", RegisterInput{Username: "bad name!", Password: goodPassword, AckNoRecovery: true}},
		{"保留用户名", RegisterInput{Username: "admin", Password: goodPassword, AckNoRecovery: true}},
		{"密码太短", RegisterInput{Username: "bob123", Password: "short7!", AckNoRecovery: true}},
		{"弱口令", RegisterInput{Username: "bob123", Password: "password1", AckNoRecovery: true}},
		{"纯数字密码", RegisterInput{Username: "bob123", Password: "1234567890", AckNoRecovery: true}},
		{"密码等于用户名", RegisterInput{Username: "mypasswd12", Password: "mypasswd12", AckNoRecovery: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, _, _ := newSvc()
			if _, err := svc.Register(context.Background(), tt.in, identity.IssueInput{}); !errors.Is(err, errs.ErrBadRequest) {
				t.Fatalf("应 BAD_REQUEST，得到 %v", err)
			}
		})
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	in := RegisterInput{Username: "carol_9", Password: goodPassword, AckNoRecovery: true}
	if _, err := svc.Register(context.Background(), in, identity.IssueInput{}); err != nil {
		t.Fatal(err)
	}
	// 大小写不同也算同一个用户名（都规范化为小写）
	_, err := svc.Register(context.Background(), RegisterInput{Username: "Carol_9", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复用户名应 CONFLICT，得到 %v", err)
	}
}

func TestLogin_Enumeration(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	svc.Register(context.Background(), RegisterInput{Username: "dave_12", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})

	// 未知用户与错密码返回相同错误码（防枚举）
	_, e1 := svc.Login(context.Background(), LoginInput{Username: "nobody_x", Password: goodPassword}, identity.IssueInput{})
	_, e2 := svc.Login(context.Background(), LoginInput{Username: "dave_12", Password: "wrong-pass-9"}, identity.IssueInput{})
	if !errors.Is(e1, errs.ErrNeedLogin) || !errors.Is(e2, errs.ErrNeedLogin) {
		t.Fatalf("两种失败都应 NEED_LOGIN：%v / %v", e1, e2)
	}
	if errs.From(e1).Message != errs.From(e2).Message {
		t.Fatalf("两种失败提示应一致：%q vs %q", errs.From(e1).Message, errs.From(e2).Message)
	}
}

func TestLogin_Banned(t *testing.T) {
	svc, repo, _, _, _ := newSvc()
	svc.Register(context.Background(), RegisterInput{Username: "evan_22", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})
	repo.byName["evan_22"].Status = StatusBanned
	if _, err := svc.Login(context.Background(), LoginInput{Username: "evan_22", Password: goodPassword}, identity.IssueInput{}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("封禁账号登录应 FORBIDDEN，得到 %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	svc, _, iss, _, _ := newSvc()
	res, _ := svc.Register(context.Background(), RegisterInput{Username: "fay_33", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})
	uid := res.User.ID

	// 旧密码错 → 400
	if err := svc.ChangePassword(context.Background(), uid, ChangePasswordInput{OldPassword: "nope-1234", NewPassword: "brand-new-9"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("旧密码错应 BAD_REQUEST，得到 %v", err)
	}
	// 新密码弱 → 400
	if err := svc.ChangePassword(context.Background(), uid, ChangePasswordInput{OldPassword: goodPassword, NewPassword: "password1"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("弱新密码应 BAD_REQUEST，得到 %v", err)
	}
	// 新旧相同 → 400
	if err := svc.ChangePassword(context.Background(), uid, ChangePasswordInput{OldPassword: goodPassword, NewPassword: goodPassword}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("新旧相同应 BAD_REQUEST，得到 %v", err)
	}
	// 成功：吊销全部会话，旧密码失效、新密码可用
	if err := svc.ChangePassword(context.Background(), uid, ChangePasswordInput{OldPassword: goodPassword, NewPassword: "brand-new-9"}); err != nil {
		t.Fatal(err)
	}
	if len(iss.revoked) != 1 || iss.revoked[0] != uid {
		t.Fatalf("改密应吊销该用户全部会话：%v", iss.revoked)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Username: "fay_33", Password: goodPassword}, identity.IssueInput{}); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("改密后旧密码应失效，得到 %v", err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Username: "fay_33", Password: "brand-new-9"}, identity.IssueInput{}); err != nil {
		t.Fatalf("改密后新密码应可登录：%v", err)
	}
}

// 失败过多要求人机验证：无 / 无效挑战 428，有效挑战 + 正确密码放行并清计数。
func TestLogin_Challenge(t *testing.T) {
	svc, _, _, fails, chal := newSvc()
	ctx := context.Background()
	svc.Register(ctx, RegisterInput{Username: "grace_7", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})

	fails.counts["u:grace_7"] = userFailThreshold
	if _, err := svc.Login(ctx, LoginInput{Username: "grace_7", Password: goodPassword}, identity.IssueInput{}); !errors.Is(err, errs.ErrChallengeRequired) {
		t.Fatalf("失败过多应要求人机验证，得到 %v", err)
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "grace_7", Password: goodPassword, Challenge: &ChallengeAnswer{Nonce: "n", Solution: "s"}}, identity.IssueInput{}); !errors.Is(err, errs.ErrChallengeRequired) {
		t.Fatalf("无效挑战应仍要求，得到 %v", err)
	}
	chal.accept = true
	if _, err := svc.Login(ctx, LoginInput{Username: "grace_7", Password: goodPassword, Challenge: &ChallengeAnswer{Nonce: "n", Solution: "s"}}, identity.IssueInput{}); err != nil {
		t.Fatalf("通过挑战 + 正确密码应登录成功，得到 %v", err)
	}
	if fails.counts["u:grace_7"] != 0 {
		t.Fatalf("登录成功应清用户名失败计数，剩 %d", fails.counts["u:grace_7"])
	}
}

// 失败累计到阈值后，下一次登录即便密码对也先要挑战；用户名与 IP 两个维度都计数。
func TestLogin_FailuresAccumulate(t *testing.T) {
	svc, _, _, fails, _ := newSvc()
	ctx := context.Background()
	svc.Register(ctx, RegisterInput{Username: "henry_8", Password: goodPassword, AckNoRecovery: true}, identity.IssueInput{})
	if fails.counts["u:henry_8"] != 0 {
		t.Fatalf("注册不应计失败：%d", fails.counts["u:henry_8"])
	}
	for range userFailThreshold {
		svc.Login(ctx, LoginInput{Username: "henry_8", Password: "wrong-pass-1"}, identity.IssueInput{IPPrefix: "203.0.113.0/24"})
	}
	if fails.counts["u:henry_8"] != userFailThreshold {
		t.Fatalf("应累计 %d 次用户名失败，实际 %d", userFailThreshold, fails.counts["u:henry_8"])
	}
	if fails.counts["ip:203.0.113.0/24"] != userFailThreshold {
		t.Fatalf("应累计 IP 失败，实际 %d", fails.counts["ip:203.0.113.0/24"])
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "henry_8", Password: goodPassword}, identity.IssueInput{}); !errors.Is(err, errs.ErrChallengeRequired) {
		t.Fatalf("达阈值后应要求挑战，得到 %v", err)
	}
}

func TestIssueChallenge(t *testing.T) {
	svc, _, _, _, chal := newSvc()
	c, err := svc.IssueChallenge(context.Background())
	if err != nil || c.Nonce == "" || chal.issued != 1 {
		t.Fatalf("应签发挑战：%+v / %v / issued=%d", c, err, chal.issued)
	}
}

// 资料修改（S2-3 基本校验）：清洗与长度校验，PATCH 语义（nil 字段不改）。
func TestUpdateProfile_BasicValidation(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	u, err := svc.Register(ctx, RegisterInput{Username: "profiler", Password: "s3cretpass", Nickname: "原名", AckNoRecovery: true}, identity.IssueInput{})
	if err != nil {
		t.Fatal(err)
	}
	id := u.User.ID

	ptr := func(s string) *string { return &s }

	// 简介超 80 字 → 400
	long := strings.Repeat("字", 81)
	if _, err := svc.UpdateProfile(ctx, id, ProfileUpdate{Bio: ptr(long)}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("简介超长应 400，得到 %v", err)
	}
	// 简介超过 2 个换行 → 400
	if _, err := svc.UpdateProfile(ctx, id, ProfileUpdate{Bio: ptr("a\nb\nc\nd")}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("简介换行过多应 400，得到 %v", err)
	}
	// 头像超长 → 400
	if _, err := svc.UpdateProfile(ctx, id, ProfileUpdate{Avatar: ptr(strings.Repeat("x", 501))}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("头像超长应 400，得到 %v", err)
	}

	// 正常修改：昵称折叠空白、简介保留 2 个换行、PATCH 只改传入字段
	p, err := svc.UpdateProfile(ctx, id, ProfileUpdate{
		Nickname: ptr("  新  名  "),
		Bio:      ptr("第一行\n第二行\n第三行"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Nickname != "新 名" {
		t.Fatalf("昵称应折叠空白为「新 名」，得到 %q", p.Nickname)
	}
	if p.Bio != "第一行\n第二行\n第三行" {
		t.Fatalf("简介应保留 2 个换行，得到 %q", p.Bio)
	}
	if p.Avatar != "" {
		t.Fatalf("未传头像不应改动，得到 %q", p.Avatar)
	}
	if p.NicknameStatus != "ok" {
		t.Fatalf("S2-3 审核状态恒为 ok，得到 %q", p.NicknameStatus)
	}

	// 空 PATCH：回读当前资料，不报错
	p2, err := svc.UpdateProfile(ctx, id, ProfileUpdate{})
	if err != nil || p2.Nickname != "新 名" {
		t.Fatalf("空 PATCH 应回读当前资料：%+v %v", p2, err)
	}
}
