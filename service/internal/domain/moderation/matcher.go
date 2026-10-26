// Package moderation 是社区审核域（docs/README「社区服务」）。S2-4 先做屏蔽词引擎：
// Aho-Corasick 自动机进程内常驻，词表变更后整体重建并原子替换（见 blockword.go），
// 多实例经 Valkey Pub/Sub 通知重建。评论 / 弹幕（S2-5）、昵称 / 简介（S2-9）、采集剧名等接入点由后续任务调用。
package moderation

import (
	"strings"
	"unicode"
)

// Action 是命中屏蔽词后的处理办法（docs/02 第 5.7 节）。
type Action string

const (
	ActionBlock  Action = "block"  // 直接拦下
	ActionMask   Action = "mask"   // 变成 ***
	ActionReview Action = "review" // 先送审核
)

// ValidAction 校验处理办法取值。
func ValidAction(a string) bool {
	switch Action(a) {
	case ActionBlock, ActionMask, ActionReview:
		return true
	}
	return false
}

// WordSpec 是构建自动机的一个词条（Word 为规范化后的形式）。
type WordSpec struct {
	Word   string
	Group  string
	Action Action
}

// Hit 是一次命中。Start / End 是在【原始文本】中的 rune 下标（左闭右开），
// 覆盖匹配区间内被规范化丢弃的字符（如夹在中间的零宽字符），便于按原文打码。
type Hit struct {
	Word   string
	Group  string
	Action Action
	Start  int
	End    int
}

// acNode 是 Aho-Corasick 自动机节点。
type acNode struct {
	children map[rune]*acNode
	fail     *acNode
	dictLink *acNode   // fail 链上最近的成词节点，用于枚举所有后缀匹配
	word     *WordSpec // 以本节点结尾的词；非成词节点为 nil
	depth    int       // 从根到本节点的 rune 数（= 词长）
}

// Matcher 是构建后不可变的屏蔽词自动机：并发只读，更新靠整体替换（见 Service.rebuild）。
type Matcher struct {
	root  *acNode
	empty bool
}

// BuildMatcher 从词条构建自动机。Word 会再规范化一次（幂等），空词跳过。
func BuildMatcher(specs []WordSpec) *Matcher {
	root := &acNode{children: map[rune]*acNode{}}
	n := 0
	for _, s := range specs {
		w := NormalizeWord(s.Word)
		if w == "" || !ValidAction(string(s.Action)) {
			continue
		}
		cur := root
		for _, r := range []rune(w) {
			child := cur.children[r]
			if child == nil {
				child = &acNode{children: map[rune]*acNode{}, depth: cur.depth + 1}
				cur.children[r] = child
			}
			cur = child
		}
		spec := WordSpec{Word: w, Group: s.Group, Action: s.Action}
		cur.word = &spec // 同词重复构建时后者覆盖（词表里 word 唯一，正常不会发生）
		n++
	}
	buildFailLinks(root)
	return &Matcher{root: root, empty: n == 0}
}

// buildFailLinks 用 BFS 建失败指针与字典后缀指针。
func buildFailLinks(root *acNode) {
	queue := make([]*acNode, 0, len(root.children))
	for _, c := range root.children {
		c.fail = root
		queue = append(queue, c)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for r, child := range cur.children {
			f := cur.fail
			for f != nil && f.children[r] == nil {
				f = f.fail
			}
			switch {
			case f == nil:
				child.fail = root
			default:
				child.fail = f.children[r]
			}
			if child.fail.word != nil {
				child.dictLink = child.fail
			} else {
				child.dictLink = child.fail.dictLink
			}
			queue = append(queue, child)
		}
	}
}

// Scan 返回文本中命中的全部屏蔽词（可能重叠，如「ab」与「abc」同时命中）。
func (m *Matcher) Scan(text string) []Hit {
	if m == nil || m.empty {
		return nil
	}
	norm, origIdx := normalizeText(text)
	var hits []Hit
	cur := m.root
	for i, r := range norm {
		for cur != m.root && cur.children[r] == nil {
			cur = cur.fail
		}
		if next := cur.children[r]; next != nil {
			cur = next
		}
		// 枚举以位置 i 结尾的所有成词：当前节点 + 字典后缀链
		for t := cur; t != nil; t = t.dictLink {
			if t.word == nil {
				continue
			}
			start := i - t.depth + 1
			hits = append(hits, Hit{
				Word: t.word.Word, Group: t.word.Group, Action: t.word.Action,
				Start: origIdx[start], End: origIdx[i] + 1,
			})
		}
	}
	return hits
}

// HasBlock 报告是否有 block 命中。
func (m *Matcher) HasBlock(text string) bool {
	for _, h := range m.Scan(text) {
		if h.Action == ActionBlock {
			return true
		}
	}
	return false
}

// dropRune 是规范化时需要丢弃的字符的占位返回值。
const dropRune = rune(-1)

// NormalizeWord 规范化一个屏蔽词（入库与构建都用它，保证一致）：规范化后去首尾空白。
func NormalizeWord(s string) string {
	norm, _ := normalizeText(s)
	return strings.TrimSpace(string(norm))
}

// normalizeText 规范化文本并返回规范化后的 rune 序列与每个 rune 对应的原始 rune 下标。
// 规范化：全角空格转半角、全角 ASCII 转半角、转小写、丢弃零宽字符。
func normalizeText(s string) (norm []rune, origIdx []int) {
	orig := []rune(s)
	norm = make([]rune, 0, len(orig))
	origIdx = make([]int, 0, len(orig))
	for i, r := range orig {
		f := foldRune(r)
		if f == dropRune {
			continue
		}
		norm = append(norm, f)
		origIdx = append(origIdx, i)
	}
	return norm, origIdx
}

// foldRune 折叠单个字符：零宽字符返回 dropRune（丢弃），全角转半角，其余转小写。
func foldRune(r rune) rune {
	if isZeroWidth(r) {
		return dropRune
	}
	switch {
	case r == 0x3000: // 全角空格
		return ' '
	case r >= 0xFF01 && r <= 0xFF5E: // 全角 ASCII（！..～）转半角
		r = r - 0xFF00 + 0x20
	}
	return unicode.ToLower(r)
}

// isZeroWidth 判断是否零宽 / 不可见插入字符（常被用来规避屏蔽词）。
func isZeroWidth(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF, 0x00AD:
		return true
	}
	return false
}
