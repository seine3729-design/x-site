package storage

import (
	"fmt"
	"net/netip"
	"net/url"
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
	maxPrefixBytes  = 256
	maxKeyLen       = 512
	maxSecretLen    = 1024
)

var (
	// 桶名：3–63 位小写字母、数字、点、连字符，首尾为字母或数字（S3 命名规则的常见子集）。
	bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	// R2 Account ID：32 位十六进制。
	r2AccountRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

	likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
)

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

// clean 校验并补齐默认值，按存储源类型切换必填字段。凭据只做格式校验，是否必填由调用方（Create/Update）决定。
func (s *Service) clean(in Input) (Input, error) {
	var err error
	in.Name = strings.TrimSpace(in.Name)
	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		return in, badRequest("名称不能为空")
	case n > maxNameRunes:
		return in, badRequest("名称不能超过 %d 个字", maxNameRunes)
	}

	switch in.Type {
	case TypeS3, TypeR2, TypeCustom:
	default:
		return in, badRequest("存储源类型只能是 s3、r2 或 custom")
	}

	if in.Type == TypeCustom {
		switch in.Subtype {
		case SubtypeS3Compatible, SubtypeHTTPOrigin:
		default:
			return in, badRequest("自定义存储源的子类型只能是 s3_compatible（S3 兼容）或 http_origin（HTTP 源）")
		}
	} else {
		in.Subtype = SubtypeNone
	}

	switch in.Purpose {
	case PurposeVideo, PurposeImage, PurposeBoth:
	default:
		return in, badRequest("用途只能是 video、image 或 both")
	}

	switch in.Role {
	case RolePrimary, RoleMirror, RoleReadonly:
	default:
		return in, badRequest("角色只能是 primary、mirror 或 readonly")
	}

	in.Bucket = strings.TrimSpace(in.Bucket)
	in.AccountID = strings.ToLower(strings.TrimSpace(in.AccountID))
	in.Region = strings.TrimSpace(in.Region)
	if in.PathPrefix, err = cleanPrefix(in.PathPrefix); err != nil {
		return in, err
	}

	// 按类型处理端点、区域、桶、账户
	switch {
	case in.Type == TypeS3:
		in.AccountID = ""
		if in.Region == "" {
			return in, badRequest("AWS S3（兼容）存储需要填写 Region")
		}
		if in.Endpoint != "" {
			if in.Endpoint, err = s.cleanEndpoint(in.Endpoint, false); err != nil {
				return in, err
			}
		}
		if err = requireBucket(in.Bucket); err != nil {
			return in, err
		}
	case in.Type == TypeR2:
		if !r2AccountRe.MatchString(in.AccountID) {
			return in, badRequest("R2 Account ID 为 32 位十六进制字符")
		}
		if err = requireBucket(in.Bucket); err != nil {
			return in, err
		}
		in.Endpoint = "https://" + in.AccountID + ".r2.cloudflarestorage.com" // 由 account_id 自动生成（docs/12 第 2 节）
		in.Region = R2Region
	case in.Subtype == SubtypeS3Compatible:
		if in.Endpoint == "" {
			return in, badRequest("自建 S3 兼容存储需要填写 Endpoint")
		}
		if in.Endpoint, err = s.cleanEndpoint(in.Endpoint, false); err != nil {
			return in, err
		}
		if err = requireBucket(in.Bucket); err != nil {
			return in, err
		}
		if in.Region == "" {
			in.Region = DefaultS3Region
		}
		in.AccountID = ""
	case in.Subtype == SubtypeHTTPOrigin:
		if in.Endpoint == "" {
			return in, badRequest("HTTP 源需要填写 Base URL")
		}
		if in.Endpoint, err = s.cleanEndpoint(in.Endpoint, true); err != nil {
			return in, err
		}
		in.Bucket = ""
		in.Region = ""
		in.AccountID = ""
		in.PathStyle = false
	}

	if in.MaxPresignTTL == 0 {
		in.MaxPresignTTL = DefaultPresignTTL
	}
	if in.MaxPresignTTL < MinPresignTTL || in.MaxPresignTTL > MaxPresignTTL {
		return in, badRequest("预签名有效期为 %d–%d 秒", MinPresignTTL, MaxPresignTTL)
	}

	switch in.Status {
	case "":
		in.Status = StatusActive
	case StatusActive, StatusDraining, StatusDisabled:
	default:
		return in, badRequest("状态只能是 active、draining 或 disabled")
	}

	if in.WriteCred != nil {
		if err = cleanCred(in.WriteCred, "读写"); err != nil {
			return in, err
		}
	}
	if in.ReadCred != nil {
		if err = cleanCred(in.ReadCred, "只读"); err != nil {
			return in, err
		}
	}
	return in, nil
}

func requireBucket(b string) error {
	if b == "" {
		return badRequest("请填写桶名 Bucket")
	}
	if !bucketRe.MatchString(b) {
		return badRequest("桶名只能是 3–63 位小写字母、数字、点或连字符，且以字母或数字开头结尾")
	}
	return nil
}

// cleanEndpoint 校验端点地址：只允许 http(s)（非开发环境只允许 https），不带查询串 / 锚点 / 用户信息；
// allowPath 为 false 时不允许带路径（S3 端点）。IP 字面量按出站策略校验，域名在连通性测试时解析校验。
func (s *Service) cleanEndpoint(raw string, allowPath bool) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", badRequest("端点地址格式错误，应为 https://host[:port]")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || s.policy.HTTPSOnly) {
		return "", badRequest("端点地址当前环境只允许 https")
	}
	if !allowPath && u.Path != "" && u.Path != "/" {
		return "", badRequest("端点地址不要带路径，只填 https://host[:port]")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", badRequest("端点地址不要带查询串、锚点或用户信息")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		if err := s.policy.Egress.Check(ip); err != nil {
			return "", badRequest("端点地址 %s 属于内网或保留地址段，当前环境不允许连接", ip)
		}
	}
	return strings.TrimRight(raw, "/"), nil
}

func cleanPrefix(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if len(p) > maxPrefixBytes {
		return "", badRequest("路径前缀不能超过 %d 个字节", maxPrefixBytes)
	}
	if !utf8.ValidString(p) || strings.ContainsFunc(p, unicode.IsControl) {
		return "", badRequest("路径前缀不能包含控制字符")
	}
	return strings.TrimPrefix(p, "/"), nil
}

func cleanCred(c *Credential, label string) error {
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.SecretAccessKey = strings.TrimSpace(c.SecretAccessKey)
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return badRequest("%s凭据的 Access Key 与 Secret Key 必须同时填写", label)
	}
	if utf8.RuneCountInString(c.AccessKeyID) > maxKeyLen || utf8.RuneCountInString(c.SecretAccessKey) > maxSecretLen {
		return badRequest("%s凭据长度超出限制", label)
	}
	return nil
}

// secretTail 取 secret 的末 4 个字符作为显示提示（不足 4 位按实际长度）。
func secretTail(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return string(r)
	}
	return string(r[len(r)-4:])
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
