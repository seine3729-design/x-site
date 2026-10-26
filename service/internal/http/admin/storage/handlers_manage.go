package storage

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// 存储源的批量运维（docs/12 第 4.4、5 节，S1-6f）：手动补副本与迁移。
// 两者都只扫描对象并入队，实际搬运由 worker 执行，接口立即返回入队数量。

type replicateReq struct {
	Scope      string `json:"scope"`
	DramaID    int64  `json:"drama_id"`
	ProviderID int64  `json:"provider_id"`
}

type migrateReq struct {
	FromProviderID int64 `json:"from_provider_id"`
	ToProviderID   int64 `json:"to_provider_id"`
}

// Replicate 是 POST /api/admin/storage/replicate：手动触发复制（按全部 / 按剧 / 按存储源）。
// 复制任务本身幂等（已就绪的镜像会跳过），重复触发安全。
func (h *Handlers) Replicate(c *gin.Context) {
	var req replicateReq
	if !admin.Bind(c, &req) {
		return
	}
	res, err := h.ManageSvc.Replicate(c.Request.Context(), dom.ReplicateRequest{
		Scope: dom.ReplicateScope(req.Scope), DramaID: req.DramaID, ProviderID: req.ProviderID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "storage.replicate", TargetType: "storage_provider", TargetID: itoa(req.ProviderID),
		After: map[string]any{"scope": req.Scope, "drama_id": req.DramaID, "provider_id": req.ProviderID, "objects": res.Objects},
	})
	respond.OK(c, res)
}

// Migrate 是 POST /api/admin/storage/migrate：把一个存储源上的副本迁移到另一个存储源。
// 迁移不删除源存储源上的物理文件，只在迁入就绪后撤掉迁出的副本记录，之后该存储源才能删除。
func (h *Handlers) Migrate(c *gin.Context) {
	var req migrateReq
	if !admin.Bind(c, &req) {
		return
	}
	res, err := h.ManageSvc.Migrate(c.Request.Context(), dom.MigrateRequest{
		FromProviderID: req.FromProviderID, ToProviderID: req.ToProviderID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "storage.migrate", TargetType: "storage_provider", TargetID: itoa(req.FromProviderID),
		After: map[string]any{"from_provider_id": req.FromProviderID, "to_provider_id": req.ToProviderID, "objects": res.Objects},
	})
	respond.OK(c, res)
}

type adoptReq struct {
	ProviderID int64 `json:"provider_id"`
}

// Adopt 是 POST /api/admin/storage/adopt：把还没有副本记录的历史产物登记到指定存储源。
// 任务里会先校验对象在该存储源上真实存在，校验不通过的跳过，不会凭空写出副本记录。
func (h *Handlers) Adopt(c *gin.Context) {
	var req adoptReq
	if !admin.Bind(c, &req) {
		return
	}
	res, err := h.AdoptSvc.Adopt(c.Request.Context(), dom.AdoptRequest{ProviderID: req.ProviderID})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "storage.adopt", TargetType: "storage_provider", TargetID: itoa(req.ProviderID),
		After: map[string]any{"provider_id": req.ProviderID, "objects": res.Objects, "scanned": res.Scanned},
	})
	respond.OK(c, res)
}

type cleanupReq struct {
	// DryRun 为 true 时只统计会删掉什么，不实际删除。清理不可逆，建议先用它确认。
	DryRun bool `json:"dry_run"`
}

// Cleanup 是 POST /api/admin/storage/cleanup：清理已过保留期的孤立产物（分集删除、重新转封装后不再被引用的）。
// 平时由 worker 每天自动跑，这个接口用于手动提前触发或预览。
func (h *Handlers) Cleanup(c *gin.Context) {
	var req cleanupReq
	if !admin.Bind(c, &req) {
		return
	}
	res, err := h.CleanupSvc.Cleanup(c.Request.Context(), req.DryRun, time.Now())
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 预览不改数据，不记审计
	if !req.DryRun {
		admin.SetAudit(c, admin.AuditDetail{
			Action: "storage.cleanup", TargetType: "media_object",
			After: map[string]any{"deleted": res.Deleted, "files": res.Files, "bytes": res.Bytes, "kept": res.Kept},
		})
	}
	respond.OK(c, res)
}

// OrphanStats 是 GET /api/admin/storage/orphans：孤立产物的统计（已标记数、已过保留期数、占用字节）。
func (h *Handlers) OrphanStats(c *gin.Context) {
	stats, err := h.CleanupSvc.Stats(c.Request.Context(), time.Now())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{
		"total": stats.Total, "expired": stats.Expired, "bytes": stats.Bytes,
		"retention_hours": int(h.CleanupSvc.Retention().Hours()),
	})
}
