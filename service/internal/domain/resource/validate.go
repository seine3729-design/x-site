package resource

import (
	"crypto/rand"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10000
	maxQueryRunes   = 50
	maxNameRunes    = 64
	maxHostLen      = 253
	maxPathBytes    = 1024
)

var (
	// 域名的一段：小写字母、数字与连字符，不以连字符开头或结尾。
	hostLabelRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	allDigitsRe = regexp.MustCompile(`^[0-9]+$`)
	// 凭据标识会拼进环境变量名 FTP_<REF>_USER，只能是大写字母开头的大写字母、数字与下划线。
	credRefRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)

	likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	errBadHost    = errs.ErrBadRequest.WithMessage("地址只能是域名或 IP，不带协议、端口和路径（中文域名请填写 xn-- 开头的编码形式）")
	errBadCredRef = errs.ErrBadRequest.WithMessage("凭据标识只能用大写字母、数字和下划线，以字母开头，最多 32 位")
	errPassive    = errs.ErrBadRequest.WithMessage("目前只支持被动模式：主动模式需要资源机回连 worker，穿不过 NAT，也会暴露 worker 地址")
	errTLSPolicy  = errs.ErrBadRequest.WithMessage("当前环境只允许显式 TLS（FTPS）连接资源机，请把加密方式改为显式 TLS")
)

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

// clean 校验并补齐默认值。CredRef 只规范化与校验格式，留空的处理由调用方决定。
func (s *Service) clean(in FTPServerInput) (FTPServerInput, error) {
	var err error
	in.Name = strings.TrimSpace(in.Name)
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		return in, badRequest("名称不能为空")
	case n > maxNameRunes:
		return in, badRequest("名称不能超过 %d 个字", maxNameRunes)
	}
	if in.Host, err = s.cleanHost(in.Host); err != nil {
		return in, err
	}
	if in.Port == 0 {
		in.Port = DefaultPort
	}
	if in.Port < 1 || in.Port > 65535 {
		return in, badRequest("端口为 1–65535")
	}
	switch in.TLS {
	case "":
		in.TLS = TLSExplicit
	case TLSExplicit:
	case TLSNone, TLSImplicit:
		if s.policy.ExplicitTLSOnly {
			return in, errTLSPolicy
		}
	default:
		return in, badRequest("加密方式只能是 none（不加密）、explicit（显式 TLS）或 implicit（隐式 TLS）")
	}
	if !in.Passive {
		return in, errPassive
	}
	in.CredRef = strings.ToUpper(strings.TrimSpace(in.CredRef))
	if in.CredRef != "" && !credRefRe.MatchString(in.CredRef) {
		return in, errBadCredRef
	}
	switch in.Status {
	case "":
		in.Status = StatusActive
	case StatusActive, StatusInactive:
	default:
		return in, badRequest("状态只能是 active 或 inactive")
	}
	return in, nil
}

// cleanHost 规范化地址（转小写，IP 转标准写法）。IP 字面量按出站策略校验，域名在 worker 连接前解析校验。
func (s *Service) cleanHost(h string) (string, error) {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return "", badRequest("地址不能为空")
	}
	if len(h) > maxHostLen {
		return "", badRequest("地址不能超过 %d 个字符", maxHostLen)
	}
	if ip, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")); err == nil {
		if ip.Zone() != "" {
			return "", errBadHost
		}
		if err := s.policy.Egress.Check(ip); err != nil {
			return "", badRequest("地址 %s 属于内网或保留地址段，当前环境不允许连接", ip)
		}
		return ip.Unmap().String(), nil
	}
	h = strings.TrimSuffix(h, ".")
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if !hostLabelRe.MatchString(l) {
			return "", errBadHost
		}
	}
	// 顶级域不会是纯数字；拦下 10.1 这类不完整的 IP，避免被某些解析器当成 IP
	if allDigitsRe.MatchString(labels[len(labels)-1]) {
		return "", errBadHost
	}
	return h, nil
}

// newCredRef 生成「R + 6 位随机大写字母或数字」，如 RK3J9X2。
func newCredRef() string {
	return "R" + rand.Text()[:6]
}

// cleanQuery 去掉控制字符与首尾空白并限制长度，返回已转义的 ILIKE 包含模式（空词返回空）。
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
	if q == "" {
		return "", nil
	}
	return "%" + likeEscaper.Replace(q) + "%", nil
}

// cleanPath 校验列目录的路径：空为登录目录；不能含控制字符（防止 FTP 命令注入）。
func cleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if len(p) > maxPathBytes {
		return "", badRequest("路径不能超过 %d 个字节", maxPathBytes)
	}
	if !utf8.ValidString(p) || strings.ContainsFunc(p, unicode.IsControl) {
		return "", badRequest("路径不能包含控制字符")
	}
	return p, nil
}

// limitOffset 校验分页参数（0 取默认值）。
func limitOffset(page, size int) (int, int, int, int, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = defaultPageSize
	}
	if page < 1 || page > maxPage || size < 1 || size > maxPageSize {
		return 0, 0, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", maxPage, maxPageSize)
	}
	return page, size, size, (page - 1) * size, nil
}
