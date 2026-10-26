// Package site 是运营后台「框架」目录的接口：静态页、前台菜单与防丢地址、页面区块（docs/07 第 2 节，docs/specs/A3-1.md）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package site

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/content"
	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// pageDTO 是后台列表的一页（页码分页）。与 admin/ops 的同名类型一致，两个目录各自保留一份。
type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func listOf[S, T any](p dom.Page[S], f func(S) T) pageDTO[T] {
	return pageDTO[T]{List: mapSlice(p.List, f), Total: p.Total, Page: p.Page, PageSize: p.PageSize}
}

// mapSlice 转换切片；nil 也返回空数组，JSON 输出 [] 而不是 null。
func mapSlice[S, T any](items []S, f func(S) T) []T {
	out := make([]T, len(items))
	for i, it := range items {
		out[i] = f(it)
	}
	return out
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// strs 把 nil 换成空数组，JSON 输出 [] 而不是 null。
func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ids 把 nil 换成空数组，JSON 输出 [] 而不是 null。
func ids(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

// contentSort 把请求里的排序字符串转成内容域的排序类型（领域层再校验白名单）。
func contentSort(s string) content.DramaSort { return content.DramaSort(s) }

// bindQuery 解析查询参数；类型不对（如 page=abc）时已写出 400。
func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}
