package cache

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/ops"
)

// RankStore 是线上榜单（实现 ops.RankStore，docs/03 第 3.4 节）。键（都带前缀）：
//   - <榜单>：有序集合，成员是剧集编号，分数按名次倒排（第 1 名最大），ZRANGE … REV 即名次顺序（置顶不靠分数大小）
//   - <榜单>:heat：Hash，剧集编号 → 热度值（算出的分数）
//   - <榜单>:at：计算时间（RFC 3339）
//   - <榜单>:tmp:<随机>（及其 :heat）：重算时的临时键，写好后与线上键在一个 Lua 脚本里原子交换，读的一方没有空窗
type RankStore struct {
	client valkey.Client
	prefix string
}

func NewRankStore(client valkey.Client, prefix string) *RankStore {
	return &RankStore{client: client, prefix: prefix}
}

var _ ops.RankStore = (*RankStore)(nil)

// rankTmpTTL：临时键在交换前一直有效；进程中途退出时也不会永久留下。交换后去掉过期时间。
const rankTmpTTL = time.Minute

// swapRankScript：KEYS 依次是临时有序集合、临时热度、线上有序集合、线上热度、计算时间；ARGV 是计算时间与名次数。
// 名次数为 0 时清空线上榜单；临时键不完整（不该出现）时报错，不动线上榜单。
var swapRankScript = valkey.NewLuaScript(`
if tonumber(ARGV[2]) == 0 then
  redis.call('DEL', KEYS[3], KEYS[4])
else
  if redis.call('EXISTS', KEYS[1]) == 0 or redis.call('EXISTS', KEYS[2]) == 0 then
    return redis.error_reply('ERR rank tmp keys missing')
  end
  redis.call('RENAME', KEYS[1], KEYS[3])
  redis.call('PERSIST', KEYS[3])
  redis.call('RENAME', KEYS[2], KEYS[4])
  redis.call('PERSIST', KEYS[4])
end
redis.call('SET', KEYS[5], ARGV[1])
return 1
`)

// readRankScript 一次读出前 n 名、对应的热度与计算时间（与交换互斥，读不到「名次换了、热度还没换」的中间状态）。
var readRankScript = valkey.NewLuaScript(`
local ids = redis.call('ZRANGE', KEYS[1], 0, tonumber(ARGV[1]), 'REV')
local heat = {}
if #ids > 0 then
  heat = redis.call('HMGET', KEYS[2], unpack(ids))
end
return {redis.call('GET', KEYS[3]), ids, heat}
`)

func (s *RankStore) keys(b ops.RankBoard) (live, heat, at string) {
	live = s.prefix + string(b)
	return live, live + ":heat", live + ":at"
}

func (s *RankStore) Replace(ctx context.Context, b ops.RankBoard, entries []ops.RankEntry, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout) // 写临时键 + 交换两次往返
	defer cancel()
	live, heat, atKey := s.keys(b)
	tmp := live + ":tmp:" + strconv.FormatUint(rand.Uint64(), 36)
	if len(entries) > 0 {
		z := s.client.B().Zadd().Key(tmp).ScoreMember()
		h := s.client.B().Hset().Key(tmp + ":heat").FieldValue()
		for i, e := range entries {
			id := strconv.FormatInt(e.DramaID, 10)
			z = z.ScoreMember(float64(len(entries)-i), id)
			h = h.FieldValue(id, strconv.FormatFloat(e.Score, 'f', -1, 64))
		}
		ttl := rankTmpTTL.Milliseconds()
		for _, r := range s.client.DoMulti(ctx,
			z.Build(), s.client.B().Pexpire().Key(tmp).Milliseconds(ttl).Build(),
			h.Build(), s.client.B().Pexpire().Key(tmp+":heat").Milliseconds(ttl).Build(),
		) {
			if err := r.Error(); err != nil {
				return fmt.Errorf("写入临时榜单: %w", err)
			}
		}
	}
	err := swapRankScript.Exec(ctx, s.client, []string{tmp, tmp + ":heat", live, heat, atKey},
		[]string{at.UTC().Format(time.RFC3339Nano), strconv.Itoa(len(entries))}).Error()
	if err != nil {
		return fmt.Errorf("切换线上榜单: %w", err)
	}
	return nil
}

func (s *RankStore) Top(ctx context.Context, b ops.RankBoard, n int) ([]ops.RankEntry, time.Time, bool, error) {
	if n <= 0 {
		return nil, time.Time{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	live, heat, atKey := s.keys(b)
	res, err := readRankScript.Exec(ctx, s.client, []string{live, heat, atKey}, []string{strconv.Itoa(n - 1)}).ToArray()
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("读取线上榜单: %w", err)
	}
	if len(res) != 3 {
		return nil, time.Time{}, false, fmt.Errorf("读取线上榜单: 返回了 %d 项", len(res))
	}
	raw, err := res[0].ToString()
	if err != nil { // 没有计算时间：还没算过
		return nil, time.Time{}, false, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("解析榜单计算时间: %w", err)
	}
	ids, _ := res[1].AsStrSlice()
	heats, _ := res[2].ToArray()
	out := make([]ops.RankEntry, 0, len(ids))
	for i, idStr := range ids {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		e := ops.RankEntry{DramaID: id}
		if i < len(heats) {
			if v, err := heats[i].ToString(); err == nil {
				e.Score, _ = strconv.ParseFloat(v, 64)
			}
		}
		out = append(out, e)
	}
	return out, at.UTC(), true, nil
}
