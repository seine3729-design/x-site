package content

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10000
	maxQueryRunes   = 50 // 搜索词长度上限（docs/03 第 3.3 节）
	maxSlugLen      = 64
	maxBulkIDs      = 200
)

var (
	// slug：小写字母、数字与连字符，不以连字符开头或结尾。
	slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	// 取值编号：字母、数字、下划线与连字符。
	codeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	// 剧集搜索的编号：#123 只按编号，纯数字同时按编号与剧名。
	idQueryRe = regexp.MustCompile(`^#?([1-9][0-9]{0,17})$`)

	likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	errBadSlug = errs.ErrBadRequest.WithMessage("slug 只能包含小写字母、数字和连字符，不能以连字符开头或结尾，最多 64 个字符")
)

// IsErr 判断 err 是否为给定的哨兵错误。同一业务码的不同错误（如各种 409）用 errors.Is 无法区分。
func IsErr(err error, target *errs.Error) bool {
	var e *errs.Error
	return errors.As(err, &e) && e == target
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

// cleanList 去掉每项的首尾空白，丢弃空项与重复项（保留首次出现的顺序），再检查数量与每项长度。
func cleanList(label string, in []string, maxItems, maxLen int) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if utf8.RuneCountInString(s) > maxLen {
			return nil, badRequest("每个%s不能超过 %d 个字", label, maxLen)
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) > maxItems {
		return nil, badRequest("%s最多 %d 个", label, maxItems)
	}
	return out, nil
}

// uniqueIDs 去掉重复的 ID（保留首次出现的顺序），并检查数量与取值。
func uniqueIDs(label string, ids []int64, max int) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, badRequest("%s ID 无效", label)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > max {
		return nil, badRequest("%s一次最多 %d 个", label, max)
	}
	return out, nil
}

// missing 返回 want 中不在 have 里的第一个 ID；都在时返回 0。
func missing(want, have []int64) int64 {
	set := make(map[int64]bool, len(have))
	for _, id := range have {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			return id
		}
	}
	return 0
}

// sameSet 表示两组 ID 是否完全相同（不计顺序；a 已去重）。
func sameSet(a, b []int64) bool {
	return len(a) == len(b) && missing(a, b) == 0 && missing(b, a) == 0
}

// normalizeSlug 去掉首尾空白并转小写。
func normalizeSlug(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidSlug 表示 s 是否为合法 slug（先经 normalizeSlug）。
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// cleanSlug 规范化并校验 slug；空字符串原样返回（由调用方决定留空还是自动生成）。
func cleanSlug(s string) (string, error) {
	s = normalizeSlug(s)
	if s != "" && !ValidSlug(s) {
		return "", errBadSlug
	}
	return s, nil
}

// newSlug 生成「前缀-6 位随机小写字母或数字」，如 t-k3j9x2。
func newSlug(prefix string) string {
	return prefix + "-" + strings.ToLower(rand.Text()[:6])
}

// withAutoSlug 保存带 slug 的记录：slug 为空时生成随机 slug，碰巧冲突时换一个重试；用户填写的 slug 冲突直接返回 409。
func withAutoSlug[T any](slug, prefix string, save func(slug string) (T, error)) (T, error) {
	if slug != "" {
		return save(slug)
	}
	var (
		v   T
		err error
	)
	for range 3 {
		v, err = save(newSlug(prefix))
		if !IsErr(err, ErrSlugTaken) && !IsErr(err, ErrChannelSlugTaken) {
			return v, err
		}
	}
	return v, err
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

// likePattern 把搜索词转为包含匹配的 LIKE 模式（转义 \ % _）；空词返回空。pg_bigm 的 GIN 索引支持这种模式。
func likePattern(q string) string {
	if q == "" {
		return ""
	}
	return "%" + likeEscaper.Replace(q) + "%"
}

// parseDramaQuery 解析剧集搜索词：#123 只按编号，纯数字同时按编号与剧名 / 别名，其余按剧名 / 别名。
func parseDramaQuery(q string) (pattern string, id int64) {
	if m := idQueryRe.FindStringSubmatch(q); m != nil {
		id, _ = strconv.ParseInt(m[1], 10, 64)
		if strings.HasPrefix(q, "#") {
			return "", id
		}
	}
	return likePattern(q), id
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
