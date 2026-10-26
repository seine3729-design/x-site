package moderation

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"xsite/service/internal/errs"
)

// BlockwordTopic 是屏蔽词失效消息的 topic：各实例收到后重建自动机（docs/03 第 5.5 节）。
const BlockwordTopic = "blockword"

const (
	maxWordRunes  = 100
	maxGroupRunes = 50
)

// Blockword 是一条屏蔽词记录（docs/02 第 5.7 节）。word 为规范化后的形式。
type Blockword struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Group     string    `json:"group"`
	Action    Action    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

// BlockwordFilter 是后台列表筛选：Q 模糊匹配词，Group / Action 精确匹配；空表示不筛。
type BlockwordFilter struct {
	Q      string
	Group  string
	Action string
	PageRequest
}

// BlockwordQuery 是交给仓储的已规范化查询（Pattern 为 LIKE 模式，空字段表示不筛）。
type BlockwordQuery struct {
	Pattern string
	Group   string
	Action  string
	Limit   int
	Offset  int
}

// Repo 读写 blockword 表（实现见 internal/store/pg）。
type Repo interface {
	List(ctx context.Context, q BlockwordQuery) (list []Blockword, total int64, err error)
	Create(ctx context.Context, word, group, action string) (Blockword, error)
	Delete(ctx context.Context, id int64) (Blockword, error)
	// ListAll 读全部词用于构建自动机。
	ListAll(ctx context.Context) ([]WordSpec, error)
	// Groups 返回已有的分组（去重）。
	Groups(ctx context.Context) ([]string, error)
}

// Invalidator 向所有实例广播失效（实现见 internal/store/cache.Invalidator）。
type Invalidator interface {
	Publish(ctx context.Context, topic string) error
}

// Service 是屏蔽词领域服务：管理词表并常驻一份 Aho-Corasick 自动机，词表变更后整体重建、原子替换、广播失效。
type Service struct {
	repo    Repo
	inval   Invalidator
	matcher atomic.Pointer[Matcher]
}

// NewService 创建服务并装入空自动机；进程启动后应调用 Reload 载入词表。
func NewService(repo Repo, inval Invalidator) *Service {
	s := &Service{repo: repo, inval: inval}
	s.matcher.Store(BuildMatcher(nil))
	return s
}

// Matcher 返回当前自动机（并发只读、不可变）。供评论 / 弹幕（S2-5）、昵称 / 简介（S2-9）等接入点调用。
func (s *Service) Matcher() *Matcher { return s.matcher.Load() }

// Scan 对文本做屏蔽词匹配，返回全部命中。
func (s *Service) Scan(text string) []Hit { return s.matcher.Load().Scan(text) }

// Reload 从数据库重载词表并重建自动机（进程启动、收到失效广播时调用）。
func (s *Service) Reload(ctx context.Context) error {
	specs, err := s.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	s.matcher.Store(BuildMatcher(specs))
	return nil
}

// OnInvalidate 收到失效广播后重建自动机（best-effort，失败只记日志，等下次变更或重启兜底）。
func (s *Service) OnInvalidate(ctx context.Context) {
	if err := s.Reload(ctx); err != nil {
		slog.ErrorContext(ctx, "重建屏蔽词自动机失败", "err", err)
	}
}

// List 返回后台屏蔽词列表（页码分页）。
func (s *Service) List(ctx context.Context, f BlockwordFilter) (Page[Blockword], error) {
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Blockword]{}, err
	}
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Blockword]{}, err
	}
	if f.Action != "" && !ValidAction(f.Action) {
		return Page[Blockword]{}, badRequest("处理办法只能是 block、mask 或 review")
	}
	list, total, err := s.repo.List(ctx, BlockwordQuery{
		Pattern: likePattern(q), Group: f.Group, Action: f.Action, Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[Blockword]{}, err
	}
	return Page[Blockword]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Groups 返回已有分组（供后台筛选下拉）。
func (s *Service) Groups(ctx context.Context) ([]string, error) { return s.repo.Groups(ctx) }

// Create 新增一个屏蔽词：规范化词后入库，重复词返回 409（改处理办法请先删后加）。成功后重建自动机并广播。
func (s *Service) Create(ctx context.Context, word, group, action string) (Blockword, error) {
	w := NormalizeWord(word)
	if w == "" {
		return Blockword{}, badRequest("屏蔽词不能为空")
	}
	if len([]rune(w)) > maxWordRunes {
		return Blockword{}, badRequest("屏蔽词不能超过 %d 个字", maxWordRunes)
	}
	group, err := cleanText("分组", group, maxGroupRunes, false)
	if err != nil {
		return Blockword{}, err
	}
	if !ValidAction(action) {
		return Blockword{}, badRequest("处理办法只能是 block、mask 或 review")
	}
	bw, err := s.repo.Create(ctx, w, group, action)
	if err != nil {
		if errors.Is(err, errs.ErrConflict) {
			return Blockword{}, errs.ErrConflict.WithMessage("该屏蔽词已存在（改处理办法请先删后加）")
		}
		return Blockword{}, err
	}
	s.refresh(ctx)
	return bw, nil
}

// Delete 删除一个屏蔽词，成功后重建自动机并广播。
func (s *Service) Delete(ctx context.Context, id int64) (Blockword, error) {
	bw, err := s.repo.Delete(ctx, id)
	if err != nil {
		return Blockword{}, err
	}
	s.refresh(ctx)
	return bw, nil
}

// refresh 在词表变更后本实例立即重建自动机，并广播让其它实例重建。
func (s *Service) refresh(ctx context.Context) {
	if err := s.Reload(ctx); err != nil {
		slog.ErrorContext(ctx, "重建屏蔽词自动机失败", "err", err)
	}
	if s.inval != nil {
		if err := s.inval.Publish(ctx, BlockwordTopic); err != nil {
			// 广播失败不影响本次写入：其它实例靠下次变更或重启兜底
			slog.WarnContext(ctx, "广播屏蔽词失效失败", "err", err)
		}
	}
}
