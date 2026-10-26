// Package textnorm 规范化用户输入的搜索词。
// 搜索接口（domain/content，S3-4）与热搜计数（domain/ops，S3-4 / A3-4）必须用同一套规范化：
// 否则同一个词在「搜什么」和「热搜榜记了什么」两处会对不上（全角、大小写、空白）。
package textnorm

import (
	"strings"
	"unicode"
)

// Search 规范化搜索词（docs/03 第 3.3 节）：全角转半角、去掉控制与格式字符、
// 转小写、去首尾空白并把连续空白合并成一个空格。
func Search(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\u3000': // 全角空格
			return ' '
		case r >= '\uFF01' && r <= '\uFF5E': // 全角 ASCII
			return r - 0xFEE0
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			if unicode.IsSpace(r) {
				return ' '
			}
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
