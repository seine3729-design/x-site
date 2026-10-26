package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// drainHashScript 原子地把 KEYS[1]（待落库的 Hash）改名为 KEYS[2]（某次落库任务名下）并读出。
// KEYS[2] 已存在（上次落库失败、任务重试）时直接读它，不覆盖、不合并，新到的计数留在 KEYS[1] 等下一轮。
// ARGV[1] 是 KEYS[2] 的保留时长（毫秒），任务彻底失败时也不会留下永久的孤儿键。
var drainHashScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[2]) == 0 then
  if redis.call('EXISTS', KEYS[1]) == 0 then
    return {}
  end
  redis.call('RENAME', KEYS[1], KEYS[2])
  redis.call('PEXPIRE', KEYS[2], ARGV[1])
end
return redis.call('HGETALL', KEYS[2])
`)

// drainKeep 是落库中那一份的保留时长：覆盖 River 的重试窗口。
const drainKeep = 7 * 24 * time.Hour

// drainHash 取出 pending 中待落库的计数（移到 taken 名下）；落库成功后由调用方删除 taken。
func drainHash(ctx context.Context, client valkey.Client, pending, taken string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return drainHashScript.Exec(ctx, client, []string{pending, taken}, []string{strconv.FormatInt(drainKeep.Milliseconds(), 10)}).AsStrMap()
}

// deleteKey 删除一个键（带超时）。
func deleteKey(ctx context.Context, client valkey.Client, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return client.Do(ctx, client.B().Del().Key(key).Build()).Error()
}
