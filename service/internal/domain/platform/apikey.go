package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 开放平台 API Key（docs/06 第 32 节，A4-3）：供采集程序等服务端调用方对接。
//
// 完整密钥只在生成时返回一次，系统里只留哈希与开头几位。这是「只存开头几位，完整密钥仅生成时显示一次」
// 的实现：后台列表、审计记录、日志里都不可能出现完整密钥，丢了只能重新生成。

// APIKeyPrefix 是密钥的固定前缀，便于在日志与代码仓库扫描里认出这是一把 x-site 的密钥。
const APIKeyPrefix = "xsk_"

// keyRandomBytes 是密钥的随机部分长度。32 字节 = 256 bit，base64url 编码后 43 个字符。
const keyRandomBytes = 32

// keyHintLen 是保留在库里的开头几位长度（含前缀），如 xsk_a1b2c3。
const keyHintLen = 10

// APIScope 是密钥的权限范围。
type APIScope string

const (
	// ScopeDramaPush 允许推送剧集入库（S4-6 的采集接口）。
	ScopeDramaPush APIScope = "drama.push"
	// ScopeAnalyticsRead 允许只读运营数据。
	ScopeAnalyticsRead APIScope = "analytics.read"
)

// APIScopeInfo 描述一个权限范围，页面上直接展示。
type APIScopeInfo struct {
	Scope APIScope `json:"scope"`
	Title string   `json:"title"`
	Desc  string   `json:"desc"`
}

// apiScopes 是全部权限范围，按页面展示顺序排列。
var apiScopes = []APIScopeInfo{
	{Scope: ScopeDramaPush, Title: "推送剧集入库", Desc: "允许调用方提交剧集与分集数据。入库的内容为草稿，命中屏蔽词会被拒收。"},
	{Scope: ScopeAnalyticsRead, Title: "只读运营数据", Desc: "允许读取看板与剧集分析的汇总数据。不含用户个人信息，也不能写入任何内容。"},
}

// APIScopes 返回全部权限范围。
func APIScopes() []APIScopeInfo {
	out := make([]APIScopeInfo, len(apiScopes))
	copy(out, apiScopes)
	return out
}

// KeyStatus 是密钥的启用状态（库里存的）。
type KeyStatus string

const (
	KeyActive   KeyStatus = "active"
	KeyInactive KeyStatus = "inactive"
)

// KeyDisplayStatus 是后台显示的状态（docs/06 第 32 节：可用 / 停用 / 未用过）。
type KeyDisplayStatus string

const (
	// KeyDisplayUnused 是启用中但从未被调用过 —— 常见于刚生成还没交付给调用方。
	KeyDisplayUnused KeyDisplayStatus = "unused"
	KeyDisplayActive KeyDisplayStatus = "active"
	KeyDisplayOff    KeyDisplayStatus = "inactive"
)

// APIKey 是一把密钥。Hint 是开头几位，完整密钥不在这里（也不在库里）。
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner"`
	Hint       string     `json:"hint"`
	Scopes     []APIScope `json:"scopes"`
	Status     KeyStatus  `json:"status"`
	UsedCount  int64      `json:"used_count"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedBy  int64      `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// DisplayStatus 由 Status 与 LastUsedAt 算出，页面直接用。
	DisplayStatus KeyDisplayStatus `json:"display_status"`
}

// NewAPIKey 是生成后的结果。Secret 是完整密钥，**只在这一次返回**。
type NewAPIKey struct {
	APIKey
	Secret string `json:"secret"`
}

// APIKeyInput 是生成一把密钥的入参。
type APIKeyInput struct {
	Name   string
	Owner  string
	Scopes []APIScope
}

// APIKeyFilter 是后台列表的筛选。
type APIKeyFilter struct {
	Q        string
	Status   KeyStatus
	Page     int
	PageSize int
}

// APIKeyQuery 是校验后交给仓储的查询。
type APIKeyQuery struct {
	Pattern string
	Status  KeyStatus
	Limit   int
	Offset  int
}

