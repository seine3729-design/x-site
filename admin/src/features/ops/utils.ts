import { formatDateTime } from '@/lib/format'
import { fromLocalInput, toLocalInput } from '@/features/content/utils'
import type { DramaStatus } from '@/features/content/api'
import {
  ANNOUNCEMENT_PAGE_LABELS,
  ANNOUNCEMENT_PAGES,
  LOCALE_LABELS,
  LOCALES,
  MAX_RANK_BOOST,
  MAX_RANK_WEIGHT,
  type ActiveStatus,
  type Ad,
  type AdDevice,
  type AdInput,
  type AdSlot,
  type Announcement,
  type AnnouncementInput,
  type AnnouncementPage,
  type CreativeType,
  type HotWord,
  type Locale,
  type RankOverride,
  type RankOverrideInput,
  type RankRow,
  type RankWeights,
} from './api'

/** 站内路径（/ 开头，不能是 // 或 /\）或 http(s) 完整地址；留空也可以。与后端校验一致。 */
export function isLinkOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (/[\s\p{Cc}]/u.test(s)) return false
  if (s.startsWith('/')) return !s.startsWith('//') && !s.startsWith('/\\')
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '' && u.username === '' && u.password === ''
  } catch {
    return false
  }
}

export const LINK_HINT = '/ 开头的站内路径，或 http:// / https:// 开头的完整地址'

/** 媒体字段额外接受服务端生成的 img/ 或 video/ 相对 key；不放宽普通跳转链接。 */
export function isMediaRefOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (/^(img|video)\//.test(s)) {
    return !/[\s\p{Cc}\\]/u.test(s) && s.split('/').every((part) => part !== '' && part !== '.' && part !== '..')
  }
  return isLinkOrEmpty(s)
}

export const MEDIA_REF_HINT = '上传后自动写入 img/ 或 video/ 路径；历史资源也可保留 / 开头或 http(s) 地址'

/** 按固定顺序排列多选项（与后端保存顺序一致，比较是否改动时不受勾选顺序影响）。 */
export function ordered<T extends string>(values: readonly T[], all: readonly T[]): T[] {
  return all.filter((v) => values.includes(v))
}

/** 「出现页面」摘要：不选为「全部页面」。 */
export function pagesSummary(pages: readonly AnnouncementPage[]): string {
  if (pages.length === 0 || pages.length === ANNOUNCEMENT_PAGES.length) return '全部页面'
  return ordered(pages, ANNOUNCEMENT_PAGES)
    .map((p) => ANNOUNCEMENT_PAGE_LABELS[p])
    .join('、')
}

/** 「投放语言」摘要：不选为「全部语言」。 */
export function localesSummary(locales: readonly Locale[]): string {
  if (locales.length === 0 || locales.length === LOCALES.length) return '全部语言'
  return ordered(locales, LOCALES)
    .map((l) => LOCALE_LABELS[l])
    .join('、')
}

/** 起止时间摘要。 */
export function windowSummary(start: string | null, end: string | null): string {
  if (!start && !end) return '不限'
  const s = start ? formatDateTime(start).slice(0, 16) : '立即'
  const e = end ? formatDateTime(end).slice(0, 16) : '不结束'
  return `${s} – ${e}`
}

/** 公告表单的值（时间用 datetime-local 的本地时间字符串）。 */
export type AnnouncementForm = {
  kind: AnnouncementInput['kind']
  content: string
  click_url: string
  frequency: AnnouncementInput['frequency']
  pages: AnnouncementPage[]
  locales: Locale[]
  image: string
  start_at: string
  end_at: string
  status: AnnouncementInput['status']
}

export function announcementToForm(a?: Announcement): AnnouncementForm {
  return {
    kind: a && a.kind !== 'inbox' ? a.kind : 'topbar',
    content: a?.content ?? '',
    click_url: a?.click_url ?? '',
    frequency: a?.frequency ?? 'always',
    pages: a?.pages ?? [],
    locales: a?.locales ?? [],
    image: a?.image ?? '',
    start_at: toLocalInput(a?.start_at ?? null),
    end_at: toLocalInput(a?.end_at ?? null),
    status: a?.status ?? 'active',
  }
}

