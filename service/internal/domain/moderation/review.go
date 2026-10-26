package moderation

import (
	"context"
	"time"
)

// ---------- 先审后显开关（site_config.comment_policy） ----------

// CommentPolicy 是评论策略。pre_moderation=true 时所有评论先进待审（docs/06 第 24 节）。
type CommentPolicy struct {
	PreModeration bool `json:"pre_moderation"`
}

// PolicyRepo 读写评论策略（site_config.comment_policy 段，实现见 internal/store/pg）。
type PolicyRepo interface {
	GetCommentPolicy(ctx context.Context) (CommentPolicy, error)
	SetCommentPolicy(ctx context.Context, p CommentPolicy) (CommentPolicy, error)
}

// PolicyService 提供评论策略的读写，并实现 Policy（供 CommentService 判断先审后显）。
type PolicyService struct {
	repo PolicyRepo
}

func NewPolicyService(repo PolicyRepo) *PolicyService { return &PolicyService{repo: repo} }

// Get 返回当前评论策略。
func (s *PolicyService) Get(ctx context.Context) (CommentPolicy, error) {
	return s.repo.GetCommentPolicy(ctx)
}

// Set 整体保存评论策略。
func (s *PolicyService) Set(ctx context.Context, p CommentPolicy) (CommentPolicy, error) {
	return s.repo.SetCommentPolicy(ctx, p)
}

// PreModeration 实现 Policy：读 comment_policy.pre_moderation。
func (s *PolicyService) PreModeration(ctx context.Context) (bool, error) {
	p, err := s.repo.GetCommentPolicy(ctx)
	if err != nil {
		return false, err
	}
	return p.PreModeration, nil
}

// ---------- 评论 / 弹幕审核 ----------

// 审核对象类型。
const (
	KindComment = "comment"
	KindDanmaku = "danmaku"
)

// 审核操作。
const (
	ActionApprove = "approve" // 通过并显示 → visible
	ActionHide    = "hide"    // 隐藏 → hidden
	ActionDelete  = "delete"  // 删除（评论软删可还原；弹幕硬删不可恢复）
)

const (
	moderationScanCap  = 500 // 「只看含屏蔽词」在服务层扫描的候选上限（按最新优先）
	moderationBatchMax = 200 // 单次批量操作的最多条数
)

