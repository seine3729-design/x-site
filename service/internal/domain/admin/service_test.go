package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
	"xsite/service/internal/privacy"
)

// ---------- 测试替身 ----------

type fakeAccounts struct {
	byName      map[string]Account
	logs        []LoginLog
	lookups     int
	marked      []int64
	updatedHash map[int64]string
}

func newFakeAccounts(accts ...Account) *fakeAccounts {
	f := &fakeAccounts{byName: map[string]Account{}, updatedHash: map[int64]string{}}
	for _, a := range accts {
		f.byName[a.Username] = a
	}
	return f
}

func (f *fakeAccounts) GetByUsername(_ context.Context, username string) (Account, error) {
	f.lookups++
	a, ok := f.byName[username]
	if !ok {
		return Account{}, errs.ErrNotFound
	}
	return a, nil
}

func (f *fakeAccounts) GetByID(_ context.Context, id int64) (Account, error) {
	for _, a := range f.byName {
		if a.ID == id {
			return a, nil
		}
	}
	return Account{}, errs.ErrNotFound
}

func (f *fakeAccounts) MarkLogin(_ context.Context, id int64) error {
	f.marked = append(f.marked, id)
	return nil
}

func (f *fakeAccounts) UpdatePasswordHash(_ context.Context, id int64, hash string) error {
	f.updatedHash[id] = hash
	return nil
}

func (f *fakeAccounts) InsertLoginLog(_ context.Context, l LoginLog) error {
	f.logs = append(f.logs, l)
	return nil
}

func (f *fakeAccounts) lastLog() LoginLog { return f.logs[len(f.logs)-1] }

type fakeSessions struct {
	m       map[string]Session
	ttl     map[string]time.Duration
	deleted []string
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{m: map[string]Session{}, ttl: map[string]time.Duration{}}
}

func (f *fakeSessions) Create(_ context.Context, key string, s Session, ttl time.Duration) error {
	f.m[key], f.ttl[key] = s, ttl
	return nil
}

func (f *fakeSessions) GetAndTouch(_ context.Context, key string, ttl time.Duration) (Session, error) {
	s, ok := f.m[key]
	if !ok {
		return Session{}, errs.ErrNotFound
	}
	f.ttl[key] = ttl
	return s, nil
}

func (f *fakeSessions) Delete(_ context.Context, key string) error {
	delete(f.m, key)
	f.deleted = append(f.deleted, key)
	return nil
}

type counterEntry struct {
	n   int64
	ttl time.Duration
}

type fakeCounter struct{ m map[string]counterEntry }

func (f *fakeCounter) Get(_ context.Context, key string) (int64, time.Duration, error) {
	e := f.m[key]
	return e.n, e.ttl, nil
}

func (f *fakeCounter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err // 与真实 Valkey 客户端一致：ctx 已取消时写入失败
	}
	e := f.m[key]
	e.n++
	e.ttl = window
	f.m[key] = e
	return e.n, nil
}

func (f *fakeCounter) Reset(_ context.Context, key string) error {
	delete(f.m, key)
	return nil
}

// fakeHasher：哈希串为 "hash:" + 明文；rehash 控制是否提示重哈希。
type fakeHasher struct {
	verifies   int
	equalizes  int
	needRehash bool
}

func (f *fakeHasher) Hash(_ context.Context, plain string) (string, error) {
	return "hash2:" + plain, nil
}

func (f *fakeHasher) Verify(_ context.Context, plain, encoded string) (bool, bool, error) {
	f.verifies++
	ok := encoded == "hash:"+plain
	return ok, ok && f.needRehash, nil
}

func (f *fakeHasher) Equalize(context.Context, string) { f.equalizes++ }

// fakeMFAVerifier：validCode[id] 是能通过校验的唯一验证码；err 模拟密钥环故障。
type fakeMFAVerifier struct {
	validCode map[int64]string
	err       error
	calls     int
}

func (f *fakeMFAVerifier) VerifyTOTP(_ context.Context, adminID int64, code string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	want, ok := f.validCode[adminID]
	return ok && code == want, nil
}

