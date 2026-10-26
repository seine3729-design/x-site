import { describe, expect, it } from 'vitest'
import type { PermissionNode } from './api'
import { checkState, codesOf, countGranted, filterTree, groupCodes, toggleNode, viewLockReason } from './permission-tree'

const page = (code: string, title: string, actions: string[]): PermissionNode => ({
  code,
  kind: 'page',
  title,
  permissions: actions.map((a) => ({ code: `${code}.${a}`, name: a === 'view' ? '查看' : a === 'edit' ? '修改' : a, sensitive: false })),
  children: [],
})

const dashboard = page('dashboard', '运营数据看板', ['view', 'edit'])
const dramas = page('dramas', '剧集', ['view', 'edit'])
const ads = page('ads', '广告位', ['view', 'edit', 'custom_html'])
const content: PermissionNode = { code: 'content', kind: 'group', title: '内容', permissions: [], children: [dramas, ads] }
const tree = [dashboard, content]
const none = () => false
// 当前管理员没有「自定义 HTML」权限点
const customHtmlLocked = (c: string) => c === 'ads.custom_html'

describe('codesOf / countGranted', () => {
  it('目录汇总其下页面的权限点', () => {
    expect(codesOf(content)).toEqual(['dramas.view', 'dramas.edit', 'ads.view', 'ads.edit', 'ads.custom_html'])
    expect(countGranted(content, new Set(['dramas.view', 'x.view']))).toBe(1)
  })
})

describe('checkState', () => {
  it('全部授予为 true，部分为 indeterminate，都没有为 false', () => {
    expect(checkState(dramas, new Set(['dramas.view', 'dramas.edit']))).toBe(true)
    expect(checkState(content, new Set(['dramas.view']))).toBe('indeterminate')
    expect(checkState(content, new Set())).toBe(false)
  })
})

describe('toggleNode', () => {
  it('勾选目录授予其下全部权限点', () => {
    expect([...toggleNode(new Set(), content, true, none)].toSorted()).toEqual(
      ['ads.custom_html', 'ads.edit', 'ads.view', 'dramas.edit', 'dramas.view']
    )
  })

  it('取消页面收回该页面全部权限点，不影响其他页面', () => {
    const granted = new Set(['dramas.view', 'dramas.edit', 'ads.view'])
    expect([...toggleNode(granted, dramas, false, none)]).toEqual(['ads.view'])
  })

  it('跳过锁定的权限点；仍有授予的权限点时保留查看', () => {
    expect([...toggleNode(new Set(), ads, true, customHtmlLocked)].toSorted()).toEqual(['ads.edit', 'ads.view'])
    const granted = new Set(['ads.view', 'ads.edit', 'ads.custom_html'])
    expect([...toggleNode(granted, ads, false, customHtmlLocked)].toSorted()).toEqual(['ads.custom_html', 'ads.view'])
  })
})

describe('viewLockReason', () => {
  it('页面还有不能收回的权限点时，不能取消查看', () => {
    expect(viewLockReason(ads, new Set(['ads.view', 'ads.custom_html']), customHtmlLocked)).toBe(
      '该页面还有你不能收回的权限，不能取消查看'
    )
    expect(viewLockReason(ads, new Set(['ads.view', 'ads.edit']), customHtmlLocked)).toBeNull()
    expect(viewLockReason(ads, new Set(), customHtmlLocked)).toBeNull()
  })
})

describe('filterTree', () => {
  it('空关键字返回原树', () => {
    expect(filterTree(tree, '  ')).toBe(tree)
  })

  it('按页面名称、权限点名称或编码过滤，保留所属目录', () => {
    expect(filterTree(tree, '广告').map((n) => [n.code, n.children.map((c) => c.code)])).toEqual([['content', ['ads']]])
    expect(filterTree(tree, 'CUSTOM_html').map((n) => n.code)).toEqual(['content'])
    expect(filterTree(tree, '看板').map((n) => n.code)).toEqual(['dashboard'])
  })

  it('目录名称命中时保留整个目录', () => {
    expect(filterTree(tree, '内容')[0].children).toHaveLength(2)
  })

  it('groupCodes 返回全部目录编码', () => {
    expect(groupCodes(tree)).toEqual(['content'])
  })
})
