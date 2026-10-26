package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/ops"
)

// HotCounter 是热搜词待落库的搜索计数（实现 ops.HotCounter，docs/specs/A3-4.md）。键（都带前缀）：
//   - pending：Hash，字段「s」+ 词 为搜索次数、「m」+ 词 为片库没有结果的次数（HINCRBY）
//   - pending:<token>：worker 取出、正在落库的那一份（见 drainHash）
//
// 一轮（一小时）内收录的不同词有上限，已有的词照常累加、新词丢弃，防止有人刷随机词撑爆内存。
type HotCounter struct {
	client    valkey.Client
	prefix    string
	maxFields int
}

// defaultHotMaxFields 约等于一小时内最多 2 万个不同的词（每个词最多两个字段）。
const defaultHotMaxFields = 40000

func NewHotCounter(client valkey.Client, prefix string) *HotCounter {
	return &HotCounter{client: client, prefix: prefix, maxFields: defaultHotMaxFields}
}

var _ ops.HotCounter = (*HotCounter)(nil)

// addSearchScript：KEYS[1] 待落库的 Hash；ARGV[1] 搜索次数字段，ARGV[2] 片库没有的字段（空串表示不记），ARGV[3] 字段上限。
var addSearchScript = valkey.NewLuaScript(`
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 and redis.call('HLEN', KEYS[1]) >= tonumber(ARGV[3]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], ARGV[1], 1)
if ARGV[2] ~= '' then
  redis.call('HINCRBY', KEYS[1], ARGV[2], 1)
end
return 1
`)

func (c *HotCounter) AddSearch(ctx context.Context, word string, miss bool) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	missField := ""
	if miss {
		missField = "m" + word
	}
	err := addSearchScript.Exec(ctx, c.client, []string{c.prefix + "pending"},
		[]string{"s" + word, missField, strconv.Itoa(c.maxFields)}).Error()
	if err != nil {
		return fmt.Errorf("热搜计数: %w", err)
	}
	return nil
}

func (c *HotCounter) TakePending(ctx context.Context, token string) (map[string]ops.HotCounts, error) {
	m, err := drainHash(ctx, c.client, c.prefix+"pending", c.prefix+"pending:"+token)
	if err != nil {
		return nil, fmt.Errorf("取出待落库的热搜计数: %w", err)
	}
	out := make(map[string]ops.HotCounts, len(m)/2)
	for field, raw := range m {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 || len(field) < 2 {
			continue // 不认识的字段跳过，不让一条坏数据卡住整批
		}
		word := field[1:]
		cnt := out[word]
		switch field[0] {
		case 's':
			cnt.Searches += n
		case 'm':
			cnt.Misses += n
		default:
			continue
		}
		out[word] = cnt
	}
	return out, nil
}

func (c *HotCounter) DonePending(ctx context.Context, token string) error {
	if err := deleteKey(ctx, c.client, c.prefix+"pending:"+token); err != nil {
		return fmt.Errorf("清理已落库的热搜计数: %w", err)
	}
	return nil
}