// fakeTickets 是后台二次验证会话状态（登录票据 + 重新验证标记）的内存实现（admin.MFAStore）。
type fakeTickets struct {
	m            map[string]int64  // ticket → adminID
	reauthed     map[string]bool   // sessionKey → 已标记重新验证
	reauthPrefix map[string]string // sessionKey → 标记时的 IP 网段
}

func newFakeTickets() *fakeTickets {
	return &fakeTickets{m: map[string]int64{}, reauthed: map[string]bool{}, reauthPrefix: map[string]string{}}
}

func (f *fakeTickets) CreateLoginTicket(_ context.Context, ticket string, adminID int64, _ time.Duration) error {
	f.m[ticket] = adminID
	return nil
}

func (f *fakeTickets) LookupLoginTicket(_ context.Context, ticket string) (int64, bool, error) {
	id, ok := f.m[ticket]
	return id, ok, nil
}

func (f *fakeTickets) DeleteLoginTicket(_ context.Context, ticket string) error {
	delete(f.m, ticket)
	return nil
}

func (f *fakeTickets) MarkReauthed(_ context.Context, sessionKey, ipPrefix string, _ time.Duration) error {
	f.reauthed[sessionKey] = true
	f.reauthPrefix[sessionKey] = ipPrefix
	return nil
}

type fixture struct {
	svc      *Service
	accounts *fakeAccounts
	sessions *fakeSessions
	counter  *fakeCounter
	hasher   *fakeHasher
	verifier *fakeMFAVerifier
	tickets  *fakeTickets
	now      time.Time
}

const testUA = "Mozilla/5.0 Test"

func newFixture(accts ...Account) *fixture {
	f := &fixture{
		accounts: newFakeAccounts(accts...),
		sessions: newFakeSessions(),
		counter:  &fakeCounter{m: map[string]counterEntry{}},
		hasher:   &fakeHasher{},
		now:      time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}
	f.svc = NewService(f.accounts, f.sessions, f.counter, f.hasher)
	f.svc.now = func() time.Time { return f.now }
	return f
}

// newMFALoginFixture 额外注入二次验证（WithMFA），用于登录第二步的测试。
func newMFALoginFixture(accts ...Account) *fixture {
	f := &fixture{
		accounts: newFakeAccounts(accts...),
		sessions: newFakeSessions(),
		counter:  &fakeCounter{m: map[string]counterEntry{}},
		hasher:   &fakeHasher{},
		verifier: &fakeMFAVerifier{validCode: map[int64]string{}},
		tickets:  newFakeTickets(),
		now:      time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}
	f.svc = NewService(f.accounts, f.sessions, f.counter, f.hasher, WithMFA(f.verifier, f.tickets))
	f.svc.now = func() time.Time { return f.now }
	return f
}

// mfaAdmin 是启用了二次验证（绑定 TOTP）的在用管理员。
func mfaAdmin() Account {
	a := activeAdmin()
	a.MFAEnabled = true
	a.HasTOTP = true
	return a
}

func operatorRole() RoleRef {
	return RoleRef{ID: 2, Code: "operator", Name: "运营", Active: true}
}

func activeAdmin() Account {
	return Account{
		ID: 7, Username: "alice", PasswordHash: "hash:right-password", Status: StatusActive, SessionVersion: 3,
		Role: operatorRole(), Permissions: []string{"dramas.edit", "dramas.view"},
	}
}

func login(f *fixture, username, password, ip string) (LoginResult, error) {
	return f.svc.Login(context.Background(), LoginInput{Username: username, Password: password, ClientIP: ip, UserAgent: testUA})
}

func assertBusinessError(t *testing.T, err error, want *errs.Error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("期望业务码 %s，得到 %v", want.Code, err)
	}
}

// ---------- 登录 ----------

