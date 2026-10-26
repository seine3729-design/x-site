package moderation

import (
	"context"
	"strconv"
	"time"

	"xsite/service/internal/errs"
)

const maxReportDetailRunes = 500

// reportTargets 是可举报的对象类型（docs/02 第 5.6 节）。
var reportTargets = map[string]bool{
	"comment":       true,
	"danmaku":       true,
	"drama":         true,
	"comment_image": true,
}

// reportReasons 是举报原因枚举（docs/02 第 5.6 节）。
var reportReasons = map[string]bool{
	"attack":  true, // 人身攻击
	"vulgar":  true, // 低俗色情
	"illegal": true, // 违法违规
	"minor":   true, // 涉未成年
	"ad":      true, // 广告引流
	"spoiler": true, // 剧透
	"other":   true, // 其他
}

// NewReport 是要创建的举报（仓储层用）。
type NewReport struct {
	Reporter   Author
	TargetType string
	TargetID   int64
	Reason     string
	Detail     string
}

// 举报状态。
const (
	ReportPending  = "pending"
	ReportResolved = "resolved"
	ReportRejected = "rejected"
)

// ReportView 是后台举报队列的一项（§25）。
type ReportView struct {
	ID         int64      `json:"id"`
	TargetType string     `json:"target_type"`
	TargetID   int64      `json:"target_id"`
	Reason     string     `json:"reason"`
	Detail     string     `json:"detail"`
	Status     string     `json:"status"`
	HandleNote string     `json:"handle_note"`
	Reporter   AuthorView `json:"reporter"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ReportFilter 是后台举报列表的筛选条件。Q 匹配说明与举报人用户名；原因 / 对象类型 / 状态为精确筛选。
type ReportFilter struct {
	Q          string
	Status     string
	TargetType string
	Reason     string
	PageRequest
}

// ReportQuery 是仓储层的举报查询参数（Pattern 已是 LIKE 模式）。
type ReportQuery struct {
	Pattern    string
	Status     string
	TargetType string
	Reason     string
	Limit      int
	Offset     int
}

// ReportRepo 读写举报表（实现见 internal/store/pg）。
type ReportRepo interface {
	Create(ctx context.Context, in NewReport) error
	List(ctx context.Context, q ReportQuery) ([]ReportView, int64, error)
	// Handle 保存处理结果（状态 + 处理说明），返回处理后的举报；不存在返回 ErrNotFound。
	Handle(ctx context.Context, id int64, status, note string) (ReportView, error)
}

// ReportService 处理举报的提交（docs/03 第 5.3 节）。举报队列的处理在后台（§25）。
// 同一举报人对同一对象 24 小时内只计一次（Valkey 去重，窗口由注入的 Limiter 的 TTL 决定）。
type ReportService struct {
	repo    ReportRepo
	limiter Limiter
}

func NewReportService(repo ReportRepo, limiter Limiter) *ReportService {
	return &ReportService{repo: repo, limiter: limiter}
}

// SubmitReportInput 是提交举报的入参。
type SubmitReportInput struct {
	Reporter   Author
	TargetType string
	TargetID   int64
	Reason     string
	Detail     string
}

// Submit 提交一条举报。身份有效（注册用户或游客）→ 校验对象类型 / 原因 / 详情 → 24 小时去重 → 落库。
// 24 小时内重复举报同一对象：幂等放行、不重复落库（docs 要求「只计一次」）。
func (s *ReportService) Submit(ctx context.Context, in SubmitReportInput) error {
	if !in.Reporter.valid() {
		return errs.ErrNeedLogin
	}
	if !reportTargets[in.TargetType] {
		return errs.ErrBadRequest.WithMessage("举报对象类型不支持")
	}
	if in.TargetID <= 0 {
		return errs.ErrBadRequest.WithMessage("举报对象无效")
	}
	if !reportReasons[in.Reason] {
		return errs.ErrBadRequest.WithMessage("举报原因不支持")
	}
	detail := cleanContent(in.Detail)
	if runeLen(detail) > maxReportDetailRunes {
		return errs.ErrBadRequest.WithMessage("补充说明最多 500 个字")
	}
	first, err := s.limiter.FirstPost(ctx, reportDedupKey(in.Reporter, in.TargetType, in.TargetID))
	if err != nil {
		return err
	}
	if !first {
		return nil // 24 小时内已举报过同一对象，幂等放行
	}
	return s.repo.Create(ctx, NewReport{
		Reporter: in.Reporter, TargetType: in.TargetType, TargetID: in.TargetID,
		Reason: in.Reason, Detail: detail,
	})
}

// reportDedupKey 复用 dedupKey：作用域 report，内容为「对象类型:对象 ID」。
func reportDedupKey(a Author, targetType string, targetID int64) string {
	return dedupKey("report", a, targetType+":"+strconv.FormatInt(targetID, 10))
}

// List 返回后台举报队列（页码分页，§25）。按状态 / 对象类型 / 原因筛选，Q 搜说明与举报人用户名。
func (s *ReportService) List(ctx context.Context, f ReportFilter) (Page[ReportView], error) {
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[ReportView]{}, err
	}
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[ReportView]{}, err
	}
	if f.Status != "" && f.Status != ReportPending && f.Status != ReportResolved && f.Status != ReportRejected {
		return Page[ReportView]{}, badRequest("状态只能是 pending、resolved 或 rejected")
	}
	if f.TargetType != "" && !reportTargets[f.TargetType] {
		return Page[ReportView]{}, badRequest("对象类型不支持")
	}
	if f.Reason != "" && !reportReasons[f.Reason] {
		return Page[ReportView]{}, badRequest("举报原因不支持")
	}
	list, total, err := s.repo.List(ctx, ReportQuery{
		Pattern: likePattern(q), Status: f.Status, TargetType: f.TargetType, Reason: f.Reason, Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[ReportView]{}, err
	}
	return Page[ReportView]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Handle 保存举报处理结果（§25）：状态只能置为 resolved（已处理）或 rejected（已驳回），附处理说明 ≤500 字。
// 处罚举报人 / 被举报人（改账号状态）依赖用户管理（§26），本切片不含。
func (s *ReportService) Handle(ctx context.Context, id int64, status, note string) (ReportView, error) {
	if status != ReportResolved && status != ReportRejected {
		return ReportView{}, badRequest("处理结果只能是已处理或已驳回")
	}
	note, err := cleanText("处理说明", note, maxReportDetailRunes, false)
	if err != nil {
		return ReportView{}, err
	}
	return s.repo.Handle(ctx, id, status, note)
}