/** 表单值 → 整体保存的请求体（已通过校验）。只有弹窗保存配图。 */
export function formToAnnouncementInput(f: AnnouncementForm): AnnouncementInput {
  return {
    kind: f.kind,
    content: f.content.trim(),
    click_url: f.click_url.trim(),
    frequency: f.frequency,
    pages: ordered(f.pages, ANNOUNCEMENT_PAGES),
    locales: ordered(f.locales, LOCALES),
    image: f.kind === 'popup' ? f.image.trim() : '',
    start_at: fromLocalInput(f.start_at),
    end_at: fromLocalInput(f.end_at),
    status: f.status,
  }
}

// ---------- 广告 ----------

/** 广告表单的值：权重与次数上限先按字符串编辑，结束时间用 datetime-local 的本地时间字符串；新建时位置为空。 */
export type AdForm = {
  name: string
  slot: string
  creative_type: CreativeType
  image: string
  video_url: string
  text: string
  html: string
  alt: string
  click_url: string
  device: AdDevice
  locales: Locale[]
  weight: string
  freq_cap: string
  end_at: string
  status: ActiveStatus
}

export const DEFAULT_AD_WEIGHT = 10

export function adToForm(a?: Ad): AdForm {
  return {
    name: a?.name ?? '',
    slot: a?.slot ?? '',
    creative_type: a?.creative_type ?? 'image',
    image: a?.image ?? '',
    video_url: a?.video_url ?? '',
    text: a?.text ?? '',
    html: a?.html ?? '',
    alt: a?.alt ?? '',
    click_url: a?.click_url ?? '',
    device: a?.device ?? 'all',
    locales: a?.locales ?? [],
    weight: String(a?.weight ?? DEFAULT_AD_WEIGHT),
    freq_cap: String(a?.freq_cap ?? 0),
    end_at: toLocalInput(a?.end_at ?? null),
    status: a?.status ?? 'active',
  }
}

/** 表单值 → 整体保存的请求体（已通过校验）。只提交该素材类型用得到的字段，其余清空（与后端保存结果一致）。 */
export function formToAdInput(f: AdForm): AdInput {
  const t = f.creative_type
  const media = t === 'image' || t === 'video'
  return {
    name: f.name.trim(),
    slot: f.slot as AdSlot,
    creative_type: t,
    image: media ? f.image.trim() : '',
    video_url: t === 'video' ? f.video_url.trim() : '',
    text: t === 'text' ? f.text.trim() : '',
    html: t === 'html' ? f.html.trim() : '',
    alt: media ? f.alt.trim() : '',
    click_url: f.click_url.trim(),
    device: f.device,
    locales: ordered(f.locales, LOCALES),
    weight: Number.parseInt(f.weight.trim(), 10),
    freq_cap: Number.parseInt(f.freq_cap.trim(), 10),
    end_at: fromLocalInput(f.end_at),
    status: f.status,
  }
}

/** 不带符号、不带小数的整数，且在 [min, max] 内（权重、次数上限）。 */
export function isIntInRange(value: string, min: number, max: number): boolean {
  const s = value.trim()
  if (!/^\d{1,6}$/.test(s)) return false
  const n = Number(s)
  return n >= min && n <= max
}

/** 点击率：点击 ÷ 曝光，保留两位小数；没有曝光时为「—」。 */
export function ctr(impressions: number, clicks: number): string {
  if (impressions <= 0) return '—'
  return `${((clicks / impressions) * 100).toFixed(2)}%`
}

/** 列表里一行显示的素材摘要。 */
export function creativeSummary(a: Pick<Ad, 'creative_type' | 'image' | 'video_url' | 'text' | 'html'>): string {
  switch (a.creative_type) {
    case 'image':
      return a.image
    case 'video':
      return a.video_url
    case 'text':
      return a.text
    default:
      return `HTML，${a.html.length} 个字符（已清洗）`
  }
}

// ---------- 热搜词 ----------

export type HotPatch = { visible?: boolean; pinned?: boolean }

/** 按后端规则算出修改后的显示与置顶：置顶时自动显示，不显示时同时取消置顶。 */
export function applyHotPatch(w: Pick<HotWord, 'visible' | 'pinned'>, patch: HotPatch): { visible: boolean; pinned: boolean } {
  let { visible, pinned } = w
  if (patch.visible !== undefined) {
    visible = patch.visible
    pinned = pinned && visible
  }
  if (patch.pinned !== undefined) {
    pinned = patch.pinned
    visible = visible || pinned
  }
  return { visible, pinned }
}

