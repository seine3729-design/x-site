package community

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/user"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type userListQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type userStatusRequest struct {
	Status string `json:"status"`
}

// ListUsers 是 GET /api/admin/users：注册用户列表（§26），搜用户名 / 昵称，按账号状态筛，按注册时间倒序。
func (h *Handlers) ListUsers(c *gin.Context) {
	var q userListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Users.ListUsers(c.Request.Context(), dom.AdminFilter{
		Q: q.Q, Status: q.Status,
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, page)
}

// ListGuests 是 GET /api/admin/guests：游客设备列表（§26 的「类型＝游客」），搜设备编号或已并入的账号用户名。
// 走 users 页面的权限点：它和注册用户同属用户管理页，只是形状差太多，塞进同一个列表接口会让一半字段恒空。
// 游客只读 —— guest_device 没有账号状态列，要限制游客只能用「游客互动」总开关。
func (h *Handlers) ListGuests(c *gin.Context) {
	var q userListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Users.ListGuests(c.Request.Context(), dom.AdminFilter{
		Q: q.Q, Status: q.Status,
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, page)
}

// GetUser 是 GET /api/admin/users/:id：用户详情（§26），含资料审核状态与社区足迹。
// 会员状态 / 等级成长值 / 金币余额 / 邀请关系 / 风控标记要等 S3-10 与 M6，暂不下发。
func (h *Handlers) GetUser(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	v, err := h.Users.GetUser(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, v)
}

// SetUserStatus 是 POST /api/admin/users/:id/status：改账号状态（§26）。
// normal / muted / banned 三选一；封禁会同时下线该账号的全部登录设备（docs/11 S2-7）。
// 不提供重设用户密码（adr/0001）。
func (h *Handlers) SetUserStatus(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req userStatusRequest
	if !admin.Bind(c, &req) {
		return
	}
	before, err := h.Users.GetUser(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	v, err := h.Users.SetStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "user.status", TargetType: "user", TargetID: itoa(id),
		Before: gin.H{"status": before.Status},
		After:  gin.H{"status": v.Status, "username": v.Username},
	})
	respond.OK(c, v)
}
