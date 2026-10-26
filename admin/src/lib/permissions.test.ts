import { describe, expect, it } from 'vitest'
import { can, editOf, viewOf } from './permissions'

describe('can', () => {
  const editor = { isSuper: false, permissions: new Set(['dramas.view', 'dramas.edit']) }

  it('只有被授予的权限点', () => {
    expect(can(editor, 'dramas.edit')).toBe(true)
    expect(can(editor, 'system.edit')).toBe(false)
  })

  it('超级管理员拥有全部权限点', () => {
    expect(can({ isSuper: true, permissions: new Set() }, 'anything.edit')).toBe(true)
  })

  it('未登录时没有任何权限', () => {
    expect(can(null, 'dramas.view')).toBe(false)
  })

  it('页面权限点编码', () => {
    expect(viewOf('dramas')).toBe('dramas.view')
    expect(editOf('admin_menus')).toBe('admin_menus.edit')
  })
})
