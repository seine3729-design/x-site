package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// 清理孤立产物（docs/12 第 5 节第 6 条，S1-6f）：分集删除后产物不再被引用、重新转封装后旧目录不再被引用，
// 这些对象在保留期（回收站 30 天）后连同各存储源上的物理文件一起删除。
//
// 两段式，保证误删不可能发生：
//  1. 标记：扫描不被任何分集引用的对象，首次发现时写 orphaned_at。保留期从此开始算。
//  2. 清理：删除 orphaned_at 已过保留期的对象 —— 删文件前再查一次引用，仍被引用就跳过。
//
// 对象重新被引用时会撤销标记（正常不会发生，是兜底）。

// DefaultOrphanRetention 是孤立产物的保留期（docs/12：进回收站的内容保留 30 天后删除）。
const DefaultOrphanRetention = 30 * 24 * time.Hour

// OrphanObject 是一个待清理的孤立对象。
type OrphanObject struct {
	ID         int64
	Kind       ObjectKind
	Key        string
	SizeBytes  int64
	OrphanedAt time.Time
}

// OrphanStats 是孤立对象的统计（看板与清理前的预览）。
type OrphanStats struct {
	Total   int64 `json:"total"`   // 已标记为孤立的对象数
	Expired int64 `json:"expired"` // 其中已过保留期、可以清理的
	Bytes   int64 `json:"bytes"`   // 已标记对象的总字节数
}

// CleanupRepo 是清理需要的查询（Repo 的一部分）。
type CleanupRepo interface {
	// MarkOrphans 标记不再被引用的对象，返回新标记的数量。
	MarkOrphans(ctx context.Context) (int64, error)
	// UnmarkOrphans 撤销重新被引用的对象的标记，返回撤销的数量。
	UnmarkOrphans(ctx context.Context) (int64, error)
	// ListExpiredOrphans 列出已过保留期的孤立对象（游标分页）。
	ListExpiredOrphans(ctx context.Context, before time.Time, cursorID int64, limit int) ([]OrphanObject, error)
	// OrphanStats 统计孤立对象。
	OrphanStats(ctx context.Context, before time.Time) (OrphanStats, error)
	// DeleteObject 删除媒体对象（副本记录级联删除）；仍被分集引用时返回 false 不删。
	DeleteObject(ctx context.Context, objectID int64) (bool, error)
}

// ObjectRemover 删除存储源上的物理文件（实现在 store/blob）。
type ObjectRemover interface {
	// RemoveObject 删除一个对象在某存储源上的全部文件，返回删除的文件数。
	RemoveObject(ctx context.Context, conn Conn, cred Credential, kind ObjectKind, key string) (int, error)
}

// CleanupResult 是一次清理的结果。
type CleanupResult struct {
	Scanned int   `json:"scanned"` // 扫描到的过期孤立对象数
	Deleted int   `json:"deleted"` // 已删除的对象数
	Files   int   `json:"files"`   // 删除的物理文件数
	Bytes   int64 `json:"bytes"`   // 释放的字节数（按对象登记的大小）
	Kept    int   `json:"kept"`    // 跳过的对象数（重新被引用，或文件删除失败）
	DryRun  bool  `json:"dry_run"` // 只预览、不实际删除
}

// CleanupService 标记并清理孤立产物。
type CleanupService struct {
	repo      Repo
	creds     WriteCredentialOpener
	remover   ObjectRemover
	retention time.Duration
}

func NewCleanupService(repo Repo, creds WriteCredentialOpener, remover ObjectRemover, retention time.Duration) *CleanupService {
	if retention <= 0 {
		retention = DefaultOrphanRetention
	}
	return &CleanupService{repo: repo, creds: creds, remover: remover, retention: retention}
}

// Retention 返回当前的保留期。
func (s *CleanupService) Retention() time.Duration { return s.retention }

// Stats 返回孤立对象的统计（不改数据）。
func (s *CleanupService) Stats(ctx context.Context, now time.Time) (OrphanStats, error) {
	return s.repo.OrphanStats(ctx, now.Add(-s.retention))
}

