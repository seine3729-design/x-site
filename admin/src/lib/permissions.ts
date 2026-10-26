/**
 * 后台权限的「显示控制」。权限在运营后台的「角色」「权限矩阵」中配置（docs/adr/0006），
 * 真正的判断在后端：每个接口按权限点校验，前端只决定显示哪些菜单与按钮。
 *
 * 权限点编码为 `<页面>.<动作>`，如 `dramas.view`、`dramas.edit`、`seo.analytics_code`。
 * 超级管理员拥有全部权限点（/api/admin/me 返回 is_super=true、permissions 为空）。
 */

export type PermissionSubject = {
  isSuper: boolean
  permissions: ReadonlySet<string>
}

/** 是否拥有某个权限点。未登录时一律没有。 */
export function can(subject: PermissionSubject | null, code: string): boolean {
  if (!subject) return false
  return subject.isSuper || subject.permissions.has(code)
}

/** 页面的「查看」权限点。 */
export const viewOf = (page: string) => `${page}.view`

/** 页面的「修改」权限点。 */
export const editOf = (page: string) => `${page}.edit`
