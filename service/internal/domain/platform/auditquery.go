package platform

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// AuditFilter 是审计列表的筛选条件（docs/06 第 34 节）。
type AuditFilter struct {
	Q          string // 搜操作人、动作或对象
	Action     string
	TargetType string
	AdminID    int64
	From       *time.Time
	To         *time.Time
	Page       int
	PageSize   int
}

// AuditQuery 是校验后交给仓储的查询。Pattern 是已转义的 ILIKE 模式，空表示不按文本搜。
type AuditQuery struct {
	Pattern    string
	Action     string
	TargetType string
	AdminID    int64
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// AuditItem 是审计列表的一项。不含改动内容：diff 可能很大，详情单独取。
type AuditItem struct {
	ID         int64     `json:"id"`
	AdminID    int64     `json:"admin_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	IPPrefix   string    `json:"ip_prefix"`
	RequestID  string    `json:"request_id"`
	CreatedAt  time.Time `json:"created_at"`
	Chained    bool      `json:"chained"`  // 是否在哈希链上（启用之前的记录为 false）
	HasDiff    bool      `json:"has_diff"` // 有改动前后内容可看
}

// AuditDetail 是单条审计的详情，含改动前后。
type AuditDetail struct {
	AuditItem
	Before   json.RawMessage `json:"before"`
	After    json.RawMessage `json:"after"`
	PrevHash string          `json:"prev_hash"`
	Hash     string          `json:"hash"`
}

// AuditFacet 是筛选下拉的一项。
type AuditFacet struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// AuditAdminFacet 是操作人下拉的一项。
type AuditAdminFacet struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Count    int64  `json:"count"`
}

// AuditFacets 是全部筛选选项。
type AuditFacets struct {
	Actions     []AuditFacet      `json:"actions"`
	TargetTypes []AuditFacet      `json:"target_types"`
	Admins      []AuditAdminFacet `json:"admins"`
}

// AuditQueryRepo 查询审计记录。
type AuditQueryRepo interface {
	ListAudit(ctx context.Context, q AuditQuery) ([]AuditItem, int64, error)
	GetAudit(ctx context.Context, id int64) (AuditDetail, error)
	AuditFacets(ctx context.Context) (AuditFacets, error)
}

// AuditService 是后台审计日志的查询与校验（docs/06 第 34 节，S4-7）。
type AuditService struct {
	repo  AuditQueryRepo
	chain AuditChainRepo
}

func NewAuditService(repo AuditQueryRepo, chain AuditChainRepo) *AuditService {
	return &AuditService{repo: repo, chain: chain}
}

const (
	auditDefaultPageSize = 20
	auditMaxPageSize     = 100
	auditMaxPage         = 10000
	auditMaxQueryRunes   = 100
)

var auditLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// List 返回审计记录的一页。
func (s *AuditService) List(ctx context.Context, f AuditFilter) ([]AuditItem, int64, int, int, error) {
	q, page, size, err := cleanAuditFilter(f)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	list, total, err := s.repo.ListAudit(ctx, q)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return list, total, page, size, nil
}

// Detail 返回单条审计的详情。
func (s *AuditService) Detail(ctx context.Context, id int64) (AuditDetail, error) {
	return s.repo.GetAudit(ctx, id)
}

// Facets 返回筛选下拉的可选项。
func (s *AuditService) Facets(ctx context.Context) (AuditFacets, error) {
	return s.repo.AuditFacets(ctx)
}

// cleanAuditFilter 校验筛选条件并补默认值。
func cleanAuditFilter(f AuditFilter) (AuditQuery, int, int, error) {
	q := AuditQuery{Action: strings.TrimSpace(f.Action), TargetType: strings.TrimSpace(f.TargetType), AdminID: f.AdminID}

	word := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, f.Q))
	if utf8.RuneCountInString(word) > auditMaxQueryRunes {
		return AuditQuery{}, 0, 0, badRequest("搜索词不能超过 %d 个字", auditMaxQueryRunes)
	}
	if word != "" {
		q.Pattern = "%" + auditLikeEscaper.Replace(word) + "%"
	}
	if q.AdminID < 0 {
		return AuditQuery{}, 0, 0, badRequest("操作人编号无效")
	}
	if f.From != nil && f.To != nil && !f.To.After(*f.From) {
		return AuditQuery{}, 0, 0, badRequest("结束时间要晚于开始时间")
	}
	q.From, q.To = f.From, f.To

	page, size := f.Page, f.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = auditDefaultPageSize
	}
	if page < 1 || page > auditMaxPage || size < 1 || size > auditMaxPageSize {
		return AuditQuery{}, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", auditMaxPage, auditMaxPageSize)
	}
	q.Limit, q.Offset = size, (page-1)*size
	return q, page, size, nil
}