// Mark 刷新孤立标记：标记新出现的孤立对象、撤销重新被引用的。
func (s *CleanupService) Mark(ctx context.Context) (marked, unmarked int64, err error) {
	if unmarked, err = s.repo.UnmarkOrphans(ctx); err != nil {
		return 0, 0, err
	}
	if marked, err = s.repo.MarkOrphans(ctx); err != nil {
		return 0, unmarked, err
	}
	if marked > 0 || unmarked > 0 {
		slog.InfoContext(ctx, "孤立产物标记已刷新", "marked", marked, "unmarked", unmarked)
	}
	return marked, unmarked, nil
}

// Cleanup 删除已过保留期的孤立对象：先删各存储源上的文件，全部成功后再删对象记录。
// dryRun 为 true 时只统计不删除，用于让运营先确认会删掉什么。
//
// 删文件前会再查一次引用：对象在标记后重新被分集引用（重新转封装切回旧产物等）时跳过，不删。
// 某个存储源上的文件删不掉时保留对象记录，下一轮再试 —— 宁可留着垃圾文件，也不能让数据库和存储不一致。
func (s *CleanupService) Cleanup(ctx context.Context, dryRun bool, now time.Time) (CleanupResult, error) {
	res := CleanupResult{DryRun: dryRun}
	before := now.Add(-s.retention)
	var cursor int64
	for {
		objs, err := s.repo.ListExpiredOrphans(ctx, before, cursor, objectScanBatch)
		if err != nil {
			return res, err
		}
		if len(objs) == 0 {
			return res, nil
		}
		res.Scanned += len(objs)
		for _, obj := range objs {
			if dryRun {
				res.Deleted++
				res.Bytes += obj.SizeBytes
				continue
			}
			files, err := s.purge(ctx, obj)
			if err != nil {
				res.Kept++
				slog.WarnContext(ctx, "清理孤立产物失败，保留记录下轮再试",
					"object_id", obj.ID, "key", obj.Key, "err", err)
				continue
			}
			if files < 0 { // 重新被引用，跳过
				res.Kept++
				continue
			}
			res.Deleted++
			res.Files += files
			res.Bytes += obj.SizeBytes
		}
		cursor = objs[len(objs)-1].ID
		if len(objs) < objectScanBatch {
			return res, nil
		}
	}
}

// purge 删除一个对象在全部存储源上的文件，然后删对象记录。
// 返回 -1 表示对象重新被引用、已跳过；返回 error 表示没能删干净（对象记录保留）。
func (s *CleanupService) purge(ctx context.Context, obj OrphanObject) (int, error) {
	replicas, err := s.repo.ListReplicas(ctx, obj.ID)
	if err != nil {
		return 0, err
	}
	var files int
	for _, rep := range replicas {
		row, err := s.repo.Get(ctx, rep.ProviderID)
		if err != nil {
			// 存储源已被删除：副本记录会随对象一起级联删掉，不阻塞清理
			slog.WarnContext(ctx, "清理时存储源已不存在，跳过该副本",
				"object_id", obj.ID, "provider_id", rep.ProviderID)
			continue
		}
		if isHTTPOrigin(row.Type, row.Subtype) {
			continue // 只读 HTTP 源上没有我们写入的文件
		}
		cred, err := s.creds.WriteCredential(row)
		if err != nil {
			return 0, fmt.Errorf("存储源「%s」没有可用凭据: %w", row.Name, err)
		}
		n, err := s.remover.RemoveObject(ctx, row.Conn(), cred, obj.Kind, obj.Key)
		if err != nil {
			return 0, fmt.Errorf("删除存储源「%s」上的文件: %w", row.Name, err)
		}
		files += n
	}
	// 文件已删干净，再删记录；DeleteObject 内部再查一次引用，返回 false 表示对象又被引用了
	deleted, err := s.repo.DeleteObject(ctx, obj.ID)
	if err != nil {
		return 0, err
	}
	if !deleted {
		slog.WarnContext(ctx, "清理时对象重新被引用，已跳过（文件可能已被删除，需重新转封装）",
			"object_id", obj.ID, "key", obj.Key)
		return -1, nil
	}
	slog.InfoContext(ctx, "已清理孤立产物", "object_id", obj.ID, "key", obj.Key, "files", files)
	return files, nil
}
