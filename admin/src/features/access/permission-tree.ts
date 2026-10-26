import type { PermissionNode } from './api'
import { viewCodeOf } from './grants'

/** 勾选框状态：全部授予为 true，部分授予为 'indeterminate'，都没有为 false。 */
export type CheckState = boolean | 'indeterminate'

/** 节点下的全部页面（页面节点返回自己）。 */
export function pagesOf(node: PermissionNode): PermissionNode[] {
  return node.kind === 'page' ? [node] : node.children
}

/** 节点下的全部权限点编码。 */
export function codesOf(node: PermissionNode): string[] {
  return pagesOf(node).flatMap((p) => p.permissions.map((x) => x.code))
}

/** 节点下已授予的权限点数。 */
export function countGranted(node: PermissionNode, granted: ReadonlySet<string>): number {
  return codesOf(node).filter((c) => granted.has(c)).length
}

export function checkState(node: PermissionNode, granted: ReadonlySet<string>): CheckState {
  const n = countGranted(node, granted)
  if (n === 0) return false
  return n === codesOf(node).length ? true : 'indeterminate'
}

/**
 * 勾选或取消整个节点（目录或页面）下的权限点；locked(code) 为 true 的保持不变。
 * 收回后页面仍有授予的权限点时保留「查看」（与后端自动补「查看」一致）。
 */
export function toggleNode(
  granted: ReadonlySet<string>,
  node: PermissionNode,
  checked: boolean,
  locked: (code: string) => boolean
): Set<string> {
  const next = new Set(granted)
  for (const page of pagesOf(node)) {
    for (const { code } of page.permissions) {
      if (locked(code)) continue
      if (checked) next.add(code)
      else next.delete(code)
    }
    const view = viewCodeOf(page)
    if (view && page.permissions.some((p) => next.has(p.code))) next.add(view)
  }
  return next
}

/**
 * 取消「查看」会一并收回该页面的其余权限点；其中有当前管理员不能收回的权限点时，不能取消「查看」。
 * 返回原因，可以取消或当前未授予时返回 null。
 */
export function viewLockReason(
  page: PermissionNode,
  granted: ReadonlySet<string>,
  locked: (code: string) => boolean
): string | null {
  const view = viewCodeOf(page)
  if (!view || !granted.has(view)) return null
  const blocked = page.permissions.some((p) => p.code !== view && granted.has(p.code) && locked(p.code))
  return blocked ? '该页面还有你不能收回的权限，不能取消查看' : null
}

/** 全部目录的编码（用于「展开全部」）。 */
export function groupCodes(nodes: PermissionNode[]): string[] {
  return nodes.filter((n) => n.kind === 'group').map((n) => n.code)
}

/**
 * 按关键字过滤权限树：页面名称 / 编码或其任一权限点的名称 / 编码命中时保留该页面；
 * 目录名称命中时保留整个目录；目录只保留命中的页面，没有命中的目录去掉。关键字为空时原样返回。
 */
export function filterTree(nodes: PermissionNode[], query: string): PermissionNode[] {
  const q = query.trim().toLowerCase()
  if (!q) return nodes
  const hit = (...texts: string[]) => texts.some((t) => t.toLowerCase().includes(q))
  const pageHit = (p: PermissionNode) => hit(p.title, p.code, ...p.permissions.flatMap((x) => [x.name, x.code]))
  const out: PermissionNode[] = []
  for (const n of nodes) {
    if (n.kind === 'page') {
      if (pageHit(n)) out.push(n)
    } else if (hit(n.title, n.code)) {
      out.push(n)
    } else {
      const children = n.children.filter(pageHit)
      if (children.length > 0) out.push({ ...n, children })
    }
  }
  return out
}
