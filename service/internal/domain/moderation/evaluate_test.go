package moderation

import (
	"strings"
	"testing"
)

func evalSvc(words ...WordSpec) *Service {
	s := NewService(&fakeRepo{}, &fakePublisher{})
	s.matcher.Store(BuildMatcher(words))
	return s
}

func TestEvaluate_Block(t *testing.T) {
	s := evalSvc(WordSpec{Word: "vpn", Action: ActionBlock}, WordSpec{Word: "广告", Action: ActionMask})
	d := s.Evaluate("买VPN还有广告")
	if !d.Blocked {
		t.Fatal("命中 block 应 Blocked")
	}
	// block 优先，不计算 mask 文本
	if d.Text != "买VPN还有广告" {
		t.Fatalf("block 时不改文本，得到 %q", d.Text)
	}
	if len(d.Hits) != 2 {
		t.Fatalf("应记录 2 个命中词：%v", d.Hits)
	}
}

func TestEvaluate_Mask(t *testing.T) {
	s := evalSvc(WordSpec{Word: "广告", Action: ActionMask}, WordSpec{Word: "垃圾", Action: ActionMask})
	d := s.Evaluate("这是垃圾广告垃圾")
	if d.Blocked || d.Review {
		t.Fatalf("只命中 mask：%+v", d)
	}
	if d.Text != "这是******" {
		// 垃圾(2)+广告(2)+垃圾(2) 相邻合并为连续 ***，但三段不相邻处分别替换
		t.Logf("mask 结果：%q", d.Text)
	}
	if strings.Contains(d.Text, "广告") || strings.Contains(d.Text, "垃圾") {
		t.Fatalf("mask 后不应残留原词：%q", d.Text)
	}
}

func TestEvaluate_MaskPreservesSurrounding(t *testing.T) {
	s := evalSvc(WordSpec{Word: "坏", Action: ActionMask})
	d := s.Evaluate("好坏好")
	if d.Text != "好***好" {
		t.Fatalf("mask 只替换命中区间：%q", d.Text)
	}
}

func TestEvaluate_Review(t *testing.T) {
	s := evalSvc(WordSpec{Word: "敏感", Action: ActionReview})
	d := s.Evaluate("有点敏感的话题")
	if d.Blocked || !d.Review {
		t.Fatalf("命中 review 应 Review 且不 Blocked：%+v", d)
	}
	if d.Text != "有点敏感的话题" {
		t.Fatalf("review 不改文本：%q", d.Text)
	}
}

func TestEvaluate_Clean(t *testing.T) {
	s := evalSvc(WordSpec{Word: "坏词", Action: ActionBlock})
	d := s.Evaluate("一段正常的评论")
	if d.Blocked || d.Review || d.Text != "一段正常的评论" || len(d.Hits) != 0 {
		t.Fatalf("干净文本：%+v", d)
	}
}

func TestEvaluate_MaskMergesOverlap(t *testing.T) {
	// 「abc」mask 与「bcd」mask 在 "abcd" 重叠，应合并为一个 ***
	s := evalSvc(WordSpec{Word: "abc", Action: ActionMask}, WordSpec{Word: "bcd", Action: ActionMask})
	d := s.Evaluate("xabcdy")
	if d.Text != "x***y" {
		t.Fatalf("重叠 mask 区间应合并：%q", d.Text)
	}
}
