import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「功能开关与年龄门」的接口（docs/06 第 36 节，S3-3 / A3-3；
 * 契约 service/api/openapi.yaml 的 AdminFeature* schema）。
 *
 * 两段都存在 site_config 里，保存后站点版本 +1 并广播失效，各端几秒内生效。
 */

export type FeatureFlag =
  | 'comments'
  | 'danmaku'
  | 'register'
  | 'guest_interaction'
  | 'ranks'
  | 'share_poster'
  | 'pwa'
  | 'search_suggest'

export type FlagInfo = {
  flag: FeatureFlag
  title: string
  /** 关掉会怎样。由后端给出：开关的实际行为由后端决定，两边分开写必然漂移。 */
  impact: string
  on: boolean
}

export type AgeGate = {
  enabled: boolean
  title: string
  desc: string
  confirm_text: string
  deny_text: string
  /** 点「拒绝」后跳转的地址，留空则留在当前页。 */
  deny_url: string
  /** 确认后多少天内不再弹，0 表示每次打开都弹。 */
  remember_days: number
}

export type FeatureConfig = {
  flags: FlagInfo[]
  age_gate: AgeGate
}

export const featureKeys = {
  config: ['platform', 'features'] as const,
}

export const featureConfigQuery = queryOptions({
  queryKey: featureKeys.config,
  queryFn: () => http.get<FeatureConfig>('/features'),
})

export const featureApi = {
  /** 整体保存功能开关。未传的按打开保存，所以要把全部开关一起发。 */
  setFlags: (flags: Record<string, boolean>) => http.put<FeatureConfig>('/features/flags', { flags }),
  setAgeGate: (gate: AgeGate) => http.put<FeatureConfig>('/features/age-gate', gate),
}

/** 重复间隔的中文说明。 */
export function rememberLabel(days: number): string {
  if (days <= 0) return '每次打开都确认'
  if (days === 1) return '确认后 1 天内不再提示'
  return `确认后 ${days} 天内不再提示`
}
