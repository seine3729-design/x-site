import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「框架」目录的接口（docs/07 第 2 节，docs/specs/A3-1.md；契约 service/api/openapi.yaml：静态页 0.33.0）。
 * 类型与契约中的 Admin* schema 逐字段对应；X0-5 接入生成的类型后替换。
 */

// ---------- 静态页 ----------

/** 启用状态（菜单）。 */
export type ActiveStatus = 'active' | 'inactive'

export const ACTIVE_LABELS: Record<ActiveStatus, string> = { active: '启用', inactive: '停用' }

export type PageStatus = 'draft' | 'published'
/** rich 用富文本编辑器，html 直接写 HTML；两种格式入库前都按同一白名单清洗。 */
export type BodyFormat = 'rich' | 'html'

export type StaticPage = {
  id: number
  title: string
  /** 前台地址 /p/{slug} */
  slug: string
  /** 清洗后的 HTML（保存时清洗，这里是清洗结果） */
  body: string
  body_format: BodyFormat
  status: PageStatus
  seo_title: string
  seo_desc: string
  /** 已发布且标记为页脚页的会出现在前台页脚「帮助与条款」 */
  is_footer: boolean
  created_at: string
  updated_at: string
}

/** 新建与整体保存静态页。 */
export type StaticPageInput = {
  title: string
  slug: string
  body: string
  body_format: BodyFormat
  status: PageStatus
  seo_title: string
  seo_desc: string
  is_footer: boolean
}

export type PageListParams = {
  q?: string
  status?: PageStatus
  page: number
  page_size: number
}

export const PAGE_STATUS_LABELS: Record<PageStatus, string> = { draft: '草稿', published: '已发布' }

export const BODY_FORMAT_LABELS: Record<BodyFormat, string> = { rich: '普通', html: 'HTML' }

/** 与后端一致的长度上限（清洗前）。 */
export const MAX_PAGE_TITLE = 100
export const MAX_PAGE_BODY = 100_000
export const MAX_PAGE_SEO_TITLE = 100
export const MAX_PAGE_SEO_DESC = 200

// ---------- 查询与写操作 ----------

export const siteKeys = {
  all: ['site'] as const,
  pages: ['site', 'pages'] as const,
  pageList: (p: PageListParams) => ['site', 'pages', 'list', p] as const,
  menus: ['site', 'menus'] as const,
  addresses: ['site', 'addresses'] as const,
  blocks: ['site', 'blocks'] as const,
  blockList: (page: string) => ['site', 'blocks', 'list', page] as const,
  blockPages: ['site', 'blocks', 'pages'] as const,
}

export const pageListQuery = (p: PageListParams) =>
  queryOptions({
    queryKey: siteKeys.pageList(p),
    queryFn: () => http.get<Page<StaticPage>>('/pages', { query: p }),
    placeholderData: keepPreviousData,
  })

/** 全部前台菜单（条数很少，一次取完）。 */
export const menuListQuery = () =>
  queryOptions({
    queryKey: siteKeys.menus,
    queryFn: () => http.get<{ list: SiteMenu[] }>('/menus'),
  })

export const addressesQuery = () =>
  queryOptions({
    queryKey: siteKeys.addresses,
    queryFn: () => http.get<SiteAddresses>('/addresses'),
  })

export const siteApi = {
  createPage: (body: StaticPageInput) => http.post<StaticPage>('/pages', body),
  updatePage: (id: number, body: StaticPageInput) => http.patch<StaticPage>(`/pages/${id}`, body),
  deletePage: (id: number) => http.delete<{ id: number }>(`/pages/${id}`),
  createMenu: (body: SiteMenuInput) => http.post<SiteMenu>('/menus', body),
  updateMenu: (id: number, body: SiteMenuInput) => http.patch<SiteMenu>(`/menus/${id}`, body),
  deleteMenu: (id: number) => http.delete<{ id: number }>(`/menus/${id}`),
  /** 按位置重排：ids 必须恰好是该位置现有的全部菜单，否则 409。 */
  reorderMenus: (position: MenuPosition, ids: number[]) => http.post<{ position: MenuPosition; ids: number[] }>('/menus/reorder', { position, ids }),
  /** 保存防丢地址，返回保存结果与新的站点配置版本。 */
  setAddresses: (body: SiteAddresses) => http.put<{ addresses: SiteAddresses; site_version: number }>('/addresses', body),
  createBlock: (body: BlockInput) => http.post<Block>('/blocks', body),
  updateBlock: (id: number, body: BlockInput) => http.patch<Block>(`/blocks/${id}`, body),
  deleteBlock: (id: number) => http.delete<{ id: number }>(`/blocks/${id}`),
  /** 按页面重排：ids 必须恰好是该页面现有的全部区块，否则 409。 */
  reorderBlocks: (page: string, ids: number[]) => http.post<{ page: string; ids: number[] }>('/blocks/reorder', { page, ids }),
}

