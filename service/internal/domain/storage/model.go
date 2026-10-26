// Package storage 是存储源域（docs/12，S1-6a）：视频 / 图片媒体资源的存储后端（S3 / Cloudflare R2 / 自定义）的
// 后台登记、凭据信封加密与连通性测试。线路 storage_line 在 S1-6b，上传与复制在 S1-6f。
package storage

import (
	"context"
	"time"
)

// Type 是存储源类型。
type Type string

const (
	TypeS3     Type = "s3"     // AWS S3 或任何 S3 兼容云存储
	TypeR2     Type = "r2"     // Cloudflare R2
	TypeCustom Type = "custom" // 自建 S3 兼容 或 只读 HTTP 源
)

// Subtype 仅在 Type=custom 时有意义。
type Subtype string

const (
	SubtypeNone         Subtype = ""              // 非 custom 类型
	SubtypeS3Compatible Subtype = "s3_compatible" // 自建 S3 兼容（SeaweedFS、Garage、Ceph RGW 等）
	SubtypeHTTPOrigin   Subtype = "http_origin"   // 只读 HTTP 源（base_url 存入 Endpoint）
)

// Purpose 是用途。
type Purpose string

const (
	PurposeVideo Purpose = "video" // 切片与清单
	PurposeImage Purpose = "image" // 封面、头像、广告图
	PurposeBoth  Purpose = "both"
)

// Role 是存储源角色。
type Role string

const (
	RolePrimary  Role = "primary"  // 新内容写入
	RoleMirror   Role = "mirror"   // 异步复制
	RoleReadonly Role = "readonly" // 只读历史资源
)

// Status 是存储源状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusDraining Status = "draining" // 不再写入，只读
	StatusDisabled Status = "disabled"
)

const (
	// DefaultPresignTTL 是预签名默认有效期（秒）。
	DefaultPresignTTL = 600
	// MinPresignTTL、MaxPresignTTL 是预签名有效期的上下限（上限为 S3/R2 协议上限 7 天）。
	MinPresignTTL = 60
	MaxPresignTTL = 7 * 24 * 3600
	// R2Region 是 Cloudflare R2 固定使用的 region（S3 协议要求非空）。
	R2Region = "auto"
	// DefaultS3Region 是 S3 兼容服务未填 region 时的默认值（自建服务通常不校验 region）。
	DefaultS3Region = "us-east-1"
)

// Credential 是一组存储访问凭据（明文，只在内存中流转，不入库、不记日志）。
type Credential struct {
	AccessKeyID     string
	SecretAccessKey string
}

// IsZero 表示未填写任何凭据。
func (c Credential) IsZero() bool { return c.AccessKeyID == "" && c.SecretAccessKey == "" }

