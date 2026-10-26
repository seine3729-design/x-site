import { RULE_SORT_LABELS, RULE_WINDOW_LABELS, type RuleSort, type RuleWindow } from '@/features/content/api'
import {
  ANNOUNCE_KIND_LABELS,
  DEFAULT_BLOCK_COLS,
  DEFAULT_BLOCK_ITEMS,
  HOME_PAGE,
  INTERNAL_PAGE_LABELS,
  isDramaBlock,
  MAX_MENU_ICON,
  MENU_LINK_TYPE_LABELS,
  type ActiveStatus,
  type AnnounceKind,
  type Block,
  type BlockInput,
  type BlockPageRef,
  type BlockSource,
  type BlockSourceType,
  type BlockType,
  type InternalPage,
  type MenuDevice,
  type MenuLinkType,
  type MenuPosition,
  type MenuVisibleTo,
  type PageStatus,
  type SiteAddresses,
  type SiteMenu,
  type SiteMenuInput,
  type StaticPage,
  type StaticPageInput,
} from './api'

/** 静态页表单的值（与请求体同名，便于 zod 校验后直接提交）。正文统一用富文本编辑器，不再让运营选格式。 */
export type StaticPageForm = {
  title: string
  slug: string
  body: string
  status: PageStatus
  seo_title: string
  seo_desc: string
  is_footer: boolean
}

export function pageToForm(p?: StaticPage): StaticPageForm {
  return {
    title: p?.title ?? '',
    slug: p?.slug ?? '',
    body: p?.body ?? '',
    status: p?.status ?? 'draft',
    seo_title: p?.seo_title ?? '',
    seo_desc: p?.seo_desc ?? '',
    is_footer: p?.is_footer ?? false,
  }
}

/**
 * 表单值 → 整体保存的请求体（已通过校验）。slug 转小写，与后端保存结果一致。
 * 正文一律按富文本（rich）保存：后端两种格式用同一套白名单清洗，格式只是编辑器形态，统一成 rich 无损，
 * 老的 html 页面重新保存后也变成 rich（docs/06 第 13 节）。
 */
export function formToPageInput(f: StaticPageForm): StaticPageInput {
  return {
    title: f.title.trim(),
    slug: f.slug.trim().toLowerCase(),
    body: f.body.trim(),
    body_format: 'rich',
    status: f.status,
    seo_title: f.seo_title.trim(),
    seo_desc: f.seo_desc.trim(),
    is_footer: f.is_footer,
  }
}

/** 前台地址：/p/<slug>。slug 为空时返回空串。 */
export function pagePath(slug: string): string {
  const s = slug.trim().toLowerCase()
  return s === '' ? '' : `/p/${s}`
}

/**
 * 正文是否被后端清洗过（保存后用来提示）：只比较去掉首尾空白后的文本。
 * 后端会去掉脚本、样式表、iframe、表单与事件属性，并给站外链接加 noopener。
 */
export function bodyWasSanitized(submitted: string, saved: string): boolean {
  return submitted.trim() !== saved.trim()
}

