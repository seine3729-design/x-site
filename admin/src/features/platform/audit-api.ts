import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「审计日志」的接口（docs/06 第 34 节，A4-3 / S4-7；契约 service/api/openapi.yaml 的 AdminAudit* schema）。
 */

export type AuditItem = {
  id: number
  admin_id: number
  username: string
  action: string
  target_type: string
  target_id: string
  ip_prefix: string
  request_id: string
  created_at: string
  /** 是否在哈希链上。启用哈希链之前的历史记录为 false。 */
  chained: boolean
  has_diff: boolean
}

export type AuditDetail = AuditItem & {
  before: unknown
  after: unknown
  prev_hash: string
  hash: string
}

export type AuditFacet = { value: string; count: number }
export type AuditAdminFacet = { id: number; username: string; count: number }

export type AuditFacets = {
  actions: AuditFacet[]
  target_types: AuditFacet[]
  admins: AuditAdminFacet[]
}

export type AuditBreak = {
  id: number
  reason: string
  action: string
  at: string
}

export type AuditVerifyResult = {
  ok: boolean
  checked: number
  chained: number
  /** 启用哈希链之前的记录数，不参与校验。 */
  unchained: number
  first_at: string
  last_at: string
  broken: AuditBreak | null
}

export type AuditListParams = {
  q?: string
  action?: string
  target_type?: string
  admin_id?: number
  from?: string
  to?: string
  page?: number
  page_size?: number
}

export const auditKeys = {
  all: ['platform', 'audit'] as const,
  list: (p: AuditListParams) => ['platform', 'audit', 'list', p] as const,
  facets: ['platform', 'audit', 'facets'] as const,
  verify: ['platform', 'audit', 'verify'] as const,
}

export const auditListQuery = (p: AuditListParams) =>
  queryOptions({
    queryKey: auditKeys.list(p),
    queryFn: () => http.get<Page<AuditItem>>('/audit', { query: p }),
    placeholderData: keepPreviousData,
  })

export const auditFacetsQuery = queryOptions({
  queryKey: auditKeys.facets,
  queryFn: () => http.get<AuditFacets>('/audit/facets'),
  staleTime: 5 * 60 * 1000,
})

export const auditApi = {
  detail: (id: number) => http.get<AuditDetail>(`/audit/${id}`),
  /** 校验哈希链。扫全量可能要几秒，单独放宽超时。 */
  verify: () => http.get<AuditVerifyResult>('/audit/verify', { timeoutMs: 60_000 }),
}

/**
 * 把日期输入框的值（YYYY-MM-DD，浏览器本地时区）转成带时区的 RFC3339。
 *
 * 必须带时区：后端收到裸日期会按服务端时区（UTC）解释，用户选「今天」会差几个小时。
 * end 为真时取次日零点，这样「到 10-24」包含 10-24 一整天（后端按 < to 过滤）。
 */
export function dayBound(value: string, end = false): string | undefined {
  if (!value) return undefined
  const d = new Date(`${value}T00:00:00`)
  if (Number.isNaN(d.getTime())) return undefined
  if (end) d.setDate(d.getDate() + 1)
  return d.toISOString()
}

/** 动作编码 → 中文。按「对象.操作」拆开翻译，未收录的原样显示（动作有 60 多种且会继续增加）。 */
const ACTION_VERBS: Record<string, string> = {
  create: '新建',
  update: '修改',
  delete: '删除',
  restore: '还原',
  merge: '合并',
  reorder: '排序',
  import: '导入',
  export: '导出',
  purge: '清除',
  test: '测试连接',
  list: '查看目录',
  transcode: '提交转封装',
  triage: '分诊',
  set_permissions: '设置权限',
  reset_password: '重置密码',
  revoke_sessions: '强制下线',
  recompute: '重新计算',
  replicate: '补副本',
  migrate: '迁移',
  adopt: '接管',
  cleanup: '清理',
}

const TARGET_LABELS: Record<string, string> = {
  admin: '管理员',
  role: '角色',
  admin_menu: '后台菜单',
  drama: '剧集',
  episode: '分集',
  tag: '标签',
  taxonomy: '筛选维度',
  term: '维度取值',
  topic: '专题',
  page: '静态页',
  block: '页面区块',
  ad: '广告',
  announcement: '公告',
  rank: '排行',
  hotsearch: '热搜词',
  storage_provider: '存储源',
  storage_line: '线路',
  storage_settings: '存储设置',
  ftp_server: '资源机',
  cache: '缓存',
  config: '配置备份',
  site_config: '站点设置',
  media: '媒体',
  fb_feedback: '反馈',
  fb_project: '内测项目',
  fb_post: '项目动态',
  fb_api_key: '开放接口密钥',
}

/** 动作的中文说明，如 taxonomy.create → 新建筛选维度。 */
export function actionLabel(action: string): string {
  // 中间件兜底写的是「方法 + 路由模板」，原样显示
  if (action.includes(' ')) return action
  const cut = action.lastIndexOf('.')
  if (cut < 0) return action
  const target = action.slice(0, cut)
  const verb = action.slice(cut + 1)
  const verbText = ACTION_VERBS[verb]
  const targetText = TARGET_LABELS[target]
  if (!verbText && !targetText) return action
  return `${verbText ?? verb}${targetText ?? target}`
}

/** 对象类型的中文名。 */
export function targetLabel(targetType: string): string {
  return TARGET_LABELS[targetType] ?? targetType
}
