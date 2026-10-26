import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「缓存」页的接口（docs/06 第 28 节，S4-4；契约 service/api/openapi.yaml 的 AdminCache* schema）。
 */

export type PurgeScope =
  | 'site_config'
  | 'home'
  | 'list'
  | 'drama'
  | 'play'
  | 'topic'
  | 'rank'
  | 'ops'
  | 'all'

export type CacheScopeInfo = {
  scope: PurgeScope
  title: string
  desc: string
  needs_drama: boolean
}

export type CacheKeyPrefix = {
  prefix: string
  title: string
  purgeable: boolean
  reason: string
  keys: number
}

export type CacheOverview = {
  site_version: number
  total_keys: number
  memory_used: number
  memory_text: string
  scopes: CacheScopeInfo[]
  prefixes: CacheKeyPrefix[]
}

export type PurgeResult = {
  scope: PurgeScope
  keys: number
  site_version?: number
}

export const cacheKeys = {
  overview: ['platform', 'cache'] as const,
}

export const cacheOverviewQuery = queryOptions({
  queryKey: cacheKeys.overview,
  queryFn: () => http.get<CacheOverview>('/cache'),
})

export const cacheApi = {
  purge: (scope: PurgeScope, dramaID?: number) =>
    http.post<PurgeResult>('/cache/purge', { scope, drama_id: dramaID }),
}

/** 字节数转可读文本。 */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${i === 0 ? value : value.toFixed(value >= 100 ? 0 : 1)} ${units[i]}`
}
