package content

import (
	"context"

	"xsite/service/internal/errs"
)

// 仓储把唯一键冲突映射为下面的哨兵错误（同为 409 CONFLICT，用 IsErr 区分）。
var (
	ErrSlugTaken          = errs.ErrConflict.WithMessage("slug 已被使用")
	ErrChannelSlugTaken   = errs.ErrConflict.WithMessage("slug 已被其他频道使用，频道的 slug 全站唯一")
	ErrTermCodeTaken      = errs.ErrConflict.WithMessage("编号已被其他取值使用")
	ErrEpisodeNumberTaken = errs.ErrConflict.WithMessage("该集号已存在")
	// ErrStale 由仓储在事务内发现数据已被其他人修改时返回（如合并标签时其中一个已被合并）。
	ErrStale = errs.ErrConflict.WithMessage("数据已变化，请刷新后重试")
)

// Repo 读写内容域。查不到（含回收站中的剧及其分集）时返回 errs.ErrNotFound，唯一键冲突返回上面的哨兵错误。
// 「替换」类方法在事务内先锁住所属的剧或专题，再整体删除、按传入顺序写入。
type Repo interface {
	// ---------- 剧集 ----------
	ListDramas(ctx context.Context, q DramaQuery) ([]Drama, int64, error)
	GetDrama(ctx context.Context, id int64) (Drama, error)
	CreateDrama(ctx context.Context, in DramaInput) (Drama, error)
	UpdateDrama(ctx context.Context, id int64, in DramaInput) (Drama, error)
	// SoftDeleteDrama 写 deleted_at 放进回收站，分集与关联保留；返回删除前的剧。
	SoftDeleteDrama(ctx context.Context, id int64) (Drama, error)

	ListDramaTags(ctx context.Context, dramaID int64) ([]Tag, error)
	ListDramaTerms(ctx context.Context, dramaID int64) ([]TermRef, error)
	ReplaceDramaTags(ctx context.Context, dramaID int64, tagIDs []int64) error
	ReplaceDramaTerms(ctx context.Context, dramaID int64, termIDs []int64) error

	// BulkSetStatus 改状态不同的剧（上架时没有上架时间的写入当前时间），返回受影响的剧数。
	BulkSetStatus(ctx context.Context, ids []int64, status DramaStatus) (int64, error)
	// BulkAddTags 追加标签，返回新加了标签的剧数。
	BulkAddTags(ctx context.Context, ids, tagIDs []int64) (int64, error)
	// BulkAddToTopic 按传入顺序追加到专题末尾（已在专题中的不动），返回新加入的剧数。
	BulkAddToTopic(ctx context.Context, topicID int64, ids []int64) (int64, error)
	// BulkSetTerms 先移除 singleTaxonomyIDs（单选维度）中的其他取值，再追加 termIDs，返回取值有变化的剧数。
	BulkSetTerms(ctx context.Context, ids, termIDs, singleTaxonomyIDs []int64) (int64, error)

	// 引用校验：返回其中存在的记录。
	LiveDramaIDs(ctx context.Context, ids []int64) ([]int64, error)
	TagsByIDs(ctx context.Context, ids []int64) ([]Tag, error)
	TermsByIDs(ctx context.Context, ids []int64) ([]TermRef, error)
	ExistingTermCodes(ctx context.Context, codes []string) ([]string, error)

	// ---------- 分集（所属的剧在回收站中时视为不存在） ----------
	ListEpisodes(ctx context.Context, dramaID int64) ([]Episode, error)
	GetEpisode(ctx context.Context, id int64) (Episode, error)
	// CreateEpisode、UpdateEpisode、DeleteEpisode 在事务内锁住剧，并刷新 drama.latest_episode。
	CreateEpisode(ctx context.Context, dramaID int64, in EpisodeInput) (Episode, error)
	UpdateEpisode(ctx context.Context, id int64, in EpisodeInput) (Episode, error)
	DeleteEpisode(ctx context.Context, id int64) (Episode, error)

	// ---------- 标签 ----------
	ListTags(ctx context.Context, q TagQuery) ([]Tag, int64, error)
	GetTag(ctx context.Context, id int64) (Tag, error)
	CreateTag(ctx context.Context, in TagInput) (Tag, error)
	UpdateTag(ctx context.Context, id int64, in TagInput) (Tag, error)
	// MergeTag 在事务内锁住两个标签并确认都未被合并（否则 ErrStale），把源标签的剧挂到目标标签，
	// 源标签与原先并入源标签的标签都改指目标。返回源标签原有的剧数。
	MergeTag(ctx context.Context, sourceID, targetID int64) (int64, error)

	// ---------- 维度与取值 ----------
	ListTaxonomies(ctx context.Context) ([]Taxonomy, error) // 含取值
	GetTaxonomy(ctx context.Context, id int64) (Taxonomy, error)
	CreateTaxonomy(ctx context.Context, in TaxonomyInput) (Taxonomy, error) // 排在最后
	UpdateTaxonomy(ctx context.Context, id int64, in TaxonomyInput) (Taxonomy, error)
	// DeleteTaxonomy 连同取值与剧的关联一起删除，返回删除前的维度（含取值）。
	DeleteTaxonomy(ctx context.Context, id int64) (Taxonomy, error)
	// CountMultiTermDramas 统计在该维度选了多个取值的剧数（多选改单选前检查）。
	CountMultiTermDramas(ctx context.Context, taxonomyID int64) (int64, error)
	TaxonomyIDs(ctx context.Context) ([]int64, error)
	// SetTaxonomySort 按 ids 的顺序重排（sort = 下标）。
	SetTaxonomySort(ctx context.Context, ids []int64) error

	GetTerm(ctx context.Context, taxonomyID, termID int64) (Term, error)
	CreateTerm(ctx context.Context, taxonomyID int64, in TermInput) (Term, error) // 排在最后
	UpdateTerm(ctx context.Context, taxonomyID, termID int64, in TermInput) (Term, error)
	DeleteTerm(ctx context.Context, taxonomyID, termID int64) (Term, error)
	TermIDs(ctx context.Context, taxonomyID int64) ([]int64, error)
	SetTermSort(ctx context.Context, taxonomyID int64, ids []int64) error

	// ---------- 专题 ----------
	ListTopics(ctx context.Context, q TopicQuery) ([]Topic, int64, error)
	GetTopic(ctx context.Context, id int64) (Topic, error)
	CreateTopic(ctx context.Context, in TopicInput) (Topic, error)
	UpdateTopic(ctx context.Context, id int64, in TopicInput) (Topic, error)
	DeleteTopic(ctx context.Context, id int64) (Topic, error)
	// ListTopicDramas 返回手选专题中的剧（按展示顺序，不含回收站中的剧）。
	ListTopicDramas(ctx context.Context, topicID int64) ([]TopicDrama, error)
	// ReplaceTopicDramas 整体替换手选剧；回收站中的剧保留在专题里，还原后回到原位置。
	ReplaceTopicDramas(ctx context.Context, topicID int64, dramaIDs []int64) error
}
