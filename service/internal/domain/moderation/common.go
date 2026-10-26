package moderation

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10000
	maxQueryRunes   = 50 // 后台搜索框的长度上限
)

// PageRequest 是后台列表的页码参数；0 表示取默认值。
type PageRequest struct {
	Page     int
	PageSize int
}

// Page 是后台列表的一页。
type Page[T any] struct {
	List     []T
	Total    int64
	Page     int
	PageSize int
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

// cleanText 去掉首尾空白并检查长度（按字符数）。
func cleanText(label, s string, max int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", badRequest("%s不能为空", label)
	}
	if utf8.RuneCountInString(s) > max {
		return "", badRequest("%s不能超过 %d 个字", label, max)
	}
	return s, nil
}

// cleanQuery 去掉控制字符与首尾空白，并限制长度。
func cleanQuery(q string) (string, error) {
	q = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, q))
	if utf8.RuneCountInString(q) > maxQueryRunes {
		return "", badRequest("搜索词不能超过 %d 个字", maxQueryRunes)
	}
	return q, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePattern 把搜索词转为包含匹配的 LIKE 模式（转义 \ % _）；空词返回空。
func likePattern(q string) string {
	if q == "" {
		return ""
	}
	return "%" + likeEscaper.Replace(q) + "%"
}

// limitOffset 校验分页参数（0 取默认值），返回规范化后的参数与 LIMIT / OFFSET。
func limitOffset(p PageRequest) (PageRequest, int, int, error) {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PageSize == 0 {
		p.PageSize = defaultPageSize
	}
	if p.Page < 1 || p.Page > maxPage || p.PageSize < 1 || p.PageSize > maxPageSize {
		return p, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", maxPage, maxPageSize)
	}
	return p, p.PageSize, (p.Page - 1) * p.PageSize, nil
}

// nonNil 把 nil 切片换成空切片。JSON 里 nil 是 null，运营后台对它做 .map() 会整页白屏，
// 所以凡是要下发给后台的数组字段都经过这里（审核列表的 hit_words 踩过一次）。
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
