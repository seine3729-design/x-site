import type { MenuRow } from './api'

/** 菜单按排序值、再按 id 排列（与后端 ListMenus 一致）。 */
export const bySort = (a: MenuRow, b: MenuRow) => a.sort - b.sort || a.id - b.id

/** 一级菜单及其二级菜单（只有目录有二级菜单）。 */
export type MenuTreeNode = MenuRow & { children: MenuRow[] }

/** 把菜单列表组成两级树：一级菜单按排序值排列，目录下的二级菜单同样按排序值排列。 */
export function buildMenuTree(rows: MenuRow[]): MenuTreeNode[] {
  return rows
    .filter((r) => r.parent_id === null)
    .toSorted(bySort)
    .map((top) => ({ ...top, children: rows.filter((r) => r.parent_id === top.id).toSorted(bySort) }))
}

/**
 * 计算把 list[index] 与相邻项交换后需要更新的排序值。排序值互不相同时只交换两项；
 * 有重复时把整组重新编号为 10、20、30…，保证结果确定。越界时返回空数组。
 */
export function reorder(list: MenuRow[], index: number, dir: -1 | 1): { menu: MenuRow; sort: number }[] {
  const j = index + dir
  if (index < 0 || index >= list.length || j < 0 || j >= list.length) return []
  const a = list[index]
  const b = list[j]
  if (a.sort !== b.sort) {
    return [
      { menu: a, sort: b.sort },
      { menu: b, sort: a.sort },
    ]
  }
  const order = [...list]
  ;[order[index], order[j]] = [order[j], order[index]]
  return order.map((m, i) => ({ menu: m, sort: (i + 1) * 10 })).filter((x) => x.menu.sort !== x.sort)
}
