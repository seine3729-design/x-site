import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/** 管理员、角色、角色权限、后台菜单的接口（docs/07 第 2 节「管理员 / 角色 / 权限 / 后台菜单」）。 */

export type AdminStatus = 'active' | 'inactive' | 'unused'

export type RoleRef = { id: number; code: string; name: string; is_super: boolean }

export type AdminRow = {
  id: number
  username: string
  status: AdminStatus
  mfa_enabled: boolean
  role: RoleRef
  last_login_at: string | null
  created_at: string
}

export type RoleRow = {
  id: number
  code: string
  name: string
  description: string
  is_super: boolean
  builtin: boolean
  status: 'active' | 'inactive'
  sort: number
  member_count: number
  permissions: string[]
}

export type PermissionPoint = { code: string; name: string; sensitive: boolean }

/**
 * 权限树的节点（GET /api/admin/permissions）：目录（kind=group）只有 children（其下的页面）；
 * 页面（kind=page，一级或二级）只有 permissions。
 */
export type PermissionNode = {
  code: string
  kind: 'group' | 'page'
  title: string
  permissions: PermissionPoint[]
  children: PermissionNode[]
}

/** 菜单类型：目录（只能是一级菜单）、页面（系统提供）、外链。 */
export type MenuKind = 'group' | 'page' | 'link'

export type MenuRow = {
  id: number
  parent_id: number | null
  kind: MenuKind
  code: string
  title: string
  path: string
  icon: string
  sort: number
  visible: boolean
  builtin: boolean
}

type List<T> = { list: T[]; total: number }

export const accessKeys = {
  all: ['access'] as const,
  admins: ['access', 'admins'] as const,
  roles: ['access', 'roles'] as const,
  permissions: ['access', 'permissions'] as const,
  menus: ['access', 'menus'] as const,
}

// 字典类数据，变化少（docs/06 第 0.5 节）
const DICT_STALE = 10 * 60 * 1000

export const adminsQuery = queryOptions({
  queryKey: accessKeys.admins,
  queryFn: () => http.get<List<AdminRow>>('/admins').then((r) => r.list),
})

export const rolesQuery = queryOptions({
  queryKey: accessKeys.roles,
  queryFn: () => http.get<List<RoleRow>>('/roles').then((r) => r.list),
})

export const permissionsQuery = queryOptions({
  queryKey: accessKeys.permissions,
  queryFn: () => http.get<PermissionNode[]>('/permissions'),
  staleTime: DICT_STALE,
})

export const menusQuery = queryOptions({
  queryKey: accessKeys.menus,
  queryFn: () => http.get<List<MenuRow>>('/admin-menus').then((r) => r.list),
})

export type AdminCreateInput = { username: string; password: string; role_id: number }
export type AdminUpdateInput = { role_id: number; status: 'active' | 'inactive' }
export type RoleInput = { code?: string; name: string; description: string; status: 'active' | 'inactive'; sort: number }
export type MenuInput = {
  kind?: 'group' | 'link'
  parent_id?: number | null
  title: string
  path: string
  icon: string
  sort: number
  visible: boolean
}

export const accessApi = {
  createAdmin: (body: AdminCreateInput) => http.post<{ id: number }>('/admins', body),
  updateAdmin: (id: number, body: AdminUpdateInput) => http.patch<void>(`/admins/${id}`, body),
  resetPassword: (id: number, password: string) => http.post<void>(`/admins/${id}/password`, { password }),
  revokeSessions: (id: number) => http.delete<void>(`/admins/${id}/sessions`),
  resetMfa: (id: number) => http.post<void>(`/admins/${id}/reset-mfa`),

  createRole: (body: RoleInput) => http.post<{ id: number }>('/roles', body),
  updateRole: (id: number, body: RoleInput) => http.patch<void>(`/roles/${id}`, body),
  deleteRole: (id: number) => http.delete<void>(`/roles/${id}`),
  setRolePermissions: (id: number, codes: string[]) =>
    http.put<{ permissions: string[] }>(`/roles/${id}/permissions`, { codes }),

  createMenu: (body: MenuInput) => http.post<{ id: number }>('/admin-menus', body),
  updateMenu: (id: number, body: MenuInput) => http.patch<void>(`/admin-menus/${id}`, body),
  deleteMenu: (id: number) => http.delete<void>(`/admin-menus/${id}`),
}

export const ADMIN_STATUS_LABELS: Record<AdminStatus, string> = {
  active: '可登录',
  inactive: '停用',
  unused: '未登录过',
}

export const MENU_KIND_LABELS: Record<MenuKind, string> = {
  group: '目录',
  page: '页面',
  link: '外链',
}