/** 列表里显示的正文摘要：去掉标签、压缩空白，超长截断。 */
export function bodySummary(body: string, max = 60): string {
  const text = body
    .replace(/<[^>]*>/g, ' ')
    .replace(/&nbsp;/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return text.length > max ? `${text.slice(0, max)}…` : text
}

// ---------- 前台菜单 ----------

/** 菜单表单的值（与请求体同名）。 */
export type MenuForm = {
  position: MenuPosition
  label: string
  link_type: MenuLinkType
  link_value: string
  group_title: string
  icon: string
  tabbar_icon: string
  badge: string
  visible_to: MenuVisibleTo
  device: MenuDevice
  new_tab: boolean
  status: ActiveStatus
}

export function menuToForm(m?: SiteMenu, position: MenuPosition = 'top'): MenuForm {
  return {
    position: m?.position ?? position,
    label: m?.label ?? '',
    link_type: m?.link_type ?? 'internal',
    link_value: m?.link_value ?? 'home',
    group_title: m?.group_title ?? '',
    icon: m?.icon ?? '',
    tabbar_icon: m?.tabbar_icon ?? '',
    badge: m?.badge ?? '',
    visible_to: m?.visible_to ?? 'all',
    device: m?.device ?? 'all',
    new_tab: m?.new_tab ?? false,
    status: m?.status ?? 'active',
  }
}

/** 表单值 → 请求体（已通过校验）。slug 类链接转小写；不适用的字段清空（与后端保存结果一致）。 */
export function formToMenuInput(f: MenuForm): SiteMenuInput {
  const slugLike = f.link_type !== 'internal' && f.link_type !== 'external'
  return {
    position: f.position,
    label: f.label.trim(),
    link_type: f.link_type,
    link_value: slugLike ? f.link_value.trim().toLowerCase() : f.link_value.trim(),
    group_title: f.position === 'footer' ? f.group_title.trim() : '',
    icon: f.icon.trim(),
    tabbar_icon: f.position === 'mobile_tabbar' ? f.tabbar_icon.trim() : '',
    badge: f.badge.trim(),
    visible_to: f.visible_to,
    device: f.device,
    new_tab: f.new_tab,
    status: f.status,
  }
}

/** 列表里显示的链接：站内页显示中文名，其余显示类型 + 取值。 */
export function menuLinkSummary(m: Pick<SiteMenu, 'link_type' | 'link_value'>): string {
  if (m.link_type === 'internal') {
    return INTERNAL_PAGE_LABELS[m.link_value as InternalPage] ?? m.link_value
  }
  if (m.link_type === 'external') return m.link_value
  return `${MENU_LINK_TYPE_LABELS[m.link_type]}：${m.link_value}`
}

/** 各链接类型的输入提示。 */
export function linkValueHint(kind: MenuLinkType): string {
  switch (kind) {
    case 'internal':
      return '选择一个前台页面。'
    case 'external':
      return '填 http:// 或 https:// 开头的完整地址。'
    default:
      return `填${MENU_LINK_TYPE_LABELS[kind]}的 slug，保存时校验是否存在（可以先指向草稿，发布后自动出现）。`
  }
}

/** lucide 图标名：字母、数字与连字符；留空也可以。 */
export function isIconNameOrEmpty(value: string): boolean {
  const s = value.trim()
  return s === '' || (s.length <= MAX_MENU_ICON && /^[A-Za-z0-9-]+$/.test(s))
}

/** http(s) 完整地址（防丢地址与外部菜单用）；留空也可以。 */
export function isExternalURLOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (/[\s\p{Cc}]/u.test(s)) return false
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '' && u.username === '' && u.password === ''
  } catch {
    return false
  }
}

export const EXTERNAL_URL_HINT = 'http:// 或 https:// 开头的完整地址'

/** 按位置分组（保持服务端返回的顺序）。 */
export function groupByPosition(list: readonly SiteMenu[]): Record<MenuPosition, SiteMenu[]> {
  const out = { top: [], mobile_tabbar: [], mobile_drawer: [], footer: [] } as Record<MenuPosition, SiteMenu[]>
  for (const m of list) out[m.position]?.push(m)
  return out
}

/** 某个位置启用中的条数（手机底栏上限提示用）。 */
export function activeCount(list: readonly SiteMenu[]): number {
  return list.filter((m) => m.status === 'active').length
}

// ---------- 防丢地址 ----------

/** 防丢地址表单：备用地址与社交账号用 useFieldArray，所以包成对象数组。 */
export type AddressForm = {
  notice: string
  latest_url: string
  permanent_url: string
  backup_urls: { url: string }[]
  publish_page: string
  support_url: string
  telegram: string
  socials: { name: string; url: string }[]
}

export function addressesToForm(a?: SiteAddresses): AddressForm {
  return {
    notice: a?.notice ?? '',
    latest_url: a?.latest_url ?? '',
    permanent_url: a?.permanent_url ?? '',
    backup_urls: (a?.backup_urls ?? []).map((url) => ({ url })),
    publish_page: a?.publish_page ?? '',
    support_url: a?.support_url ?? '',
    telegram: a?.telegram ?? '',
    socials: (a?.socials ?? []).map((s) => ({ name: s.name, url: s.url })),
  }
}

