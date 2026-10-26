package cache

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Dedup 用 Valkey SET NX 做「短时间内首次出现」判定（S2-5 评论防刷：同作者相同内容 10 秒内拒绝重复）。
type Dedup struct {
	client valkey.Client
	prefix string
	ttl    time.Duration
}

func NewDedup(client valkey.Client, prefix string, ttl time.Duration) *Dedup {
	return &Dedup{client: client, prefix: prefix, ttl: ttl}
}

// FirstPost 报告 key 是否在窗口内首次出现：首次返回 true 并占位 ttl；窗口内重复返回 false。
// Valkey 不可用时放行（返回 true），不因缓存抖动挡住正常发表。
func (d *Dedup) FirstPost(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	err := d.client.Do(ctx, d.client.B().Set().Key(d.prefix+key).Value("1").Nx().Px(d.ttl).Build()).Error()
	if err == nil {
		return true, nil // 成功写入 = 首次
	}
	if valkey.IsValkeyNil(err) {
		return false, nil // NX 未写入 = 键已存在 = 重复
	}
	return true, nil // 其它错误放行
}
