import { queryOptions } from '@tanstack/react-query'
import { API_BASE, http } from '@/lib/http'

/**
 * 后台「资源预热」的接口（docs/06 第 10 节，S4-5 / A4-2；契约 service/api/openapi.yaml 的 AdminResourceWarm* schema）。
 */

/** 预热深度。 */
export type WarmMode = 'playlist_only' | 'with_segments'

/** 一轮预热的状态。 */
export type WarmStatus = 'running' | 'done' | 'stopped' | 'interrupted'

export type WarmStatusInfo = {
  status: WarmStatus
  title: string
  desc: string
}

export type WarmJob = {
  id: number
  /** 0 表示全部剧。 */
  drama_id: number
  drama_title: string
  mode: WarmMode
  mode_title: string
  segments_per_quality: number
  concurrency: number
  include_unpublished: boolean
  status: WarmStatus
  status_title: string
  episodes_total: number
  episodes_done: number
  /** 热好的资源数（清单与切片各算一个）。 */
  warmed: number
  /** 拿不到的资源数。它们会同时写进资源体检的结果里。 */
  failed: number
  fetched: number
  traffic_bytes: number
  /** 当前在热什么。 */
  current: string
  stop_requested: boolean
  error: string
  admin_id: number
  started_at: string
  heartbeat_at: string
  finished_at: string | null
  elapsed_ms: number
  /** 0–100。 */
  percent: number
}

export type WarmProgress = {
  /** 在跑的那一轮；没有在跑的时候是最近一轮。为 null 表示从来没跑过。 */
  job: WarmJob | null
  running: boolean
  statuses: WarmStatusInfo[]
  /** 对预热效果的说明（哪一层缓存被热到了），由后端给出。 */
  note: string
}

export type WarmStartPayload = {
  drama_id?: number
  mode?: WarmMode
  segments_per_quality?: number
  concurrency?: number
  include_unpublished?: boolean
}

/** MPEG-TS Profile 的运营默认值，与 service/internal/domain/resourcewarm 保持一致。 */
export const DEFAULT_WARM_SEGMENTS_PER_QUALITY = 2
export const DEFAULT_WARM_CONCURRENCY = 4

/** 开始预热对话框的本地表单值。字符串保留输入过程，提交时才转为 API 数字字段。 */
export type WarmStartForm = {
  scope: 'all' | 'one'
  dramaID: string
  includeUnpublished: boolean
  mode: WarmMode
  segments: string
  concurrency: string
}

/** 把开始预热表单转换为 API 请求体，默认值由页面传入，便于纯函数回归测试。 */
export function warmStartPayload(form: WarmStartForm): WarmStartPayload {
  return {
    ...(form.scope === 'one' ? { drama_id: Number(form.dramaID) } : { include_unpublished: form.includeUnpublished }),
    mode: form.mode,
    segments_per_quality: Number(form.segments),
    concurrency: Number(form.concurrency),
  }
}

export const warmKeys = {
  all: ['content', 'resource-warm'] as const,
  progress: ['content', 'resource-warm', 'progress'] as const,
  history: ['content', 'resource-warm', 'history'] as const,
}

export const warmProgressQuery = queryOptions({
  queryKey: warmKeys.progress,
  queryFn: () => http.get<WarmProgress>('/resource-warm/progress'),
})

export const warmHistoryQuery = queryOptions({
  queryKey: warmKeys.history,
  queryFn: () => http.get<{ list: WarmJob[] }>('/resource-warm/history'),
})

export const warmApi = {
  start: (body: WarmStartPayload) => http.post<WarmJob>('/resource-warm/start', body),
  /** 停止：只设信号，后台处理完当前这一集就收尾，已经热好的算数。 */
  stop: (id: number) => http.post<WarmJob>('/resource-warm/stop', { id }),
}

/** SSE 的地址。EventSource 不能带自定义头，但 cookie 会自动带上，后台用的就是 cookie 会话。 */
export const warmStreamURL = `${API_BASE}/resource-warm/progress?stream=1`

const STATUS_VARIANTS: Record<WarmStatus, 'default' | 'outline' | 'secondary' | 'destructive'> = {
  running: 'default',
  done: 'outline',
  stopped: 'secondary',
  interrupted: 'destructive',
}

export function warmStatusVariant(s: WarmStatus) {
  return STATUS_VARIANTS[s] ?? 'outline'
}

/** 字节数的人话。 */
export function bytesLabel(n: number): string {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`
}

/** 时长的人话。 */
export function durationLabel(ms: number): string {
  if (ms <= 0) return '—'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分 ${s % 60} 秒`
  return `${Math.floor(m / 60)} 小时 ${m % 60} 分`
}

/** 范围的人话。 */
export function scopeLabel(job: WarmJob): string {
  if (job.drama_id > 0) return job.drama_title ? `${job.drama_title}（#${job.drama_id}）` : `#${job.drama_id}`
  return job.include_unpublished ? '全部剧（含未上架）' : '全部已上架的剧'
}
