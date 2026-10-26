import { describe, expect, it } from 'vitest'
import {
  cleanColors,
  coverAspect,
  isHexColorOrEmpty,
  isHttpURLOrEmpty,
  isImageKeyOrEmpty,
  optionLabel,
  previewColors,
  previewScheme,
} from './branding-utils'

describe('品牌与主题的校验', () => {
  it('图片地址收相对路径与完整网址', () => {
    expect(isImageKeyOrEmpty('')).toBe(true)
    expect(isImageKeyOrEmpty('img/logo.webp')).toBe(true)
    expect(isImageKeyOrEmpty('https://cdn.example.com/logo.png')).toBe(true)
    // 路径穿越与非 http 协议都要拦下（与后端同一条规则）
    expect(isImageKeyOrEmpty('../../etc/passwd')).toBe(false)
    expect(isImageKeyOrEmpty('javascript:alert(1)')).toBe(false)
    expect(isImageKeyOrEmpty('/leading/slash')).toBe(false)
    expect(isImageKeyOrEmpty('有中文.png')).toBe(false)
  })

  it('客服链接只收 http(s) 完整地址', () => {
    expect(isHttpURLOrEmpty('')).toBe(true)
    expect(isHttpURLOrEmpty('https://t.me/support')).toBe(true)
    expect(isHttpURLOrEmpty('t.me/support')).toBe(false)
    expect(isHttpURLOrEmpty('javascript:alert(1)')).toBe(false)
    expect(isHttpURLOrEmpty('https://a b.com')).toBe(false)
  })

  it('色值只收 #RGB 与 #RRGGBB，留空表示用预设色', () => {
    expect(isHexColorOrEmpty('')).toBe(true)
    expect(isHexColorOrEmpty('#abc')).toBe(true)
    expect(isHexColorOrEmpty('#FF0033')).toBe(true)
    expect(isHexColorOrEmpty('red')).toBe(false)
    expect(isHexColorOrEmpty('#12345')).toBe(false)
  })
})

describe('提交前的整理', () => {
  it('颜色去空值并转小写', () => {
    expect(cleanColors({ primary: ' #FF0033 ', accent: '', bg: '#ABC' })).toEqual({ primary: '#ff0033', bg: '#abc' })
  })
})

describe('预览取色', () => {
  const theme = { preset: 'midnight', color_scheme: 'dark', colors: {} }

  it('用预设色板，覆盖色优先', () => {
    const base = previewColors(theme, 'dark')
    expect(base.primary).toBe('#3b82f6')
    const overridden = previewColors({ ...theme, colors: { primary: '#ff0033' } }, 'dark')
    expect(overridden.primary).toBe('#ff0033')
    // 留空的键不覆盖预设色
    expect(previewColors({ ...theme, colors: { primary: '' } }, 'dark').primary).toBe('#3b82f6')
  })

  it('浅色预览换掉底色与文字色', () => {
    const light = previewColors(theme, 'light')
    expect(light.bg).toBe('#f6f6f8')
    expect(light.text).toBe('#18181b')
    // 主色仍来自预设
    expect(light.primary).toBe('#3b82f6')
  })

  it('未知预设退回经典红，跟随系统按深色画', () => {
    expect(previewColors({ ...theme, preset: 'unknown' }, 'dark').primary).toBe('#e11d48')
    expect(previewScheme('system')).toBe('dark')
    expect(previewScheme('light')).toBe('light')
    expect(previewScheme('dark')).toBe('dark')
  })

  it('封面比例转 CSS', () => {
    expect(coverAspect('2:3')).toBe('2 / 3')
    expect(coverAspect('16:9')).toBe('16 / 9')
    expect(coverAspect('坏值')).toBe('2 / 3')
  })
})

describe('下拉中文名', () => {
  const options = [
    { value: 'classic', label: '经典红' },
    { value: 'midnight', label: '午夜蓝' },
  ]

  it('找不到时退回原值（后端新增取值而前端还没更新）', () => {
    expect(optionLabel(options, 'midnight')).toBe('午夜蓝')
    expect(optionLabel(options, 'neon')).toBe('neon')
    expect(optionLabel(undefined, 'neon')).toBe('neon')
  })
})
