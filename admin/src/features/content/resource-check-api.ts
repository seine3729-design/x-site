import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「资源体检」的接口（docs/06 第 9 节，S4-5 / A4-2；契约 service/api/openapi.yaml 的 AdminResourceCheck* schema）。
 *
 * 它查的是**资源**在不在（一个一个对象去读），不是线路好不好 —— 后者在「选线预览与健康」页。
 */

/** 资源大类。 */
export type CheckFileType = 'video' | 'cover'

/** 具体哪个字段。只有大类分不出是竖封面还是横封面。 */
export type CheckField =
  | 'cover_portrait'
  | 'cover_landscape'
  | 'episode_cover'
  | 'episode_poster'
  | 'video'

/** 体检结果（docs/02 第 4.3 节的六个枚举值）。 */
export type CheckResult =
  | 'ok'
  | 'not_found'
  | 'forbidden'
  | 'server_error'
  | 'unopenable'
  | 'unreachable'

export type ResultInfo = {
  result: CheckResult
  title: string
  desc: string
}

export type CheckRow = {
  id: number
  drama_id: number
  drama_title: string
  /** 0 表示剧级资源（封面）。 */
  episode_id: number
  episode_number: number
  file_type: CheckFileType
  field: CheckField
  /** 字段的中文名，后端给出。 */
  field_title: string
  /** 不含查询串的地址（签名材料不入库）。 */
  url: string
  /** 0 表示没经线路（运营把封面填成了完整网址）。 */
  line_id: number
  line_name: string
  result: CheckResult
  /** 结果的中文名，后端给出。 */
  result_title: string
  status_code: number
  elapsed_ms: number
  message: string
  checked_at: string
}

export type CheckSummary = {
  total: number
  bad: number
  last_checked_at: string | null
}

export type CheckOverview = {
  list: CheckRow[]
  /** 符合筛选条件的总数（可能大于 shown）。 */
  total: number
  shown: number
  limit: number
  summary: CheckSummary
  results: ResultInfo[]
  /** 非空时说明有一类资源没有可用线路、体检会跳过它们。 */
  line_warning: string
}

export type CheckListParams = {
  drama_id?: number
  bad_only?: boolean
  q?: string
  limit?: number
}

export type RunResult = { dramas: number; queued: number }

export const checkKeys = {
  all: ['content', 'resource-checks'] as const,
  list: (p: CheckListParams) => ['content', 'resource-checks', 'list', p] as const,
}

export const checkListQuery = (p: CheckListParams) =>
  queryOptions({
    queryKey: checkKeys.list(p),
    queryFn: () => http.get<CheckOverview>('/resource-checks', { query: p }),
    placeholderData: keepPreviousData,
  })

export const checkApi = {
  /** 发起体检。立即返回，由 worker 按剧分片跑，页面轮询列表看进展。 */
  run: (body: { drama_id?: number; include_unpublished?: boolean }) =>
    http.post<RunResult>('/resource-checks/run', body),
  /** 清空结果。drama_id 省略或为 0 时清全部。只清体检表，不动源文件。 */
  clear: (drama_id?: number) => http.post<{ deleted: number }>('/resource-checks/clear', { drama_id: drama_id ?? 0 }),
}

/** 结果 → 徽标样式。只有「正常」是中性色，其余都要显眼。 */
const RESULT_VARIANTS: Record<CheckResult, 'outline' | 'destructive' | 'secondary'> = {
  ok: 'outline',
  not_found: 'destructive',
  forbidden: 'destructive',
  server_error: 'secondary',
  unopenable: 'secondary',
  unreachable: 'secondary',
}

export function resultVariant(r: CheckResult) {
  return RESULT_VARIANTS[r] ?? 'secondary'
}

const FILE_TYPE_LABELS: Record<CheckFileType, string> = { video: '视频', cover: '封面' }

export function fileTypeLabel(t: CheckFileType): string {
  return FILE_TYPE_LABELS[t] ?? t
}

/** 耗时的人话。 */
export function elapsedLabel(ms: number): string {
  if (ms <= 0) return '—'
  if (ms < 1000) return `${ms} 毫秒`
  return `${(ms / 1000).toFixed(1)} 秒`
}