func TestLogin_Success(t *testing.T) {
	f := newFixture(activeAdmin())
	f.counter.m["u:alice"] = counterEntry{n: 3, ttl: time.Minute}

	res, err := login(f, "  Alice ", "right-password", "203.0.113.9")
	if err != nil {
		t.Fatalf("登录应成功：%v", err)
	}
	if len(res.Token) != 43 {
		t.Fatalf("令牌应为 256 bit base64url（43 字符），得到 %q", res.Token)
	}
	id := res.Identity
	if id.AdminID != 7 || id.Username != "alice" || id.Role.Code != "operator" || !id.Can("dramas.edit") || id.Can("system.edit") ||
		id.CSRFToken == "" || id.CSRFToken == res.Token {
		t.Fatalf("身份不符合预期：%+v", id)
	}

	sum := sha256.Sum256([]byte(res.Token))
	key := hex.EncodeToString(sum[:])
	sess, ok := f.sessions.m[key]
	if !ok || id.SessionKey != key {
		t.Fatal("会话应以令牌的 SHA-256 为键保存，且不保存令牌本身")
	}
	if sess.CSRFToken != id.CSRFToken || sess.UADigest != privacy.UADigest(testUA) || !sess.CreatedAt.Equal(f.now) || sess.SessionVersion != 3 {
		t.Fatalf("会话内容不符合预期：%+v", sess)
	}
	if f.sessions.ttl[key] != SessionIdleTimeout {
		t.Fatalf("新会话的有效期应为空闲超时 2 小时，得到 %v", f.sessions.ttl[key])
	}
	if _, ok := f.counter.m["u:alice"]; ok {
		t.Fatal("登录成功后应清除该用户名的失败计数")
	}
	if len(f.accounts.marked) != 1 {
		t.Fatal("应记录登录时间（unused 状态由仓储改为 active）")
	}
	l := f.accounts.lastLog()
	if l.Result != LoginSuccess || l.AdminID == nil || *l.AdminID != 7 || l.IPPrefix != "203.0.0.0/16" || l.UsernameTried != "alice" {
		t.Fatalf("登录记录不符合预期：%+v", l)
	}
}

func TestLogin_UnknownUserAndWrongPasswordAreIndistinguishable(t *testing.T) {
	f := newFixture(activeAdmin())

	_, errWrong := login(f, "alice", "wrong-password", "203.0.113.9")
	_, errUnknown := login(f, "nobody", "wrong-password", "203.0.113.9")

	a, b := errs.From(errWrong), errs.From(errUnknown)
	if a.Code != "NEED_LOGIN" || a.Code != b.Code || a.Status != b.Status || a.Message != b.Message {
		t.Fatalf("两种失败的响应必须一致：%+v / %+v", a, b)
	}
	if f.hasher.equalizes != 1 {
		t.Fatal("用户名不存在时应执行一次同等代价的哈希计算")
	}
	if l := f.accounts.logs[0]; l.Result != LoginBadPassword || l.AdminID == nil {
		t.Fatalf("密码错误的记录应关联管理员：%+v", l)
	}
	if l := f.accounts.logs[1]; l.Result != LoginBadPassword || l.AdminID != nil || l.UsernameTried != "nobody" {
		t.Fatalf("用户名不存在的记录不关联管理员：%+v", l)
	}
	if f.counter.m["u:alice"].n != 1 || f.counter.m["ip:203.0.113.9"].n != 2 {
		t.Fatalf("失败计数应同时累计用户名与 IP：%+v", f.counter.m)
	}
}

func TestLogin_LocksUsernameAfterFiveFailures(t *testing.T) {
	f := newFixture(activeAdmin())
	for i := range maxUserFailures {
		_, err := login(f, "alice", "wrong-password", "203.0.113.9")
		assertBusinessError(t, err, errs.ErrNeedLogin)
		if i == maxUserFailures-1 && f.hasher.verifies != maxUserFailures {
			t.Fatalf("前 5 次都应校验密码，实际 %d 次", f.hasher.verifies)
		}
	}

	// 第 6 次即使密码正确也被拒绝，且不再校验密码
	_, err := login(f, "alice", "right-password", "198.51.100.1")
	assertBusinessError(t, err, errs.ErrRateLimited)
	if errs.RetryAfter(err) != failureWindow {
		t.Fatalf("应带上剩余锁定时间，得到 %v", errs.RetryAfter(err))
	}
	if f.hasher.verifies != maxUserFailures {
		t.Fatal("锁定期间不应再校验密码")
	}
	if l := f.accounts.lastLog(); l.Result != LoginLocked {
		t.Fatalf("应记录 locked，得到 %+v", l)
	}
	if len(f.sessions.m) != 0 {
		t.Fatal("锁定时不能创建会话")
	}
}

