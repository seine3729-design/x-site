import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/** 存储源接口（docs/07 第 2 节「存储源」，docs/12 第 2、4.4、8.1 节）。凭据只写不读，列表只返回是否已配置与 secret 末 4 位。 */

export type StorageType = 's3' | 'r2' | 'custom'
export type StorageSubtype = '' | 's3_compatible' | 'http_origin'
export type StoragePurpose = 'video' | 'image' | 'both'
export type StorageRole = 'primary' | 'mirror' | 'readonly'
export type StorageStatus = 'active' | 'draining' | 'disabled'

export type StorageProvider = {
  id: number
  name: string
  type: StorageType
  subtype: StorageSubtype
  purpose: StoragePurpose
  role: StorageRole
  endpoint: string
  region: string
  bucket: string
  account_id: string
  path_style: boolean
  path_prefix: string
  has_write_cred: boolean
  has_read_cred: boolean
  cred_tail: string
  read_cred_tail: string
  max_presign_ttl: number
  status: StorageStatus
  created_at: string
  updated_at: string
}

export type StorageCredential = { access_key_id: string; secret_access_key: string }

export type StorageProviderInput = {
  name: string
  type: StorageType
  subtype: StorageSubtype
  purpose: StoragePurpose
  role: StorageRole
  endpoint: string
  region: string
  bucket: string
  account_id: string
  path_style: boolean
  path_prefix: string
  max_presign_ttl: number
  status: StorageStatus
  write_cred?: StorageCredential
  read_cred?: StorageCredential
}

export type ProbeStepName = 'resolve' | 'put' | 'get' | 'presign' | 'delete' | 'head'
export type ProbeStep = { name: ProbeStepName; ok: boolean; ms: number; message: string }
export type ProbeResult = { ok: boolean; elapsed_ms: number; steps: ProbeStep[] }

type List<T> = { list: T[]; total: number; page: number; page_size: number }

export const storageKeys = {
  all: ['storage'] as const,
  providers: ['storage', 'providers'] as const,
}

/** 存储源很少，一次拉全部（最多 100 条）在前端展示。 */
export const providersQuery = queryOptions({
  queryKey: storageKeys.providers,
  queryFn: () => http.get<List<StorageProvider>>('/storage/providers', { query: { page_size: 100 } }).then((r) => r.list),
})

export const storageApi = {
  create: (body: StorageProviderInput) => http.post<StorageProvider>('/storage/providers', body),
  update: (id: number, body: StorageProviderInput) => http.patch<StorageProvider>(`/storage/providers/${id}`, body),
  remove: (id: number) => http.delete<void>(`/storage/providers/${id}`),
  test: (id: number) => http.post<ProbeResult>(`/storage/providers/${id}/test`),
}

export const TYPE_LABELS: Record<StorageType, string> = {
  s3: 'AWS S3 / 兼容',
  r2: 'Cloudflare R2',
  custom: '自定义',
}
export const SUBTYPE_LABELS: Record<Exclude<StorageSubtype, ''>, string> = {
  s3_compatible: 'S3 兼容',
  http_origin: 'HTTP 源',
}
export const PURPOSE_LABELS: Record<StoragePurpose, string> = {
  video: '视频',
  image: '图片',
  both: '视频 + 图片',
}
export const ROLE_LABELS: Record<StorageRole, string> = {
  primary: '主存储',
  mirror: '镜像',
  readonly: '只读',
}
export const STATUS_LABELS: Record<StorageStatus, string> = {
  active: '启用',
  draining: '停写（只读）',
  disabled: '停用',
}
export const PROBE_STEP_LABELS: Record<ProbeStepName, string> = {
  resolve: '解析端点',
  put: '写入',
  get: '读取校验',
  presign: '预签名下载',
  delete: '删除',
  head: 'HTTP 可达性',
}

// ---------- 线路与地区规则（S1-6b） ----------

export type LineKind = 'video' | 'image'
export type AccessMode = 'presign' | 'cdn' | 'public'
export type SignType = 's3_presign' | 'hmac_token' | 'cdn_vendor' | 'none'
export type RegionScope = 'rules' | 'global'
export type VideoProcess = 'standard' | 'text_to_image'
export type LineStatus = 'active' | 'watch' | 'disabled'
export type RuleEffect = 'include' | 'exclude'

export type StorageRule = {
  id: number
  line_id: number
  country: string
  province: string
  isp: string
  asn: string
  effect: RuleEffect
  boost: number
}

