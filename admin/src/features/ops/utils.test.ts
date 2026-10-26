import { describe, expect, it } from 'vitest'
import type { Ad, Announcement, RankOverride, RankRow } from './api'
import {
  adToForm,
  announcementToForm,
  applyHotPatch,
  creativeSummary,
  ctr,
  formatHeat,
  formToAdInput,
  formToAnnouncementInput,
  formToWeights,
  hotPatchMessage,
  isIntInRange,
  isLinkOrEmpty,
  isRankBoost,
  isRankWeight,
  localesSummary,
  newRankOverrideRow,
  ordered,
  pagesSummary,
  rankDiffers,
  rankMove,
  rankOverrideToRow,
  rowToRankOverrideInput,
  weightsToForm,
  windowSummary,
} from './utils'

const announcement: Announcement = {
  id: 3,
  kind: 'popup',
  content: '国庆会员 5 折',
  click_url: '/vip',
  frequency: 'daily',
  pages: ['home', 'play'],
  locales: ['en'],
  image: '/ads/p.webp',
  start_at: '2026-10-24T01:30:00Z',
  end_at: null,
  status: 'active',
  display_status: 'live',
  created_at: '2026-10-24T00:00:00Z',
  updated_at: '2026-10-24T00:00:00Z',
}

describe('isLinkOrEmpty（与后端校验一致）', () => {
  it('接受站内路径、http(s) 地址与空值', () => {
    for (const ok of ['', '  ', '/vip', '/d/12?from=a', 'https://example.com/a', 'http://cdn.example.com/1.webp']) {
      expect(isLinkOrEmpty(ok)).toBe(true)
    }
  })
  it('拒绝可执行协议、协议相对地址与带账号的地址', () => {
    for (const bad of [
      'javascript:alert(1)',
      '//evil.example.com',
      '/\\evil.example.com',
      'ftp://a.com/x',
      'example.com',
      'https://u:p@a.com/',
      '/a b',
    ]) {
      expect(isLinkOrEmpty(bad)).toBe(false)
    }
  })
})