func TestLogin_LocksIPAfterTwentyFailures(t *testing.T) {
	f := newFixture(activeAdmin())
	for i := range maxIPFailures {
		// 换不同用户名，避免先触发用户名锁定
		_, err := login(f, "user"+strings.Repeat("x", i%3)+string(rune('a'+i)), "pw-wrong-1", "203.0.113.9")
		assertBusinessError(t, err, errs.ErrNeedLogin)
	}
	_, err := login(f, "alice", "right-password", "203.0.113.9")
	assertBusinessError(t, err, errs.ErrRateLimited)

	// 其他 IP 不受影响
	if _, err := login(f, "alice", "right-password", "198.51.100.1"); err != nil {
		t.Fatalf("其他 IP 应能正常登录：%v", err)
	}
}

func TestLogin_IPv6CountedPerSlash64(t *testing.T) {
	f := newFixture(activeAdmin())
	for i := range maxIPFailures {
		// 同一 /64 内每次换一个地址
		ip := fmt.Sprintf("2001:db8:aa:bb::%x", i+1)
		_, err := login(f, fmt.Sprintf("user%02d", i), "pw-wrong-1", ip)
		assertBusinessError(t, err, errs.ErrNeedLogin)
	}
	_, err := login(f, "alice", "right-password", "2001:db8:aa:bb:ffff::1")
	assertBusinessError(t, err, errs.ErrRateLimited)

	if _, err := login(f, "alice", "right-password", "2001:db8:aa:cc::1"); err != nil {
		t.Fatalf("其他 /64 网段不受影响：%v", err)
	}
}

