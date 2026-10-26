/**
 * 时间与数字的展示格式。
 *
 * 时间一律同时给相对时间（看一眼就懂）和绝对时间（放在 title/datetime 里，供屏幕阅读器
 * 与需要精确时间的人使用），这是 feedback/docs/06 第 5 节的要求。
 */

const ABSOLUTE = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

const DATE_ONLY = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

/** 绝对时间，如 2026/10/24 14:05。 */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  return ABSOLUTE.format(new Date(ms))
}

/** 只到日期，如 2026/10/24。 */
export function formatDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  return DATE_ONLY.format(new Date(ms))
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/**
 * 相对时间，如「3 分钟前」。
 *
 * now 由调用方传入：服务端渲染时传请求时刻，组件内不读 Date.now()，
 * 这样同一次渲染里所有时间的基准一致，也不会在水合时和服务端结果不一致。
 */
export function formatRelative(iso: string | null | undefined, now: number): string {
  if (!iso) return ''
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  const diff = now - ms
  if (diff < 0) return '刚刚'
  if (diff < MINUTE) return '刚刚'
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)} 分钟前`
  if (diff < DAY) return `${Math.floor(diff / HOUR)} 小时前`
  if (diff < 30 * DAY) return `${Math.floor(diff / DAY)} 天前`
  return formatDate(iso)
}

/** 大数字缩写，如 1.2 万。列表里的票数、回答数用。 */
export function formatCount(n: number): string {
  if (n < 10_000) return String(n)
  const w = n / 10_000
  return `${w >= 10 ? Math.round(w) : w.toFixed(1)} 万`
}

/** 剩余时间，用于议题截止。到点返回空串，由调用方改显示「已结束」。 */
export function formatRemaining(iso: string | null | undefined, now: number): string {
  if (!iso) return ''
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  const diff = ms - now
  if (diff <= 0) return ''
  if (diff < HOUR) return `剩 ${Math.max(1, Math.floor(diff / MINUTE))} 分钟`
  if (diff < DAY) return `剩 ${Math.floor(diff / HOUR)} 小时`
  return `剩 ${Math.floor(diff / DAY)} 天`
}
