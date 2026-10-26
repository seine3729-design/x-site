package textnorm

import "testing"

func TestSearch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"甜宠", "甜宠"},
		{"  总裁   的   小娇妻  ", "总裁 的 小娇妻"},
		{"ＡＢＣ１２３", "abc123"},    // 全角 ASCII 转半角并转小写
		{"甜宠\u3000逆袭", "甜宠 逆袭"}, // 全角空格按空白处理
		{"甜\u200b宠", "甜宠"},      // 零宽字符（Cf）去掉
		{"甜\t宠\n", "甜 宠"},       // 控制字符里的空白变空格
		{"HELLO World", "hello world"},
	}
	for _, c := range cases {
		if got := Search(c.in); got != c.want {
			t.Errorf("Search(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
