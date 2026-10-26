package storage

import (
	"context"
	"fmt"
	"time"
)

// 媒体副本（docs/12 第 5 节，S1-6f）：一个媒体对象在哪些存储源上有副本。
// 产物写入 role=primary 的存储源后记一条 ready 副本，再异步复制到全部 role=mirror；
// 选源只考虑 ready 的存储源，因此镜像复制完成前不会把对应线路下发给客户端。

// ReplicaStatus 是一个副本的状态。
type ReplicaStatus string

const (
	ReplicaPending ReplicaStatus = "pending" // 已登记、尚未复制完成
	ReplicaReady   ReplicaStatus = "ready"   // 可用于选源
	ReplicaFailed  ReplicaStatus = "failed"  // 复制失败，等重试或人工处理
)

// ObjectKind 是媒体对象的类型（与 media_object.kind 一致）。
type ObjectKind string

const (
	ObjectHLSDir ObjectKind = "hls_dir" // 一集转封装产物的目录
	ObjectFile   ObjectKind = "file"    // 单个文件（封面等）
)

// Replica 是一条副本记录。
type Replica struct {
	ObjectID     int64         `json:"object_id"`
	ProviderID   int64         `json:"provider_id"`
	Status       ReplicaStatus `json:"status"`
	Error        string        `json:"error"`
	ReplicatedAt *time.Time    `json:"replicated_at"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// MediaObject 是复制任务需要的对象信息。
type MediaObject struct {
	ID        int64
	Kind      ObjectKind
	Key       string // 存储源内的相对路径（不含存储源 path_prefix）
	SizeBytes int64
	// OrphanedAt 是首次被发现不再被任何分集引用的时间（零值表示在用）。清理的保留期从它开始算。
	OrphanedAt time.Time
}

// ReplicaRepo 读写媒体副本（Repo 的一部分，单独成接口便于 worker 只依赖所需方法）。
type ReplicaRepo interface {
	// SaveReplica 记录或更新一条副本（复制重试会重复写入同一对）。
	SaveReplica(ctx context.Context, objectID, providerID int64, status ReplicaStatus, errMsg string) (Replica, error)
	// ListReplicas 返回一个对象的全部副本。
	ListReplicas(ctx context.Context, objectID int64) ([]Replica, error)
	// ReadyProviders 返回一个对象上已就绪的存储源编号。
	ReadyProviders(ctx context.Context, objectID int64) ([]int64, error)
	// CountReplicasByProvider 统计某存储源上的副本数（删除存储源前的检查）。
	CountReplicasByProvider(ctx context.Context, providerID int64) (int64, error)
	// GetObject 读取媒体对象。
	GetObject(ctx context.Context, objectID int64) (MediaObject, error)
	// DeleteReplica 删除一条副本记录。
	DeleteReplica(ctx context.Context, objectID, providerID int64) error
	// ListObjectIDs 游标分页列出媒体对象编号（手动触发复制与迁移时逐批入队）。
	ListObjectIDs(ctx context.Context, q ObjectQuery) ([]int64, error)
	// ListOrphanObjectIDs 游标分页列出还没有任何副本记录的对象（接管历史产物时用）。
	ListOrphanObjectIDs(ctx context.Context, cursorID int64, limit int) ([]int64, error)
}

// ObjectQuery 是媒体对象的筛选：DramaID / ProviderID 为 0 表示不筛选该项。
type ObjectQuery struct {
	DramaID    int64
	ProviderID int64 // 只列该存储源上有副本的对象
	CursorID   int64
	Limit      int
}

// ---------- 主存储与镜像的解析 ----------

// PlacementRepo 是解析主存储 / 镜像需要的查询（Repo 的子集）。
type PlacementRepo interface {
	List(ctx context.Context, q Query) ([]Row, int64, error)
	Get(ctx context.Context, id int64) (Row, error)
	GetSettings(ctx context.Context) (Settings, error)
}

// Placement 解析「产物该写哪个存储源、该复制到哪些存储源」（docs/12 第 5 节第 1、3 条）。
type Placement struct {
	repo PlacementRepo
}

func NewPlacement(repo PlacementRepo) *Placement { return &Placement{repo: repo} }

// Primary 返回某类型内容的主存储：优先用存储设置里显式指定的主存储，
// 未指定时回退到「用途匹配 + role=primary + 状态 active」中编号最小的一个。
// draining（不再写入）与 disabled 的存储源都不会被选中。
func (p *Placement) Primary(ctx context.Context, kind LineKind) (Row, error) {
	if st, err := p.repo.GetSettings(ctx); err == nil {
		id := st.PrimaryVideoProviderID
		if kind == LineImage {
			id = st.PrimaryImageProviderID
		}
		if id > 0 {
			row, err := p.repo.Get(ctx, id)
			if err != nil {
				return Row{}, fmt.Errorf("读取设置里指定的主存储 %d: %w", id, err)
			}
			if err := checkWritable(row, kind); err != nil {
				return Row{}, err
			}
			return row, nil
		}
	}
	rows, err := p.activeRows(ctx)
	if err != nil {
		return Row{}, err
	}
	for _, row := range rows {
		if row.Role == RolePrimary && purposeAllowsKind(row.Purpose, kind) && !isHTTPOrigin(row.Type, row.Subtype) {
			return row, nil
		}
	}
	return Row{}, fmt.Errorf("没有可用于 %s 的主存储：请在「存储设置」里指定，或把一个存储源设为 primary 并启用", kind)
}

// Mirrors 返回某类型内容要复制到的镜像存储源（role=mirror、状态 active、用途匹配），不含主存储。
// draining 的镜像不再写入，因此不参与复制。
func (p *Placement) Mirrors(ctx context.Context, kind LineKind, primaryID int64) ([]Row, error) {
	rows, err := p.activeRows(ctx)
	if err != nil {
		return nil, err
	}
	var out []Row
	for _, row := range rows {
		if row.ID == primaryID || row.Role != RoleMirror {
			continue
		}
		if !purposeAllowsKind(row.Purpose, kind) || isHTTPOrigin(row.Type, row.Subtype) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// activeRows 列出全部状态为 active 的存储源（按编号升序，结果稳定）。
func (p *Placement) activeRows(ctx context.Context) ([]Row, error) {
	rows, _, err := p.repo.List(ctx, Query{Status: StatusActive, Limit: maxPageSize, Offset: 0})
	if err != nil {
		return nil, err
	}
	// List 按编号倒序，这里反转成升序，让「回退选第一个」稳定地取到编号最小的
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, nil
}

// checkWritable 校验一个存储源能否作为该类型的写入目标。
func checkWritable(row Row, kind LineKind) error {
	switch row.Status {
	case StatusDraining:
		return fmt.Errorf("存储源「%s」已设为停写（draining），不能作为主存储写入", row.Name)
	case StatusDisabled:
		return fmt.Errorf("存储源「%s」已停用，不能作为主存储写入", row.Name)
	}
	if !purposeAllowsKind(row.Purpose, kind) {
		return fmt.Errorf("存储源「%s」的用途不支持写入 %s 内容", row.Name, kind)
	}
	if isHTTPOrigin(row.Type, row.Subtype) {
		return fmt.Errorf("存储源「%s」是只读 HTTP 源，不能写入", row.Name)
	}
	if row.Role == RoleReadonly {
		return fmt.Errorf("存储源「%s」是只读角色，不能作为主存储写入", row.Name)
	}
	return nil
}

// ---------- 复制 ----------

// CopyRequest 是把一个对象从源存储源复制到目标存储源。
type CopyRequest struct {
	Kind      ObjectKind
	Key       string // 对象的相对路径（两端各自拼自己的 path_prefix）
	From      Conn
	FromCred  Credential
	To        Conn
	ToCred    Credential
	SameStore bool // 两端是同一存储服务（endpoint 与凭据相同），可用服务端复制 API
}

// CopyResult 是一次复制的结果。
type CopyResult struct {
	Objects int   // 复制的对象数（hls_dir 是目录下的文件数）
	Bytes   int64 // 复制的字节数
}

// Copier 把对象从一个存储源复制到另一个（实现在 store/blob，domain 不依赖 AWS SDK）。
type Copier interface {
	CopyObject(ctx context.Context, req CopyRequest) (CopyResult, error)
}
