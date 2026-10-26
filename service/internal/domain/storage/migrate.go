package storage

import (
	"context"
	"fmt"
	"log/slog"
)

// 手动触发复制与存储源迁移（docs/12 第 4.4、5 节第 5 条，S1-6f）。
// 两者都是「扫描出对象集合 → 逐个入队 → worker 实际搬运」：接口快速返回入队数量，不阻塞在长任务上。

// 单批扫描的对象数：逐批入队，避免一次把几十万对象读进内存。
const objectScanBatch = 500

// 一次手动操作最多入队的对象数：防止误操作把队列打满（超出时返回已入队数量，可再次触发继续）。
const maxEnqueuePerCall = 50000

// ReplicateEnqueuer 把一个对象的复制任务入队（River，worker 执行）。
type ReplicateEnqueuer interface {
	EnqueueReplicate(ctx context.Context, objectID int64) error
}

// MigrateEnqueuer 把一个对象的迁移任务入队。
type MigrateEnqueuer interface {
	EnqueueMigrate(ctx context.Context, objectID, fromProviderID, toProviderID int64) error
}

// ReplicateScope 是手动触发复制的范围。
type ReplicateScope string

const (
	ScopeAll      ReplicateScope = "all"      // 全部媒体对象
	ScopeDrama    ReplicateScope = "drama"    // 某部剧的全部对象
	ScopeProvider ReplicateScope = "provider" // 某存储源上有副本的全部对象
)

// ReplicateRequest 是手动触发复制的请求。
type ReplicateRequest struct {
	Scope      ReplicateScope
	DramaID    int64
	ProviderID int64
}

// EnqueueResult 是一次批量入队的结果。
type EnqueueResult struct {
	Objects int  `json:"objects"` // 已入队的对象数
	Capped  bool `json:"capped"`  // 达到单次上限被截断，可再次触发继续
	Scanned int  `json:"scanned"` // 扫描到的对象数
}

// ManageService 是存储源的批量运维操作（手动补副本、迁移）。
type ManageService struct {
	repo      Repo
	placement *Placement
	replicate ReplicateEnqueuer
	migrate   MigrateEnqueuer
}

func NewManageService(repo Repo, placement *Placement, replicate ReplicateEnqueuer, migrate MigrateEnqueuer) *ManageService {
	return &ManageService{repo: repo, placement: placement, replicate: replicate, migrate: migrate}
}

// Replicate 手动触发复制：按范围扫描媒体对象并逐个入队复制任务。
// 任务本身是幂等的（已就绪的镜像会被跳过），所以重复触发安全。
func (s *ManageService) Replicate(ctx context.Context, req ReplicateRequest) (EnqueueResult, error) {
	q := ObjectQuery{Limit: objectScanBatch}
	switch req.Scope {
	case ScopeAll:
	case ScopeDrama:
		if req.DramaID <= 0 {
			return EnqueueResult{}, badRequest("按剧触发时请指定剧集编号")
		}
		q.DramaID = req.DramaID
	case ScopeProvider:
		if req.ProviderID <= 0 {
			return EnqueueResult{}, badRequest("按存储源触发时请指定存储源编号")
		}
		if _, err := s.repo.Get(ctx, req.ProviderID); err != nil {
			return EnqueueResult{}, err
		}
		q.ProviderID = req.ProviderID
	default:
		return EnqueueResult{}, badRequest("触发范围只能是 all、drama 或 provider")
	}
	if s.replicate == nil {
		return EnqueueResult{}, badRequest("当前进程不能入队复制任务")
	}
	return s.scanEnqueue(ctx, q, func(ctx context.Context, objectID int64) error {
		return s.replicate.EnqueueReplicate(ctx, objectID)
	})
}

// MigrateRequest 是把一个存储源上的副本迁移到另一个存储源。
type MigrateRequest struct {
	FromProviderID int64
	ToProviderID   int64
}