func TestLimiterIP(t *testing.T) {
	tests := map[string]string{
		"203.0.113.9":         "203.0.113.9",
		"::ffff:203.0.113.9":  "203.0.113.9",
		"2001:db8:aa:bb:1::2": "2001:db8:aa:bb::/64",
		"fe80::1%eth0":        "fe80::/64",
		"not-an-ip":           "not-an-ip",
	}
	for in, want := range tests {
		if got := limiterIP(in); got != want {
			t.Errorf("limiterIP(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestLogin_FailureCountedEvenIfClientDisconnects(t *testing.T) {
	f := newFixture(activeAdmin())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟客户端已断开、请求 ctx 已取消

	_, err := f.svc.Login(ctx, LoginInput{Username: "alice", Password: "wrong-password", ClientIP: "203.0.113.9", UserAgent: testUA})
	assertBusinessError(t, err, errs.ErrNeedLogin)
	if f.counter.m["u:alice"].n != 1 || len(f.accounts.logs) != 1 {
		t.Fatalf("断开的请求也要计入失败次数并写登录记录：%+v %d", f.counter.m, len(f.accounts.logs))
	}
}

func TestLogin_InactiveAccount(t *testing.T) {
	a := activeAdmin()
	a.Status = StatusInactive
	f := newFixture(a)

	_, err := login(f, "alice", "right-password", "203.0.113.9")
	assertBusinessError(t, err, errs.ErrForbidden)
	if len(f.sessions.m) != 0 {
		t.Fatal("停用账号不能创建会话")
	}
	if l := f.accounts.lastLog(); l.Result != LoginInactive {
		t.Fatalf("应记录 inactive，得到 %+v", l)
	}
	if f.counter.m["u:alice"].n != 0 {
		t.Fatal("密码正确时不累计失败次数")
	}
}

func TestLogin_RoleDisabled(t *testing.T) {
	a := activeAdmin()
	a.Role.Active = false
	f := newFixture(a)

	_, err := login(f, "alice", "right-password", "203.0.113.9")
	assertBusinessError(t, err, errs.ErrForbidden)
	if len(f.sessions.m) != 0 || f.accounts.lastLog().Result != LoginInactive {
		t.Fatal("所属角色停用时不能创建会话，并记录 inactive")
	}
}

func TestLogin_UnusedAccountCanLogin(t *testing.T) {
	a := activeAdmin()
	a.Status = StatusUnused
	f := newFixture(a)
	if _, err := login(f, "alice", "right-password", "203.0.113.9"); err != nil {
		t.Fatalf("未登录过的账号应能登录：%v", err)
	}
	if len(f.accounts.marked) != 1 {
		t.Fatal("首次登录应记录登录时间")
	}
}

func TestLogin_RehashesOutdatedHash(t *testing.T) {
	f := newFixture(activeAdmin())
	f.hasher.needRehash = true
	if _, err := login(f, "alice", "right-password", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if f.accounts.updatedHash[7] != "hash2:right-password" {
		t.Fatalf("参数过旧时应重新哈希保存，得到 %q", f.accounts.updatedHash[7])
	}
}

func TestLogin_InvalidUsernameSkipsLookup(t *testing.T) {
	f := newFixture(activeAdmin())
	_, err := login(f, "bad\x00name 中文", "whatever-pw", "203.0.113.9")
	assertBusinessError(t, err, errs.ErrNeedLogin)
	if f.accounts.lookups != 0 {
		t.Fatal("格式不合法的用户名不应查库")
	}
	if f.hasher.equalizes != 1 {
		t.Fatal("仍应执行同等代价的哈希计算")
	}
	if got := f.accounts.lastLog().UsernameTried; got != "badname 中文" {
		t.Fatalf("登录记录中的用户名应去掉控制字符，得到 %q", got)
	}
}

// ---------- 会话 ----------

func loggedIn(t *testing.T, f *fixture) LoginResult {
	t.Helper()
	res, err := login(f, "alice", "right-password", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAuthenticate_Valid(t *testing.T) {
	f := newFixture(activeAdmin())
	res := loggedIn(t, f)
	f.sessions.ttl[res.Identity.SessionKey] = time.Minute // 模拟已过去一段时间

	id, err := f.svc.Authenticate(context.Background(), res.Token, testUA)
	if err != nil {
		t.Fatal(err)
	}
	want := res.Identity
	if id.AdminID != want.AdminID || id.Role != want.Role || id.CSRFToken != want.CSRFToken || id.SessionKey != want.SessionKey ||
		!id.Can("dramas.view") {
		t.Fatalf("身份应与登录时一致：%+v / %+v", id, want)
	}
	if f.sessions.ttl[id.SessionKey] != SessionIdleTimeout {
		t.Fatal("每次请求应把空闲过期时间重置为 2 小时")
	}
}

func TestAuthenticate_RoleAndPermissionChangesTakeEffectImmediately(t *testing.T) {
	f := newFixture(activeAdmin())
	res := loggedIn(t, f)
	a := f.accounts.byName["alice"]
	a.Role = RoleRef{ID: 4, Code: "readonly", Name: "只读", Active: true}
	a.Permissions = []string{"dramas.view"}
	f.accounts.byName["alice"] = a

	id, err := f.svc.Authenticate(context.Background(), res.Token, testUA)
	if err != nil || id.Role.Code != "readonly" || id.Can("dramas.edit") || !id.Can("dramas.view") {
		t.Fatalf("改角色或权限后已有会话应立即按新权限判定：%+v %v", id, err)
	}
}

func TestIdentity_SuperRoleCanEverything(t *testing.T) {
	id := NewIdentity(1, "root", RoleRef{ID: 1, Code: "superadmin", IsSuper: true, Active: true}, nil)
	if !id.Can("anything.edit") || len(id.Permissions()) != 0 {
		t.Fatal("超级角色拥有全部权限点，且不需要逐条授予")
	}
	if NewIdentity(2, "ro", RoleRef{ID: 4, Code: "readonly", Active: true}, []string{"dramas.view"}).Can("dramas.edit") {
		t.Fatal("普通角色只有被授予的权限点")
	}
}

func TestAuthenticate_Rejections(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(f *fixture, res LoginResult) (token, ua string)
		wantDrop bool
	}{
		{"令牌格式不对", func(f *fixture, res LoginResult) (string, string) { return "short", testUA }, false},
		{"会话不存在", func(f *fixture, res LoginResult) (string, string) {
			delete(f.sessions.m, res.Identity.SessionKey)
			return res.Token, testUA
		}, false},
		{"超过绝对时长 7 天", func(f *fixture, res LoginResult) (string, string) {
			f.now = f.now.Add(SessionAbsoluteTimeout + time.Second)
			return res.Token, testUA
		}, true},
		{"浏览器 UA 变化", func(f *fixture, res LoginResult) (string, string) { return res.Token, "curl/8.0" }, true},
		{"账号被停用", func(f *fixture, res LoginResult) (string, string) {
			a := f.accounts.byName["alice"]
			a.Status = StatusInactive
			f.accounts.byName["alice"] = a
			return res.Token, testUA
		}, true},
		{"账号已删除", func(f *fixture, res LoginResult) (string, string) {
			delete(f.accounts.byName, "alice")
			return res.Token, testUA
		}, true},
		{"所属角色被停用", func(f *fixture, res LoginResult) (string, string) {
			a := f.accounts.byName["alice"]
			a.Role.Active = false
			f.accounts.byName["alice"] = a
			return res.Token, testUA
		}, true},
		{"被重置密码或强制下线（会话版本变化）", func(f *fixture, res LoginResult) (string, string) {
			a := f.accounts.byName["alice"]
			a.SessionVersion++
			f.accounts.byName["alice"] = a
			return res.Token, testUA
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(activeAdmin())
			res := loggedIn(t, f)
			token, ua := tt.mutate(f, res)

			_, err := f.svc.Authenticate(context.Background(), token, ua)
			assertBusinessError(t, err, errs.ErrNeedLogin)
			_, stillThere := f.sessions.m[res.Identity.SessionKey]
			if tt.wantDrop && stillThere {
				t.Fatal("失效的会话应被删除")
			}
		})
	}
}

func TestLogout(t *testing.T) {
	f := newFixture(activeAdmin())
	res := loggedIn(t, f)
	if err := f.svc.Logout(context.Background(), res.Identity.SessionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(context.Background(), res.Token, testUA); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("登出后会话应失效，得到 %v", err)
	}
}

// ---------- 二次验证登录（S2-6）----------

func issueTicket(t *testing.T, f *fixture) string {
	t.Helper()
	res, err := login(f, "alice", "right-password", "203.0.113.9")
	if err != nil {
		t.Fatalf("第一步登录应成功：%v", err)
	}
	if !res.MFARequired || res.MFATicket == "" || res.Token != "" || len(res.MFAMethods) == 0 {
		t.Fatalf("启用二次验证的账号第一步应返回挑战而非会话：%+v", res)
	}
	return res.MFATicket
}

func loginMFA(f *fixture, ticket, method, code string) (LoginResult, error) {
	return f.svc.LoginMFA(context.Background(), LoginMFAInput{
		Ticket: ticket, Method: method, Code: code, ClientIP: "203.0.113.9", UserAgent: testUA,
	})
}

func TestLogin_MFARequiredIssuesTicketNotSession(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)

	if f.tickets.m[ticket] != 7 {
		t.Fatalf("票据应映射到该管理员：%+v", f.tickets.m)
	}
	if len(f.sessions.m) != 0 {
		t.Fatal("第一步不能创建会话")
	}
	// 第一步不记 success（登录尚未完成）
	for _, l := range f.accounts.logs {
		if l.Result == LoginSuccess {
			t.Fatal("二次验证未完成不应记 success")
		}
	}
}

func TestLogin_MFANotEnforcedWhenUnconfigured(t *testing.T) {
	// 未注入 WithMFA（本地未配置 ADMIN_TOTP_KEKS）：即便账号 MFAEnabled 也直接完成登录
	f := newFixture(mfaAdmin())
	res, err := login(f, "alice", "right-password", "203.0.113.9")
	if err != nil {
		t.Fatalf("未启用二次验证时应直接登录：%v", err)
	}
	if res.MFARequired || res.Token == "" {
		t.Fatalf("应直接返回会话：%+v", res)
	}
}

func TestLoginMFA_Success(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	f.counter.m["u:alice"] = counterEntry{n: 2, ttl: time.Minute} // 第一步之前的失败残留
	ticket := issueTicket(t, f)
	f.verifier.validCode[7] = "123456"

	res, err := loginMFA(f, ticket, "totp", "123456")
	if err != nil {
		t.Fatalf("验证码正确应登录成功：%v", err)
	}
	if res.MFARequired || res.Token == "" || res.Identity.AdminID != 7 {
		t.Fatalf("应返回会话：%+v", res)
	}
	if _, ok := f.tickets.m[ticket]; ok {
		t.Fatal("成功后票据应一次性删除")
	}
	if len(f.sessions.m) != 1 {
		t.Fatal("应创建一个会话")
	}
	if _, ok := f.counter.m["u:alice"]; ok {
		t.Fatal("登录完成应清除失败计数")
	}
	if l := f.accounts.lastLog(); l.Result != LoginSuccess || l.AdminID == nil || *l.AdminID != 7 {
		t.Fatalf("应记 success：%+v", l)
	}
	// 返回的令牌可用于后续鉴权
	if _, err := f.svc.Authenticate(context.Background(), res.Token, testUA); err != nil {
		t.Fatalf("新会话应可鉴权：%v", err)
	}
}

func TestLoginMFA_WrongCodeKeepsTicketAndCounts(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)
	f.verifier.validCode[7] = "123456"

	_, err := loginMFA(f, ticket, "totp", "000000")
	assertBusinessError(t, err, errs.ErrBadRequest)
	if _, ok := f.tickets.m[ticket]; !ok {
		t.Fatal("验证码错误应保留票据，允许重试")
	}
	if len(f.sessions.m) != 0 {
		t.Fatal("验证码错误不能创建会话")
	}
	if f.counter.m["u:alice"].n != 1 || f.counter.m["ip:203.0.113.9"].n != 1 {
		t.Fatalf("验证码错误应计入失败锁定：%+v", f.counter.m)
	}
	if l := f.accounts.lastLog(); l.Result != LoginMFAFailed || l.AdminID == nil || *l.AdminID != 7 {
		t.Fatalf("应记 mfa_failed：%+v", l)
	}
}

func TestLoginMFA_InvalidTicket(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	_, err := loginMFA(f, "no-such-ticket", "totp", "123456")
	assertBusinessError(t, err, errs.ErrNeedLogin)
	if f.verifier.calls != 0 {
		t.Fatal("票据无效时不应校验验证码")
	}
}

func TestLoginMFA_UnsupportedMethod(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)
	_, err := loginMFA(f, ticket, "passkey", "123456")
	assertBusinessError(t, err, errs.ErrBadRequest)
	if f.verifier.calls != 0 {
		t.Fatal("不支持的方式应在校验前拒绝")
	}
}

func TestLoginMFA_AccountDisabledAfterChallenge(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)
	f.verifier.validCode[7] = "123456"
	a := f.accounts.byName["alice"]
	a.Status = StatusInactive
	f.accounts.byName["alice"] = a

	_, err := loginMFA(f, ticket, "totp", "123456")
	assertBusinessError(t, err, errs.ErrForbidden)
	if _, ok := f.tickets.m[ticket]; ok {
		t.Fatal("账号停用应作废票据")
	}
	if len(f.sessions.m) != 0 {
		t.Fatal("停用账号不能创建会话")
	}
	if l := f.accounts.lastLog(); l.Result != LoginInactive {
		t.Fatalf("应记 inactive：%+v", l)
	}
}

func TestLoginMFA_LocksAfterRepeatedWrongCodes(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)
	f.verifier.validCode[7] = "123456"

	for range maxUserFailures {
		_, err := loginMFA(f, ticket, "totp", "000000")
		assertBusinessError(t, err, errs.ErrBadRequest)
	}
	// 锁定后即使验证码正确也被拒绝，且不再校验
	before := f.verifier.calls
	_, err := loginMFA(f, ticket, "totp", "123456")
	assertBusinessError(t, err, errs.ErrRateLimited)
	if errs.RetryAfter(err) != failureWindow {
		t.Fatalf("应带剩余锁定时间，得到 %v", errs.RetryAfter(err))
	}
	if f.verifier.calls != before {
		t.Fatal("锁定期间不应再校验验证码")
	}
	if len(f.sessions.m) != 0 {
		t.Fatal("锁定时不能创建会话")
	}
}