export type StorageLine = {
  id: number
  provider_id: number
  kind: LineKind
  name: string
  base_url: string
  access_mode: AccessMode
  sign_type: SignType
  sign_key_ref: string
  token_ttl: number
  priority: number
  weight: number
  region_scope: RegionScope
  video_process: VideoProcess
  probe_path: string
  status: LineStatus
  note: string
  rules: StorageRule[]
  created_at: string
  updated_at: string
}

export type LineInput = {
  provider_id?: number
  kind: LineKind
  name: string
  base_url: string
  access_mode: AccessMode
  sign_type: SignType
  sign_key_ref: string
  token_ttl: number
  priority: number
  weight: number
  region_scope: RegionScope
  video_process: VideoProcess
  probe_path: string
  status: LineStatus
  note: string
}

export type RuleInput = {
  country: string
  province: string
  isp: string
  asn: string
  effect: RuleEffect
  boost: number
}

export type LineProbeResult = {
  ok: boolean
  elapsed_ms: number
  url: string
  http_status: number
  content_type: string
  cors: boolean
  cors_origin: string
  message: string
}

export const lineKeys = {
  all: ['storage', 'lines'] as const,
  byProvider: (providerId: number) => ['storage', 'lines', providerId] as const,
}

export const linesQuery = (providerId: number) =>
  queryOptions({
    queryKey: lineKeys.byProvider(providerId),
    queryFn: () => http.get<{ list: StorageLine[]; total: number }>('/storage/lines', { query: { provider_id: providerId } }).then((r) => r.list),
    enabled: providerId > 0,
  })

export const lineApi = {
  create: (body: LineInput) => http.post<StorageLine>('/storage/lines', body),
  update: (id: number, body: LineInput) => http.patch<StorageLine>(`/storage/lines/${id}`, body),
  remove: (id: number) => http.delete<void>(`/storage/lines/${id}`),
  setRules: (id: number, rules: RuleInput[]) => http.put<{ rules: StorageRule[] }>(`/storage/lines/${id}/rules`, { rules }),
  test: (id: number) => http.post<LineProbeResult>(`/storage/lines/${id}/test`),
}

export const LINE_KIND_LABELS: Record<LineKind, string> = { video: '视频', image: '图片' }
export const ACCESS_MODE_LABELS: Record<AccessMode, string> = { presign: '预签名直连', cdn: 'CDN 域名', public: '公开读' }
export const SIGN_TYPE_LABELS: Record<SignType, string> = {
  s3_presign: 'S3 预签名',
  hmac_token: 'HMAC 令牌',
  cdn_vendor: 'CDN 厂商鉴权',
  none: '不签名',
}
export const REGION_SCOPE_LABELS: Record<RegionScope, string> = { rules: '按地区规则', global: '全局兜底' }
export const LINE_STATUS_LABELS: Record<LineStatus, string> = { active: '启用', watch: '观察中', disabled: '停用' }
export const EFFECT_LABELS: Record<RuleEffect, string> = { include: '包含', exclude: '排除' }

// ---------- 存储全局设置（S1-6d 设置部分） ----------

/** 交付自检的一条问题：主存储没挂线路时，上传成功的资源前台仍然取不到。 */
export type DeliveryIssue = {
  kind: 'video' | 'image'
  code: 'no_primary' | 'no_line_on_primary' | 'no_line_at_all'
  message: string
}

export type StorageSettings = {
  candidate_count: number
  default_ttl: number
  smart_routing: boolean
  primary_video_provider_id: number
  primary_image_provider_id: number
  replicate_on_upload: boolean
  updated_at: string
  /** 交付自检（GET 才返回）。 */
  delivery?: { ok: boolean; issues: DeliveryIssue[] }
}

export type StorageSettingsInput = {
  candidate_count: number
  default_ttl: number
  smart_routing: boolean
  primary_video_provider_id: number
  primary_image_provider_id: number
  replicate_on_upload: boolean
}

export const settingsQuery = queryOptions({
  queryKey: ['storage', 'settings'],
  queryFn: () => http.get<StorageSettings>('/storage/settings'),
})

export const settingsApi = {
  update: (body: StorageSettingsInput) => http.put<StorageSettings>('/storage/settings', body),
}

// ---------- 选线预览（S1-6d） ----------

export type EvalStatus = 'candidate' | 'overflow' | 'excluded' | 'unmatched' | 'unhealthy' | 'disabled'

export type ScoreDetail = {
  precision: number
  boost: number
  priority: number
  health: number
  watch: number
  total: number
}

