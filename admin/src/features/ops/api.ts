import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { DramaStatus, Page } from '@/features/content/api'

/**
 * 后台「运营」目录的接口（docs/07 第 2 节，docs/specs/A3-4.md；契约 service/api/openapi.yaml：公告 0.18.0、广告 0.22.0、热搜词 0.27.0、排行 0.28.0）。
 * 类型与契约中的 Admin* schema 逐字段对应；X0-5 接入生成的类型后替换。
 */

export type ActiveStatus = 'active' | 'inactive'

/** 用户端支持的界面语言（docs/04 A.4）；「投放语言」不选表示全部语言。 */
export const LOCALES = ['zh-CN', 'en', 'es', 'vi', 'ja', 'ko', 'th', 'hi', 'ar'] as const
export type Locale = (typeof LOCALES)[number]

export const LOCALE_LABELS: Record<Locale, string> = {
  'zh-CN': '简体中文',
  en: '英语',
  es: '西班牙语',
  vi: '越南语',
  ja: '日语',
  ko: '韩语',
  th: '泰语',
  hi: '印地语',
  ar: '阿拉伯语',
}

export const ACTIVE_LABELS: Record<ActiveStatus, string> = {
  active: '启用',
  inactive: '停用',
}

// ---------- 公告 ----------

export type AnnouncementKind = 'topbar' | 'popup' | 'inbox'
export type Frequency = 'always' | 'once' | 'daily'
export type AnnouncementDisplayStatus = 'live' | 'scheduled' | 'ended' | 'inactive'

/** 公告可以出现的前台页面；「出现页面」不选表示全部页面。 */
export const ANNOUNCEMENT_PAGES = ['home', 'explore', 'channel', 'tag', 'topic', 'drama', 'play', 'shua', 'rank', 'search', 'me'] as const
export type AnnouncementPage = (typeof ANNOUNCEMENT_PAGES)[number]

export type Announcement = {
  id: number
  kind: AnnouncementKind
  content: string
  click_url: string
  frequency: Frequency
  pages: AnnouncementPage[]
  locales: Locale[]
  image: string
  start_at: string | null
  end_at: string | null
  status: ActiveStatus
  display_status: AnnouncementDisplayStatus
  created_at: string
  updated_at: string
}

/** 新建与整体保存公告（站内信暂未开放）。 */
export type AnnouncementInput = {
  kind: Exclude<AnnouncementKind, 'inbox'>
  content: string
  click_url: string
  frequency: Frequency
  pages: AnnouncementPage[]
  locales: Locale[]
  image: string
  start_at: string | null
  end_at: string | null
  status: ActiveStatus
}

export type AnnouncementListParams = {
  q?: string
  kind?: AnnouncementKind
  status?: ActiveStatus
  page: number
  page_size: number
}

export const ANNOUNCEMENT_KIND_LABELS: Record<AnnouncementKind, string> = {
  topbar: '顶部滚动条',
  popup: '弹窗',
  inbox: '站内信',
}

export const FREQUENCY_LABELS: Record<Frequency, string> = {
  always: '每次显示',
  once: '每人一次',
  daily: '每人每天一次',
}

export const ANNOUNCEMENT_DISPLAY_LABELS: Record<AnnouncementDisplayStatus, string> = {
  live: '显示中',
  scheduled: '未开始',
  ended: '已结束',
  inactive: '已停用',
}

export const ANNOUNCEMENT_PAGE_LABELS: Record<AnnouncementPage, string> = {
  home: '首页',
  explore: '探索',
  channel: '频道',
  tag: '标签页',
  topic: '专题',
  drama: '剧集详情',
  play: '播放页',
  shua: '刷剧',
  rank: '排行',
  search: '搜索',
  me: '我的',
}

// ---------- 广告 ----------

/** 广告位置（docs/06 第 18 节）。 */
export const AD_SLOTS = [
  'global_popup',
  'home_top',
  'home_mid',
  'home_bottom',
  'footer_banner',
  'list_feed',
  'detail_banner',
  'preroll',
  'pause',
  'immersive_feed',
  'text_link',
  'app_download',
] as const
export type AdSlot = (typeof AD_SLOTS)[number]
export type CreativeType = 'image' | 'video' | 'text' | 'html'
export type AdDevice = 'all' | 'mobile' | 'desktop'
export type AdDisplayStatus = 'live' | 'ended' | 'inactive'

export type Ad = {
  id: number
  name: string
  slot: AdSlot
  creative_type: CreativeType
  image: string
  video_url: string
  text: string
  /** 清洗后的 HTML */
  html: string
  alt: string
  click_url: string
  device: AdDevice
  locales: Locale[]
  weight: number
  end_at: string | null
  freq_cap: number
  status: ActiveStatus
  display_status: AdDisplayStatus
  imp_count: number
  click_count: number
  created_at: string
  updated_at: string
}