// ---------- 高敏操作重新验证（S2-6）----------

func mfaIdentity() Identity {
	id := NewIdentity(7, "alice", operatorRole(), []string{"dramas.edit"})
	id.MFAEnabled = true
	id.SessionKey = "sess-key-7"
	return id
}

func TestReauth_Success(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	f.verifier.validCode[7] = "123456"
	f.counter.m["u:alice"] = counterEntry{n: 2, ttl: time.Minute}

	if err := f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "123456", nil, "203.0.113.9"); err != nil {
		t.Fatalf("验证码正确应通过：%v", err)
	}
	if !f.tickets.reauthed["sess-key-7"] {
		t.Fatal("通过后应标记该会话最近重新验证")
	}
	if f.tickets.reauthPrefix["sess-key-7"] != "203.0.0.0/16" {
		t.Fatalf("应记录重新验证时的 IP 网段，得到 %q", f.tickets.reauthPrefix["sess-key-7"])
	}
	if _, ok := f.counter.m["u:alice"]; ok {
		t.Fatal("通过后应清除失败计数")
	}
}

func TestReauth_WrongCode(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	f.verifier.validCode[7] = "123456"

	err := f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "000000", nil, "203.0.113.9")
	assertBusinessError(t, err, errs.ErrBadRequest)
	if f.tickets.reauthed["sess-key-7"] {
		t.Fatal("验证码错误不应标记重新验证")
	}
	if f.counter.m["u:alice"].n != 1 {
		t.Fatalf("验证码错误应计入失败锁定：%+v", f.counter.m)
	}
}

