package content

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/contentimport"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// ContentImportService 是 CSV 批量导入（domain/contentimport.Service）。
type ContentImportService interface {
	Import(ctx context.Context, in dom.Input, adminID int64) (dom.Result, error)
	Logs(ctx context.Context, f dom.LogFilter) ([]dom.Log, int64, int, int, error)
}

// PreviewImport 是 POST /api/admin/import/preview：解析 CSV 并逐行给出会发生什么，**不写库**。
//
// 需要 import.view（只读）：预览不改任何数据，也不记导入记录（docs/06 第 8 节「仅记正式导入」）。
func (h *Handlers) PreviewImport(c *gin.Context) {
	h.runImport(c, true)
}

// RunImport 是 POST /api/admin/import/run：正式导入。
func (h *Handlers) RunImport(c *gin.Context) {
	h.runImport(c, false)
}

// runImport 解析上传的 CSV 并执行导入。预览与正式导入走同一条路径，只差 dryRun。
func (h *Handlers) runImport(c *gin.Context, dryRun bool) {
	file, err := c.FormFile("file")
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请选择要导入的 CSV 文件"))
		return
	}
	if file.Size > dom.MaxBytes {
		respond.Error(c, errs.ErrBadRequest.WithMessage("文件不能超过 1 MB，请把它拆开分批导入"))
		return
	}
	f, err := file.Open()
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("读不出上传的文件"))
		return
	}
	defer f.Close()
	// 多读一个字节：正好等于上限时也能发现超限（上面按 Size 判过一次，这里挡住谎报大小的请求）
	data, err := io.ReadAll(io.LimitReader(f, dom.MaxBytes+1))
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("读不出上传的文件"))
		return
	}
	if len(data) > dom.MaxBytes {
		respond.Error(c, errs.ErrBadRequest.WithMessage("文件不能超过 1 MB，请把它拆开分批导入"))
		return
	}

	actor, _ := admin.CurrentAdmin(c)
	out, err := h.ContentImports.Import(c.Request.Context(), dom.Input{
		CSV:      data,
		FileName: file.Filename,
		Source:   c.PostForm("source"),
		DryRun:   dryRun,
	}, actor.AdminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if !dryRun {
		admin.SetAudit(c, admin.AuditDetail{
			Action: "import.run", TargetType: "import",
			TargetID: strconv.FormatInt(out.LogID, 10),
			After: gin.H{
				"file_name": file.Filename, "source": c.PostForm("source"),
				"rows_total": out.Stats.RowsTotal, "dramas_created": out.Stats.DramasCreated,
				"dramas_updated": out.Stats.DramasUpdated, "skipped": out.Stats.Skipped,
				"rejected": out.Stats.Rejected, "tags_created": out.Stats.TagsCreated,
			},
		})
	}
	respond.OK(c, out)
}

type importLogQuery struct {
	// From / To 是浏览器本地时区的日期或带时区的时间戳，由前端换算后传来。
	From     string `form:"from"`
	To       string `form:"to"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// ListImportLogs 是 GET /api/admin/import/logs：正式导入的记录（docs/06 第 8 节「可搜日期」）。
func (h *Handlers) ListImportLogs(c *gin.Context) {
	var q importLogQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	from, err := parseDayBound(q.From)
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("开始日期格式不对"))
		return
	}
	to, err := parseDayBound(q.To)
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("结束日期格式不对"))
		return
	}
	list, total, page, size, err := h.ContentImports.Logs(c.Request.Context(), dom.LogFilter{
		From: from, To: to, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": total, "page": page, "page_size": size})
}

// parseDayBound 解析日期筛选。
//
// 优先按带时区的 RFC3339 解析（前端按浏览器时区换算后传来，这样「今天」是用户的今天，
// 不是服务端 UTC 的今天 —— 和审计日志页的处理一致）；只给了裸日期时按 UTC 解释。
func parseDayBound(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