// ---------- 页面区块（docs/06 第 15 节）----------

/** 可编排的页面：home 或 channel:{频道 slug}。 */
export const HOME_PAGE = 'home'

export type BlockPageRef = { value: string; label: string }

export const BLOCK_TYPES = [
  'carousel',
  'feature',
  'poster_grid',
  'h_scroll',
  'rank',
  'topic_group',
  'tag_cloud',
  'ad',
  'announce',
  'custom_html',
] as const
export type BlockType = (typeof BLOCK_TYPES)[number]

export const BLOCK_TYPE_LABELS: Record<BlockType, string> = {
  carousel: '轮播大图',
  feature: '精选大卡',
  poster_grid: '海报网格',
  h_scroll: '横滑列表',
  rank: '排行榜',
  topic_group: '专题组',
  tag_cloud: '标签云',
  ad: '广告位',
  announce: '公告条',
  custom_html: '自定义 HTML',
}

export const BLOCK_TYPE_HINTS: Record<BlockType, string> = {
  carousel: '首屏大图轮播，建议 3–6 部。',
  feature: '一到两部重点剧的大卡片。',
  poster_grid: '竖图网格，最常用的列表区块。',
  h_scroll: '横向滑动的一排卡片。',
  rank: '显示某张榜单的前若干名，剧由榜单服务提供。',
  topic_group: '若干专题的入口卡片。',
  tag_cloud: '标签入口，点进标签页。',
  ad: '只占位，素材由前台另取（按设备频控）。',
  announce: '只占位，公告内容由前台另取。',
  custom_html: '自定义内容，前台在沙箱中渲染；需要「自定义 HTML」权限。',
}

/** 需要配剧来源的区块类型。 */
export const DRAMA_BLOCK_TYPES: readonly BlockType[] = ['carousel', 'feature', 'poster_grid', 'h_scroll']
export const isDramaBlock = (t: BlockType) => DRAMA_BLOCK_TYPES.includes(t)

export const BLOCK_SOURCE_TYPES = ['manual', 'topic', 'auto'] as const
export type BlockSourceType = (typeof BLOCK_SOURCE_TYPES)[number]

export const BLOCK_SOURCE_LABELS: Record<BlockSourceType, string> = {
  manual: '手选剧集',
  topic: '用某个专题',
  auto: '按规则自动选',
}

/** 榜单与广告位直接用运营模块的枚举（RANK_BOARDS / AD_SLOTS）。公告条只能选这两种方式（站内信不在前台显示）。 */
export const ANNOUNCE_KINDS = ['topbar', 'popup'] as const
export type AnnounceKind = (typeof ANNOUNCE_KINDS)[number]
export const ANNOUNCE_KIND_LABELS: Record<AnnounceKind, string> = { topbar: '顶部滚动条', popup: '弹窗' }

/** 区块的来源与类型参数：按类型只用得到其中几项。 */
export type BlockSource = {
  drama_ids: number[]
  topic_id: number
  term_codes: string[]
  tag_id: number | null
  sort: string
  window: string
  rank_board: string
  ad_slot: string
  announce_kind: string
  topic_ids: number[]
  tag_ids: number[]
}

export type Block = {
  id: number
  page: string
  type: BlockType
  title: string
  subtitle: string
  item_limit: number
  status: ActiveStatus
  more_link: string
  cols_desktop: number
  device: MenuDevice
  source_type: BlockSourceType
  source: BlockSource
  /** 只有自定义 HTML 区块有值（清洗后的结果） */
  custom_html: string
  sort: number
  created_at: string
  updated_at: string
}

export type BlockInput = {
  page: string
  type: BlockType
  title: string
  subtitle: string
  item_limit: number
  status: ActiveStatus
  more_link: string
  cols_desktop: number
  device: MenuDevice
  source_type: BlockSourceType
  source: BlockSource
  custom_html: string
}