func TestReauth_NoFactor(t *testing.T) {
	f := newMFALoginFixture(activeAdmin()) // 未绑定二次验证
	id := mfaIdentity()
	id.MFAEnabled = false

	err := f.svc.Reauth(context.Background(), id, "totp", "123456", nil, "203.0.113.9")
	assertBusinessError(t, err, errs.ErrBadRequest)
	if f.verifier.calls != 0 {
		t.Fatal("未绑定二次验证时不应校验验证码")
	}
}

func TestReauth_Unavailable(t *testing.T) {
	f := newFixture(mfaAdmin()) // 未注入 WithMFA
	err := f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "123456", nil, "203.0.113.9")
	assertBusinessError(t, err, errs.ErrBadRequest)
}

func TestReauth_UnsupportedMethod(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	err := f.svc.Reauth(context.Background(), mfaIdentity(), "sms", "123456", nil, "203.0.113.9")
	assertBusinessError(t, err, errs.ErrBadRequest)
	if f.verifier.calls != 0 {
		t.Fatal("不支持的方式应在校验前拒绝")
	}
}

func TestReauth_LockedAfterFailures(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	f.verifier.validCode[7] = "123456"
	for range maxUserFailures {
		assertBusinessError(t, f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "000000", nil, "203.0.113.9"), errs.ErrBadRequest)
	}
	// 锁定后即使验证码正确也被拒绝，且不再校验
	before := f.verifier.calls
	err := f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "123456", nil, "203.0.113.9")
	assertBusinessError(t, err, errs.ErrRateLimited)
	if f.verifier.calls != before {
		t.Fatal("锁定期间不应再校验验证码")
	}
	if f.tickets.reauthed["sess-key-7"] {
		t.Fatal("锁定时不应标记重新验证")
	}
}