describe('摘要', () => {
  it('不选或全选为「全部」，否则按固定顺序列出', () => {
    expect(pagesSummary([])).toBe('全部页面')
    expect(pagesSummary(['play', 'home'])).toBe('首页、播放页')
    expect(localesSummary([])).toBe('全部语言')
    expect(localesSummary(['ar', 'zh-CN'])).toBe('简体中文、阿拉伯语')
    expect(ordered(['c', 'a'], ['a', 'b', 'c'])).toEqual(['a', 'c'])
  })
  it('起止时间', () => {
    expect(windowSummary(null, null)).toBe('不限')
    expect(windowSummary(null, '2026-10-30T00:00:00Z')).toMatch(/^立即 – \d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
    expect(windowSummary('2026-10-30T00:00:00Z', null)).toMatch(/ – 不结束$/)
  })
})

describe('公告表单', () => {
  it('往返保留全部字段（整体保存不会清掉没改的字段）', () => {
    const input = formToAnnouncementInput(announcementToForm(announcement))
    expect(input).toEqual({
      kind: 'popup',
      content: '国庆会员 5 折',
      click_url: '/vip',
      frequency: 'daily',
      pages: ['home', 'play'],
      locales: ['en'],
      image: '/ads/p.webp',
      start_at: '2026-10-24T01:30:00.000Z',
      end_at: null,
      status: 'active',
    })
  })
  it('新建的默认值；改为顶部滚动条时不保存配图，多选按固定顺序', () => {
    const blank = announcementToForm()
    expect(blank).toMatchObject({
      kind: 'topbar',
      frequency: 'always',
      status: 'active',
      pages: [],
      locales: [],
      start_at: '',
      end_at: '',
    })
    const input = formToAnnouncementInput({
      ...announcementToForm(announcement),
      kind: 'topbar',
      pages: ['play', 'home'],
      content: '  滚动  ',
    })
    expect(input).toMatchObject({
      kind: 'topbar',
      image: '',
      pages: ['home', 'play'],
      content: '滚动',
    })
  })
})

const ad: Ad = {
  id: 7,
  name: '首页顶部会员',
  slot: 'home_top',
  creative_type: 'video',
  image: '/ads/poster.webp',
  video_url: 'https://cdn.example.com/a.mp4',
  text: '',
  html: '',
  alt: '会员 5 折',
  click_url: '/vip',
  device: 'mobile',
  locales: ['en'],
  weight: 30,
  end_at: '2026-11-01T00:00:00Z',
  freq_cap: 3,
  status: 'active',
  display_status: 'live',
  imp_count: 200,
  click_count: 3,
  created_at: '2026-10-24T00:00:00Z',
  updated_at: '2026-10-24T00:00:00Z',
}

describe('广告表单', () => {
  it('往返保留全部字段（整体保存不会清掉没改的字段）', () => {
    expect(formToAdInput(adToForm(ad))).toEqual({
      name: '首页顶部会员',
      slot: 'home_top',
      creative_type: 'video',
      image: '/ads/poster.webp',
      video_url: 'https://cdn.example.com/a.mp4',
      text: '',
      html: '',
      alt: '会员 5 折',
      click_url: '/vip',
      device: 'mobile',
      locales: ['en'],
      weight: 30,
      end_at: '2026-11-01T00:00:00.000Z',
      freq_cap: 3,
      status: 'active',
    })
  })
  it('新建的默认值：未选位置、图片素材、权重 10、不限次数', () => {
    expect(adToForm()).toMatchObject({
      slot: '',
      creative_type: 'image',
      device: 'all',
      weight: '10',
      freq_cap: '0',
      end_at: '',
      status: 'active',
      locales: [],
    })
  })
  it('只提交该素材类型用得到的字段', () => {
    const f = { ...adToForm(ad), text: '残留文字', html: '<b>残留</b>' }
    expect(formToAdInput({ ...f, creative_type: 'text', text: '  新剧上线  ' })).toMatchObject({
      text: '新剧上线',
      image: '',
      video_url: '',
      html: '',
      alt: '',
    })
    expect(formToAdInput({ ...f, creative_type: 'html' })).toMatchObject({
      html: '<b>残留</b>',
      image: '',
      video_url: '',
      text: '',
      alt: '',
    })
    expect(formToAdInput({ ...f, creative_type: 'image' })).toMatchObject({
      image: '/ads/poster.webp',
      alt: '会员 5 折',
      video_url: '',
      text: '',
      html: '',
    })
  })
  it('整数范围校验', () => {
    expect(isIntInRange('10', 1, 100)).toBe(true)
    expect(isIntInRange(' 100 ', 1, 100)).toBe(true)
    for (const bad of ['0', '101', '-1', '1.5', 'abc', '']) expect(isIntInRange(bad, 1, 100)).toBe(false)
    expect(isIntInRange('0', 0, 1000)).toBe(true)
  })
  it('点击率与素材摘要', () => {
    expect(ctr(0, 0)).toBe('—')
    expect(ctr(200, 3)).toBe('1.50%')
    expect(creativeSummary(ad)).toBe('https://cdn.example.com/a.mp4')
    expect(creativeSummary({ ...ad, creative_type: 'html', html: '<p>x</p>' })).toBe('HTML，8 个字符（已清洗）')
  })
})

describe('热搜词的显示与置顶（与后端规则一致）', () => {
  it('置顶时自动显示，不显示时同时取消置顶', () => {
    expect(applyHotPatch({ visible: false, pinned: false }, { pinned: true })).toEqual({ visible: true, pinned: true })
    expect(applyHotPatch({ visible: true, pinned: true }, { visible: false })).toEqual({ visible: false, pinned: false })
    expect(applyHotPatch({ visible: true, pinned: true }, { pinned: false })).toEqual({ visible: true, pinned: false })
    expect(applyHotPatch({ visible: false, pinned: false }, { visible: true })).toEqual({ visible: true, pinned: false })
  })
  it('提示里说明连带的修改', () => {
    expect(hotPatchMessage({ word: '重生', visible: false, pinned: false }, { pinned: true })).toBe('「重生」已置顶，已显示在热搜榜')
    expect(hotPatchMessage({ word: '重生', visible: true, pinned: true }, { visible: false })).toBe('「重生」已取消置顶，已从热搜榜隐藏')
    expect(hotPatchMessage({ word: '重生', visible: true, pinned: false }, { visible: true })).toBe('「重生」没有变化')
  })
})

describe('排行干预的人工调整', () => {
  const base: RankOverride = {
    drama_id: 12,
    title: '重生',
    drama_status: 'published',
    in_trash: false,
    pinned: false,
    blocked: false,
    boost: 0,
    updated_at: '2026-10-24T00:00:00Z',
  }
  it('调整 → 表单：按置顶 / 不让上榜 / 加减分区分方式，加分 0 显示为空', () => {
    expect(rankOverrideToRow({ ...base, pinned: true })).toMatchObject({ mode: 'pinned', boost: '' })
    expect(rankOverrideToRow({ ...base, pinned: true, boost: 300 })).toMatchObject({ mode: 'pinned', boost: '300' })
    expect(rankOverrideToRow({ ...base, blocked: true })).toMatchObject({ mode: 'blocked', boost: '' })
    expect(rankOverrideToRow({ ...base, boost: -200 })).toMatchObject({ mode: 'boost', boost: '-200', title: '重生', drama_status: 'published' })
  })
  it('新加的行默认置顶、不加分，剧名与状态等保存后补上', () => {
    expect(newRankOverrideRow(18)).toEqual({ drama_id: 18, title: '', drama_status: null, in_trash: false, mode: 'pinned', boost: '' })
    expect(newRankOverrideRow(18, '霸道总裁').title).toBe('霸道总裁')
  })
  it('表单 → 请求体：置顶与不让上榜互斥，不让上榜按 0 分保存', () => {
    const row = rankOverrideToRow(base)
    expect(rowToRankOverrideInput({ ...row, mode: 'pinned', boost: '' })).toEqual({ drama_id: 12, pinned: true, blocked: false, boost: 0 })
    expect(rowToRankOverrideInput({ ...row, mode: 'pinned', boost: ' 50 ' })).toEqual({ drama_id: 12, pinned: true, blocked: false, boost: 50 })
    expect(rowToRankOverrideInput({ ...row, mode: 'blocked', boost: '999' })).toEqual({ drama_id: 12, pinned: false, blocked: true, boost: 0 })
    expect(rowToRankOverrideInput({ ...row, mode: 'boost', boost: '-200' })).toEqual({ drama_id: 12, pinned: false, blocked: false, boost: -200 })
  })
  it('加分为绝对值不超过 10 亿的整数', () => {
    for (const ok of ['0', '500', '-200', ' 1000000000 ', '-1000000000']) expect(isRankBoost(ok)).toBe(true)
    for (const bad of ['', '1.5', '+5', '1e3', 'abc', '1000000001', '-1000000001', '99999999999']) expect(isRankBoost(bad)).toBe(false)
  })
})

describe('排行干预的计分系数', () => {
  it('0–1000，最多两位小数', () => {
    for (const ok of ['0', '1', '2.5', '3.25', '1000', ' 7 ']) expect(isRankWeight(ok)).toBe(true)
    for (const bad of ['', '-1', '1000.01', '1001', '1.234', '.5', 'abc']) expect(isRankWeight(bad)).toBe(false)
  })
  it('表单与系数互转', () => {
    const w = { play: 1, follow: 3, like: 2, finish: 2.5 }
    expect(weightsToForm(w)).toEqual({ play: '1', follow: '3', like: '2', finish: '2.5' })
    expect(formToWeights({ play: ' 1.25 ', follow: '3', like: '0', finish: '2' })).toEqual({ play: 1.25, follow: 3, like: 0, finish: 2 })
  })
})

const row = (rank: number, drama_id: number, heat: number): RankRow => ({ rank, drama_id, title: `#${drama_id}`, heat, pinned: false })

describe('排行干预的预览', () => {
  const live = [row(1, 10, 500), row(2, 11, 300), row(3, 12, 100)]
  it('名次或分数不同就提示重新排名', () => {
    expect(
      rankDiffers(
        live,
        live.map((r) => ({ ...r, pinned: r.drama_id === 10 }))
      )
    ).toBe(false)
    expect(rankDiffers(live, [row(1, 12, 100), row(2, 10, 500), row(3, 11, 300)])).toBe(true)
    expect(rankDiffers(live, [row(1, 10, 501), row(2, 11, 300), row(3, 12, 100)])).toBe(true)
    expect(rankDiffers(live, live.slice(0, 2))).toBe(true)
    expect(rankDiffers([], [])).toBe(false)
  })
  it('相对线上榜单的名次变化：上升为正、下降为负、新上榜为 null', () => {
    expect(rankMove(live, { drama_id: 12, rank: 1 })).toBe(2)
    expect(rankMove(live, { drama_id: 10, rank: 2 })).toBe(-1)
    expect(rankMove(live, { drama_id: 11, rank: 2 })).toBe(0)
    expect(rankMove(live, { drama_id: 99, rank: 3 })).toBeNull()
  })
  it('分数显示千分位、最多两位小数', () => {
    expect(formatHeat(1300)).toBe('1,300')
    expect(formatHeat(2.5)).toBe('2.5')
    expect(formatHeat(1234.567)).toBe('1,234.57')
    expect(formatHeat(-200)).toBe('-200')
  })
})
