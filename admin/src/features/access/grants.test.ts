import { describe, expect, it } from 'vitest'
import type { PermissionNode, RoleRow } from './api'
import { countChanges, grantLockReason, sameSet, toggleGrant } from './grants'

const ads: PermissionNode = {
  code: 'ads',
  kind: 'page',
  title: '广告位',
  permissions: [
    { code: 'ads.view', name: '查看', sensitive: false },
    { code: 'ads.edit', name: '修改', sensitive: false },
    { code: 'ads.custom_html', name: '自定义 HTML', sensitive: false },
  ],
  children: [],
}

const role = (over: Partial<RoleRow>): RoleRow => ({
  id: 3, code: 'editor', name: '编辑', description: '', is_super: false, builtin: false,
  status: 'active', sort: 0, member_count: 0, permissions: [], ...over,
})

describe('toggleGrant', () => {
  it('勾选修改时自动勾上查看', () => {
    expect([...toggleGrant(new Set(), ads, 'ads.edit', true)].toSorted()).toEqual(['ads.edit', 'ads.view'])
  })

  it('取消查看时一并取消该页面的其余权限点', () => {
    const granted = new Set(['ads.view', 'ads.edit', 'ads.custom_html', 'dramas.view'])
    expect([...toggleGrant(granted, ads, 'ads.view', false)]).toEqual(['dramas.view'])
  })

  it('取消其他权限点时保留查看', () => {
    const granted = new Set(['ads.view', 'ads.edit'])
    expect([...toggleGrant(granted, ads, 'ads.edit', false)]).toEqual(['ads.view'])
  })

  it('不修改传入的集合', () => {
    const granted = new Set(['ads.view'])
    toggleGrant(granted, ads, 'ads.edit', true)
    expect([...granted]).toEqual(['ads.view'])
  })
})

describe('countChanges / sameSet', () => {
  it('按权限点计数', () => {
    const a = new Set(['x', 'y'])
    const b = new Set(['y', 'z', 'w'])
    expect(countChanges(a, b)).toBe(3)
    expect(sameSet(a, new Set(['y', 'x']))).toBe(true)
    expect(sameSet(a, b)).toBe(false)
  })
})

/** 非超管的操作者：所属角色 roleId，拥有 codes 中的权限点。 */
const editorOf = (codes: string[], roleId = 2) => ({
  roleId,
  isSuper: false,
  can: (c: string) => codes.includes(c),
})

describe('grantLockReason', () => {
  it('超级角色永远不能改', () => {
    const superActor = { roleId: 1, isSuper: true, can: () => true }
    expect(grantLockReason(superActor, role({ id: 1, is_super: true }), 'ads.view')).toBe('超级管理员拥有全部权限')
  })

  it('没有 role_permissions.edit 时只读', () => {
    expect(grantLockReason(editorOf(['ads.view']), role({}), 'ads.view')).toBe('当前角色只能查看角色权限')
  })

  it('非超管不能改自己所属角色，也不能动自己没有的权限点', () => {
    const actor = editorOf(['role_permissions.edit', 'ads.view'])
    expect(grantLockReason(actor, role({ id: 2 }), 'ads.view')).toBe('不能修改自己所属角色的权限')
    expect(grantLockReason(actor, role({}), 'ads.edit')).toBe('只能授予或收回自己拥有的权限')
    expect(grantLockReason(actor, role({}), 'ads.view')).toBeNull()
  })

  it('超管可以改所有非超级角色', () => {
    expect(grantLockReason({ roleId: 1, isSuper: true, can: () => true }, role({}), 'ads.custom_html')).toBeNull()
  })
})
