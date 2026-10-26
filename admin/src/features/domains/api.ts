import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「域名与证书」接口（docs/06 第 38 节，A4-4 / S4-8；契约 service/api/openapi.yaml 的 Domain* schema）。
 *
 * 证书职责：公网入口证书由部署层 certbot 自动签发；这里的「手动生成」签的是自签证书（私钥仅返回一次、不落库），
 * 用于源站 mTLS / 本地 HTTPS / 应急占位。清单发布的签名在离线工具完成，后台不保存签名私钥。
 */

export type DomainRole = 'web_primary' | 'web_backup' | 'nav' | 'api' | 'media' | 'admin'
export type DomainStatus = 'active' | 'watch' | 'disabled'
export type CertState = 'ok' | 'expiring' | 'expired' | 'unknown'
export type KeyType = 'ec_p256' | 'rsa_2048'

export type DomainEntry = {
  id: number
  host: string
  role: DomainRole
  role_title: string
  weight: number
  region: string
  cdn: string
  dns_provider: string
  cert_expires_at: string | null
  cert_state: CertState
  cert_days_left: number | null
  status: DomainStatus
  status_title: string
  note: string
  created_at: string
  updated_at: string
}

export type DomainInput = {
  host: string
  role: DomainRole
  weight: number
  region: string
  cdn: string
  dns_provider: string
  cert_expires_at: string | null
  status: DomainStatus
  note: string
}

export type GenCertInput = {
  common_name: string
  sans: string[]
  days: number
  key_type: KeyType
}

export type GeneratedCert = {
  cert_pem: string
  key_pem: string
  key_type: KeyType
  sans: string[]
  not_before: string
  not_after: string
  fingerprint: string
}

export type CertInfo = {
  subject: string
  issuer: string
  sans: string[]
  not_before: string
  not_after: string
  fingerprint: string
  is_ca: boolean
  self_signed: boolean
}

export type ReleaseHost = { host: string; weight: number; region: string }
export type ReleaseWebEntry = { host: string; role: 'primary' | 'backup' }
export type ReleasePayload = {
  v: number
  issued_at: string
  expires_at: string
  api?: ReleaseHost[]
  web?: ReleaseWebEntry[]
  media?: ReleaseHost[]
  nav?: string
}

export type DomainRelease = {
  version: number
  payload: ReleasePayload
  signature: string
  note: string
  published_by: number | null
  published_by_name: string
  published_at: string
}

export type ReleasePreview = {
  version: number
  from_version: number | null
  hosts: number
  payload: ReleasePayload
}

export type DomainHealthRow = {
  host: string
  region: string
  probe_ok: number
  probe_fail: number
  success_rate: number | null
  failover_count: number
  fail_reasons: Record<string, number>
}

export type DomainHealthOverview = {
  list: DomainHealthRow[]
  since_days: number
  note?: string
}

export type DomainListParams = { status?: DomainStatus; role?: DomainRole; q?: string }

export const ROLE_LABELS: Record<DomainRole, string> = {
  web_primary: 'Web 主入口',
  web_backup: 'Web 备用',
  nav: '永久导航',
  api: 'API',
  media: '媒体',
  admin: '后台',
}

export const STATUS_LABELS: Record<DomainStatus, string> = {
  active: '启用',
  watch: '观察',
  disabled: '停用',
}

export const KEY_TYPE_LABELS: Record<KeyType, string> = {
  ec_p256: 'EC P-256（推荐）',
  rsa_2048: 'RSA 2048',
}

/** 证书状态 → 徽标样式。只有「正常」是中性色。 */
export const CERT_STATE_VARIANTS: Record<CertState, 'outline' | 'secondary' | 'destructive'> = {
  ok: 'outline',
  expiring: 'secondary',
  expired: 'destructive',
  unknown: 'secondary',
}

export const CERT_STATE_LABELS: Record<CertState, string> = {
  ok: '正常',
  expiring: '即将到期',
  expired: '已过期',
  unknown: '未录入',
}

export const domainKeys = {
  all: ['domains'] as const,
  list: (p: DomainListParams) => ['domains', 'list', p] as const,
  releases: (page: number, pageSize: number) => ['domains', 'releases', page, pageSize] as const,
  health: ['domains', 'health'] as const,
}

export const domainsQuery = (p: DomainListParams) =>
  queryOptions({
    queryKey: domainKeys.list(p),
    queryFn: () => http.get<{ list: DomainEntry[]; total: number }>('/domains', { query: p }),
    placeholderData: keepPreviousData,
  })

export type ReleasesData = {
  list: DomainRelease[]
  total: number
  page: number
  page_size: number
  current: DomainRelease | null
}

export const releasesQuery = (page: number, pageSize: number) =>
  queryOptions({
    queryKey: domainKeys.releases(page, pageSize),
    queryFn: () => http.get<ReleasesData>('/domain-releases', { query: { page, page_size: pageSize } }),
    placeholderData: keepPreviousData,
  })

export const healthQuery = () =>
  queryOptions({
    queryKey: domainKeys.health,
    queryFn: () => http.get<DomainHealthOverview>('/domain-health'),
  })

export const domainApi = {
  create: (body: DomainInput) => http.post<DomainEntry>('/domains', body),
  update: (id: number, body: DomainInput) => http.patch<DomainEntry>(`/domains/${id}`, body),
  remove: (id: number) => http.delete<null>(`/domains/${id}`),
  generateCert: (body: GenCertInput) => http.post<GeneratedCert>('/domain-certificates', body),
  inspectCert: (certPEM: string) => http.post<CertInfo>('/domain-certificates/inspect', { cert_pem: certPEM }),
  uploadCert: (id: number, certPEM: string) =>
    http.post<{ domain: DomainEntry; cert: CertInfo }>(`/domains/${id}/certificate`, { cert_pem: certPEM }),
  previewRelease: (fromVersion?: number) =>
    http.get<ReleasePreview>('/domain-releases/preview', { query: { from_version: fromVersion } }),
  publishRelease: (body: { payload: ReleasePayload; signature: string; note: string }) =>
    http.post<DomainRelease>('/domain-releases', body),
}

/** 证书到期剩余天数的人话。 */
export function certDaysLabel(state: CertState, days: number | null): string {
  if (state === 'unknown' || days === null) return '未录入到期日'
  if (state === 'expired') return `已过期 ${Math.abs(days)} 天`
  return `还有 ${days} 天到期`
}