/** 新建与整体保存广告。只有该素材类型用得到的字段会被保存。 */
export type AdInput = {
  name: string
  slot: AdSlot
  creative_type: CreativeType
  image: string
  video_url: string
  text: string
  html: string
  alt: string
  click_url: string
  device: AdDevice
  locales: Locale[]
  weight: number
  end_at: string | null
  freq_cap: number
  status: ActiveStatus
}

export type AdListParams = {
  q?: string
  slot?: AdSlot
  status?: ActiveStatus
  page: number
  page_size: number
}

export const AD_SLOT_LABELS: Record<AdSlot, string> = {
  global_popup: '全站弹窗',
  home_top: '首页顶部横幅',
  home_mid: '首页中部横幅',
  home_bottom: '首页底部横幅',
  footer_banner: '页脚横幅',
  list_feed: '列表信息流',
  detail_banner: '详情页横幅',
  preroll: '播放前贴片',
  pause: '暂停贴片',
  immersive_feed: '沉浸流插入',
  text_link: '文字链',
  app_download: 'App 下载浮窗',
}

/** 各位置的建议素材（docs/06 第 18 节「各位置给建议尺寸」）。 */
export const AD_SLOT_HINTS: Record<AdSlot, string> = {
  global_popup: '建议图片约 3:4，如 600×800。',
  home_top: '建议图片约 4:1，如 1200×300。',
  home_mid: '建议图片约 6:1，如 1200×200。',
  home_bottom: '建议图片约 6:1，如 1200×200。',
  footer_banner: '建议图片约 10:1，如 1200×120。',
  list_feed: '与剧集封面同比例，建议竖版 3:4，如 600×800。',
  detail_banner: '建议图片约 6:1，如 1200×200。',
  preroll: '建议 16:9 视频（如 1280×720），5–15 秒；前台只在电脑端播放，可跳过。',
  pause: '建议图片 16:9，如 640×360。',
  immersive_feed: '建议竖屏 9:16 图片或视频，如 1080×1920。',
  text_link: '建议文字 20 字以内。',
  app_download: '建议图片约 4:1，如 640×160；只在网页端、播放页以外显示。',
}

export const CREATIVE_LABELS: Record<CreativeType, string> = {
  image: '图片',
  video: '视频',
  text: '文字',
  html: '自定义 HTML',
}

export const AD_DEVICE_LABELS: Record<AdDevice, string> = {
  all: '手机和电脑',
  mobile: '只投手机',
  desktop: '只投电脑',
}

export const AD_STATUS_LABELS: Record<ActiveStatus, string> = {
  active: '投放中',
  inactive: '停用',
}

export const AD_DISPLAY_LABELS: Record<AdDisplayStatus, string> = {
  live: '投放中',
  ended: '已结束',
  inactive: '已停用',
}

// ---------- 热搜词 ----------

export type HotSource = 'manual' | 'auto'

export type HotWord = {
  id: number
  /** 规范化后的词（全角转半角、小写、合并空白） */
  word: string
  search_count: number
  /** 搜了但片库没有结果的次数 */
  miss_count: number
  pinned: boolean
  pin_sort: number
  visible: boolean
  source: HotSource
  last_searched_at: string | null
  created_at: string
  updated_at: string
}

export type HotListParams = {
  q?: string
  visible?: 'true' | 'false'
  pinned?: 'true' | 'false'
  missing?: 'true'
  page: number
  page_size: number
}

/** 热搜榜条数，也是置顶上限。 */
export const HOT_BOARD_SIZE = 10

export const HOT_SOURCE_LABELS: Record<HotSource, string> = { manual: '手动添加', auto: '自动收录' }

// ---------- 排行干预 ----------

export const RANK_BOARDS = ['drama_week', 'drama_month'] as const
export type RankBoard = (typeof RANK_BOARDS)[number]

export const RANK_BOARD_LABELS: Record<RankBoard, string> = { drama_week: '剧集周榜', drama_month: '剧集月榜' }

/** 计分系数（两张榜共用）：分数 = 播放 × play + 追剧 × follow + 点赞 × like + 看完 × finish + 运营加分。 */
export type RankWeights = { play: number; follow: number; like: number; finish: number }

/** 一部剧在一张榜上的人工调整。置顶与不让上榜互斥；boost 为加分，负数为减分。 */
export type RankOverride = {
  drama_id: number
  title: string
  drama_status: DramaStatus
  in_trash: boolean
  pinned: boolean
  blocked: boolean
  boost: number
  updated_at: string
}

export type RankOverrideInput = Pick<RankOverride, 'drama_id' | 'pinned' | 'blocked' | 'boost'>

