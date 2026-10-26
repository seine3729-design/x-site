package moderation

import (
	"sort"
	"strings"
)

// Decision 是对一段用户内容过屏蔽词后的判定（评论 / 弹幕语义：block / mask / review 都生效）。
// 昵称 / 简介（S2-9，mask 不生效）、采集剧名（S4-6，只认 block）用各自的判定，不走这里。
type Decision struct {
	Blocked bool     // 命中 block：拒绝发布
	Review  bool     // 命中 review：先送审（status=pending）
	Text    string   // 命中 mask 后替换为 *** 的文本；未命中 mask 时与原文相同
	Hits    []string // 命中的词（去重，供审核展示 / 日志）
}

// Evaluate 对评论 / 弹幕内容过屏蔽词并给出判定。block 优先：命中 block 时直接拒绝、不再计算 mask 文本。
func (s *Service) Evaluate(text string) Decision {
	hits := s.matcher.Load().Scan(text)
	d := Decision{Text: text}
	seen := map[string]bool{}
	var maskSpans [][2]int
	for _, h := range hits {
		switch h.Action {
		case ActionBlock:
			d.Blocked = true
		case ActionReview:
			d.Review = true
		case ActionMask:
			maskSpans = append(maskSpans, [2]int{h.Start, h.End})
		}
		if !seen[h.Word] {
			seen[h.Word] = true
			d.Hits = append(d.Hits, h.Word)
		}
	}
	if !d.Blocked && len(maskSpans) > 0 {
		d.Text = maskText(text, maskSpans)
	}
	return d
}

// maskText 把 spans（原文 rune 区间，左闭右开）先合并重叠 / 相邻区间，每段整体替换为一个 ***。
func maskText(text string, spans [][2]int) string {
	runes := []rune(text)
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	merged := make([][2]int, 0, len(spans))
	for _, sp := range spans {
		if sp[0] >= sp[1] {
			continue
		}
		if n := len(merged); n > 0 && sp[0] <= merged[n-1][1] { // 重叠或相邻
			if sp[1] > merged[n-1][1] {
				merged[n-1][1] = sp[1]
			}
			continue
		}
		merged = append(merged, [2]int{sp[0], sp[1]})
	}
	var b strings.Builder
	prev := 0
	for _, sp := range merged {
		if sp[0] > prev {
			b.WriteString(string(runes[prev:sp[0]]))
		}
		b.WriteString("***")
		prev = sp[1]
	}
	if prev < len(runes) {
		b.WriteString(string(runes[prev:]))
	}
	return b.String()
}
