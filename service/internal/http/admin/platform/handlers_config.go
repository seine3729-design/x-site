package platform

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// ConfigService 是配置导出导入服务（domain/platform.ConfigService）。
type ConfigService interface {
	Export(ctx context.Context, adminID int64, store bool) (dom.Backup, error)
	Import(ctx context.Context, adminID int64, b dom.Backup) (dom.ImportResult, error)
	Backups(ctx context.Context) ([]dom.BackupSummary, error)
	Backup(ctx context.Context, id int64) (dom.Backup, error)
	Restore(ctx context.Context, adminID int64, id int64) (dom.ImportResult, error)
}

// ExportConfig 是 GET /api/admin/config/export：下载备份。
//
// 这是读请求（只要 config.view），但会在库里留一条存档：导出意味着一份配置副本离开了系统，
// 留痕才能回答「这份文件是谁什么时候导出的」。
func (h *Handlers) ExportConfig(c *gin.Context) {
	actor, _ := admin.CurrentAdmin(c)
	out, err := h.Config.Export(c.Request.Context(), actor.AdminID, true)
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 带文件名下发，浏览器直接存成文件；文件名带时间便于区分多次导出
	name := "xsite-config-" + out.ExportedAt.Format("20060102-150405") + ".json"
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "private, no-store")
	respond.OK(c, out)
}

// ListConfigBackups 是 GET /api/admin/config/backups：备份存档列表（含导入前自动存的那些）。
func (h *Handlers) ListConfigBackups(c *gin.Context) {
	list, err := h.Config.Backups(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": len(list)})
}

// GetConfigBackup 是 GET /api/admin/config/backups/:id：取回一份存档（用于回滚：下载后再导入）。
func (h *Handlers) GetConfigBackup(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	out, err := h.Config.Backup(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	name := "xsite-config-backup-" + strconv.FormatInt(id, 10) + ".json"
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "private, no-store")
	respond.OK(c, out)
}

// importRequest 是导入请求：直接把备份文件的内容作为请求体。
type importRequest struct {
	Format     int            `json:"format"`
	ExportedAt time.Time      `json:"exported_at"`
	Site       string         `json:"site"`
	Signature  string         `json:"signature"`
	Data       dom.BackupData `json:"data"`
}

// ImportConfig 是 POST /api/admin/config/import：校验并导入，覆盖现有配置。
//
// 高敏操作（config.edit，迁移 00003 标了 sensitive）：S2-6 接入二次验证后这里还要求 5 分钟内重新验证过。
func (h *Handlers) ImportConfig(c *gin.Context) {
	var req importRequest
	// 备份文件可能比默认的请求体上限大，这里单独放宽
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBackupBytes)
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("备份文件不是合法的 JSON，或超过 "+strconv.Itoa(maxBackupBytes/1024/1024)+" MB"))
		return
	}
	actor, _ := admin.CurrentAdmin(c)
	res, err := h.Config.Import(c.Request.Context(), actor.AdminID, dom.Backup{
		Format: req.Format, ExportedAt: req.ExportedAt, Site: req.Site, Signature: req.Signature, Data: req.Data,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 审计只记数量与存档编号，不记备份正文（会把审计表撑爆）
	admin.SetAudit(c, admin.AuditDetail{
		Action: "config.import", TargetType: "config", TargetID: strconv.FormatInt(res.BackupID, 10),
		Before: gin.H{"backup_id": res.BackupID},
		After: gin.H{
			"format": res.Format, "signed": res.Signed, "applied": res.Applied,
			"taxonomies_kept": res.TaxonomiesKept, "terms_kept": res.TermsKept, "ads_replaced": res.AdsReplaced,
		},
	})
	respond.OK(c, res)
}

// maxBackupBytes 是导入文件的大小上限。
const maxBackupBytes = 8 << 20 // 8 MiB

// RestoreConfigBackup 是 POST /api/admin/config/backups/:id/restore：从库里的存档回滚。
//
// 不验签：存档是系统自己生成、一直存在本库里的（见 ConfigService.Restore 的说明）。
// 下载下来再上传的路径仍然走 ImportConfig 的验签。
func (h *Handlers) RestoreConfigBackup(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	actor, _ := admin.CurrentAdmin(c)
	res, err := h.Config.Restore(c.Request.Context(), actor.AdminID, id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "config.restore", TargetType: "config", TargetID: strconv.FormatInt(id, 10),
		Before: gin.H{"backup_id": res.BackupID},
		After: gin.H{
			"restored_from": id, "applied": res.Applied,
			"taxonomies_kept": res.TaxonomiesKept, "terms_kept": res.TermsKept, "ads_replaced": res.AdsReplaced,
		},
	})
	respond.OK(c, res)
}