// APIKeyRepo 读写密钥。
type APIKeyRepo interface {
	ListAPIKeys(ctx context.Context, q APIKeyQuery) ([]APIKey, int64, error)
	CreateAPIKey(ctx context.Context, in APIKeyInput, keyHash, hint string, createdBy int64) (APIKey, error)
	GetAPIKey(ctx context.Context, id int64) (APIKey, error)
	UpdateAPIKey(ctx context.Context, id int64, name, owner string) (APIKey, error)
	SetAPIKeyStatus(ctx context.Context, id int64, status KeyStatus) (APIKey, error)
	// AuthenticateAPIKey 按哈希查启用中的密钥（S4-6 的调用方鉴权用）。
	AuthenticateAPIKey(ctx context.Context, keyHash string) (id int64, name string, scopes []APIScope, err error)
	TouchAPIKeyUsage(ctx context.Context, id int64) error
}

// APIKeyService 是后台的 API Key 管理。
type APIKeyService struct {
	repo APIKeyRepo
	now  func() time.Time
}

func NewAPIKeyService(repo APIKeyRepo) *APIKeyService {
	return &APIKeyService{repo: repo, now: time.Now}
}

const (
	apiKeyDefaultPageSize = 20
	apiKeyMaxPageSize     = 100
	apiKeyMaxPage         = 10000
	apiKeyMaxName         = 60
	apiKeyMaxOwner        = 60
	apiKeyMaxQueryRunes   = 60
)

var apiKeyLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// List 返回密钥的一页。
func (s *APIKeyService) List(ctx context.Context, f APIKeyFilter) ([]APIKey, int64, int, int, error) {
	q, page, size, err := cleanAPIKeyFilter(f)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	list, total, err := s.repo.ListAPIKeys(ctx, q)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	for i := range list {
		list[i].fillDisplayStatus()
	}
	return list, total, page, size, nil
}

// Create 生成一把密钥。返回值里的 Secret 是唯一一次能拿到完整密钥的机会。
func (s *APIKeyService) Create(ctx context.Context, in APIKeyInput, createdBy int64) (NewAPIKey, error) {
	clean, err := cleanAPIKeyInput(in)
	if err != nil {
		return NewAPIKey{}, err
	}
	secret, err := generateKey()
	if err != nil {
		return NewAPIKey{}, err
	}
	row, err := s.repo.CreateAPIKey(ctx, clean, HashAPIKey(secret), keyHint(secret), createdBy)
	if err != nil {
		return NewAPIKey{}, err
	}
	row.fillDisplayStatus()
	return NewAPIKey{APIKey: row, Secret: secret}, nil
}

// Update 改用途与使用方。
//
// 刻意不支持改权限范围：改范围等于改这把密钥能做什么，而密钥已经在调用方手里了。
// 要缩小或扩大权限应当重新生成一把、换掉旧的，这样「谁在用什么权限」始终可追溯。
func (s *APIKeyService) Update(ctx context.Context, id int64, in APIKeyInput) (APIKey, error) {
	name, owner, err := cleanAPIKeyText(in.Name, in.Owner)
	if err != nil {
		return APIKey{}, err
	}
	row, err := s.repo.UpdateAPIKey(ctx, id, name, owner)
	if err != nil {
		return APIKey{}, err
	}
	row.fillDisplayStatus()
	return row, nil
}

// SetStatus 停用或重新启用。停用即时生效：鉴权只认 status='active' 的密钥。
func (s *APIKeyService) SetStatus(ctx context.Context, id int64, status KeyStatus) (APIKey, error) {
	if status != KeyActive && status != KeyInactive {
		return APIKey{}, badRequest("状态只能是 active（可用）或 inactive（停用）")
	}
	row, err := s.repo.SetAPIKeyStatus(ctx, id, status)
	if err != nil {
		return APIKey{}, err
	}
	row.fillDisplayStatus()
	return row, nil
}

// Get 返回一把密钥。
func (s *APIKeyService) Get(ctx context.Context, id int64) (APIKey, error) {
	row, err := s.repo.GetAPIKey(ctx, id)
	if err != nil {
		return APIKey{}, err
	}
	row.fillDisplayStatus()
	return row, nil
}

// Authenticate 校验调用方给的密钥，返回它的权限范围（S4-6 接入时用）。
//
// 失败一律返回同一个错误，不区分「格式不对」「不存在」「已停用」：
// 区分了就等于告诉试探者哪一把密钥真实存在。
func (s *APIKeyService) Authenticate(ctx context.Context, secret string) (int64, []APIScope, error) {
	if !strings.HasPrefix(secret, APIKeyPrefix) {
		return 0, nil, errKeyInvalid
	}
	id, _, scopes, err := s.repo.AuthenticateAPIKey(ctx, HashAPIKey(secret))
	if err != nil {
		return 0, nil, errKeyInvalid
	}
	return id, scopes, nil
}

