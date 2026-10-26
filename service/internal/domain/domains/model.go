// Package domains 是运营后台「域名与证书」（docs/06 第 38 节、docs/09 第 2、4 节，A4-4 / S4-8）：
// 入口域名池的增删改、证书到期管理、手动生成 / 上传 SSL 证书，以及签名域名清单的发布历史与回滚。
//
// 证书的职责边界：面向用户的入口证书由部署层 ACME（certbot）自动签发（docs/09 第 2.2 节），
// 本模块的「手动生成」用标准库现场签出自签证书供下载（源站回源 mTLS、本地 / 测试 HTTPS、应急占位），
// 私钥只在响应里返回一次、不落库；「上传」则解析一张已签发证书的到期日回填到域名上，便于到期告警。
package domains

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

// CertAlertDays 是证书到期告警提前量（docs/06 第 38 节：到期前 21 天告警）。
const CertAlertDays = 21

// Role 是域名角色（docs/02 第 6.4 节、docs/09 第 2.1 节）。
type Role string

const (
	RoleWebPrimary Role = "web_primary" // Web 主入口
	RoleWebBackup  Role = "web_backup"  // Web 备用入口
	RoleNav        Role = "nav"         // 永久导航（回家的路）
	RoleAPI        Role = "api"         // API 域名池
	RoleMedia      Role = "media"       // 媒体域名
	RoleAdmin      Role = "admin"       // 运营后台
)

var roleTitles = map[Role]string{
	RoleWebPrimary: "Web 主入口",
	RoleWebBackup:  "Web 备用",
	RoleNav:        "永久导航",
	RoleAPI:        "API",
	RoleMedia:      "媒体",
	RoleAdmin:      "后台",
}

func (r Role) valid() bool { _, ok := roleTitles[r]; return ok }

// Title 返回角色中文名。
func (r Role) Title() string {
	if s, ok := roleTitles[r]; ok {
		return s
	}
	return string(r)
}

// Status 是域名状态。
type Status string

const (
	StatusActive   Status = "active"   // 启用
	StatusWatch    Status = "watch"    // 观察（仍在清单里但排在后面）
	StatusDisabled Status = "disabled" // 停用
)

var statusTitles = map[Status]string{
	StatusActive:   "启用",
	StatusWatch:    "观察",
	StatusDisabled: "停用",
}

func (s Status) valid() bool { _, ok := statusTitles[s]; return ok }

// Title 返回状态中文名。
func (s Status) Title() string {
	if t, ok := statusTitles[s]; ok {
		return t
	}
	return string(s)
}

// 证书状态（派生，供页面标色与到期告警）。
const (
	CertUnknown  = "unknown"  // 没有录入到期日
	CertOK       = "ok"       // 距到期 > 21 天
	CertExpiring = "expiring" // 距到期 ≤ 21 天
	CertExpired  = "expired"  // 已过期
)

// Entry 是一条域名记录。CertState / CertDaysLeft / RoleTitle / StatusTitle 是派生展示字段。
type Entry struct {
	ID            int64      `json:"id"`
	Host          string     `json:"host"`
	Role          Role       `json:"role"`
	RoleTitle     string     `json:"role_title"`
	Weight        int32      `json:"weight"`
	Region        string     `json:"region"`
	CDN           string     `json:"cdn"`
	DNSProvider   string     `json:"dns_provider"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
	CertState     string     `json:"cert_state"`
	CertDaysLeft  *int       `json:"cert_days_left"`
	Status        Status     `json:"status"`
	StatusTitle   string     `json:"status_title"`
	Note          string     `json:"note"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// enrich 填充派生字段。now 一般传请求时刻。
func (e *Entry) enrich(now time.Time) {
	e.RoleTitle = e.Role.Title()
	e.StatusTitle = e.Status.Title()
	e.CertState, e.CertDaysLeft = certState(e.CertExpiresAt, now)
}

// certState 按到期日算证书状态与剩余天数（向上取整，宁可显示得比实际多一点，不让人以为还来得及）。
func certState(expires *time.Time, now time.Time) (string, *int) {
	if expires == nil {
		return CertUnknown, nil
	}
	d := expires.Sub(now)
	days := int((d + 24*time.Hour - time.Nanosecond) / (24 * time.Hour)) // 向上取整
	switch {
	case d < 0:
		return CertExpired, &days
	case days <= CertAlertDays:
		return CertExpiring, &days
	default:
		return CertOK, &days
	}
}

// EntryInput 是新建 / 整体保存域名的输入。
type EntryInput struct {
	Host          string
	Role          Role
	Weight        int32
	Region        string
	CDN           string
	DNSProvider   string
	CertExpiresAt *time.Time
	Status        Status
	Note          string
}

// normalize 归一并校验输入。返回处理后的副本。
func (in EntryInput) normalize() (EntryInput, error) {
	in.Host = strings.TrimSpace(strings.ToLower(in.Host))
	if !validHost(in.Host) {
		return in, badRequest("请填写正确的域名（只填主机名，不要带协议、端口或路径）")
	}
	if !in.Role.valid() {
		return in, badRequest("域名角色不正确")
	}
	if in.Status == "" {
		in.Status = StatusActive
	}
	if !in.Status.valid() {
		return in, badRequest("域名状态不正确")
	}
	if in.Weight < 1 || in.Weight > 100 {
		return in, badRequest("权重为 1–100")
	}
	in.Region = strings.TrimSpace(in.Region)
	if in.Region == "" {
		in.Region = "*"
	}
	if utf8.RuneCountInString(in.Region) > 16 {
		return in, badRequest("地区填写不正确")
	}
	in.CDN = strings.TrimSpace(in.CDN)
	in.DNSProvider = strings.TrimSpace(in.DNSProvider)
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.CDN) > 64 {
		return in, badRequest("CDN 名称最多 64 个字符")
	}
	if utf8.RuneCountInString(in.DNSProvider) > 64 {
		return in, badRequest("DNS 服务商最多 64 个字符")
	}
	if utf8.RuneCountInString(in.Note) > 200 {
		return in, badRequest("备注最多 200 个字符")
	}
	return in, nil
}