/** 表单值 → 请求体：去空白，整行留空的备用地址与社交账号丢掉（与后端一致）。 */
export function formToAddresses(f: AddressForm): SiteAddresses {
  return {
    notice: f.notice.trim(),
    latest_url: f.latest_url.trim(),
    permanent_url: f.permanent_url.trim(),
    backup_urls: f.backup_urls.map((b) => b.url.trim()).filter((u) => u !== ''),
    publish_page: f.publish_page.trim(),
    support_url: f.support_url.trim(),
    telegram: f.telegram.trim(),
    socials: f.socials.map((s) => ({ name: s.name.trim(), url: s.url.trim() })).filter((s) => s.name !== '' || s.url !== ''),
  }
}

// ---------- 页面区块 ----------

/**
 * 区块表单的值。数字与「某些类型才用」的字段都按字符串 / 可空对象存，提交时按类型裁剪成请求体，
 * 与后端「只保存该类型用得到的参数」一致。
 */
export type BlockForm = {
  page: string
  type: BlockType
  title: string
  subtitle: string
  item_limit: string
  status: ActiveStatus
  more_link: string
  cols_desktop: string
  device: MenuDevice
  source_type: BlockSourceType
  /** 手选：按顺序显示 */
  drama_ids: { id: number; title?: string }[]
  /** 用某个专题 */
  topic: { id: number; label: string } | null
  /** 自动选 */
  term_codes: string[]
  tag: { id: number; label: string } | null
  sort: RuleSort
  window: RuleWindow
  rank_board: string
  ad_slot: string
  announce_kind: AnnounceKind
  /** 专题组、标签云：留空各自取「列在专题列表页的专题」与「热门标签」 */
  topic_ids: { id: number; label: string }[]
  tag_ids: { id: number; label: string }[]
  custom_html: string
}

export function blockToForm(b?: Block, page = HOME_PAGE): BlockForm {
  return {
    page: b?.page ?? page,
    type: b?.type ?? 'poster_grid',
    title: b?.title ?? '',
    subtitle: b?.subtitle ?? '',
    item_limit: String(b?.item_limit ?? DEFAULT_BLOCK_ITEMS),
    status: b?.status ?? 'active',
    more_link: b?.more_link ?? '',
    cols_desktop: String(b?.cols_desktop ?? DEFAULT_BLOCK_COLS),
    device: b?.device ?? 'all',
    source_type: b?.source_type ?? 'auto',
    drama_ids: (b?.source.drama_ids ?? []).map((id) => ({ id })),
    topic: b?.source.topic_id ? { id: b.source.topic_id, label: `专题 #${b.source.topic_id}` } : null,
    term_codes: b?.source.term_codes ?? [],
    tag: b?.source.tag_id ? { id: b.source.tag_id, label: `标签 #${b.source.tag_id}` } : null,
    sort: (b?.source.sort || 'latest') as RuleSort,
    window: (b?.source.window || 'all') as RuleWindow,
    rank_board: b?.source.rank_board ?? '',
    ad_slot: b?.source.ad_slot ?? '',
    announce_kind: (b?.source.announce_kind || 'topbar') as AnnounceKind,
    topic_ids: (b?.source.topic_ids ?? []).map((id) => ({ id, label: `专题 #${id}` })),
    tag_ids: (b?.source.tag_ids ?? []).map((id) => ({ id, label: `标签 #${id}` })),
    custom_html: b?.custom_html ?? '',
  }
}

/** 空来源：每种类型只填自己用得到的字段，其余保持零值（与后端保存结果一致）。 */
function emptySource(): BlockSource {
  return {
    drama_ids: [],
    topic_id: 0,
    term_codes: [],
    tag_id: null,
    sort: '',
    window: '',
    rank_board: '',
    ad_slot: '',
    announce_kind: '',
    topic_ids: [],
    tag_ids: [],
  }
}

