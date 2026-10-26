package moderation

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func specs(pairs ...[2]string) []WordSpec {
	out := make([]WordSpec, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, WordSpec{Word: p[0], Group: "g", Action: Action(p[1])})
	}
	return out
}

func TestMatcher_Basic(t *testing.T) {
	m := BuildMatcher(specs(
		[2]string{"she", "block"},
		[2]string{"he", "mask"},
		[2]string{"his", "review"},
		[2]string{"hers", "block"},
	))
	hits := m.Scan("ushers")
	// ushers 含 she(1-4)、he(2-4)、hers(2-6)
	words := map[string]Action{}
	for _, h := range hits {
		words[h.Word] = h.Action
	}
	if words["she"] != ActionBlock || words["he"] != ActionMask || words["hers"] != ActionBlock {
		t.Fatalf("重叠命中不全：%+v", hits)
	}
	if _, ok := words["his"]; ok {
		t.Fatalf("不应命中 his：%+v", hits)
	}
}

func TestMatcher_Normalization(t *testing.T) {
	m := BuildMatcher(specs([2]string{"vpn", "block"}, [2]string{"翻墙", "review"}))
	for _, text := range []string{
		"VPN",             // 大小写
		"ＶＰＮ",             // 全角
		"v\u200bp\u200cn", // 零宽字符插入
		"买VPN",            // 前缀干扰
	} {
		if !m.HasBlock(text) {
			t.Errorf("%q 应命中 vpn(block)", text)
		}
	}
	if hits := m.Scan("教你翻\ufeff墙"); len(hits) != 1 || hits[0].Word != "翻墙" {
		t.Fatalf("零宽字符应被忽略后命中翻墙：%+v", hits)
	}
}

func TestMatcher_OriginalPositions(t *testing.T) {
	m := BuildMatcher(specs([2]string{"vpn", "mask"}))
	// 原文含零宽字符，命中区间应覆盖它，便于按原文打码
	text := "用v\u200bpn吗"
	hits := m.Scan(text)
	if len(hits) != 1 {
		t.Fatalf("应命中一次：%+v", hits)
	}
	runes := []rune(text)
	// Start 指向 'v'（下标 1），End 指向 n 之后（下标 5，因为 v,零宽,p,n 占原文 1..4）
	got := string(runes[hits[0].Start:hits[0].End])
	if got != "v\u200bpn" {
		t.Fatalf("命中原文区间应覆盖零宽字符，得到 %q", got)
	}
}

func TestMatcher_Empty(t *testing.T) {
	m := BuildMatcher(nil)
	if m.Scan("anything") != nil || m.HasBlock("anything") {
		t.Fatal("空词表不应命中")
	}
	// 空词、非法 action 跳过
	m2 := BuildMatcher([]WordSpec{{Word: "  ", Action: ActionBlock}, {Word: "x", Action: "bad"}})
	if m2.HasBlock("x  ") {
		t.Fatal("空词与非法 action 应被跳过")
	}
}

func TestNormalizeWord(t *testing.T) {
	for in, want := range map[string]string{
		"  VPN  ":  "vpn",
		"ＡＢＣ":      "abc",
		"a\u200bb": "ab",
		"翻墙":       "翻墙",
	} {
		if got := NormalizeWord(in); got != want {
			t.Errorf("NormalizeWord(%q)=%q，期望 %q", in, got, want)
		}
	}
}

// 验收（docs/11 S2-4）：1 万词表单次匹配 < 50µs。
func TestMatcher_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过性能测试")
	}
	words := make([]WordSpec, 0, 10000)
	for i := 0; i < 10000; i++ {
		// 等宽 4 位，保证没有词是另一个词的子串，便于断言命中数
		words = append(words, WordSpec{Word: fmt.Sprintf("敏感词%04d", i), Group: "g", Action: ActionBlock})
	}
	m := BuildMatcher(words)
	// 一段代表性文本（评论级长度，含中英文与两处命中）
	text := strings.Repeat("这是一段正常的评论内容 hello world ", 5) + "顺便说下敏感词7777 和 敏感词0012"
	const n = 2000
	start := time.Now()
	for i := 0; i < n; i++ {
		_ = m.Scan(text)
	}
	per := time.Since(start) / n
	if hits := m.Scan(text); len(hits) != 2 {
		t.Fatalf("代表性文本应命中 2 个词，得到 %d", len(hits))
	}
	t.Logf("1 万词表、%d 字文本单次匹配平均 %v", len([]rune(text)), per)
	if per > 50*time.Microsecond {
		t.Fatalf("单次匹配 %v 超过 50µs 验收线", per)
	}
}

// 确保命中顺序可复现（按结束位置、再按词），便于上层去重。
func TestMatcher_DeterministicOrder(t *testing.T) {
	m := BuildMatcher(specs([2]string{"ab", "block"}, [2]string{"abc", "mask"}, [2]string{"bc", "review"}))
	hits := m.Scan("xabcy")
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].End != hits[j].End {
			return hits[i].End < hits[j].End
		}
		return hits[i].Word < hits[j].Word
	})
	var got []string
	for _, h := range hits {
		got = append(got, h.Word)
	}
	// ab 结束于 index2，abc/bc 结束于 index3
	want := []string{"ab", "abc", "bc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("命中集合不对：%v", got)
	}
}
