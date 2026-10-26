package admin

import "github.com/gin-gonic/gin"

// Routes 注册 /api/admin 的登录、会话与权限管理路由，返回已挂好「会话 → CSRF → 审计」中间件的分组。
// 业务模块（S1-3 起）注册在返回的分组上，并用 RequirePage / RequirePermission 声明所需权限点，例如：
//
//	dramas := authed.Group("/dramas", admin.RequirePage("dramas"))
func (h *Handlers) Routes(r gin.IRouter, origins []string, rec AuditRecorder) *gin.RouterGroup {
	g := r.Group("/api/admin", OriginCheck(origins))
	g.POST("/login", h.Login)
	// 二次验证登录第二步（S2-6）：公开（尚无会话），凭登录第一步发的一次性票据 + 验证码 / Passkey 断言换会话。
	g.POST("/login/mfa", h.LoginMFA)
	g.POST("/login/passkey/begin", h.BeginLoginPasskey)

	authed := g.Group("", h.RequireSession(), RequireSuperMFA(), CSRFToken(), Audit(rec))
	authed.GET("/me", h.Me)
	authed.GET("/me/menus", h.MyMenus)
	authed.POST("/logout", h.Logout)

	// 二次验证自助绑定（S2-6，docs/06 第 33 节）：任何登录管理员管理自己的二次验证，无需额外权限点。
	authed.POST("/mfa/totp/setup", h.BeginTOTPSetup)
	authed.POST("/mfa/totp/verify", h.VerifyTOTPSetup)
	// Passkey 自助注册与管理（S2-6）。
	authed.POST("/mfa/passkey/register/begin", h.BeginPasskeyRegister)
	authed.POST("/mfa/passkey/register/finish", h.FinishPasskeyRegister)
	authed.GET("/mfa/passkeys", h.ListPasskeys)
	authed.DELETE("/mfa/passkeys/:id", h.DeletePasskey)
	// 高敏操作重新验证（S2-6，docs/03 第 2.3 节）：通过后 5 分钟内可执行高敏写操作。
	authed.POST("/reauth", h.Reauth)
	authed.POST("/reauth/passkey/begin", h.BeginReauthPasskey)

	// 管理员（docs/06 第 33 节）
	admins := authed.Group("/admins", RequirePage("admins"))
	admins.GET("", h.ListAdmins)
	admins.POST("", h.CreateAdmin)
	admins.PATCH("/:id", h.UpdateAdmin)
	admins.POST("/:id/password", h.ResetAdminPassword)
	admins.DELETE("/:id/sessions", h.RevokeAdminSessions)
	admins.POST("/:id/reset-mfa", h.ResetAdminMFA)

	// 角色与角色权限（docs/06 第 33a、33b 节）。角色列表也供管理员页选择角色。
	authed.GET("/roles", RequirePermission("roles.view", "admins.view", "role_permissions.view"), h.ListRoles)
	authed.POST("/roles", RequirePermission("roles.edit"), h.CreateRole)
	authed.PATCH("/roles/:id", RequirePermission("roles.edit"), h.UpdateRole)
	authed.DELETE("/roles/:id", RequirePermission("roles.edit"), h.DeleteRole)
	authed.PUT("/roles/:id/permissions", RequirePermission("role_permissions.edit"), h.SetRolePermissions)
	authed.GET("/permissions", RequirePermission("role_permissions.view", "roles.view"), h.PermissionTree)

	// 后台菜单（docs/06 第 33c 节）
	menus := authed.Group("/admin-menus", RequirePage("admin_menus"))
	menus.GET("", h.ListMenus)
	menus.POST("", h.CreateMenu)
	menus.PATCH("/:id", h.UpdateMenu)
	menus.DELETE("/:id", h.DeleteMenu)

	return authed
}
