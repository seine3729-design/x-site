import type { PermissionNode, RoleRow } from './api'

/**
 * 授权的编辑规则，与后端 SetRolePermissions 一致：
 * - 勾选某页面的任一权限点时自动勾上该页面的「查看」，否则页面在菜单中不可见；
 * - 取消「查看」时同时取消该页面的其余权限点（后端会自动补回「查看」，前端提前对齐，避免看起来没取消）。
 */
export function toggleGrant(
  granted: ReadonlySet<string>,
  page: PermissionNode,
  code: string,
  checked: boolean
): Set<string> {
  const next = new Set(granted)
  const view = viewCodeOf(page)
  if (checked) {
    next.add(code)
    if (view) next.add(view)
  } else if (code === view) {
    for (const p of page.permissions) next.delete(p.code)
  } else {
    next.delete(code)
  }
  return next
}

/** 页面的「查看」权限点。 */
export const viewCodeOf = (page: PermissionNode) => page.permissions.find((p) => p.code.endsWith('.view'))?.code

/** 两个集合是否相同。 */
export function sameSet(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
  if (a.size !== b.size) return false
  for (const x of a) if (!b.has(x)) return false
  return true
}

/** 草稿与原值相差的权限点数。 */
export function countChanges(original: ReadonlySet<string>, draft: ReadonlySet<string>): number {
  let n = 0
  for (const x of draft) if (!original.has(x)) n++
  for (const x of original) if (!draft.has(x)) n++
  return n
}

export type GrantActor = { roleId: number | null; isSuper: boolean; can: (code: string) => boolean }

/**
 * 当前管理员能否授予或收回某个角色的某个权限点（与后端限制一致）：
 * 需要 role_permissions.edit；超级角色不能改；非超管不能改自己所属的角色，也不能授予或收回自己没有的权限点。
 * 返回不能修改的原因，能改时返回 null。
 */
export function grantLockReason(actor: GrantActor, role: RoleRow, code: string): string | null {
  if (role.is_super) return '超级管理员拥有全部权限'
  if (!actor.can('role_permissions.edit')) return '当前角色只能查看角色权限'
  if (actor.isSuper) return null
  if (actor.roleId === role.id) return '不能修改自己所属角色的权限'
  if (!actor.can(code)) return '只能授予或收回自己拥有的权限'
  return null
}
