package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
)

// FeedbackSessionStore 把反馈社区的用户会话存在 Valkey（实现 feedback.SessionStore）。
// 键为「前缀 + 令牌摘要」，值只含用户 ID、会话版本与创建时间；原始令牌不落库也不入日志。
type FeedbackSessionStore struct {
	client valkey.Client
	prefix string
}

func NewFeedbackSessionStore(client valkey.Client, prefix string) *FeedbackSessionStore {
	return &FeedbackSessionStore{client: client, prefix: prefix}
}

type fbSessionRecord struct {
	UserID         int64 `json:"uid"`
	SessionVersion int32 `json:"ver"`
	CreatedAt      int64 `json:"created_at"` // Unix 秒
}

func (s *FeedbackSessionStore) Create(ctx context.Context, key string, sess feedback.Session, ttl time.Duration) error {
	b, err := json.Marshal(fbSessionRecord{
		UserID: sess.UserID, SessionVersion: sess.SessionVersion, CreatedAt: sess.CreatedAt.Unix(),
	})
	if err != nil {
		return fmt.Errorf("会话编码失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	// 键由 256 bit 随机令牌派生，NX 只是防御：万一重复也不覆盖别人的会话
	cmd := s.client.B().Set().Key(s.prefix + key).Value(valkey.BinaryString(b)).Nx().Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("写入会话: %w", err)
	}
	return nil
}

func (s *FeedbackSessionStore) GetAndTouch(ctx context.Context, key string, ttl time.Duration) (feedback.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := s.client.Do(ctx, s.client.B().Getex().Key(s.prefix+key).Px(ttl).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return feedback.Session{}, errs.ErrNotFound
	}
	if err != nil {
		return feedback.Session{}, fmt.Errorf("读取会话: %w", err)
	}
	var r fbSessionRecord
	if err := json.Unmarshal(b, &r); err != nil {
		// 结构不认识的会话视为不存在，用户重新登录即可
		return feedback.Session{}, errs.ErrNotFound
	}
	return feedback.Session{
		UserID: r.UserID, SessionVersion: r.SessionVersion, CreatedAt: time.Unix(r.CreatedAt, 0),
	}, nil
}

func (s *FeedbackSessionStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.prefix+key).Build()).Error(); err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}