/** 榜单的一个名次。pinned 只在预览里标出（线上榜单不存这个标记）。 */
export type RankRow = { rank: number; drama_id: number; title: string; heat: number; pinned: boolean }

/** 一张榜的页面数据：人工调整、前台正在显示的前 20 名（computed_at 为排名时间）、按当前设置现算的预览。 */
export type RankBoardView = {
  board: RankBoard
  overrides: RankOverride[]
  computed_at: string | null
  live: RankRow[]
  preview: RankRow[]
}

/** 每张榜最多调整几部剧、加分范围与系数上限（与后端一致）。 */
export const MAX_RANK_OVERRIDES = 100
export const MAX_RANK_BOOST = 1_000_000_000
export const MAX_RANK_WEIGHT = 1000

// ---------- 查询与写操作 ----------

export const opsKeys = {
  all: ['ops'] as const,
  announcements: ['ops', 'announcements'] as const,
  announcementList: (p: AnnouncementListParams) => ['ops', 'announcements', 'list', p] as const,
  ads: ['ops', 'ads'] as const,
  adList: (p: AdListParams) => ['ops', 'ads', 'list', p] as const,
  hotsearch: ['ops', 'hotsearch'] as const,
  hotList: (p: HotListParams) => ['ops', 'hotsearch', 'list', p] as const,
  ranks: ['ops', 'ranks'] as const,
  rankBoards: ['ops', 'ranks', 'board'] as const,
  rankBoard: (b: RankBoard) => ['ops', 'ranks', 'board', b] as const,
  rankingConfig: ['ops', 'ranks', 'config'] as const,
}

export const announcementListQuery = (p: AnnouncementListParams) =>
  queryOptions({
    queryKey: opsKeys.announcementList(p),
    queryFn: () => http.get<Page<Announcement>>('/announcements', { query: p }),
    placeholderData: keepPreviousData,
  })

export const adListQuery = (p: AdListParams) =>
  queryOptions({
    queryKey: opsKeys.adList(p),
    queryFn: () => http.get<Page<Ad>>('/ads', { query: p }),
    placeholderData: keepPreviousData,
  })

export const hotListQuery = (p: HotListParams) =>
  queryOptions({
    queryKey: opsKeys.hotList(p),
    queryFn: () => http.get<Page<HotWord>>('/hotsearch', { query: p }),
    placeholderData: keepPreviousData,
  })

/** 全部置顶词（最多 HOT_BOARD_SIZE 个，一页取完），按置顶顺序。 */
export const pinnedHotQuery = () => hotListQuery({ pinned: 'true', page: 1, page_size: HOT_BOARD_SIZE })

export const rankBoardQuery = (b: RankBoard) =>
  queryOptions({
    queryKey: opsKeys.rankBoard(b),
    queryFn: () => http.get<RankBoardView>(`/ranks/${b}`),
  })

export const rankingConfigQuery = () =>
  queryOptions({
    queryKey: opsKeys.rankingConfig,
    queryFn: () => http.get<RankWeights>('/ranking-config'),
  })

export const opsApi = {
  createAnnouncement: (body: AnnouncementInput) => http.post<Announcement>('/announcements', body),
  updateAnnouncement: (id: number, body: AnnouncementInput) => http.patch<Announcement>(`/announcements/${id}`, body),
  deleteAnnouncement: (id: number) => http.delete<void>(`/announcements/${id}`),
  createAd: (body: AdInput) => http.post<Ad>('/ads', body),
  updateAd: (id: number, body: AdInput) => http.patch<Ad>(`/ads/${id}`, body),
  deleteAd: (id: number) => http.delete<void>(`/ads/${id}`),
  createHotWord: (word: string) => http.post<HotWord>('/hotsearch', { word }),
  /** 部分修改：只传要改的项。置顶时后端自动设为显示，不显示时同时取消置顶。 */
  updateHotWord: (id: number, patch: { visible?: boolean; pinned?: boolean }) => http.patch<HotWord>(`/hotsearch/${id}`, patch),
  reorderHotWords: (ids: number[]) => http.post<void>('/hotsearch/reorder', { ids }),
  deleteHotWord: (id: number) => http.delete<void>(`/hotsearch/${id}`),
  /** 整体保存一张榜的人工调整（不在列表里的删除），返回保存后的页面数据。重新排名后才生效。 */
  setRankOverrides: (b: RankBoard, overrides: RankOverrideInput[]) => http.put<RankBoardView>(`/ranks/${b}`, { overrides }),
  /** 马上重新排名（两张榜）。done 为 false 表示后台还没算完，稍后自动完成。 */
  recomputeRanks: () => http.post<{ done: boolean }>('/ranks/recompute'),
  setRankingConfig: (w: RankWeights) => http.put<RankWeights>('/ranking-config', w),
}
