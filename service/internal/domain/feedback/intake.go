package feedback

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

// RateCounter 是带滑动窗口的计数器（由 store/cache.Counter 实现）。
type RateCounter interface {
	Get(ctx context.Context, key string) (int64, time.Duration, error)
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
}

// 开放提交接口的突发上限：同一个 Key 每分钟最多这么多条，日上限在 Key 上单独配置。
const intakeBurstPerMinute = 60

// API Key 明文形如 fbk_<22 位 base64url>，前缀便于在日志与代码里识别。
const apiKeyPrefix = "fbk_"

var errBadAPIKey = errs.ErrSignatureInvalid.WithMessage("API Key 无效或已停用")

// IntakeService 处理外部网站经 API Key 提交的反馈，以及后台对 Key 的管理。
type IntakeService struct {
	repo    APIKeyRepo
	limiter RateCounter
	now     func() time.Time
}

func NewIntakeService(repo APIKeyRepo, limiter RateCounter) *IntakeService {
	return &IntakeService{repo: repo, limiter: limiter, now: time.Now}
}

// Submit 校验 Key 与配额后写入一条反馈，返回可展示给提交者的编号。
func (s *IntakeService) Submit(ctx context.Context, rawKey string, in IntakeInput) (IntakeResult, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return IntakeResult{}, errBadAPIKey
	}
	key, projectStatus, err := s.repo.APIKeyByHash(ctx, HashAPIKey(rawKey))
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return IntakeResult{}, errBadAPIKey
		}
		return IntakeResult{}, err
	}
	if !key.Usable() {
		return IntakeResult{}, errBadAPIKey
	}
	// 项目必须处于可接收状态，否则提交会堆进一个没人看的项目
	if !(Project{Status: projectStatus}).AcceptsFeedback() {
		return IntakeResult{}, errs.ErrConflict.WithMessage("该项目暂不接受新反馈")
	}
	if err := s.checkOrigin(key, in.Origin); err != nil {
		return IntakeResult{}, err
	}
	in, err = validateIntake(in)
	if err != nil {
		return IntakeResult{}, err
	}
	if err := s.checkQuota(ctx, key); err != nil {
		return IntakeResult{}, err
	}

	fb, err := s.repo.CreateFeedbackFromAPI(ctx, key, in)
	if err != nil {
		return IntakeResult{}, err
	}
	return IntakeResult{Key: fb.Key, Number: fb.Number, ProjectSlug: key.ProjectSlug, Status: fb.Status}, nil
}

// checkOrigin 在 Key 配了来源白名单时校验浏览器来源。
// 服务端对服务端调用不带 Origin：这种情况下配了白名单就必须拒绝，否则白名单可被绕过。
func (s *IntakeService) checkOrigin(key APIKey, origin string) error {
	if len(key.AllowedOrigins) == 0 {
		return nil
	}
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin == "" {
		return errs.ErrForbidden.WithMessage("这个 Key 限定了提交来源，请从已登记的站点提交")
	}
	for _, allowed := range key.AllowedOrigins {
		if origin == allowed {
			return nil
		}
	}
	return errs.ErrForbidden.WithMessage("提交来源未登记，请联系管理员添加")
}

// checkQuota 检查每分钟突发与每日上限。计数器不可用时放行（不因缓存故障拒收反馈）。
func (s *IntakeService) checkQuota(ctx context.Context, key APIKey) error {
	id := strconv.FormatInt(key.ID, 10)
	minuteKey := "m:" + id
	if n, err := s.limiter.Incr(ctx, minuteKey, time.Minute); err == nil && n > intakeBurstPerMinute {
		return errs.WithRetryAfter(errs.ErrRateLimited.WithMessage("提交太频繁，请稍后再试"), time.Minute)
	}
	dayKey := "d:" + id + ":" + s.now().UTC().Format("20060102")
	if n, err := s.limiter.Incr(ctx, dayKey, 24*time.Hour); err == nil && n > int64(key.DailyLimit) {
		return errs.WithRetryAfter(errs.ErrRateLimited.WithMessage("今天的提交量已达上限，请明天再试"), time.Hour)
	}
	return nil
}

// ---------- 后台管理 ----------

// ListAPIKeys 返回 API Key 列表。
func (s *IntakeService) ListAPIKeys(ctx context.Context, projectID int64, page, pageSize int) (Page[APIKey], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[APIKey]{}, err
	}
	list, total, err := s.repo.ListAPIKeys(ctx, projectID, p, size)
	if err != nil {
		return Page[APIKey]{}, err
	}
	return Page[APIKey]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// CreateAPIKey 生成一个 Key。明文只在这里返回一次，库里只存哈希。
func (s *IntakeService) CreateAPIKey(ctx context.Context, in APIKeyInput) (APIKey, string, error) {
	in, err := validateAPIKeyInput(in)
	if err != nil {
		return APIKey{}, "", err
	}
	raw, err := NewAPIKey()
	if err != nil {
		return APIKey{}, "", err
	}
	// 提示串取明文末 6 位：前缀固定，末尾更有辨识度
	hint := raw[len(raw)-6:]
	key, err := s.repo.CreateAPIKey(ctx, HashAPIKey(raw), hint, in)
	if err != nil {
		return APIKey{}, "", err
	}
	return key, raw, nil
}

// UpdateAPIKey 整体保存 Key 的配置（不能改所属项目，避免历史反馈归属错乱）。
func (s *IntakeService) UpdateAPIKey(ctx context.Context, id int64, in APIKeyInput) (before, after APIKey, err error) {
	before, err = s.repo.APIKey(ctx, id)
	if err != nil {
		return APIKey{}, APIKey{}, err
	}
	in.ProjectID = before.ProjectID
	in, err = validateAPIKeyInput(in)
	if err != nil {
		return APIKey{}, APIKey{}, err
	}
	after, err = s.repo.UpdateAPIKey(ctx, id, in)
	return before, after, err
}

// RevokeAPIKey 作废 Key。
func (s *IntakeService) RevokeAPIKey(ctx context.Context, id int64) (APIKey, error) {
	return s.repo.RevokeAPIKey(ctx, id)
}

// NewAPIKey 生成 API Key 明文。
func NewAPIKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 API Key: %w", err)
	}
	return apiKeyPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashAPIKey 返回 API Key 的存储哈希。Key 是高熵随机串，用 SHA-256 即可。
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}
