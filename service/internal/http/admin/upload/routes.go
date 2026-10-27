// Package upload 提供运营后台媒体上传接口。文件内容经同源、带 CSRF 的流式 PUT 进入对象存储，
// 图片和视频分别落到 img/ 与 video/ 前缀，视频完成后由 media worker 异步转码。
package upload

import (
	"github.com/gin-gonic/gin"

	"xsite/service/internal/http/admin"
)

// Routes 注册在已挂后台会话、CSRF、审计中间件的分组上；具体用途权限在 Init 中动态检查。
func (h *Handlers) Routes(authed gin.IRouter) {
	g := authed.Group("/uploads")
	g.POST("", h.Init)
	g.PUT("/:id/content", h.PutContent)
	g.POST("/:id/complete", h.Complete)
	g.GET("/:id", h.Get)
	// 图片辅助接口单独挂在 /images 下：放到 /uploads 里会和 GET /uploads/:id 的参数段撞成路由冲突。
	// preview 只要已登录的后台会话（理由见 handlers_preview.go）；optimize 按用途要求对应模块的修改权限。
	authed.GET("/images/preview", h.PreviewImage)
	authed.POST("/images/optimize", h.OptimizeImage)
	admin.DeclarePermissions("dramas.edit", "announcements.edit", "ads.edit", "fb_projects.edit", "pages.edit")
}
