import { describe, expect, it } from 'vitest'
import { cleanTemplates, cleanVerifications, insertVariable, isVerifyName, previewTemplate, unknownVariables } from './seo-utils'

const variables = [
  { value: 'site_name', label: '站名' },
  { value: 'drama_title', label: '剧名' },
  { value: 'total_episodes', label: '总集数' },
]

describe('模板变量', () => {
  it('挑出未登记的变量名', () => {
    expect(unknownVariables('{site_name} - {drama_title}', variables)).toEqual([])
    // 写错大小写或驼峰都要报出来：否则前台会把 {dramaTitle} 原样显示给用户
    expect(unknownVariables('{dramaTitle}', variables)).toEqual(['dramaTitle'])
    expect(unknownVariables('{site_name}{unknown}{unknown}', variables)).toEqual(['unknown'])
    // 变量名两侧的空白按去掉后比较（与后端一致）
    expect(unknownVariables('{ site_name }', variables)).toEqual([])
    expect(unknownVariables('没有变量', variables)).toEqual([])
  })

  it('在光标处插入变量', () => {
    expect(insertVariable('A - B', 'site_name', 4, 4)).toEqual({ value: 'A - {site_name}B', cursor: 15 })
    // 没有选区时追加到末尾
    expect(insertVariable('A', 'site_name')).toEqual({ value: 'A{site_name}', cursor: 12 })
    // 有选区时替换选中的内容
    expect(insertVariable('AXB', 'site_name', 1, 2)).toEqual({ value: 'A{site_name}B', cursor: 12 })
  })

  it('预览把变量换成示例值，未知变量原样留着', () => {
    expect(previewTemplate('{drama_title}（全{total_episodes}集）- {site_name}')).toBe('夏夜未眠（全24集）- 短剧站')
    expect(previewTemplate('{unknown}')).toBe('{unknown}')
    expect(previewTemplate('')).toBe('')
  })
})

describe('站长验证', () => {
  it('名称只收字母、数字、点、下划线与连字符', () => {
    expect(isVerifyName('google-site-verification')).toBe(true)
    expect(isVerifyName('msvalidate.01')).toBe(true)
    expect(isVerifyName('')).toBe(false)
    expect(isVerifyName('坏 名称')).toBe(false)
    expect(isVerifyName('-开头')).toBe(false)
  })

  it('提交前丢掉整行留空的，并去首尾空白', () => {
    expect(
      cleanVerifications([
        { name: ' google ', content: ' abc ' },
        { name: '', content: '' },
        { name: 'only-name', content: '' },
      ])
    ).toEqual([
      { name: 'google', content: 'abc' },
      { name: 'only-name', content: '' },
    ])
  })
})

describe('提交前的整理', () => {
  it('模板去掉留空的页面（表示用前台默认）', () => {
    expect(cleanTemplates({ home: ' {site_name} ', play: '', drama: '{drama_title}' })).toEqual({
      home: '{site_name}',
      drama: '{drama_title}',
    })
  })
})