// Filter 是域名列表的筛选。Pattern 由 service 填好。
type Filter struct {
	Status  Status
	Role    Role
	Q       string
	Pattern string
}

// Release 是一次清单发布记录。Payload 是被签名的清单正文（含 v）。
type Release struct {
	Version         int64           `json:"version"`
	Payload         json.RawMessage `json:"payload"`
	Signature       string          `json:"signature"`
	Note            string          `json:"note"`
	PublishedBy     *int64          `json:"published_by"`
	PublishedByName string          `json:"published_by_name"`
	PublishedAt     time.Time       `json:"published_at"`
}

// ReleaseRow 是写入一条清单发布的参数。
type ReleaseRow struct {
	Version     int64
	Payload     []byte
	Signature   string
	Note        string
	PublishedBy *int64
}

// ReleasePayload 是签名域名清单的正文（docs/09 第 4.1 节），不含 sig。
type ReleasePayload struct {
	V         int64       `json:"v"`
	IssuedAt  time.Time   `json:"issued_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	API       []HostEntry `json:"api"`
	Web       []WebEntry  `json:"web"`
	Media     []HostEntry `json:"media,omitempty"`
	Nav       string      `json:"nav,omitempty"`
}

// HostEntry 是清单里按权重 / 地区分发的一个主机（api、media）。
type HostEntry struct {
	Host   string `json:"host"`
	Weight int32  `json:"weight"`
	Region string `json:"region"`
}

// WebEntry 是清单里的一个 Web 入口。
type WebEntry struct {
	Host string `json:"host"`
	Role string `json:"role"` // primary / backup
}

// ReleasePreview 是待离线签名的清单预览。
type ReleasePreview struct {
	Version     int64           `json:"version"`      // 发布时将使用的版本号
	FromVersion *int64          `json:"from_version"` // 基于哪个历史版本的内容（nil = 当前域名池）
	Hosts       int             `json:"hosts"`        // 清单里一共多少个主机
	Payload     json.RawMessage `json:"payload"`      // 规范化后的待签 JSON
}

// PublishInput 是上传已签名清单的输入。
type PublishInput struct {
	Payload     json.RawMessage
	Signature   string
	Note        string
	PublishedBy *int64
}

// HealthDaily 是健康日表的一行（仓储返回的明细）。
type HealthDaily struct {
	Day                 time.Time
	Host                string
	Region              string
	ProbeOK             int64
	ProbeFail           int64
	ClientFailoverCount int64
	FailReasons         map[string]int64
}

// HealthOverview 是健康看板（按域名 + 地区聚合）。
type HealthOverview struct {
	List      []HealthRow `json:"list"`
	SinceDays int         `json:"since_days"`
	// Note 非空时说明看板为什么是空的（上报端点未接入等）。
	Note string `json:"note"`
}

// HealthRow 是看板里的一行：某域名在某地区窗口内的汇总。
type HealthRow struct {
	Host          string           `json:"host"`
	Region        string           `json:"region"`
	ProbeOK       int64            `json:"probe_ok"`
	ProbeFail     int64            `json:"probe_fail"`
	SuccessRate   *float64         `json:"success_rate"` // 无探测样本时为空
	FailoverCount int64            `json:"failover_count"`
	FailReasons   map[string]int64 `json:"fail_reasons"`
}

// validHost 粗校验主机名：只含域名字符、不带协议 / 端口 / 路径（与资源机地址一致的宽松规则）。
func validHost(h string) bool {
	if n := len(h); n < 1 || n > 253 {
		return false
	}
	if strings.ContainsAny(h, " \t/:") || strings.Contains(h, "://") {
		return false
	}
	for _, r := range h {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	// 至少要有一个点（a.b），纯主机名（localhost）在入口域名场景没有意义
	return strings.Contains(h, ".")
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}
