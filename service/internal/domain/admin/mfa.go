package admin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"xsite/service/internal/crypto/envelope"
	"xsite/service/internal/errs"
)

// 后台二次验证（S2-6，docs/03 第 2.3 节、docs/06 第 33 节）。本切片做 TOTP 自助绑定：
// setup 生成种子并暂存、verify 校验一次性验证码后信封加密落库。登录强制校验、reauth、Passkey 随后续切片。

const (
	totpIssuer     = "x-site 运营后台"
	totpPendingTTL = 10 * time.Minute
	totpPeriod     = 30 // 秒
	totpSkew       = 1  // 允许前后各一个时间片的时钟漂移
)

// MFARepo 读写管理员的二次验证因子（TOTP 种子密文，base64 承载在 totp_secret_enc）。
type MFARepo interface {
	SetTOTP(ctx context.Context, adminID int64, enc []byte) error
	GetTOTPSecret(ctx context.Context, adminID int64) ([]byte, error)
	ClearTOTP(ctx context.Context, adminID int64) error
}

// TOTPPendingStore 暂存「待确认的 TOTP 种子」（setup 与 verify 之间）。
type TOTPPendingStore interface {
	SetPendingTOTP(ctx context.Context, adminID int64, secret string, ttl time.Duration) error
	GetPendingTOTP(ctx context.Context, adminID int64) (secret string, found bool, err error)
	DelPendingTOTP(ctx context.Context, adminID int64) error
}

// MFAService 处理后台二次验证的自助绑定。keyring 为 nil（未配置 ADMIN_TOTP_KEKS）时 TOTP 不可用。
type MFAService struct {
	repo    MFARepo
	pending TOTPPendingStore
	keyring *envelope.Keyring
	now     func() time.Time
}

func NewMFAService(repo MFARepo, pending TOTPPendingStore, keyring *envelope.Keyring) *MFAService {
	return &MFAService{repo: repo, pending: pending, keyring: keyring, now: time.Now}
}

// TOTPSetup 是开始绑定 TOTP 的结果：OtpauthURL 生成二维码，Secret 供手动输入。
type TOTPSetup struct {
	Secret     string
	OtpauthURL string
}

var (
	errTOTPUnavailable = errs.ErrUnavailable.WithMessage("后台未配置二次验证密钥（ADMIN_TOTP_KEKS）")
	errTOTPCode        = errs.ErrBadRequest.WithMessage("验证码不正确，请检查认证器时间后重试")
	errTOTPNoPending   = errs.ErrBadRequest.WithMessage("请先开始绑定再输入验证码")
)

// BeginTOTP 为当前管理员生成新的 TOTP 种子并暂存（尚未写库、尚未启用）。
func (s *MFAService) BeginTOTP(ctx context.Context, adminID int64, username string) (TOTPSetup, error) {
	if s.keyring == nil {
		return TOTPSetup{}, errTOTPUnavailable
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: username})
	if err != nil {
		return TOTPSetup{}, fmt.Errorf("生成 TOTP 种子: %w", err)
	}
	if err := s.pending.SetPendingTOTP(ctx, adminID, key.Secret(), totpPendingTTL); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: key.Secret(), OtpauthURL: key.String()}, nil
}

// ConfirmTOTP 校验用户输入的验证码，通过后信封加密种子落库、启用 TOTP。
func (s *MFAService) ConfirmTOTP(ctx context.Context, adminID int64, code string) error {
	if s.keyring == nil {
		return errTOTPUnavailable
	}
	secret, ok, err := s.pending.GetPendingTOTP(ctx, adminID)
	if err != nil {
		return err
	}
	if !ok {
		return errTOTPNoPending
	}
	valid, err := totp.ValidateCustom(code, secret, s.now().UTC(), totp.ValidateOpts{
		Period: totpPeriod, Skew: totpSkew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !valid {
		return errTOTPCode
	}
	if err := s.repo.SetTOTP(ctx, adminID, s.keyring.Seal(totpAAD(adminID), []byte(secret))); err != nil {
		return err
	}
	_ = s.pending.DelPendingTOTP(ctx, adminID) // 落库后清理暂存，失败不影响结果
	return nil
}

// VerifyTOTP 校验某管理员已绑定的 TOTP 验证码（登录第二步、高敏重验共用，实现 TOTPVerifier）。
// 账号未绑定返回 (false, nil)；密钥环未配置或密文损坏返回错误（调用方按失败处理，不泄露原因）。
func (s *MFAService) VerifyTOTP(ctx context.Context, adminID int64, code string) (bool, error) {
	if s.keyring == nil {
		return false, errTOTPUnavailable
	}
	enc, err := s.repo.GetTOTPSecret(ctx, adminID)
	if err != nil {
		return false, err
	}
	if len(enc) == 0 {
		return false, nil // 未绑定 TOTP
	}
	secret, err := s.keyring.Open(totpAAD(adminID), enc)
	if err != nil {
		return false, fmt.Errorf("解开 TOTP 种子: %w", err)
	}
	valid, err := totp.ValidateCustom(code, string(secret), s.now().UTC(), totp.ValidateOpts{
		Period: totpPeriod, Skew: totpSkew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false, nil // 验证码格式不对等，按无效处理
	}
	return valid, nil
}

// totpAAD 把密文绑定到「表|列|管理员」，防止挪用到其他列或账号。
func totpAAD(adminID int64) []byte {
	return []byte("admin_user|totp_secret_enc|" + strconv.FormatInt(adminID, 10))
}
