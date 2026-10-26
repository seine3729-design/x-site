package cache

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// 批量扫描与删除的整体超时。比单键读写的 opTimeout 宽松得多：清缓存是后台的低频运维操作，
// 键多的时候 SCAN 要走很多轮，200 毫秒根本不够。
const purgeTimeout = 20 * time.Second

// 每轮 SCAN 的建议返回条数。太小轮次多，太大单轮阻塞久。
const scanCount = 500

// 单次 UNLINK 携带的键数上限，避免一条命令过大。
const unlinkBatch = 200

// Purger 负责按键模式统计与删除 Valkey 中的键（S4-4 缓存管理）。
//
// 它不带键前缀：清缓存要跨多个前缀（xsite:content:、xsite:ops:…），
// 由领域层的白名单决定清什么，这里只机械执行，但会拒绝过于笼统的模式兜底。
//
// 删除用 UNLINK 而不是 DEL：释放内存在后台线程做，不阻塞 Valkey 主线程。
// 扫描用 SCAN 而不是 KEYS：KEYS 会阻塞主线程，键多时会拖垮整个实例。
type Purger struct {
	client valkey.Client
}

func NewPurger(client valkey.Client) *Purger {
	return &Purger{client: client}
}

// Stats 读取键总数与内存占用（签名对齐 domain/platform.CacheKeyStore，省掉一层适配）。
// keys 是整个库的键数，含会话、限流计数等有状态数据。
func (p *Purger) Stats(ctx context.Context) (keys int64, memoryUsed int64, memoryText string, err error) {
	ctx, cancel := context.WithTimeout(ctx, purgeTimeout)
	defer cancel()

	keys, err = p.client.Do(ctx, p.client.B().Dbsize().Build()).AsInt64()
	if err != nil {
		return 0, 0, "", fmt.Errorf("读取键总数: %w", err)
	}

	// INFO memory 是文本，取 used_memory 与 used_memory_human 两行。
	// 拿不到不影响清缓存，只是页面上少一个数，所以不往上报错。
	info, err := p.client.Do(ctx, p.client.B().Info().Section("memory").Build()).ToString()
	if err != nil {
		return keys, 0, "", nil
	}
	for line := range strings.SplitSeq(info, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch name {
		case "used_memory":
			memoryUsed, _ = strconv.ParseInt(value, 10, 64)
		case "used_memory_human":
			memoryText = value
		}
	}
	return keys, memoryUsed, memoryText, nil
}

// Count 返回匹配该模式的键数量。模式不含 `*` 时退化为存在性检查（0 或 1）。
func (p *Purger) Count(ctx context.Context, pattern string) (int64, error) {
	if err := checkPattern(pattern); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, purgeTimeout)
	defer cancel()

	if !strings.Contains(pattern, "*") {
		n, err := p.client.Do(ctx, p.client.B().Exists().Key(pattern).Build()).AsInt64()
		if err != nil {
			return 0, fmt.Errorf("检查键是否存在: %w", err)
		}
		return n, nil
	}

	var total int64
	err := p.scan(ctx, pattern, func(keys []string) error {
		total += int64(len(keys))
		return nil
	})
	return total, err
}

// Purge 删除匹配该模式的键，返回删除的键数。
func (p *Purger) Purge(ctx context.Context, pattern string) (int64, error) {
	if err := checkPattern(pattern); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, purgeTimeout)
	defer cancel()

	if !strings.Contains(pattern, "*") {
		n, err := p.client.Do(ctx, p.client.B().Unlink().Key(pattern).Build()).AsInt64()
		if err != nil {
			return 0, fmt.Errorf("删除键: %w", err)
		}
		return n, nil
	}

	var removed int64
	err := p.scan(ctx, pattern, func(keys []string) error {
		for chunk := range slices.Chunk(keys, unlinkBatch) {
			n, err := p.client.Do(ctx, p.client.B().Unlink().Key(chunk...).Build()).AsInt64()
			if err != nil {
				return fmt.Errorf("删除键: %w", err)
			}
			removed += n
		}
		return nil
	})
	return removed, err
}

// scan 按模式遍历键，每轮回调一次。
//
// 注意：SCAN 的游标是单节点语义。本项目连的是单实例 Valkey（cache.NewClient 走 ParseURL 单点），
// 将来若换成集群，这里要改成逐节点扫描。
func (p *Purger) scan(ctx context.Context, pattern string, fn func(keys []string) error) error {
	var cursor uint64
	for {
		cmd := p.client.B().Scan().Cursor(cursor).Match(pattern).Count(scanCount).Build()
		entry, err := p.client.Do(ctx, cmd).AsScanEntry()
		if err != nil {
			return fmt.Errorf("扫描键: %w", err)
		}
		if len(entry.Elements) > 0 {
			if err := fn(entry.Elements); err != nil {
				return err
			}
		}
		if entry.Cursor == 0 {
			return nil
		}
		cursor = entry.Cursor
	}
}

// checkPattern 拦下会命中整个库的模式。真正的白名单在领域层（domain/platform/cachekeys.go），
// 这里只是最后一道兜底：别让一个拼错的模式把会话和限流计数一起清掉。
func checkPattern(pattern string) error {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return fmt.Errorf("键模式不能为空")
	}
	if trimmed != pattern {
		return fmt.Errorf("键模式 %q 含首尾空白", pattern)
	}
	// 必须限定到具体用途：至少两级（xsite:content:…），且 `*` 之前要有足够的限定长度。
	head, _, _ := strings.Cut(pattern, "*")
	if strings.Count(head, ":") < 2 {
		return fmt.Errorf("键模式 %q 过于笼统，至少要限定到 `前缀:用途:`", pattern)
	}
	return nil
}
