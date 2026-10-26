import { describe, expect, it } from 'vitest'
import type { DramaDetail } from './api'
import {
  CODE_PATTERN,
  dramaToForm,
  episodeSummary,
  formatDuration,
  formToDramaInput,
  fromLocalInput,
  lookupReason,
  moveItem,
  parseIdList,
  sameMembers,
  sameOrder,
  SLUG_PATTERN,
  splitList,
  toggleTerm,
  toLocalInput,
} from './utils'

/** 只有「标签」查看权限的管理员。 */
const onlyTagsView = (code: string) => code === 'tags.view'

const drama: DramaDetail = {
  id: 7,
  title: '霸道总裁',
  aliases: ['总裁', '心动'],
  slug: 'ceo',
  summary: '简介',
  cover_portrait: '/covers/7.jpg',
  cover_landscape: '',
  badge: 'HOT',
  score: 9.5,
  year: null,
  status: 'published',
  display_status: 'published',
  publish_at: '2026-10-21T08:30:00Z',
  is_finished: false,
  sort_weight: 5,
  view_base: 100,
  view_count: 3,
  hot_score: 103,
  latest_episode: 12,
  seo_title: '',
  seo_desc: '',
  source: '',
  created_at: '2026-10-20T00:00:00Z',
  updated_at: '2026-10-21T00:00:00Z',
  tags: [],
  terms: [],
}

describe('splitList', () => {
  it('按中英文逗号、顿号与换行拆分，去掉空项与重复项', () => {
    expect(splitList(' 总裁, 心动，总裁、 \n甜宠 ,,')).toEqual(['总裁', '心动', '甜宠'])
    expect(splitList('')).toEqual([])
  })
})

describe('parseIdList', () => {
  it('支持 # 前缀与多种分隔符，保留顺序并去重', () => {
    expect(parseIdList('#12, 13 14，12、#15')).toEqual({ ids: [12, 13, 14, 15], invalid: [] })
  })
  it('无法识别的片段单独列出', () => {
    expect(parseIdList('12 abc #0 -3 7')).toEqual({ ids: [12, 7], invalid: ['abc', '#0', '-3'] })
  })
})

describe('moveItem', () => {
  it('把元素移到目标位置', () => {
    expect(moveItem(['a', 'b', 'c', 'd'], 0, 2)).toEqual(['b', 'c', 'a', 'd'])
    expect(moveItem(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b'])
  })
  it('越界或原地移动时返回副本', () => {
    const list = ['a', 'b']
    expect(moveItem(list, 0, 5)).toEqual(list)
    expect(moveItem(list, 1, 1)).not.toBe(list)
  })
})

describe('本地时间转换', () => {
  it('ISO ↔ datetime-local 往返不丢分钟', () => {
    const local = toLocalInput('2026-10-21T08:30:00Z')
    expect(local).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    expect(fromLocalInput(local)).toBe('2026-10-21T08:30:00.000Z')
  })
  it('空值与无效值', () => {
    expect(toLocalInput(null)).toBe('')
    expect(toLocalInput('not-a-date')).toBe('')
    expect(fromLocalInput('')).toBeNull()
  })
})

describe('显示', () => {
  it('更新至 / 全 N 集', () => {
    expect(episodeSummary({ latest_episode: 0, is_finished: false })).toBe('暂无可播分集')
    expect(episodeSummary({ latest_episode: 12, is_finished: false })).toBe('更新至 12 集')
    expect(episodeSummary({ latest_episode: 80, is_finished: true })).toBe('全 80 集')
  })
  it('关联选择缺少其他页面的查看权限时给出原因', () => {
    expect(lookupReason(onlyTagsView, 'tags')).toBeNull()
    expect(lookupReason(onlyTagsView, 'taxonomies')).toBe('需要「筛选维度」的查看权限')
    expect(lookupReason(onlyTagsView, 'topics')).toBe('需要「专题」的查看权限')
  })
  it('时长', () => {
    expect(formatDuration(0)).toBe('未知')
    expect(formatDuration(95)).toBe('1:35')
    expect(formatDuration(3725)).toBe('1:02:05')
  })
})

describe('toggleTerm', () => {
  const region = { multi_select: false, terms: [{ id: 1 }, { id: 2 }] }
  const genre = { multi_select: true, terms: [{ id: 3 }, { id: 4 }] }
  it('单选维度替换原来的取值，再点一次取消', () => {
    expect(toggleTerm([1, 3], region, { id: 2 })).toEqual([3, 2])
    expect(toggleTerm([3, 2], region, { id: 2 })).toEqual([3])
  })
  it('多选维度直接增减', () => {
    expect(toggleTerm([1, 3], genre, { id: 4 })).toEqual([1, 3, 4])
    expect(toggleTerm([1, 3, 4], genre, { id: 3 })).toEqual([1, 4])
  })
})

describe('剧资料表单', () => {
  it('往返保留全部字段（整体保存不会清掉没改的字段）', () => {
    const input = formToDramaInput(dramaToForm(drama))
    expect(input).toEqual({
      title: '霸道总裁',
      aliases: ['总裁', '心动'],
      slug: 'ceo',
      summary: '简介',
      cover_portrait: '/covers/7.jpg',
      cover_landscape: '',
      badge: 'HOT',
      score: 9.5,
      year: null,
      status: 'published',
      publish_at: '2026-10-21T08:30:00.000Z',
      is_finished: false,
      sort_weight: 5,
      view_base: 100,
      seo_title: '',
      seo_desc: '',
    })
  })
  it('数字留空：评分、年份为 null，排序值与初始播放量为 0；清除定时为 null', () => {
    const form = { ...dramaToForm(drama), score: '', year: ' ', sort_weight: '', view_base: '', publish_at: '' }
    const input = formToDramaInput(form)
    expect([input.score, input.year, input.sort_weight, input.view_base, input.publish_at]).toEqual([null, null, 0, 0, null])
  })
})

describe('规则', () => {
  it('slug 与后端一致', () => {
    for (const ok of ['', 'ceo', 'ceo-love', 'a1', '2024']) expect(SLUG_PATTERN.test(ok)).toBe(true)
    for (const bad of ['-a', 'a-', 'A', 'a_b', '中文', 'a'.repeat(65)]) expect(SLUG_PATTERN.test(bad)).toBe(false)
  })
  it('取值编号', () => {
    expect(CODE_PATTERN.test('City_01-x')).toBe(true)
    expect(CODE_PATTERN.test('城市')).toBe(false)
  })
  it('ID 列表比较', () => {
    expect(sameOrder([1, 2], [1, 2])).toBe(true)
    expect(sameOrder([1, 2], [2, 1])).toBe(false)
    expect(sameMembers([1, 2], [2, 1])).toBe(true)
    expect(sameMembers([1, 2], [1, 3])).toBe(false)
  })
})
