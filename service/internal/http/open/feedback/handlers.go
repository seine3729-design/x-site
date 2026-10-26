package feedback

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// ipBurstPerMinute 是同一来源 IP 每分钟允许的提交次数。
// Key 维度的配额在领域层按 Key 计数；这一层只是为了让无效 Key 的扫库请求不至于无限打到数据库。
const ipBurstPerMinute = 120

// submitReq 是站外网页提交的一条反馈。
// 字段刻意保持最小：项目由 API Key 决定，提交者不能指定，避免越权写入别的项目。
type submitReq struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Env     string `json:"env"`
	PageURL string `json:"page_url"`
	Contact string `json:"contact"`
}

// Submit 是 POST /open-api/feedback。
func (h *Handlers) Submit(c *gin.Context) {
	if !h.allowIP(c) {
		return
	}
	var req submitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求内容格式错误"))
		return
	}
	res, err := h.Svc.Submit(c.Request.Context(), c.GetHeader(HeaderAPIKey), dom.IntakeInput{
		Kind: dom.Kind(req.Kind), Title: req.Title, Body: req.Body, Env: req.Env,
		PageURL: req.PageURL, Contact: req.Contact,
		Origin: c.GetHeader("Origin"),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// allowIP 做按 IP 的固定窗口限流；限流器不可用时放行，不因缓存故障拒收反馈。
func (h *Handlers) allowIP(c *gin.Context) bool {
	if h.Limiter == nil {
		return true
	}
	minute := time.Now().Unix() / 60
	key := c.ClientIP() + ":" + strconv.FormatInt(minute, 10)
	n, err := h.Limiter.Incr(c.Request.Context(), key, time.Minute)
	if err != nil {
		return true
	}
	if n > ipBurstPerMinute {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited.WithMessage("提交太频繁，请稍后再试"), time.Minute))
		return false
	}
	return true
}