/** 修改后的提示：连带改了另一项时说明。 */
export function hotPatchMessage(w: Pick<HotWord, 'word' | 'visible' | 'pinned'>, patch: HotPatch): string {
  const next = applyHotPatch(w, patch)
  const parts: string[] = []
  if (next.pinned !== w.pinned) parts.push(next.pinned ? '已置顶' : '已取消置顶')
  if (next.visible !== w.visible) parts.push(next.visible ? '已显示在热搜榜' : '已从热搜榜隐藏')
  return `「${w.word}」${parts.join('，') || '没有变化'}`
}

// ---------- 排行干预 ----------

/** 调整方式：置顶（还可以加减分，决定多部置顶之间的先后）、不让上榜、只加减分。置顶与不让上榜互斥。 */
export const RANK_MODES = ['pinned', 'blocked', 'boost'] as const
export type RankMode = (typeof RANK_MODES)[number]

export const RANK_MODE_LABELS: Record<RankMode, string> = { pinned: '置顶', blocked: '不让上榜', boost: '加分 / 减分' }

/** 人工调整表单的一行：加分先按字符串编辑；新加的行还不知道剧名与状态（保存后由后端补上）。 */
export type RankOverrideRow = {
  drama_id: number
  title: string
  drama_status: DramaStatus | null
  in_trash: boolean
  mode: RankMode
  boost: string
}

export function rankOverrideToRow(o: RankOverride): RankOverrideRow {
  return {
    drama_id: o.drama_id,
    title: o.title,
    drama_status: o.drama_status,
    in_trash: o.in_trash,
    mode: o.pinned ? 'pinned' : o.blocked ? 'blocked' : 'boost',
    boost: o.boost === 0 ? '' : String(o.boost),
  }
}

/** 新加的一行：默认置顶、不加分。 */
export function newRankOverrideRow(dramaId: number, title = ''): RankOverrideRow {
  return { drama_id: dramaId, title, drama_status: null, in_trash: false, mode: 'pinned', boost: '' }
}

/** 表单的一行 → 保存的请求体（已通过校验）。不让上榜时加分没有意义，按 0 保存。 */
export function rowToRankOverrideInput(r: RankOverrideRow): RankOverrideInput {
  const s = r.boost.trim()
  return {
    drama_id: r.drama_id,
    pinned: r.mode === 'pinned',
    blocked: r.mode === 'blocked',
    boost: r.mode === 'blocked' || s === '' ? 0 : Number.parseInt(s, 10),
  }
}

/** 加分：可带负号的整数，绝对值不超过 MAX_RANK_BOOST（与后端一致）。 */
export function isRankBoost(value: string): boolean {
  const s = value.trim()
  if (!/^-?\d{1,10}$/.test(s)) return false
  return Math.abs(Number(s)) <= MAX_RANK_BOOST
}

/** 计分系数：0–MAX_RANK_WEIGHT，最多两位小数（后端也按两位小数保存）。 */
export function isRankWeight(value: string): boolean {
  const s = value.trim()
  if (!/^\d{1,4}(\.\d{1,2})?$/.test(s)) return false
  return Number(s) <= MAX_RANK_WEIGHT
}

export type RankWeightsForm = Record<keyof RankWeights, string>

export function weightsToForm(w: RankWeights): RankWeightsForm {
  return { play: String(w.play), follow: String(w.follow), like: String(w.like), finish: String(w.finish) }
}

export function formToWeights(f: RankWeightsForm): RankWeights {
  return { play: Number(f.play.trim()), follow: Number(f.follow.trim()), like: Number(f.like.trim()), finish: Number(f.finish.trim()) }
}

/** 预览与线上榜单是否不同（名次或分数）。不同说明还没重新排名，改动尚未生效。 */
export function rankDiffers(live: readonly RankRow[], preview: readonly RankRow[]): boolean {
  if (live.length !== preview.length) return true
  return live.some((r, i) => r.drama_id !== preview[i]?.drama_id || r.heat !== preview[i]?.heat)
}

/**
 * 预览中一部剧相对线上榜单的名次变化：正数为上升几名、负数为下降、0 为不变；线上榜单里没有时为 null（新上榜）。
 */
export function rankMove(live: readonly RankRow[], row: Pick<RankRow, 'drama_id' | 'rank'>): number | null {
  const before = live.find((r) => r.drama_id === row.drama_id)
  return before ? before.rank - row.rank : null
}

/** 分数：千分位，最多两位小数。 */
export function formatHeat(v: number): string {
  return v.toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}
