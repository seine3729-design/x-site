// Package resource 是资源与任务域（docs/02 第 4 节）：资源机 FTP 的登记、测连与列目录（S1-6，docs/specs/S1-6.md）。
// 扫描与入库在 S4-6，资源体检与预热在 S4-5。
package resource

import (
	"context"
	"time"
)

// TLSMode 是资源机的加密方式。
type TLSMode string

const (
	TLSNone     TLSMode = "none"     // 不加密，只允许在开发环境使用
	TLSExplicit TLSMode = "explicit" // 显式 TLS（AUTH TLS），非开发环境唯一允许的方式（docs/10 第 2 节）
	TLSImplicit TLSMode = "implicit" // 隐式 TLS（连接即握手，常见端口 990），只允许在开发环境使用
)

// Status 是资源机状态：停用后不参与扫描入库（S4-6），仍可测连。
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// DefaultPort 是 FTP 默认端口。
const DefaultPort = 21

// FTPServer 是一台资源机。账号密码不入库，CredRef 是凭据标识（见 CredEnv）。
type FTPServer struct {
	ID        int64
	Name      string
	Host      string
	Port      int32
	TLS       TLSMode
	Passive   bool
	CredRef   string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CredEnv 返回 worker 读取该凭据标识的账号与密码的环境变量名，如 FTP_HK1_USER、FTP_HK1_PASSWORD。
// 连不上时把这两个名字发给技术，在 worker 的密钥管理中配置（docs/06 第 31 节）。
func CredEnv(ref string) (user, password string) {
	return "FTP_" + ref + "_USER", "FTP_" + ref + "_PASSWORD"
}

// FTPServerInput 是新建与整体保存的字段。Port 为 0、TLS 与 Status 为空时取默认值；
// CredRef 为空时：新建自动生成，保存时保持原值。
type FTPServerInput struct {
	Name    string
	Host    string
	Port    int32
	TLS     TLSMode
	Passive bool
	CredRef string
	Status  Status
}

// Filter 是资源机列表的筛选：Q 匹配名称或地址。Page 从 1 开始，PageSize ≤ 100；为 0 时取默认值。
type Filter struct {
	Q        string
	Status   Status
	Page     int
	PageSize int
}

// Query 是校验后交给仓储的查询。Pattern 是已转义的 ILIKE 模式，空表示不按文本搜。
type Query struct {
	Pattern string
	Status  Status
	Limit   int
	Offset  int
}

// Page 是一页结果。
type Page[T any] struct {
	List     []T
	Total    int64
	Page     int
	PageSize int
}

// ---------- 测连与列目录 ----------

// ProbeOp 是在 worker 上执行的资源机操作。
type ProbeOp string

const (
	ProbeTest ProbeOp = "test" // 测连：读取登录目录，只报告条目数
	ProbeList ProbeOp = "list" // 列目录：返回条目
)

// ProbeRequest 是一次测连或列目录：Server 是发起时的资源机配置，Path 只用于列目录（空为登录目录）。
type ProbeRequest struct {
	Op     ProbeOp
	Server FTPServer
	Path   string
}

// StepName 是探测的步骤，按顺序执行，某一步失败后不再继续。
type StepName string

const (
	StepResolve StepName = "resolve" // 解析地址并校验出站策略
	StepConnect StepName = "connect" // 建立连接（含 TLS 握手）
	StepLogin   StepName = "login"   // 登录
	StepList    StepName = "list"    // 进入目录并读取
)

// Step 是一步的结果：Message 是给运营看的中文说明（失败原因或结果摘要）。
type Step struct {
	Name    StepName
	OK      bool
	Ms      int64
	Message string
}

// EntryType 是目录条目的类型。
type EntryType string

const (
	EntryDir  EntryType = "dir"
	EntryFile EntryType = "file"
	EntryLink EntryType = "link"
)

// Entry 是目录中的一项。ModifiedAt 为 nil 表示服务器没有给出时间；Target 是符号链接的目标。
type Entry struct {
	Name       string
	Type       EntryType
	Size       uint64
	ModifiedAt *time.Time
	Target     string
}

// MaxEntries 是列目录最多返回的条目数，超出时 ProbeResult.Truncated 为 true。
const MaxEntries = 1000

// ProbeResult 是测连或列目录的结果。OK 表示全部步骤成功；Path 是服务器返回的实际目录。
type ProbeResult struct {
	OK        bool
	ElapsedMs int64
	Steps     []Step
	Path      string
	Entries   []Entry
	Truncated bool
}

// Prober 在 worker 上执行测连与列目录（docs/03 第 9.7 节：连接由 worker 出口发起）。
// API 进程里是 jobs.FTPProbeClient（经 River 入队并等待结果）。探测本身失败写在结果的步骤里，
// 返回 error 只表示没能拿到结果（如 worker 超时未处理）。
type Prober interface {
	Probe(ctx context.Context, req ProbeRequest) (ProbeResult, error)
}
