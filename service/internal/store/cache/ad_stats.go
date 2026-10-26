package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/ops"
)

// AdStats 是广告的事件去重、待落库计数与频控计数（实现 ops.AdStats，docs/specs/A3-4.md）。键（都带前缀）：
//   - dedup:<事件>:<广告>:<设备>：去重窗口，1 秒过期（SET NX PX）
//   - pending：待落库的累计（Hash，字段 <广告>:imp / <广告>:clk，HINCRBY）
//   - pending:<token>：worker 取出、正在落库的那一份，7 天后自动删除
//   - freq:<北京日期>:<设备>：设备当天对各广告的曝光次数（Hash，字段 <广告>），只记有频控的广告，25 小时过期
//
// 设备标识在 HTTP 层已做摘要，键里不出现原始设备 ID 或 IP。
type AdStats struct {
	client valkey.Client
	prefix string
}

func NewAdStats(client valkey.Client, prefix string) *AdStats {
	return &AdStats{client: client, prefix: prefix}
}

var _ ops.AdStats = (*AdStats)(nil)

const (
	adDedupWindow = time.Second
	adFreqTTL     = 25 * time.Hour // 覆盖一整个北京日期，之后换新键
)

func (s *AdStats) FirstEvent(ctx context.Context, device string, adID int64, kind ops.AdEventKind) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := s.prefix + "dedup:" + string(kind) + ":" + strconv.FormatInt(adID, 10) + ":" + device
	err := s.client.Do(ctx, s.client.B().Set().Key(key).Value("1").Nx().Px(adDedupWindow).Build()).Error()
	if valkey.IsValkeyNil(err) { // 键已存在：窗口内的重复事件
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("广告事件去重: %w", err)
	}
	return true, nil
}

func (s *AdStats) AddEvent(ctx context.Context, adID int64, kind ops.AdEventKind, device, capDay string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	id := strconv.FormatInt(adID, 10)
	field := id + ":imp"
	if kind == ops.AdClick {
		field = id + ":clk"
	}
	cmds := valkey.Commands{s.client.B().Hincrby().Key(s.prefix + "pending").Field(field).Increment(1).Build()}
	if capDay != "" {
		freq := s.prefix + "freq:" + capDay + ":" + device
		cmds = append(cmds,
			s.client.B().Hincrby().Key(freq).Field(id).Increment(1).Build(),
			s.client.B().Pexpire().Key(freq).Milliseconds(adFreqTTL.Milliseconds()).Build(),
		)
	}
	for _, r := range s.client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return fmt.Errorf("广告事件计数: %w", err)
		}
	}
	return nil
}

func (s *AdStats) DailyImpressions(ctx context.Context, device, day string, ids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	fields := make([]string, len(ids))
	for i, id := range ids {
		fields[i] = strconv.FormatInt(id, 10)
	}
	vals, err := s.client.Do(ctx, s.client.B().Hmget().Key(s.prefix+"freq:"+day+":"+device).Field(fields...).Build()).ToArray()
	if err != nil {
		return nil, fmt.Errorf("读取广告频控计数: %w", err)
	}
	for i, v := range vals {
		if n, err := v.AsInt64(); err == nil && n > 0 && i < len(ids) {
			out[ids[i]] = n
		}
	}
	return out, nil
}

func (s *AdStats) TakePending(ctx context.Context, token string) (map[int64]ops.AdCounts, error) {
	m, err := drainHash(ctx, s.client, s.prefix+"pending", s.prefix+"pending:"+token)
	if err != nil {
		return nil, fmt.Errorf("取出待落库的广告计数: %w", err)
	}
	out := make(map[int64]ops.AdCounts, len(m))
	for field, raw := range m {
		idStr, kind, ok := strings.Cut(field, ":")
		id, err1 := strconv.ParseInt(idStr, 10, 64)
		n, err2 := strconv.ParseInt(raw, 10, 64)
		if !ok || err1 != nil || err2 != nil || id <= 0 || n <= 0 {
			continue // 不认识的字段跳过，不让一条坏数据卡住整批
		}
		c := out[id]
		switch kind {
		case "imp":
			c.Impressions += n
		case "clk":
			c.Clicks += n
		default:
			continue
		}
		out[id] = c
	}
	return out, nil
}

func (s *AdStats) DonePending(ctx context.Context, token string) error {
	if err := deleteKey(ctx, s.client, s.prefix+"pending:"+token); err != nil {
		return fmt.Errorf("清理已落库的广告计数: %w", err)
	}
	return nil
}
