package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/storage"
)

// LineQualityStore 把客户端质量上报聚合到 Valkey 的分钟桶（Hash，实现 storage.QualityStore）。
// key = prefix + {line_id}:{country}:{isp}:{minute}，字段用 HINCRBY 累加，TTL 覆盖 15 分钟滑动窗口。
type LineQualityStore struct {
	client valkey.Client
	prefix string
}

func NewLineQualityStore(client valkey.Client, prefix string) *LineQualityStore {
	return &LineQualityStore{client: client, prefix: prefix}
}

var _ storage.QualityStore = (*LineQualityStore)(nil)

// qualityBucketTTL 比 15 分钟窗口略长，确保读取端能覆盖整窗。
const qualityBucketTTL = 16 * time.Minute

func (s *LineQualityStore) key(lineID int64, country, isp string, minute int64) string {
	return s.prefix + strconv.FormatInt(lineID, 10) + ":" + country + ":" + isp + ":" + strconv.FormatInt(minute, 10)
}

// AddQuality 把增量累加到地区桶，同时累加一份地区无关的汇总桶
// （供健康看板总览，以及选线时本地区样本不足的回退）。地区本身就未知时只写一次。
func (s *LineQualityStore) AddQuality(ctx context.Context, lineID int64, country, isp string, minute int64, d storage.QualityDelta) error {
	if d.IsZero() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	keys := []string{s.key(lineID, country, isp, minute)}
	all := s.key(lineID, storage.QualityAllRegions, storage.QualityAllRegions, minute)
	if all != keys[0] {
		keys = append(keys, all)
	}
	cmds := make(valkey.Commands, 0, len(keys)*6)
	for _, k := range keys {
		cmds = append(cmds,
			s.client.B().Hincrby().Key(k).Field("samples").Increment(d.Samples).Build(),
			s.client.B().Hincrby().Key(k).Field("ff_sum").Increment(d.FirstFrameSum).Build(),
			s.client.B().Hincrby().Key(k).Field("ff_n").Increment(d.FirstFrameN).Build(),
			s.client.B().Hincrby().Key(k).Field("errors").Increment(d.Errors).Build(),
			s.client.B().Hincrby().Key(k).Field("stalls").Increment(d.Stalls).Build(),
			s.client.B().Pexpire().Key(k).Milliseconds(qualityBucketTTL.Milliseconds()).Build(),
		)
	}
	for _, r := range s.client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return fmt.Errorf("聚合质量上报: %w", err)
		}
	}
	return nil
}

// SumQuality 用一次 pipeline 读齐「线路 × 分钟桶」的全部 Hash 并累加。
// 单次命令数为 len(lineIDs)×len(minutes)，选线时约 3–5 条线路 × 15 分钟，一次往返即可。
func (s *LineQualityStore) SumQuality(
	ctx context.Context, lineIDs []int64, country, isp string, minutes []int64,
) (map[int64]storage.QualityDelta, error) {
	out := make(map[int64]storage.QualityDelta, len(lineIDs))
	if len(lineIDs) == 0 || len(minutes) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	cmds := make(valkey.Commands, 0, len(lineIDs)*len(minutes))
	for _, id := range lineIDs {
		for _, m := range minutes {
			cmds = append(cmds, s.client.B().Hgetall().Key(s.key(id, country, isp, m)).Build())
		}
	}
	res := s.client.DoMulti(ctx, cmds...)
	for i, r := range res {
		m, err := r.AsStrMap()
		if err != nil || len(m) == 0 {
			continue // 桶不存在或已过期
		}
		id := lineIDs[i/len(minutes)]
		d := out[id]
		d.Samples += parseField(m, "samples")
		d.FirstFrameSum += parseField(m, "ff_sum")
		d.FirstFrameN += parseField(m, "ff_n")
		d.Errors += parseField(m, "errors")
		d.Stalls += parseField(m, "stalls")
		out[id] = d
	}
	// 没有任何数据的线路不留空条目，交由调用方按「无数据」处理
	for id, d := range out {
		if d.IsZero() {
			delete(out, id)
		}
	}
	return out, nil
}

func parseField(m map[string]string, field string) int64 {
	n, err := strconv.ParseInt(m[field], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
