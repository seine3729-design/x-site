package cache

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/billing"
)

const vipStatusTTL = 5 * time.Minute

// VIPStore 实现 billing.Cache：用户会员状态缓存 5 分钟，套餐按客户端缓存 60 秒。
// 缓存不可用时一律回源，不得因 Valkey 故障错误地放行或拒绝会员权益。
type VIPStore struct {
	client       valkey.Client
	statusPrefix string
	plans        *Store
}

func NewVIPStore(client valkey.Client, prefix string) *VIPStore {
	return &VIPStore{
		client:       client,
		statusPrefix: prefix,
		plans:        New(client, prefix+"plans:"),
	}
}

var _ billing.Cache = (*VIPStore)(nil)

func (s *VIPStore) GetVIPUntil(ctx context.Context, userID int64) (time.Time, bool) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(s.statusKey(userID)).Build()).AsBytes()
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			slog.WarnContext(ctx, "读取会员状态缓存失败，直接回源", "err", err)
		}
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil {
		slog.WarnContext(ctx, "会员状态缓存格式无效，直接回源", "err", err)
		return time.Time{}, false
	}
	return until.UTC(), true
}

func (s *VIPStore) SetVIPUntil(ctx context.Context, userID int64, until time.Time) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
	defer cancel()
	value := until.UTC().Format(time.RFC3339Nano)
	cmd := s.client.B().Set().Key(s.statusKey(userID)).Value(value).Px(jitter(vipStatusTTL)).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		slog.WarnContext(ctx, "写入会员状态缓存失败", "err", err)
	}
}

func (s *VIPStore) DeleteVIPUntil(ctx context.Context, userID int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.statusKey(userID)).Build()).Error(); err != nil {
		slog.WarnContext(ctx, "删除会员状态缓存失败", "err", err)
	}
}

func (s *VIPStore) GetOrLoadPlans(ctx context.Context, client string, load func(context.Context) ([]billing.Plan, error)) ([]billing.Plan, error) {
	var plans []billing.Plan
	err := s.plans.GetOrLoadJSON(ctx, client, time.Minute, &plans, func(ctx context.Context) (any, error) {
		loaded, err := load(ctx)
		if err != nil {
			return nil, err
		}
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	if plans == nil {
		plans = []billing.Plan{}
	}
	return plans, nil
}

func (s *VIPStore) InvalidatePlans(ctx context.Context) {
	for _, client := range []string{billing.ClientWebPC, billing.ClientWebMobile, billing.ClientAppOfficial, billing.ClientAppStore} {
		if err := s.plans.Delete(ctx, client); err != nil {
			slog.WarnContext(ctx, "删除会员套餐缓存失败", "err", err)
		}
	}
}

func (s *VIPStore) statusKey(userID int64) string {
	return s.statusPrefix + strconv.FormatInt(userID, 10)
}
