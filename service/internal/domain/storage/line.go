package storage

import (
	"context"
	"time"
)

// 线路（docs/12 第 3 节，S1-6b）：一个存储源可挂多条线路，线路按地区规则决定给哪些用户。

type LineKind string

const (
	LineVideo LineKind = "video"
	LineImage LineKind = "image"
)

type AccessMode string

const (
	AccessPresign AccessMode = "presign" // 直接给存储预签名 URL
	AccessCDN     AccessMode = "cdn"     // 经 CDN 域名访问
	AccessPublic  AccessMode = "public"  // 公开读（仅封面等公开资源）
)

type SignType string

const (
	SignS3Presign SignType = "s3_presign"
	SignHMACToken SignType = "hmac_token" // 目录前缀签名
	SignCDNVendor SignType = "cdn_vendor" // CDN 厂商 URL 鉴权
	SignNone      SignType = "none"
)

type RegionScope string

const (
	ScopeRules  RegionScope = "rules"  // 按地区规则
	ScopeGlobal RegionScope = "global" // 兜底，所有地区可用
)

type VideoProcess string

const (
	ProcessStandard    VideoProcess = "standard"
	ProcessTextToImage VideoProcess = "text_to_image" // 历史兼容的旧混淆封装
)

type LineStatus string

const (
	LineActive   LineStatus = "active"
	LineWatch    LineStatus = "watch" // 观察中：仍下发但排在后面
	LineDisabled LineStatus = "disabled"
)

type RuleEffect string

const (
	EffectInclude RuleEffect = "include"
	EffectExclude RuleEffect = "exclude"
)

const (
	LineDefaultTokenTTL  = 600
	LineDefaultWeight    = 100
	LineDefaultProbePath = "/_probe/256k.bin"
	maxLineNameRunes     = 64
	maxNoteRunes         = 200
	maxPriority          = 10000
	maxBoost             = 100
)

// Rule 是线路的一条地区规则。
type Rule struct {
	ID       int64      `json:"id"`
	LineID   int64      `json:"line_id"`
	Country  string     `json:"country"`
	Province string     `json:"province"`
	ISP      string     `json:"isp"`
	ASN      string     `json:"asn"`
	Effect   RuleEffect `json:"effect"`
	Boost    int32      `json:"boost"`
}

// Line 是一条访问线路，Rules 为其地区规则（region_scope=rules 时生效）。
type Line struct {
	ID           int64        `json:"id"`
	ProviderID   int64        `json:"provider_id"`
	Kind         LineKind     `json:"kind"`
	Name         string       `json:"name"`
	BaseURL      string       `json:"base_url"`
	AccessMode   AccessMode   `json:"access_mode"`
	SignType     SignType     `json:"sign_type"`
	SignKeyRef   string       `json:"sign_key_ref"`
	TokenTTL     int32        `json:"token_ttl"`
	Priority     int32        `json:"priority"`
	Weight       int32        `json:"weight"`
	RegionScope  RegionScope  `json:"region_scope"`
	VideoProcess VideoProcess `json:"video_process"`
	ProbePath    string       `json:"probe_path"`
	Status       LineStatus   `json:"status"`
	Note         string       `json:"note"`
	Rules        []Rule       `json:"rules"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// LineInput 是新建与整体保存线路的字段。ProviderID 仅新建时使用（线路不能换存储源）。
type LineInput struct {
	ProviderID   int64
	Kind         LineKind
	Name         string
	BaseURL      string
	AccessMode   AccessMode
	SignType     SignType
	SignKeyRef   string
	TokenTTL     int32
	Priority     int32
	Weight       int32
	RegionScope  RegionScope
	VideoProcess VideoProcess
	ProbePath    string
	Status       LineStatus
	Note         string
}

// RuleInput 是整体替换地区规则时的一条规则。
type RuleInput struct {
	Country  string
	Province string
	ISP      string
	ASN      string
	Effect   RuleEffect
	Boost    int32
}

// LineFilter 是线路列表的筛选。ProviderID 为 0 表示全部存储源。
type LineFilter struct {
	ProviderID int64
	Kind       LineKind
	Status     LineStatus
}

// LineQuery 是校验后交给仓储的查询。
type LineQuery struct {
	ProviderID int64
	Kind       LineKind
	Status     LineStatus
}

// ---------- 线路测试 ----------

// LineProbeRequest 是一次线路测试：对 base_url + probe_path 发起 HTTP 请求，检查状态、类型与 CORS。
type LineProbeRequest struct {
	BaseURL   string
	ProbePath string
	// Origins 是站点入口域名，用于检查线路是否对它们返回 CORS 头（hls.js / BlobImage 用 fetch 读响应体）。
	Origins []string
}

// LineProbeResult 是线路测试的结果（探测失败也返回，原因写在 message）。
type LineProbeResult struct {
	OK          bool   `json:"ok"`
	ElapsedMs   int64  `json:"elapsed_ms"`
	URL         string `json:"url"`
	HTTPStatus  int    `json:"http_status"`
	ContentType string `json:"content_type"`
	CORS        bool   `json:"cors"`        // 是否返回了允许站点域名的 CORS 头
	CORSOrigin  string `json:"cors_origin"` // 实际返回的 Access-Control-Allow-Origin
	Message     string `json:"message"`
	// 签名自检（S1-6e）：用线路当前的签名配置试生成一个示例地址，确认密钥已注入、算法可用。
	Sign SignCheck `json:"sign"`
}

// SignCheck 是线路签名配置的自检结果。SampleURL 已脱敏（去掉签名值本身），只用于确认形态正确。
type SignCheck struct {
	OK        bool     `json:"ok"`
	Type      SignType `json:"type"`
	SampleURL string   `json:"sample_url"`
	Message   string   `json:"message"`
}

// LineProber 在 API 进程内对线路做 HTTP 探测（自定义域名经 netguard 校验出站地址）。
type LineProber interface {
	ProbeLine(ctx context.Context, req LineProbeRequest) LineProbeResult
}
