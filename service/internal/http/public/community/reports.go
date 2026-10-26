package community

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// ReportService 是举报服务（domain/moderation.ReportService）。
type ReportService interface {
	Submit(ctx context.Context, in dom.SubmitReportInput) error
}

type submitReportRequest struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Reason     string `json:"reason"`
	Detail     string `json:"detail"`
}

// SubmitReport 是 POST /api/v1/reports：举报评论 / 弹幕 / 剧 / 评论图片（注册用户或游客）。
// 同一举报人 24 小时内重复举报同一对象为幂等（只计一次），一律返回 204。
func (h *Handlers) SubmitReport(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	author, ok := currentAuthor(c)
	if !ok {
		respond.Error(c, errs.ErrNeedLogin)
		return
	}
	var req submitReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	err := h.Reports.Submit(c.Request.Context(), dom.SubmitReportInput{
		Reporter: author, TargetType: req.TargetType, TargetID: req.TargetID,
		Reason: req.Reason, Detail: req.Detail,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
