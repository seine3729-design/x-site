package storage

import (
	"context"
	"sort"
	"strings"
	"time"
)

// 选源用的选线（docs/12 第 4.3 节，S1-7）：与 GET /api/v1/lines 同一套打分与加权随机（加载方式与 LinesService.List 一致），
// 另外处理算法第 6 步「用户手选线路」。只挑视频线路；清单地址与播放令牌由选源服务（domain/play）按线路签发。
// 候选先按媒体副本过滤（S1-6f）：只保留该集产物已复制就绪（media_replica.status='ready'）的存储源上的线路，
// 因此镜像尚未复制完成时不会把对应线路下发给客户端。

// PlaybackInput 是选源的选线输入。IP 非空时经 GeoResolver 解析，否则用 Geo（dev 的 X-Debug-Geo 直接给定地区）。
type PlaybackInput struct {
	IP        string
	Geo       Geo
	DeviceID  string // 同分加权随机的种子；为空时同分按线路编号排序
	Preferred int64  // 用户手选的线路（line_id）；0 表示没有
	// ObjectID 是这一集的产物对象（episode.video_object_id）。非 0 时只返回该对象已有 ready 副本的存储源上的线路
	// （docs/12 第 5 节第 4 条：镜像复制完成前不下发该线路）。为 0 时不按副本过滤。
	ObjectID int64
}

// PlaybackLine 是选源结果里的一条视频线路。
type PlaybackLine struct {
	LineID      int64
	Name        string
	RegionTag   string
	Recommended bool // 算法排第一的线路
	Manual      bool // 用户手选、已放到第一位
}

// PlaybackSelection 是选源的选线结果：解析出的地区与排好序的候选线路（不超过候选数量）。
type PlaybackSelection struct {
	Region string
	Lines  []PlaybackLine
}

// ForPlayback 为选源挑选视频线路：按选线算法打分排序取前 N 条；手选线路在候选中（含超出 N 条的）且健康时放到第一位。
func (s *LinesService) ForPlayback(ctx context.Context, in PlaybackInput) (PlaybackSelection, error) {
	geo := in.Geo
	if ip := strings.TrimSpace(in.IP); ip != "" {
		geo = s.geo.Lookup(ip)
	}
	geo = normalizeGeo(geo)

	lines, err := s.repo.ListLines(ctx, LineQuery{Kind: LineVideo})
	if err != nil {
		return PlaybackSelection{}, err
	}
	// 只保留该集产物已就绪的存储源上的线路（S1-6f）
	if in.ObjectID > 0 {
		if lines, err = s.filterByReplica(ctx, lines, in.ObjectID); err != nil {
			return PlaybackSelection{}, err
		}
	}
	ids := make([]int64, len(lines))
	for i := range lines {
		if lines[i].Rules, err = s.repo.ListRulesByLine(ctx, lines[i].ID); err != nil {
			return PlaybackSelection{}, err
		}
		ids[i] = lines[i].ID
	}
	n := 3
	if st, err := s.repo.GetSettings(ctx); err == nil && st.CandidateCount > 0 {
		n = int(st.CandidateCount)
	}
	now := time.Now()
	bd := ResolveHealth(ctx, s.health, s.quality, ids, geo, now)
	evals := Evaluate(lines, geo, healthFunc(bd), n, weightedRNG(in.DeviceID, now))
	return PlaybackSelection{Region: formatRegion(geo), Lines: pickPlayback(evals, n, in.Preferred)}, nil
}

// pickPlayback 按排名取候选，并把手选线路放到第一位（总数仍不超过 n）。
// 手选线路必须命中地区且健康：落选（被排除、地区不符、停用、不健康）时忽略，前台据「第一条不是手选」提示已切回自动。
func pickPlayback(evals []LineEval, n int, preferred int64) []PlaybackLine {
	ranked := make([]LineEval, 0, n)
	for _, e := range evals {
		if e.Rank > 0 {
			ranked = append(ranked, e)
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Rank < ranked[j].Rank })

	out := make([]PlaybackLine, 0, len(ranked)+1)
	if preferred != 0 {
		for _, e := range evals {
			if e.LineID == preferred && (e.Status == EvalCandidate || e.Status == EvalOverflow) && e.HealthScore >= healthMinKeep {
				p := playbackLine(e)
				p.Manual = true
				out = append(out, p)
				break
			}
		}
	}
	for _, e := range ranked {
		if len(out) > 0 && out[0].Manual && out[0].LineID == e.LineID {
			continue
		}
		out = append(out, playbackLine(e))
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func playbackLine(e LineEval) PlaybackLine {
	return PlaybackLine{LineID: e.LineID, Name: e.Name, RegionTag: e.RegionTag, Recommended: e.Recommended}
}

// filterByReplica 只保留该对象已有 ready 副本的存储源上的线路（docs/12 第 5 节第 4 条）。
// 没有任何副本记录时不过滤：S1-6f 之前的产物还没登记副本，按「都能提供」处理，避免老内容播不了。
func (s *LinesService) filterByReplica(ctx context.Context, lines []Line, objectID int64) ([]Line, error) {
	ready, err := s.repo.ReadyProviders(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if len(ready) == 0 {
		return lines, nil
	}
	ok := make(map[int64]bool, len(ready))
	for _, id := range ready {
		ok[id] = true
	}
	out := make([]Line, 0, len(lines))
	for _, l := range lines {
		if ok[l.ProviderID] {
			out = append(out, l)
		}
	}
	return out, nil
}