// ModerationItem 是审核列表的一项（评论与弹幕统一视图；按 kind 取用各自字段）。
type ModerationItem struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	EpisodeID   int64      `json:"episode_id"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	Author      AuthorView `json:"author"`
	ReportCount int64      `json:"report_count"`
	HitWords    []string   `json:"hit_words"`
	CreatedAt   time.Time  `json:"created_at"`
	// 评论特有
	ParentID  int64 `json:"parent_id,omitempty"`
	LikeCount int64 `json:"like_count,omitempty"`
	// 弹幕特有
	TimeMS int    `json:"time_ms,omitempty"`
	Color  string `json:"color,omitempty"`
}

// ModerationFilter 是审核列表的筛选条件。
type ModerationFilter struct {
	Status    string // pending / visible / hidden
	Reported  bool   // 只看被举报
	Blockword bool   // 只看含屏蔽词（服务层扫描）
	Q         string // 搜内容或用户名
	PageRequest
}

// ModerationQuery 是仓储层的审核查询参数（Pattern 已是 LIKE 模式）。
type ModerationQuery struct {
	Status   string
	Reported bool
	Pattern  string
	Limit    int
	Offset   int
}

// ModerationRepo 读审核列表、改状态 / 删除（实现见 internal/store/pg）。
type ModerationRepo interface {
	ListComments(ctx context.Context, q ModerationQuery) ([]ModerationItem, error)
	CountComments(ctx context.Context, q ModerationQuery) (int64, error)
	ListDanmaku(ctx context.Context, q ModerationQuery) ([]ModerationItem, error)
	CountDanmaku(ctx context.Context, q ModerationQuery) (int64, error)
	SetCommentsStatus(ctx context.Context, ids []int64, status string) (int64, error)
	SoftDeleteComments(ctx context.Context, ids []int64) (int64, error)
	SetDanmakuStatus(ctx context.Context, ids []int64, status string) (int64, error)
	DeleteDanmaku(ctx context.Context, ids []int64) (int64, error)
}

// ModerationService 处理评论 / 弹幕审核（docs/06 第 24 节）。hit_words 由进程内屏蔽词 matcher 扫描得到。
type ModerationService struct {
	repo ModerationRepo
	eval Evaluator
}

func NewModerationService(repo ModerationRepo, eval Evaluator) *ModerationService {
	return &ModerationService{repo: repo, eval: eval}
}

// List 返回某类（评论 / 弹幕）的审核列表。Blockword=true 时在服务层按最新 moderationScanCap 条扫描后筛选含屏蔽词的。
func (s *ModerationService) List(ctx context.Context, kind string, f ModerationFilter) (Page[ModerationItem], error) {
	if kind != KindComment && kind != KindDanmaku {
		return Page[ModerationItem]{}, badRequest("审核对象只能是 comment 或 danmaku")
	}
	if f.Status != "" && f.Status != StatusPending && f.Status != StatusVisible && f.Status != StatusHidden {
		return Page[ModerationItem]{}, badRequest("状态只能是 pending、visible 或 hidden")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[ModerationItem]{}, err
	}
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[ModerationItem]{}, err
	}
	mq := ModerationQuery{Status: f.Status, Reported: f.Reported, Pattern: likePattern(q)}

	if f.Blockword {
		mq.Limit, mq.Offset = moderationScanCap, 0
		items, err := s.list(ctx, kind, mq)
		if err != nil {
			return Page[ModerationItem]{}, err
		}
		kept := make([]ModerationItem, 0, len(items))
		for _, it := range items {
			if hits := s.eval.Evaluate(it.Content).Hits; len(hits) > 0 {
				it.HitWords = hits
				kept = append(kept, it)
			}
		}
		return Page[ModerationItem]{List: nonNil(pageSlice(kept, offset, limit)), Total: int64(len(kept)), Page: p.Page, PageSize: p.PageSize}, nil
	}

	mq.Limit, mq.Offset = limit, offset
	items, err := s.list(ctx, kind, mq)
	if err != nil {
		return Page[ModerationItem]{}, err
	}
	total, err := s.count(ctx, kind, mq)
	if err != nil {
		return Page[ModerationItem]{}, err
	}
	for i := range items {
		// 没命中屏蔽词时 Hits 是 nil，直接下发会变成 null，后台页面对它 .map() 会整页白屏
		items[i].HitWords = nonNil(s.eval.Evaluate(items[i].Content).Hits)
	}
	return Page[ModerationItem]{List: nonNil(items), Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

func (s *ModerationService) list(ctx context.Context, kind string, q ModerationQuery) ([]ModerationItem, error) {
	if kind == KindComment {
		return s.repo.ListComments(ctx, q)
	}
	return s.repo.ListDanmaku(ctx, q)
}

func (s *ModerationService) count(ctx context.Context, kind string, q ModerationQuery) (int64, error) {
	if kind == KindComment {
		return s.repo.CountComments(ctx, q)
	}
	return s.repo.CountDanmaku(ctx, q)
}

// Act 对一批评论 / 弹幕执行审核操作，返回实际影响条数。
func (s *ModerationService) Act(ctx context.Context, kind, action string, ids []int64) (int64, error) {
	if kind != KindComment && kind != KindDanmaku {
		return 0, badRequest("审核对象只能是 comment 或 danmaku")
	}
	if action != ActionApprove && action != ActionHide && action != ActionDelete {
		return 0, badRequest("操作只能是 approve、hide 或 delete")
	}
	ids = dedupIDs(ids)
	if len(ids) == 0 {
		return 0, badRequest("请选择要操作的条目")
	}
	if len(ids) > moderationBatchMax {
		return 0, badRequest("单次最多操作 %d 条", moderationBatchMax)
	}
	switch kind {
	case KindComment:
		switch action {
		case ActionApprove:
			return s.repo.SetCommentsStatus(ctx, ids, StatusVisible)
		case ActionHide:
			return s.repo.SetCommentsStatus(ctx, ids, StatusHidden)
		default:
			return s.repo.SoftDeleteComments(ctx, ids)
		}
	default:
		switch action {
		case ActionApprove:
			return s.repo.SetDanmakuStatus(ctx, ids, StatusVisible)
		case ActionHide:
			return s.repo.SetDanmakuStatus(ctx, ids, StatusHidden)
		default:
			return s.repo.DeleteDanmaku(ctx, ids)
		}
	}
}

// pageSlice 从已过滤的切片里取第 offset 起的 limit 条。
func pageSlice(items []ModerationItem, offset, limit int) []ModerationItem {
	if offset >= len(items) {
		return []ModerationItem{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// dedupIDs 去重并丢弃非正数 id，保持原有顺序。
func dedupIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