// Row 是存储源在仓储层的完整行，含凭据密文。密文的加解密由 Service 负责，仓储只存取字节。
type Row struct {
	ID                int64
	Name              string
	Type              Type
	Subtype           Subtype
	Purpose           Purpose
	Role              Role
	Endpoint          string
	Region            string
	Bucket            string
	AccountID         string
	PathStyle         bool
	PathPrefix        string
	CredentialEnc     []byte // 读写凭据密文（信封加密的 Credential JSON）；nil 表示未配置
	ReadCredentialEnc []byte // 只读凭据密文；nil 表示未配置（预签名回退到读写凭据）
	CredTail          string // 读写凭据 secret 末 4 位（仅显示用）
	ReadCredTail      string
	MaxPresignTTL     int32
	Status            Status
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Provider 是存储源的展示模型（不含凭据密文或明文），用于列表与详情。
type Provider struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Type          Type      `json:"type"`
	Subtype       Subtype   `json:"subtype"`
	Purpose       Purpose   `json:"purpose"`
	Role          Role      `json:"role"`
	Endpoint      string    `json:"endpoint"`
	Region        string    `json:"region"`
	Bucket        string    `json:"bucket"`
	AccountID     string    `json:"account_id"`
	PathStyle     bool      `json:"path_style"`
	PathPrefix    string    `json:"path_prefix"`
	HasWriteCred  bool      `json:"has_write_cred"` // 是否已配置读写凭据
	HasReadCred   bool      `json:"has_read_cred"`  // 是否已配置只读凭据
	CredTail      string    `json:"cred_tail"`      // 读写凭据 secret 末 4 位，后台显示「已配置 ****abcd」
	ReadCredTail  string    `json:"read_cred_tail"`
	MaxPresignTTL int32     `json:"max_presign_ttl"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Provider 把仓储行转成展示模型，丢弃密文。
func (r Row) Provider() Provider {
	return Provider{
		ID: r.ID, Name: r.Name, Type: r.Type, Subtype: r.Subtype, Purpose: r.Purpose, Role: r.Role,
		Endpoint: r.Endpoint, Region: r.Region, Bucket: r.Bucket, AccountID: r.AccountID,
		PathStyle: r.PathStyle, PathPrefix: r.PathPrefix,
		HasWriteCred: len(r.CredentialEnc) > 0, HasReadCred: len(r.ReadCredentialEnc) > 0,
		CredTail: r.CredTail, ReadCredTail: r.ReadCredTail,
		MaxPresignTTL: r.MaxPresignTTL, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Input 是新建与整体保存的字段。WriteCred / ReadCred 为 nil 表示不修改（编辑时留空保持原值），
// 非 nil 时整体替换该组凭据。
type Input struct {
	Name          string
	Type          Type
	Subtype       Subtype
	Purpose       Purpose
	Role          Role
	Endpoint      string
	Region        string
	Bucket        string
	AccountID     string
	PathStyle     bool
	PathPrefix    string
	MaxPresignTTL int32
	Status        Status
	WriteCred     *Credential
	ReadCred      *Credential
}

// Filter 是列表筛选：Q 匹配名称或桶名。Page 从 1 开始，PageSize ≤ 100；为 0 时取默认值。
type Filter struct {
	Q        string
	Type     Type
	Status   Status
	Page     int
	PageSize int
}

// Query 是校验后交给仓储的查询。Pattern 是已转义的 ILIKE 模式，空表示不按文本搜。
type Query struct {
	Pattern string
	Type    Type
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

// ---------- 连通性测试 ----------

// ProbeRequest 是一次连通性测试：连接参数 + 解密后的凭据（只在进程内内存中传递）。
type ProbeRequest struct {
	Type       Type
	Subtype    Subtype
	Endpoint   string
	Region     string
	Bucket     string
	AccountID  string
	PathStyle  bool
	PathPrefix string
	TTL        int32      // 预签名有效期（秒）
	Write      Credential // 读写凭据
	Read       Credential // 只读凭据（为空时等同 Write）
}

// ProbeStepName 是连通性测试的步骤，按顺序执行，某一步失败后不再继续。
type ProbeStepName string

const (
	StepResolve ProbeStepName = "resolve" // 解析端点并校验出站策略（SSRF）
	StepPut     ProbeStepName = "put"     // 写入探测文件
	StepGet     ProbeStepName = "get"     // 读回并校验内容
	StepPresign ProbeStepName = "presign" // 生成预签名并访问
	StepDelete  ProbeStepName = "delete"  // 删除探测文件
	StepHead    ProbeStepName = "head"    // HTTP 源：HEAD 探测地址
)

// ProbeStep 是一步的结果；Message 是给运营看的中文说明（失败原因或结果摘要）。
type ProbeStep struct {
	Name    ProbeStepName `json:"name"`
	OK      bool          `json:"ok"`
	Ms      int64         `json:"ms"`
	Message string        `json:"message"`
}

// ProbeResult 是连通性测试的结果。OK 表示全部步骤成功。
type ProbeResult struct {
	OK        bool        `json:"ok"`
	ElapsedMs int64       `json:"elapsed_ms"`
	Steps     []ProbeStep `json:"steps"`
}

// Prober 执行连通性测试：写探测文件 → 读回 → 预签名访问 → 删除（docs/12 第 4.4 节）。
// 在 API 进程内直接执行（对象存储是主动 HTTPS 出站，不像资源机 FTP 需经 worker 回连）；
// 自定义端点先经 netguard 校验出站地址（docs/12 第 9 节）。探测失败写在结果的步骤里，
// 返回 error 只表示没能开始测试（如参数非法）。
type Prober interface {
	Probe(ctx context.Context, req ProbeRequest) ProbeResult
}