export type LineEval = {
  line_id: number
  name: string
  status: EvalStatus
  rank: number
  recommended: boolean
  region_tag: string
  health_score: number
  detail: ScoreDetail
  /** 健康分来源分解（探测分 / 客户端分 / 是否本地区数据） */
  health?: HealthBreakdown
}

export type PreviewResult = {
  region: string
  country: string
  province: string
  isp: string
  asn: string
  candidate_count: number
  lines: LineEval[]
}

export type PreviewParams = {
  kind: LineKind
  ip?: string
  country?: string
  province?: string
  isp?: string
  asn?: string
}

export const previewApi = {
  run: (p: PreviewParams) => http.get<PreviewResult>('/storage/preview', { query: p }),
}

export const EVAL_STATUS_LABELS: Record<EvalStatus, string> = {
  candidate: '候选',
  overflow: '超出候选数',
  excluded: '被排除',
  unmatched: '未命中地区',
  unhealthy: '健康分过低',
  disabled: '已停用',
}

// ---------- 线路健康看板（S1-6d：服务端探测 40% + 客户端上报 60%） ----------

/** 最近窗口内的客户端上报指标（全部地区汇总）。 */
export type ClientQuality = {
  samples: number
  first_frame_ms: number
  fail_rate_pct: number
  stalls: number
  score: number
  /** 样本是否足够、已参与健康分 */
  counted: boolean
}

export type HealthLine = {
  line_id: number
  name: string
  provider_id: number
  kind: LineKind
  status: LineStatus
  base_url: string
  /** 生效健康分（融合后） */
  score: number
  /** 是否有任一来源的数据 */
  has_data: boolean
  probe_score: number
  has_probe: boolean
  probe_ok: boolean
  http_status: number
  elapsed_ms: number
  content_type: string
  cors: boolean
  message: string
  checked_at: string | null
  client: ClientQuality
}

export type HealthOverview = {
  probe_enabled: boolean
  /** 客户端上报的统计窗口（分钟） */
  window_min: number
  probe_weight: number
  client_weight: number
  lines: HealthLine[]
}

/** 健康分来源分解（选线预览每条线路附带）。 */
export type HealthBreakdown = {
  final: number
  probe_score: number
  has_probe: boolean
  client_score: number
  has_client: boolean
  quality: {
    samples: number
    first_frame_sum: number
    first_frame_n: number
    errors: number
    stalls: number
  }
  /** 客户端数据是否取自本地区（false 表示回退到全部地区汇总） */
  by_region: boolean
}

/** 健康看板由 worker 周期探测写入 Valkey，这里每 30 秒自动刷新。 */
export const healthQuery = queryOptions({
  queryKey: ['storage', 'health'],
  queryFn: () => http.get<HealthOverview>('/storage/health'),
  refetchInterval: 30_000,
})

// ---------- 存储运维：复制 / 迁移 / 接管 / 清理（S1-6f） ----------

export type ReplicateScope = 'all' | 'drama' | 'provider'

/** 批量入队的结果（实际搬运由后台任务执行）。 */
export type EnqueueResult = {
  objects: number
  /** 达到单次上限（50000）被截断，可再次触发继续 */
  capped: boolean
  scanned: number
}

export type CleanupResult = {
  scanned: number
  deleted: number
  files: number
  bytes: number
  /** 跳过的对象数（重新被引用，或文件删除失败留到下轮） */
  kept: number
  dry_run: boolean
}

export type OrphanStats = {
  total: number
  expired: number
  bytes: number
  retention_hours: number
}

/** 孤立产物统计。清理操作区会用它提示「会删掉多少」。 */
export const orphanStatsQuery = queryOptions({
  queryKey: ['storage', 'orphans'],
  queryFn: () => http.get<OrphanStats>('/storage/orphans'),
})

export const manageApi = {
  replicate: (body: { scope: ReplicateScope; drama_id?: number; provider_id?: number }) => http.post<EnqueueResult>('/storage/replicate', body),
  migrate: (body: { from_provider_id: number; to_provider_id: number }) => http.post<EnqueueResult>('/storage/migrate', body),
  adopt: (body: { provider_id: number }) => http.post<EnqueueResult>('/storage/adopt', body),
  cleanup: (body: { dry_run: boolean }) => http.post<CleanupResult>('/storage/cleanup', body),
}

export const REPLICATE_SCOPE_LABELS: Record<ReplicateScope, string> = {
  all: '全部内容',
  drama: '指定剧集',
  provider: '指定存储源上的内容',
}

/** 把字节数显示成人能读的单位。 */
export function formatBytes(n: number): string {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / 1024 ** i
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}