func TestReauth_MarksRequestIPPrefix(t *testing.T) {
	// 从不同网段重新验证，标记应记下当前请求网段（IPv6 按 /32）
	f := newMFALoginFixture(mfaAdmin())
	f.verifier.validCode[7] = "123456"
	if err := f.svc.Reauth(context.Background(), mfaIdentity(), "totp", "123456", nil, "2001:db8:aa:bb::1"); err != nil {
		t.Fatal(err)
	}
	if got := f.tickets.reauthPrefix["sess-key-7"]; got != "2001:db8::/32" {
		t.Fatalf("应记下当前请求网段，得到 %q", got)
	}
}

func TestLoginMFA_MarksReauthWindow(t *testing.T) {
	f := newMFALoginFixture(mfaAdmin())
	ticket := issueTicket(t, f)
	f.verifier.validCode[7] = "123456"

	res, err := loginMFA(f, ticket, "totp", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if !f.tickets.reauthed[res.Identity.SessionKey] {
		t.Fatal("二次验证登录成功后应把该会话标记为最近重新验证（给高敏操作宽限）")
	}
}

func TestLogin_NoMFADoesNotMarkReauth(t *testing.T) {
	f := newMFALoginFixture(activeAdmin()) // 未绑定二次验证
	res, err := login(f, "alice", "right-password", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if f.tickets.reauthed[res.Identity.SessionKey] {
		t.Fatal("未绑定二次验证的账号登录不应标记重新验证窗口")
	}
}