// errKeyInvalid 是鉴权失败的唯一错误。
var errKeyInvalid = badRequest("API Key 无效或已停用")

// TouchUsage 记录一次调用（计数与最近使用时间）。
func (s *APIKeyService) TouchUsage(ctx context.Context, id int64) error {
	return s.repo.TouchAPIKeyUsage(ctx, id)
}

// HashAPIKey 计算密钥的哈希。
//
// 用 SHA-256 而不是 argon2 / bcrypt：密钥是 32 字节随机串、熵足够高，没有字典或暴力破解的空间，
// 而慢哈希会让每个 API 请求都付一次 KDF 的代价。管理员密码是人选的口令，那里必须用慢哈希。
func HashAPIKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// EqualHash 常量时间比较两个哈希（供调用方自行比对时使用）。
func EqualHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// generateKey 生成一把新密钥：前缀 + 32 字节随机数的 base64url 编码。
func generateKey() (string, error) {
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 API Key: %w", err)
	}
	return APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// keyHint 取密钥开头几位存库，供后台识别与搜索。
func keyHint(secret string) string {
	if len(secret) <= keyHintLen {
		return secret
	}
	return secret[:keyHintLen]
}

// fillDisplayStatus 算出页面显示的状态（docs/06 第 32 节：可用 / 停用 / 未用过）。
func (k *APIKey) fillDisplayStatus() {
	switch {
	case k.Status != KeyActive:
		k.DisplayStatus = KeyDisplayOff
	case k.LastUsedAt == nil:
		// 启用中但没被调用过：常见于刚生成还没交付给调用方，和「在用」区分开便于排查对接问题
		k.DisplayStatus = KeyDisplayUnused
	default:
		k.DisplayStatus = KeyDisplayActive
	}
}

func cleanAPIKeyInput(in APIKeyInput) (APIKeyInput, error) {
	name, owner, err := cleanAPIKeyText(in.Name, in.Owner)
	if err != nil {
		return APIKeyInput{}, err
	}
	if len(in.Scopes) == 0 {
		return APIKeyInput{}, badRequest("至少要选一个权限范围")
	}
	known := make([]APIScope, 0, len(apiScopes))
	for _, s := range apiScopes {
		known = append(known, s.Scope)
	}
	seen := map[APIScope]bool{}
	out := make([]APIScope, 0, len(in.Scopes))
	for _, s := range in.Scopes {
		if !slices.Contains(known, s) {
			names := make([]string, 0, len(known))
			for _, k := range known {
				names = append(names, string(k))
			}
			return APIKeyInput{}, badRequest("权限范围 %q 不存在（可选：%s）", s, strings.Join(names, "、"))
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return APIKeyInput{Name: name, Owner: owner, Scopes: out}, nil
}

func cleanAPIKeyText(name, owner string) (string, string, error) {
	name = strings.TrimSpace(name)
	owner = strings.TrimSpace(owner)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", "", badRequest("请填写用途（这把密钥做什么用）")
	case n > apiKeyMaxName:
		return "", "", badRequest("用途不能超过 %d 个字", apiKeyMaxName)
	}
	if utf8.RuneCountInString(owner) > apiKeyMaxOwner {
		return "", "", badRequest("使用方不能超过 %d 个字", apiKeyMaxOwner)
	}
	return name, owner, nil
}

func cleanAPIKeyFilter(f APIKeyFilter) (APIKeyQuery, int, int, error) {
	var q APIKeyQuery
	word := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, f.Q))
	if utf8.RuneCountInString(word) > apiKeyMaxQueryRunes {
		return APIKeyQuery{}, 0, 0, badRequest("搜索词不能超过 %d 个字", apiKeyMaxQueryRunes)
	}
	if word != "" {
		q.Pattern = "%" + apiKeyLikeEscaper.Replace(word) + "%"
	}
	switch f.Status {
	case "", KeyActive, KeyInactive:
		q.Status = f.Status
	default:
		return APIKeyQuery{}, 0, 0, badRequest("状态筛选只能是 active 或 inactive")
	}

	page, size := f.Page, f.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = apiKeyDefaultPageSize
	}
	if page < 1 || page > apiKeyMaxPage || size < 1 || size > apiKeyMaxPageSize {
		return APIKeyQuery{}, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", apiKeyMaxPage, apiKeyMaxPageSize)
	}
	q.Limit, q.Offset = size, (page-1)*size
	return q, page, size, nil
}