/** 表单值 → 请求体（已通过校验）：按类型裁剪来源参数。 */
export function formToBlockInput(f: BlockForm): BlockInput {
  const source = emptySource()
  let sourceType: BlockSourceType = 'auto'
  if (isDramaBlock(f.type)) {
    sourceType = f.source_type
    if (f.source_type === 'manual') source.drama_ids = f.drama_ids.map((d) => d.id)
    else if (f.source_type === 'topic') source.topic_id = f.topic?.id ?? 0
    else {
      source.term_codes = f.term_codes
      source.tag_id = f.tag?.id ?? null
      source.sort = f.sort
      source.window = f.window
    }
  } else if (f.type === 'rank') source.rank_board = f.rank_board
  else if (f.type === 'ad') source.ad_slot = f.ad_slot
  else if (f.type === 'announce') source.announce_kind = f.announce_kind
  else if (f.type === 'topic_group') source.topic_ids = f.topic_ids.map((t) => t.id)
  else if (f.type === 'tag_cloud') source.tag_ids = f.tag_ids.map((t) => t.id)

  return {
    page: f.page,
    type: f.type,
    title: f.title.trim(),
    subtitle: f.subtitle.trim(),
    item_limit: Number(f.item_limit),
    status: f.status,
    more_link: f.more_link.trim(),
    cols_desktop: Number(f.cols_desktop),
    device: f.device,
    source_type: sourceType,
    source,
    custom_html: f.type === 'custom_html' ? f.custom_html.trim() : '',
  }
}

/** 列表里显示的来源摘要（一行说清这个区块显示什么）。 */
export function blockSourceSummary(b: Block): string {
  const s = b.source
  if (isDramaBlock(b.type)) {
    switch (b.source_type) {
      case 'manual':
        return `手选 ${s.drama_ids.length} 部：${s.drama_ids
          .slice(0, 6)
          .map((id) => `#${id}`)
          .join('、')}${s.drama_ids.length > 6 ? ' …' : ''}`
      case 'topic':
        return `专题 #${s.topic_id}`
      default: {
        const parts = [RULE_SORT_LABELS[(s.sort || 'latest') as RuleSort]]
        if ((s.window || 'all') !== 'all') parts.push(RULE_WINDOW_LABELS[s.window as RuleWindow])
        if (s.term_codes.length > 0) parts.push(`取值 ${s.term_codes.join('、')}`)
        if (s.tag_id) parts.push(`标签 #${s.tag_id}`)
        return `自动：${parts.join(' · ')}`
      }
    }
  }
  switch (b.type) {
    case 'rank':
      return s.rank_board === 'drama_month' ? '剧集月榜' : '剧集周榜'
    case 'ad':
      return `广告位 ${s.ad_slot}`
    case 'announce':
      return ANNOUNCE_KIND_LABELS[(s.announce_kind || 'topbar') as AnnounceKind]
    case 'topic_group':
      return s.topic_ids.length > 0 ? `指定 ${s.topic_ids.length} 个专题` : '列在专题列表页的专题'
    case 'tag_cloud':
      return s.tag_ids.length > 0 ? `指定 ${s.tag_ids.length} 个标签` : '热门标签'
    default:
      return `${
        b.custom_html
          .replace(/<[^>]*>/g, ' ')
          .replace(/\s+/g, ' ')
          .trim()
          .slice(0, 40) || '（空）'
      }`
  }
}

/** 这个类型的区块会显示几项（广告位、公告条、自定义 HTML 不是列表）。 */
export function blockHasList(t: BlockType): boolean {
  return isDramaBlock(t) || t === 'rank' || t === 'topic_group' || t === 'tag_cloud'
}

/** 站内路径或 http(s) 完整地址；留空也可以（「查看全部」链接）。 */
export function isLinkOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (s.startsWith('/')) return !/[\s\p{Cc}]/u.test(s)
  return isExternalURLOrEmpty(s)
}

export const BLOCK_LINK_HINT = '选填。站内路径（如 /explore）或 http(s) 完整地址。'

/** 页面名 → 显示名（下拉里没有这个页面时退回原值）。 */
export function blockPageLabel(value: string, pages: readonly BlockPageRef[]): string {
  return pages.find((p) => p.value === value)?.label ?? value
}
