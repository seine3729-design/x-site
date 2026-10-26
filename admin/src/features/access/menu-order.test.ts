import { describe, expect, it } from 'vitest'
import type { MenuRow } from './api'
import { buildMenuTree, bySort, reorder } from './menu-order'

const m = (id: number, sort: number, over: Partial<MenuRow> = {}): MenuRow => ({
  id, sort, parent_id: 1, kind: 'page', code: `p${id}`, title: `页面${id}`, path: `/p${id}`,
  icon: '', visible: true, builtin: true, ...over,
})

describe('reorder', () => {
  it('排序值不同时只交换相邻两项', () => {
    const list = [m(1, 10), m(2, 20), m(3, 30)]
    expect(reorder(list, 1, -1).map((x) => [x.menu.id, x.sort])).toEqual([
      [2, 10],
      [1, 20],
    ])
  })

  it('排序值重复时整组重新编号', () => {
    const list = [m(1, 100), m(2, 100), m(3, 100)]
    expect(reorder(list, 2, -1).map((x) => [x.menu.id, x.sort])).toEqual([
      [1, 10],
      [3, 20],
      [2, 30],
    ])
  })

  it('首项不能上移、末项不能下移', () => {
    const list = [m(1, 10), m(2, 20)]
    expect(reorder(list, 0, -1)).toEqual([])
    expect(reorder(list, 1, 1)).toEqual([])
  })

  it('按排序值再按 id 排列', () => {
    expect([m(3, 10), m(1, 20), m(2, 10)].toSorted(bySort).map((x) => x.id)).toEqual([2, 3, 1])
  })
})

describe('buildMenuTree', () => {
  it('一级菜单按排序值排列，目录下挂二级菜单', () => {
    const rows = [
      m(10, 30, { kind: 'group', parent_id: null, path: '' }),
      m(11, 20, { parent_id: 10 }),
      m(12, 10, { parent_id: 10, kind: 'link', path: 'https://a.example.com' }),
      m(1, 10, { parent_id: null }),
      m(20, 20, { kind: 'group', parent_id: null, path: '' }),
    ]
    const tree = buildMenuTree(rows)
    expect(tree.map((n) => [n.id, n.children.map((c) => c.id)])).toEqual([
      [1, []],
      [20, []],
      [10, [12, 11]],
    ])
  })
})
