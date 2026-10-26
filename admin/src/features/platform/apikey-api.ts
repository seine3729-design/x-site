import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「API Key」的接口（docs/06 第 32 节，A4-3；契约 service/api/openapi.yaml 的 AdminAPIKey* schema）。
 *
 * 注意这是**开放平台**的密钥（供采集程序等服务端调用方对接），和社区模块的「接口密钥」
 * （/feedback/api-keys）是两套互不相干的东西。
 */

/** 权限范围。可选项由后端 `scopes` 字段给出，前端不写死清单。 */
export type APIScope = string

export type APIScopeInfo = {
  scope: APIScope
  title: string
  desc: string
}

/** 库里存的启用状态。 */
export type KeyStatus = 'active' | 'inactive'

/** 页面显示的状态：可用 / 停用 / 未用过（启用中但从未被调用）。 */
export type KeyDisplayStatus = 'active' | 'inactive' | 'unused'

export type APIKeyRow = {
  id: number
  name: string
  owner: string
  /** 密钥开头几位（如 xsk_a1b2c3）。完整密钥不在库里，也不会出现在任何接口响应里。 */
  hint: string
  scopes: APIScope[]
  status: KeyStatus
  used_count: number
  last_used_at: string | null
  created_by: number
  created_at: string
  updated_at: string
  display_status: KeyDisplayStatus
}

/** 生成结果。`secret` 是完整密钥，**只有这一次**能拿到。 */
export type NewAPIKey = APIKeyRow & { secret: string }

export type APIKeyListResult = Page<APIKeyRow> & { scopes: APIScopeInfo[] }

export type APIKeyListParams = {
  q?: string
  status?: KeyStatus
  page?: number
  page_size?: number
}

export type APIKeyPayload = {
  name: string
  owner: string
  scopes: APIScope[]
}

export const apiKeyKeys = {
  all: ['platform', 'apikeys'] as const,
  list: (p: APIKeyListParams) => ['platform', 'apikeys', 'list', p] as const,
}

export const apiKeyListQuery = (p: APIKeyListParams) =>
  queryOptions({
    queryKey: apiKeyKeys.list(p),
    queryFn: () => http.get<APIKeyListResult>('/apikeys', { query: p }),
    placeholderData: keepPreviousData,
  })

export const apiKeyApi = {
  create: (body: APIKeyPayload) => http.post<NewAPIKey>('/apikeys', body),
  /** 只改用途与使用方：权限范围不可改（改范围等于改已在调用方手里的密钥能做什么）。 */
  update: (id: number, body: APIKeyPayload) => http.patch<APIKeyRow>(`/apikeys/${id}`, body),
  disable: (id: number) => http.post<APIKeyRow>(`/apikeys/${id}/disable`),
  enable: (id: number) => http.post<APIKeyRow>(`/apikeys/${id}/enable`),
}

const DISPLAY_STATUS: Record<KeyDisplayStatus, { label: string; variant: 'default' | 'secondary' | 'outline' }> = {
  active: { label: '可用', variant: 'default' },
  unused: { label: '未用过', variant: 'outline' },
  inactive: { label: '停用', variant: 'secondary' },
}

export function displayStatusOf(s: KeyDisplayStatus) {
  return DISPLAY_STATUS[s] ?? { label: s, variant: 'outline' as const }
}

/** 权限范围 → 中文名。后端没给出的范围（旧数据）原样显示。 */
export function scopeTitle(scope: APIScope, scopes: APIScopeInfo[] | undefined): string {
  return scopes?.find((s) => s.scope === scope)?.title ?? scope
}
