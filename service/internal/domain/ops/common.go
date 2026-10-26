// Package ops 是运营域（docs/specs/A3-4.md、docs/06 第 18–22 节）：公告、广告、热搜词、榜单。
// 后台的增删改查规则与前台读取（下发、计数）都在这里；权限点由 HTTP 层判定。
package ops

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10000
	maxQueryRunes   = 50 // 后台搜索框的长度上限
	maxLinkLen      = 500
)

// Status 是公告、广告的启用状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// Locales 是用户端支持的界面语言（docs/04 A.4、docs/adr/0003）。「投放语言」只能从中选择，空列表表示全部语言。
var Locales = []string{"zh-CN", "en", "es", "vi", "ja", "ko", "th", "hi", "ar"}

// DefaultLocale 是前台没有带语言或带了不支持的语言时使用的语言。
const DefaultLocale = "zh-CN"

// NormalizeLocale 返回支持的语言标签；不认识的按默认语言处理（前台接口宽松处理，不因语言报错）。
func NormalizeLocale(s string) string {
	if slices.Contains(Locales, s) {
		return s
	}
	return DefaultLocale
}

// targets 表示按「投放语言」定向的记录是否投给 locale：空列表投给全部语言。
func targets(locales []string, locale string) bool {
	return len(locales) == 0 || slices.Contains(locales, locale)
}

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
	if p.Page < 1 || p.Page > maxPage || p.PageSize < 1 || p.PageSize > maxPageSize {
		return p, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", maxPage, maxPageSize)
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

// cleanMediaRef 校验可选的媒体引用。新上传的图片/视频使用服务端生成的 img/ 或 video/ 相对 key；
// 保留历史的站内绝对路径和 http(s) 地址，便于平滑迁移旧内容。
func cleanMediaRef(label, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "img/") || strings.HasPrefix(s, "video/") {
		if utf8.RuneCountInString(s) > maxLinkLen || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || strings.Contains(s, `\`) {
			return "", badRequest("%s媒体路径不合法", label)
		}
		for part := range strings.SplitSeq(s, "/") {
			if part == "" || part == "." || part == ".." {
				return "", badRequest("%s媒体路径不合法", label)
			}
		}
		return s, nil
	}
	return cleanLink(label, s)
}

func cleanSet(label string, in []string, allowed []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if !slices.Contains(allowed, v) {
			return nil, badRequest("%s无效：%q", label, v)
		}
		seen[v] = true
	}
	out := make([]string, 0, len(seen))
	for _, v := range allowed {
		if seen[v] {
			out = append(out, v)
		}
	}
	return out, nil
}

// cleanStatus 校验启用状态，空值按启用处理。
func cleanStatus(s Status) (Status, error) {
	switch s {
	case "":
		return StatusActive, nil
	case StatusActive, StatusInactive:
		return s, nil
	}
	return s, badRequest("状态只能是启用或停用")
}
