import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/** 页码分页的列表（与后端一致）。 */
export type Page<T> = { list: T[]; total: number; page: number; page_size: number }

// ---------- 数据看板（GET /analytics/overview） ----------

/** 全站计数类指标（窗口汇总 totals、环比对比段 prev、每日序列 daily 同形）。 */
export type Metrics = {
  uv: number
  play_attempts: number
  play_starts: number
  play_errors: number
  watch_seconds: number
  registrations: number
  reg_wall_views: number
  ad_impressions: number
  ad_clicks: number
}

export type DailyPoint = Metrics & { day: string }

export type Overview = {
  from: string
  to: string
  days: number
  totals: Metrics
  prev: Metrics
  daily: DailyPoint[]
}

// ---------- 剧集分析列表（GET /analytics/dramas） ----------

export type DramaSort = 'viewers' | 'play_starts' | 'watch_seconds'

export type DramaRow = {
  id: number
  title: string
  status: string
  latest_episode: number
  created_at: string
  viewers: number
  play_starts: number
  watch_seconds: number
  is_new: boolean
}

export type DramaListParams = { days?: number; sort?: DramaSort; page?: number; page_size?: number }

// ---------- 单剧分析（GET /analytics/dramas/:id） ----------

export type DramaMetrics = { viewers: number; play_starts: number; watch_seconds: number }
export type DramaDay = DramaMetrics & { day: string }

export type EpisodeRetention = {
  episode_id: number
  number: number
  title: string
  viewers: number
  play_starts: number
  watch_seconds: number
  retention_pct: number
}

export type DramaDetail = {
  id: number
  title: string
  status: string
  latest_episode: number
  created_at: string
  online_days: number
  from: string
  to: string
  days: number
  totals: DramaMetrics
  prev: DramaMetrics
  daily: DramaDay[]
  episodes: EpisodeRetention[]
}

// ---------- 查询 ----------

export const analyticsKeys = {
  all: ['analytics'] as const,
  overview: (days: number) => ['analytics', 'overview', days] as const,
  dramaList: (p: DramaListParams) => ['analytics', 'dramas', 'list', p] as const,
  drama: (id: number, days: number) => ['analytics', 'dramas', 'detail', id, days] as const,
}

export const overviewQuery = (days: number) =>
  queryOptions({
    queryKey: analyticsKeys.overview(days),
    queryFn: () => http.get<Overview>('/analytics/overview', { query: { days } }),
  })

export const dramaAnalyticsListQuery = (p: DramaListParams) =>
  queryOptions({
    queryKey: analyticsKeys.dramaList(p),
    queryFn: () => http.get<Page<DramaRow>>('/analytics/dramas', { query: p }),
    placeholderData: keepPreviousData,
  })

export const dramaAnalyticsQuery = (id: number, days: number) =>
  queryOptions({
    queryKey: analyticsKeys.drama(id, days),
    queryFn: () => http.get<DramaDetail>(`/analytics/dramas/${id}`, { query: { days } }),
  })

// ---------- 展示与格式化 ----------

/** 时间范围选项（天），与后端窗口上限一致。 */
export const DAYS_OPTIONS = [7, 30, 90] as const

export const DRAMA_SORT_LABELS: Record<DramaSort, string> = {
  viewers: '按观看人次',
  play_starts: '按起播次数',
  watch_seconds: '按观看时长',
}

export const DRAMA_STATUS_LABELS: Record<string, string> = {
  draft: '草稿',
  published: '已上架',
  offline: '已下架',
}

export function statusLabel(status: string): string {
  return DRAMA_STATUS_LABELS[status] ?? status
}

/** 千分位整数。 */
export function formatInt(n: number): string {
  return n.toLocaleString('zh-CN')
}

/** 秒转「x 小时 y 分」/「y 分」/「z 秒」，用于有效观看时长。 */
export function formatWatch(sec: number): string {
  if (sec <= 0) return '0 秒'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (h > 0) return m > 0 ? `${formatInt(h)} 小时 ${m} 分` : `${formatInt(h)} 小时`
  if (m > 0) return `${m} 分`
  return `${sec} 秒`
}

/** 比率百分比；分母为 0 返回「—」。 */
export function ratePct(num: number, den: number): string {
  if (den <= 0) return '—'
  return `${((num / den) * 100).toFixed(1)}%`
}

/** 环比 (cur-prev)/prev×100；prev=0 返回 null（前端显示「—」）。 */
export function deltaPct(cur: number, prev: number): number | null {
  if (prev === 0) return null
  return ((cur - prev) / prev) * 100
}

// ---------- 收入与成本（GET/PUT /finance） ----------

export type FinanceEntry = {
  ad_revenue: number
  referral_revenue: number
  traffic_cost: number
  server_cost: number
  payment_fee: number
  ad_note: string
  referral_note: string
  traffic_note: string
  server_note: string
  payment_note: string
  saved: boolean
  updated_at: string | null
}

export type FinanceCurrencyTotal = { currency: string; orders: number; gross: number; refund: number; net: number }
export type FinanceChannelRevenue = { channel: string; currency: string; orders: number; gross: number; refund: number }
export type FinancePlanRevenue = { plan_code: string; currency: string; orders: number; gross: number; refund: number }
export type FinanceMembership = {
  totals: FinanceCurrencyTotal[]
  by_channel: FinanceChannelRevenue[]
  by_plan: FinancePlanRevenue[]
}
export type FinanceReport = {
  month: string
  entry: FinanceEntry
  membership: FinanceMembership
  gross_profit_fen: number
}
export type FinanceSaveInput = {
  ad_revenue: number
  referral_revenue: number
  traffic_cost: number
  server_cost: number
  payment_fee: number
  ad_note: string
  referral_note: string
  traffic_note: string
  server_note: string
  payment_note: string
}

export const financeKeys = {
  all: ['finance'] as const,
  month: (m: string) => ['finance', m] as const,
}

export const financeQuery = (month: string) =>
  queryOptions({
    queryKey: financeKeys.month(month),
    queryFn: () => http.get<FinanceReport>('/finance', { query: { month } }),
  })

export function saveFinance(month: string, input: FinanceSaveInput) {
  return http.put<FinanceReport>('/finance', input, { query: { month } })
}

/** 分 → 人民币元字符串（两位小数，带千分位）。 */
export function yuan(fen: number): string {
  return (fen / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** 按币种格式化订单金额：CNY 为分 → ¥元，USDT 为 micro → USDT。 */
export function money(amount: number, currency: string): string {
  if (currency === 'USDT') {
    return `${(amount / 1_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} USDT`
  }
  return `¥${yuan(amount)}`
}

export const CHANNEL_LABELS: Record<string, string> = {
  alipay: '支付宝',
  wechat: '微信',
  usdt: 'USDT',
  iap_apple: 'App Store',
  iap_google: 'Google Play',
}

export const PLAN_LABELS: Record<string, string> = {
  day: '天卡',
  week: '周卡',
  month: '月卡',
  quarter: '季卡',
  year: '年卡',
}

export function channelLabel(code: string): string {
  return CHANNEL_LABELS[code] ?? code
}

export function planLabel(code: string): string {
  return PLAN_LABELS[code] ?? code
}

// ---------- 导出给 Agent（GET /analytics/export） ----------

export type ExportPackage = {
  days: number
  from: string
  to: string
  generated_at: string
  content: string
}

export function exportAnalytics(days: number) {
  return http.get<ExportPackage>('/analytics/export', { query: { days } })
}
