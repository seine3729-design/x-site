package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/me"
)

var _ me.HistoryStore = (*HistoryStore)(nil)

// HistoryStore 是观看历史的 Valkey 合并写缓冲（S2-3，docs/03 第 6 节）。
// 高频上报先 HSET 到单个 pending Hash（field = <uid>:<drama_id>，值为序列化的续播断点），
// worker 每分钟经 drainHash 原子取出并批量 UPSERT 落库。合并写：同一 (用户, 剧) 的重复上报覆盖为最新。
type HistoryStore struct {
	client valkey.Client
	prefix string
}

func NewHistoryStore(client valkey.Client, prefix string) *HistoryStore {
	return &HistoryStore{client: client, prefix: prefix}
}

func historyField(uid, dramaID int64) string {
	return strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(dramaID, 10)
}

// Report 写入 / 覆盖一条续播断点；payload 已由领域层序列化。
func (s *HistoryStore) Report(ctx context.Context, uid, dramaID int64, payload string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	err := s.client.Do(ctx, s.client.B().Hset().Key(s.prefix+"pending").
		FieldValue().FieldValue(historyField(uid, dramaID), payload).Build()).Error()
	if err != nil {
		return fmt.Errorf("写观看历史: %w", err)
	}
	return nil
}

// TakePending 原子取出待落库的续播断点（移到 taken 名下，token 相同时重试取同一批）。
func (s *HistoryStore) TakePending(ctx context.Context, token string) ([]me.PendingHistory, error) {
	m, err := drainHash(ctx, s.client, s.prefix+"pending", s.prefix+"pending:"+token)
	if err != nil {
		return nil, fmt.Errorf("取出待落库的观看历史: %w", err)
	}
	out := make([]me.PendingHistory, 0, len(m))
	for field, payload := range m {
		uidStr, dramaStr, ok := strings.Cut(field, ":")
		uid, err1 := strconv.ParseInt(uidStr, 10, 64)
		dramaID, err2 := strconv.ParseInt(dramaStr, 10, 64)
		if !ok || err1 != nil || err2 != nil || uid <= 0 || dramaID <= 0 {
			continue // 坏字段跳过，不卡住整批
		}
		out = append(out, me.PendingHistory{UserID: uid, DramaID: dramaID, Payload: payload})
	}
	return out, nil
}

// DonePending 清除已落库的那一份。
func (s *HistoryStore) DonePending(ctx context.Context, token string) error {
	return deleteKey(ctx, s.client, s.prefix+"pending:"+token)
}

// Clear 删除指定剧的待落库断点（用户删历史时同步清，避免又被落库重现）。
func (s *HistoryStore) Clear(ctx context.Context, uid int64, dramaIDs []int64) error {
	if len(dramaIDs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	fields := make([]string, len(dramaIDs))
	for i, d := range dramaIDs {
		fields[i] = historyField(uid, d)
	}
	if err := s.client.Do(ctx, s.client.B().Hdel().Key(s.prefix+"pending").Field(fields...).Build()).Error(); err != nil {
		return fmt.Errorf("清除待落库的观看历史: %w", err)
	}
	return nil
}

// ClearAll 删除某用户全部待落库断点（清空历史时用）。pending 是分钟级窗口、字段有限，整表读取后按 <uid>: 前缀过滤即可。
func (s *HistoryStore) ClearAll(ctx context.Context, uid int64) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := s.prefix + "pending"
	m, err := s.client.Do(ctx, s.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return fmt.Errorf("读取待落库的观看历史: %w", err)
	}
	prefix := strconv.FormatInt(uid, 10) + ":"
	fields := make([]string, 0)
	for f := range m {
		if strings.HasPrefix(f, prefix) {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if err := s.client.Do(ctx, s.client.B().Hdel().Key(key).Field(fields...).Build()).Error(); err != nil {
		return fmt.Errorf("清除待落库的观看历史: %w", err)
	}
	return nil
}