/** 与后端一致的上限与默认值。 */
export const MAX_BLOCK_TITLE = 50
export const MAX_BLOCK_SUBTITLE = 100
export const MIN_BLOCK_ITEMS = 1
export const MAX_BLOCK_ITEMS = 60
export const DEFAULT_BLOCK_ITEMS = 24
export const MIN_BLOCK_COLS = 2
export const MAX_BLOCK_COLS = 8
export const DEFAULT_BLOCK_COLS = 6
export const MAX_BLOCK_DRAMA_IDS = 60
export const MAX_BLOCK_TOPIC_IDS = 20
export const MAX_BLOCK_TAG_IDS = 50
export const MAX_BLOCK_TERM_CODES = 10
export const MAX_BLOCK_HTML = 20_000
export const MAX_BLOCKS_PER_PAGE = 50
/** 自定义 HTML 区块另要这个权限点（默认只有超级管理员）。 */
export const PERM_BLOCK_CUSTOM_HTML = 'blocks.custom_html'

/** 某个页面的全部区块（条数很少，一次取完，含停用的）。 */
export const blockListQuery = (page: string) =>
  queryOptions({
    queryKey: siteKeys.blockList(page),
    queryFn: () => http.get<{ list: Block[] }>('/blocks', { query: { page } }),
  })

/** 可编排的页面：首页 + 启用的频道页。 */
export const blockPagesQuery = () =>
  queryOptions({
    queryKey: siteKeys.blockPages,
    queryFn: () => http.get<{ list: BlockPageRef[] }>('/blocks/pages'),
  })

// ---------- 前台菜单（docs/06 第 12 节）----------

export const MENU_POSITIONS = ['top', 'mobile_tabbar', 'mobile_drawer', 'footer'] as const
export type MenuPosition = (typeof MENU_POSITIONS)[number]

export const MENU_POSITION_LABELS: Record<MenuPosition, string> = {
  top: '顶部导航',
  mobile_tabbar: '手机底栏',
  mobile_drawer: '手机侧边抽屉',
  footer: '页脚',
}

export const MENU_POSITION_HINTS: Record<MenuPosition, string> = {
  top: '电脑与手机顶部的主导航。',
  mobile_tabbar: '手机底部标签栏，最多 5 项（只算启用的）；建议配上底栏图标。',
  mobile_drawer: '手机端从侧边划出的抽屉菜单。',
  footer: '页脚链接，可按「分组标题」分栏。',
}

export const MENU_LINK_TYPES = ['internal', 'page', 'channel', 'topic', 'tag', 'external'] as const
export type MenuLinkType = (typeof MENU_LINK_TYPES)[number]

export const MENU_LINK_TYPE_LABELS: Record<MenuLinkType, string> = {
  internal: '站内页',
  page: '静态页',
  channel: '频道',
  topic: '专题',
  tag: '标签',
  external: '外部网址',
}

/** 站内页白名单（与后端一致）。 */
export const INTERNAL_PAGES = ['home', 'shua', 'explore', 'rank', 'topic', 'search', 'me', 'way_home'] as const
export type InternalPage = (typeof INTERNAL_PAGES)[number]

export const INTERNAL_PAGE_LABELS: Record<InternalPage, string> = {
  home: '首页',
  shua: '刷剧',
  explore: '探索',
  rank: '排行',
  topic: '专题列表',
  search: '搜索',
  me: '我的',
  way_home: '回家的路',
}

export type MenuVisibleTo = 'all' | 'auth' | 'guest'
export type MenuDevice = 'all' | 'mobile' | 'desktop'

export const MENU_VISIBLE_LABELS: Record<MenuVisibleTo, string> = { all: '所有人', auth: '已登录', guest: '未登录' }
export const MENU_DEVICE_LABELS: Record<MenuDevice, string> = { all: '手机和电脑', mobile: '只给手机', desktop: '只给电脑' }

export type SiteMenu = {
  id: number
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
  sort: number
  created_at: string
  updated_at: string
  /** 为空表示链接目标正常；否则是不可用的原因（前台不下发这一项，记录保留） */
  target_issue: string
}

export type SiteMenuInput = {
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

/** 与后端一致的上限。 */
export const MAX_MENU_LABEL = 20
export const MAX_MENU_GROUP = 20
export const MAX_MENU_BADGE = 6
export const MAX_MENU_ICON = 40
export const MAX_TABBAR_ITEMS = 5

// ---------- 防丢地址（site_config.addresses）----------

export type Social = { name: string; url: string }

export type SiteAddresses = {
  notice: string
  latest_url: string
  permanent_url: string
  backup_urls: string[]
  publish_page: string
  support_url: string
  telegram: string
  socials: Social[]
}

export const MAX_BACKUP_URLS = 3
export const MAX_SOCIALS = 10
export const MAX_NOTICE = 200
