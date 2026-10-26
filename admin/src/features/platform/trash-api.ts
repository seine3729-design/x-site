import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「回收站」的接口（docs/06 第 35 节，A4-3 / S4-7；契约 service/api/openapi.yaml 的 AdminTrash* schema）。
 */

/** 分类。目前只有剧集：设置类不进回收站（docs/06 第 35 节），其余模块还没实现软删。 */
export type TrashCategory = 'dramas'

export type TrashCategoryInfo = {
  category: TrashCategory
  title: string
  desc: string
  count: number
}

export type TrashOverview = {
  retention_days: number
  categories: TrashCategoryInfo[]
  /** 最近一条到期的时间；回收站为空时为 null。 */
  oldest_expires_at: string | null
}

export type TrashItem = {
  id: number
  title: string
  slug: string
  cover: string
  /** 删除前的状态，还原后恢复成它。 */
  status: string
  /** 附带内容的条数，剧集为分集数。 */
  extra: number
  deleted_at: string
  updated_at: string
  expires_at: string
  /** 剩余天数，0 表示今天到期。 */
  days_left: number
}

export type RestoreResult = { id: number; title: string; slug: string; status: string }

export type TrashListParams = {
  category: TrashCategory
  q?: string
  page?: number
  page_size?: number
}

export const trashKeys = {
  all: ['platform', 'trash'] as const,
  overview: ['platform', 'trash', 'overview'] as const,
  list: (p: TrashListParams) => ['platform', 'trash', 'list', p] as const,
}

export const trashOverviewQuery = queryOptions({
  queryKey: trashKeys.overview,
  queryFn: () => http.get<TrashOverview>('/trash'),
})

export const trashListQuery = (p: TrashListParams) =>
  queryOptions({
    queryKey: trashKeys.list(p),
    queryFn: () => http.get<Page<TrashItem>>('/trash/items', { query: p }),
    placeholderData: keepPreviousData,
  })

export const trashApi = {
  restore: (category: TrashCategory, id: number) =>
    http.post<RestoreResult>(`/trash/items/${id}/restore`, { category }),
  purge: (category: TrashCategory, id: number) =>
    http.post<TrashItem>(`/trash/items/${id}/purge`, { category }),
}

/** 剩余天数的中文说明：0 天显示「今天到期」。 */
export function expiryLabel(daysLeft: number): string {
  if (daysLeft <= 0) return '今天到期'
  if (daysLeft === 1) return '明天到期'
  return `${daysLeft} 天后到期`
}

/** 删除前的状态 → 中文（还原后会恢复成它）。 */
const STATUS_LABELS: Record<string, string> = {
  draft: '草稿',
  published: '已上架',
  offline: '已下架',
}

export function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? status
}
