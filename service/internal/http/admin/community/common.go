package community

import (
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// pageDTO 是后台列表的一页（页码分页）。
type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func pageOf[S, T any](p dom.Page[S], f func(S) T) pageDTO[T] {
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

// bindQuery 解析查询参数；类型不对时已写出 400。
func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}
