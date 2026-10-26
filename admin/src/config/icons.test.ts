import { Circle, Film, Folder, Link } from 'lucide-react'
import { describe, expect, it } from 'vitest'
import { menuIcon } from './icons'

describe('menuIcon', () => {
  it('设置了已登记的图标时用该图标', () => {
    expect(menuIcon('group', 'Film')).toBe(Film)
    expect(menuIcon('page', 'Film')).toBe(Film)
  })

  it('没有设置图标时按类型给默认图标（二级菜单也不留空）', () => {
    expect(menuIcon('group', '')).toBe(Folder)
    expect(menuIcon('page', '')).toBe(Circle)
    expect(menuIcon('link', undefined)).toBe(Link)
  })

  it('图标名未登记时按类型给默认图标', () => {
    expect(menuIcon('link', 'NotRegistered')).toBe(Link)
    expect(menuIcon('group', 'NotRegistered')).toBe(Folder)
  })
})
