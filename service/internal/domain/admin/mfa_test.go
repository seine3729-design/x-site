package admin

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"xsite/service/internal/crypto/envelope"
	"xsite/service/internal/errs"
)

// ---------- MFA 测试替身 ----------

type fakeMFARepo struct {
	enc     map[int64][]byte
	cleared []int64
}

func newFakeMFARepo() *fakeMFARepo { return &fakeMFARepo{enc: map[int64][]byte{}} }

func (f *fakeMFARepo) SetTOTP(_ context.Context, adminID int64, enc []byte) error {
	f.enc[adminID] = enc
	return nil
}

func (f *fakeMFARepo) GetTOTPSecret(_ context.Context, adminID int64) ([]byte, error) {
	return f.enc[adminID], nil
}

func (f *fakeMFARepo) ClearTOTP(_ context.Context, adminID int64) error {
	delete(f.enc, adminID)
	f.cleared = append(f.cleared, adminID)
	return nil
}

type fakePendingStore struct {
	m   map[int64]string
	ttl map[int64]time.Duration
}

func newFakePendingStore() *fakePendingStore {
	return &fakePendingStore{m: map[int64]string{}, ttl: map[int64]time.Duration{}}
}

func (f *fakePendingStore) SetPendingTOTP(_ context.Context, adminID int64, secret string, ttl time.Duration) error {
	f.m[adminID] = secret
	f.ttl[adminID] = ttl
	return nil
}

func (f *fakePendingStore) GetPendingTOTP(_ context.Context, adminID int64) (string, bool, error) {
	s, ok := f.m[adminID]
	return s, ok, nil
}

func (f *fakePendingStore) DelPendingTOTP(_ context.Context, adminID int64) error {
	delete(f.m, adminID)
	return nil
}

// testKeyring 构造一把用于测试的 TOTP 种子 KEK（secret 至少 32 字符）。
func testKeyring(t *testing.T) *envelope.Keyring {
	t.Helper()
	kr, err := envelope.ParseKeyring("dev1:0123456789abcdef0123456789abcdef", "xsite-admin-totp/v1")
	if err != nil {
		t.Fatalf("构造测试密钥环：%v", err)
	}
	return kr
}

type mfaFixture struct {
	svc     *MFAService
	repo    *fakeMFARepo
	pending *fakePendingStore
	now     time.Time
}

func newMFAFixture(t *testing.T, keyring *envelope.Keyring) *mfaFixture {
	f := &mfaFixture{
		repo:    newFakeMFARepo(),
		pending: newFakePendingStore(),
		now:     time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}
	f.svc = NewMFAService(f.repo, f.pending, keyring)
	f.svc.now = func() time.Time { return f.now }
	return f
}

// ---------- 自助绑定 ----------

func TestMFA_TOTPBindRoundTrip(t *testing.T) {
	f := newMFAFixture(t, testKeyring(t))

	setup, err := f.svc.BeginTOTP(context.Background(), 7, "alice")
	if err != nil {
		t.Fatalf("开始绑定应成功：%v", err)
	}
	if setup.Secret == "" || setup.OtpauthURL == "" {
		t.Fatalf("应返回种子与 otpauth URL：%+v", setup)
	}
	if got := f.pending.m[7]; got != setup.Secret {
		t.Fatalf("种子应暂存待确认，暂存=%q 返回=%q", got, setup.Secret)
	}
	if f.pending.ttl[7] != totpPendingTTL {
		t.Fatalf("暂存有效期应为 %v，得到 %v", totpPendingTTL, f.pending.ttl[7])
	}
	if _, ok := f.repo.enc[7]; ok {
		t.Fatal("未确认前不应写库启用")
	}

	code, err := totp.GenerateCode(setup.Secret, f.now)
	if err != nil {
		t.Fatalf("计算验证码：%v", err)
	}
	if err := f.svc.ConfirmTOTP(context.Background(), 7, code); err != nil {
		t.Fatalf("验证码正确时确认应成功：%v", err)
	}

	enc, ok := f.repo.enc[7]
	if !ok {
		t.Fatal("确认后应把种子信封加密落库")
	}
	plain, err := testKeyring(t).Open(totpAAD(7), enc)
	if err != nil || string(plain) != setup.Secret {
		t.Fatalf("落库密文应能用同一 AAD 解回原种子：%v / %q", err, plain)
	}
	if _, ok := f.pending.m[7]; ok {
		t.Fatal("确认后应清理暂存")
	}
}

func TestMFA_BeginTOTP_Unavailable(t *testing.T) {
	f := newMFAFixture(t, nil) // 未配置 ADMIN_TOTP_KEKS
	_, err := f.svc.BeginTOTP(context.Background(), 7, "alice")
	assertBusinessError(t, err, errs.ErrUnavailable)
}

func TestMFA_ConfirmTOTP_Unavailable(t *testing.T) {
	f := newMFAFixture(t, nil)
	err := f.svc.ConfirmTOTP(context.Background(), 7, "000000")
	assertBusinessError(t, err, errs.ErrUnavailable)
}

func TestMFA_ConfirmTOTP_NoPending(t *testing.T) {
	f := newMFAFixture(t, testKeyring(t))
	err := f.svc.ConfirmTOTP(context.Background(), 7, "000000")
	assertBusinessError(t, err, errs.ErrBadRequest)
}

func TestMFA_ConfirmTOTP_WrongCode(t *testing.T) {
	f := newMFAFixture(t, testKeyring(t))
	setup, err := f.svc.BeginTOTP(context.Background(), 7, "alice")
	if err != nil {
		t.Fatal(err)
	}
	// 取一个确定不等于当前有效码的错误码
	valid, _ := totp.GenerateCode(setup.Secret, f.now)
	wrong := "000000"
	if wrong == valid {
		wrong = "111111"
	}
	err = f.svc.ConfirmTOTP(context.Background(), 7, wrong)
	assertBusinessError(t, err, errs.ErrBadRequest)
	if _, ok := f.repo.enc[7]; ok {
		t.Fatal("验证码错误时不应写库")
	}
	if _, ok := f.pending.m[7]; !ok {
		t.Fatal("验证码错误时应保留暂存，便于重试")
	}
}

func TestMFA_VerifyTOTP(t *testing.T) {
	f := newMFAFixture(t, testKeyring(t))
	setup, err := f.svc.BeginTOTP(context.Background(), 7, "alice")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(setup.Secret, f.now)
	if err := f.svc.ConfirmTOTP(context.Background(), 7, code); err != nil {
		t.Fatal(err)
	}

	cur, _ := totp.GenerateCode(setup.Secret, f.now)
	if ok, err := f.svc.VerifyTOTP(context.Background(), 7, cur); err != nil || !ok {
		t.Fatalf("已绑定账号正确码应通过：ok=%v err=%v", ok, err)
	}

	wrong := "000000"
	if wrong == cur {
		wrong = "111111"
	}
	if ok, err := f.svc.VerifyTOTP(context.Background(), 7, wrong); err != nil || ok {
		t.Fatalf("错误码应不通过：ok=%v err=%v", ok, err)
	}

	if ok, err := f.svc.VerifyTOTP(context.Background(), 99, cur); err != nil || ok {
		t.Fatalf("未绑定账号应返回 (false, nil)：ok=%v err=%v", ok, err)
	}
}

func TestMFA_VerifyTOTP_Unavailable(t *testing.T) {
	f := newMFAFixture(t, nil)
	_, err := f.svc.VerifyTOTP(context.Background(), 7, "123456")
	assertBusinessError(t, err, errs.ErrUnavailable)
}
