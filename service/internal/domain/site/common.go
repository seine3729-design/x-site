// Package site 是站点编排域（docs/06 第 12、13、15 节，docs/specs/A3-1.md）：静态页、前台菜单、页面区块。
// 后台的增删改查规则、前台下发与区块取数都在这里；权限点由 HTTP 层判定。
//
// 分页与文本清洗的小工具与 domain/ops 里的同名函数重复（两个域的后台列表规则一致）；
// 等第三个域也需要时再抽到共用包，现在各自保留一份，避免跨域改动。
package site

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPageNo       = 10000
	maxQueryRunes   = 50 // 后台搜索框的长度上限
	maxLinkLen      = 500
)

// Cache 是读穿缓存（实现见 internal/store/cache.Store）。后台修改后删除对应的键，前台随后回源。
type Cache interface {
	GetOrLoadJSON(ctx context.Context, key string, ttl time.Duration, dst any, load func(context.Context) (any, error)) error
	Delete(ctx context.Context, key string) error
}

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
	if p.Page < 1 || p.Page > maxPageNo || p.PageSize < 1 || p.PageSize > maxPageSize {
		return p, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", maxPageNo, maxPageSize)
	}
	return p, p.PageSize, (p.Page - 1) * p.PageSize, nil
}

// cleanLink 校验可选的链接或资源地址：站内路径（/ 开头）或 http(s) 完整地址。
// 拒绝 javascript: 等协议、协议相对地址（//host）与带账号的地址，防止前台渲染成可执行链接或跳到别的站点。
func cleanLink(label, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	bad := badRequest("%s需为 / 开头的站内路径，或 http:// / https:// 开头的完整地址", label)
	if utf8.RuneCountInString(s) > maxLinkLen {
		return "", badRequest("%s不能超过 %d 个字符", label, maxLinkLen)
	}
	if strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", bad
	}
	if strings.HasPrefix(s, "/") {
		if strings.HasPrefix(s, "//") || strings.HasPrefix(s, `/\`) {
			return "", bad
		}
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", bad
	}
	return s, nil
}