// Migrate 迁移存储源：把 from 上有副本的对象逐个入队迁移任务（复制到 to，成功后删除 from 的副本记录）。
//
// 迁移**不删除源存储源上的物理文件**：副本记录清空后存储源就可以删除，文件由运维确认后清桶，
// 这样迁移是可逆的（重新入队复制即可恢复记录），不会因为一次误操作丢数据。
// 建议先把源存储源设为 draining（不再写入新内容），再迁移。
func (s *ManageService) Migrate(ctx context.Context, req MigrateRequest) (EnqueueResult, error) {
	if req.FromProviderID <= 0 || req.ToProviderID <= 0 {
		return EnqueueResult{}, badRequest("请指定迁出与迁入的存储源")
	}
	if req.FromProviderID == req.ToProviderID {
		return EnqueueResult{}, badRequest("迁出与迁入不能是同一个存储源")
	}
	from, err := s.repo.Get(ctx, req.FromProviderID)
	if err != nil {
		return EnqueueResult{}, err
	}
	to, err := s.repo.Get(ctx, req.ToProviderID)
	if err != nil {
		return EnqueueResult{}, err
	}
	// 迁入目标必须可写：draining / disabled / 只读 / HTTP 源都不行
	if err := checkWritable(to, LineVideo); err != nil {
		return EnqueueResult{}, badRequest("迁入目标不可用：%s", err.Error())
	}
	if s.migrate == nil {
		return EnqueueResult{}, badRequest("当前进程不能入队迁移任务")
	}
	if from.Status == StatusActive {
		slog.WarnContext(ctx, "从仍在启用的存储源迁移：建议先设为停写（draining）",
			"from", from.ID, "to", to.ID)
	}
	return s.scanEnqueue(ctx, ObjectQuery{ProviderID: from.ID, Limit: objectScanBatch},
		func(ctx context.Context, objectID int64) error {
			return s.migrate.EnqueueMigrate(ctx, objectID, from.ID, to.ID)
		})
}

// scanEnqueue 游标分页扫描对象并逐个入队。
func (s *ManageService) scanEnqueue(ctx context.Context, q ObjectQuery, enqueue func(context.Context, int64) error) (EnqueueResult, error) {
	var res EnqueueResult
	for {
		ids, err := s.repo.ListObjectIDs(ctx, q)
		if err != nil {
			return res, err
		}
		if len(ids) == 0 {
			return res, nil
		}
		res.Scanned += len(ids)
		for _, id := range ids {
			if res.Objects >= maxEnqueuePerCall {
				res.Capped = true
				return res, nil
			}
			if err := enqueue(ctx, id); err != nil {
				return res, fmt.Errorf("入队对象 %d: %w", id, err)
			}
			res.Objects++
		}
		q.CursorID = ids[len(ids)-1]
		if len(ids) < q.Limit {
			return res, nil
		}
	}
}

// ---------- 迁移的执行（worker 侧）----------

// MigrateOne 把一个对象从 from 复制到 to，成功后删除 from 的副本记录。
// from 上没有 ready 副本时直接返回（可能已经迁完或从未复制过），不算失败。
func (s *ReplicateService) MigrateOne(ctx context.Context, objectID, fromProviderID, toProviderID int64) error {
	obj, err := s.repo.GetObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("读取媒体对象 %d: %w", objectID, err)
	}
	replicas, err := s.repo.ListReplicas(ctx, objectID)
	if err != nil {
		return err
	}
	var hasSource, alreadyThere bool
	for _, rep := range replicas {
		switch rep.ProviderID {
		case fromProviderID:
			hasSource = rep.Status == ReplicaReady
		case toProviderID:
			alreadyThere = rep.Status == ReplicaReady
		}
	}
	if !hasSource {
		slog.InfoContext(ctx, "迁移跳过：源存储源上没有可用副本", "object_id", objectID, "from", fromProviderID)
		return nil
	}

	if !alreadyThere {
		from, err := s.repo.Get(ctx, fromProviderID)
		if err != nil {
			return err
		}
		to, err := s.repo.Get(ctx, toProviderID)
		if err != nil {
			return err
		}
		fromCred, err := s.creds.WriteCredential(from)
		if err != nil {
			return err
		}
		if err := s.copyTo(ctx, obj, from, fromCred, to); err != nil {
			if _, saveErr := s.repo.SaveReplica(ctx, objectID, toProviderID, ReplicaFailed, publicCopyError(err)); saveErr != nil {
				slog.ErrorContext(ctx, "记录迁移失败状态出错", "object_id", objectID, "err", saveErr)
			}
			return fmt.Errorf("迁移对象 %d: %w", objectID, err)
		}
		if _, err := s.repo.SaveReplica(ctx, objectID, toProviderID, ReplicaReady, ""); err != nil {
			return fmt.Errorf("记录迁入副本: %w", err)
		}
	}
	// 迁入已就绪后再撤掉迁出记录：中间任何一步失败都不会让对象变成「没有任何可用副本」
	if err := s.repo.DeleteReplica(ctx, objectID, fromProviderID); err != nil {
		return fmt.Errorf("删除迁出副本记录: %w", err)
	}
	slog.InfoContext(ctx, "迁移完成", "object_id", objectID, "from", fromProviderID, "to", toProviderID)
	return nil
}
