package storage

import "context"

// Repo 读写存储源。查不到时返回 errs.ErrNotFound。传入的 Row 已由 Service 校验、补齐默认值并完成凭据加密。
type Repo interface {
	List(ctx context.Context, q Query) ([]Row, int64, error)
	Get(ctx context.Context, id int64) (Row, error)
	Create(ctx context.Context, row Row) (Row, error)
	Update(ctx context.Context, id int64, row Row) (Row, error)
	// Delete 删除存储源（设置类数据，不进回收站），返回删除前的记录。
	Delete(ctx context.Context, id int64) (Row, error)

	// 线路与地区规则（S1-6b）。返回的 Line 不含 Rules，由 Service 组装。
	ListLines(ctx context.Context, q LineQuery) ([]Line, error)
	GetLine(ctx context.Context, id int64) (Line, error)
	CreateLine(ctx context.Context, in LineInput) (Line, error)
	UpdateLine(ctx context.Context, id int64, in LineInput) (Line, error)
	DeleteLine(ctx context.Context, id int64) (Line, error)
	// ListRulesByProvider 返回某存储源下全部线路的规则（供列表一次性组装）。
	ListRulesByProvider(ctx context.Context, providerID int64) ([]Rule, error)
	ListRulesByLine(ctx context.Context, lineID int64) ([]Rule, error)
	// ReplaceLineRules 在一个事务内整体替换某线路的地区规则，返回替换后的规则。
	ReplaceLineRules(ctx context.Context, lineID int64, rules []RuleInput) ([]Rule, error)

	// 存储全局设置（S1-6d 设置部分）。单行，始终存在。
	GetSettings(ctx context.Context) (Settings, error)
	UpdateSettings(ctx context.Context, in SettingsInput) (Settings, error)

	// 媒体副本（S1-6f）。
	ReplicaRepo
	// 孤立产物的标记与清理（S1-6f）。
	CleanupRepo
}
