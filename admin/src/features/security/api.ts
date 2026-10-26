import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「安全中心」接口（docs/06 第 39 节，A4-4 / S4-8；契约 service/api/openapi.yaml 的 DomainLoginLog* 等 schema）。
 * 纯只读：管理员登录记录、密钥轮换记录（只有版本号 kid，不含密钥内容）、风控概览。
 */

export type LoginResult = 'success' | 'bad_password' | 'inactive' | 'mfa_failed' | 'locked'

export type LoginLog = {
  id: number
  admin_id: number | null
  username: string
  current_username: string
  result: LoginResult
  result_title: string
  suspicious: boolean
  ip_prefix: string
  ua_digest: string
  created_at: string
}

export type ResultInfo = { result: LoginResult; title: string }

export type LoginLogPage = {
  list: LoginLog[]
  total: number
  page: number
  page_size: number
  results: ResultInfo[]
}

export type KeyVersion = {
  purpose: string
  purpose_title: string
  version: string
  primary: boolean
}

export type RiskItem = {
  kind: string
  title: string
  value: number | null
  desc: string
}

export type RiskOverview = {
  items: RiskItem[]
  note: string
}

export type LoginLogParams = {
  result?: LoginResult
  q?: string
  from?: string
  to?: string
  page: number
  page_size: number
}

export const RESULT_VARIANTS: Record<LoginResult, 'outline' | 'secondary' | 'destructive'> = {
  success: 'outline',
  bad_password: 'destructive',
  mfa_failed: 'destructive',
  locked: 'secondary',
  inactive: 'secondary',
}

export const securityKeys = {
  all: ['security'] as const,
  loginLogs: (p: LoginLogParams) => ['security', 'login-logs', p] as const,
  keyRotations: ['security', 'key-rotations'] as const,
  risk: ['security', 'risk'] as const,
}

export const loginLogsQuery = (p: LoginLogParams) =>
  queryOptions({
    queryKey: securityKeys.loginLogs(p),
    queryFn: () => http.get<LoginLogPage>('/security/login-logs', { query: p }),
    placeholderData: keepPreviousData,
  })

export const keyRotationsQuery = () =>
  queryOptions({
    queryKey: securityKeys.keyRotations,
    queryFn: () => http.get<{ list: KeyVersion[] }>('/security/key-rotations'),
  })

export const riskQuery = () =>
  queryOptions({
    queryKey: securityKeys.risk,
    queryFn: () => http.get<RiskOverview>('/security/risk'),
  })
