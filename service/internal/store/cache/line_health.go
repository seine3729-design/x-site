package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/storage"
)

// LineHealthStore 把线路健康分存在 Valkey（实现 storage.HealthStore）。键为「前缀 + 线路 ID」，
// 值是 storage.LineHealth 的 JSON（含生效分数与最近一次探测明细）。TTL 由调用方按探测间隔设置。
type LineHealthStore struct {
	client valkey.Client
	prefix string
}

func NewLineHealthStore(client valkey.Client, prefix string) *LineHealthStore {
	return &LineHealthStore{client: client, prefix: prefix}
}

var _ storage.HealthStore = (*LineHealthStore)(nil)

func (s *LineHealthStore) key(lineID int64) string {
	return s.prefix + strconv.FormatInt(lineID, 10)
}

func (s *LineHealthStore) SaveHealth(ctx context.Context, h storage.LineHealth, ttl time.Duration) error {
	b, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("健康分编码失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(s.key(h.LineID)).Value(valkey.BinaryString(b)).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("写入健康分: %w", err)
	}
	return nil
}

func (s *LineHealthStore) GetHealth(ctx context.Context, lineID int64) (storage.LineHealth, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := s.client.Do(ctx, s.client.B().Get().Key(s.key(lineID)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return storage.LineHealth{}, false, nil
	}
	if err != nil {
		return storage.LineHealth{}, false, fmt.Errorf("读取健康分: %w", err)
	}
	var h storage.LineHealth
	if err := json.Unmarshal(b, &h); err != nil {
		// 结构已变化，按无数据处理，下一轮探测会覆盖
		return storage.LineHealth{}, false, nil
	}
	return h, true, nil
}

func (s *LineHealthStore) ListHealth(ctx context.Context, lineIDs []int64) (map[int64]storage.LineHealth, error) {
	out := make(map[int64]storage.LineHealth, len(lineIDs))
	if len(lineIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	keys := make([]string, len(lineIDs))
	for i, id := range lineIDs {
		keys[i] = s.key(id)
	}
	arr, err := s.client.Do(ctx, s.client.B().Mget().Key(keys...).Build()).ToArray()
	if err != nil {
		return nil, fmt.Errorf("读取健康分: %w", err)
	}
	for i, msg := range arr {
		b, err := msg.AsBytes()
		if err != nil {
			// key 不存在（valkey nil）或类型异常：按无数据跳过
			continue
		}
		var h storage.LineHealth
		if err := json.Unmarshal(b, &h); err != nil {
			continue
		}
		out[lineIDs[i]] = h
	}
	return out, nil
}
