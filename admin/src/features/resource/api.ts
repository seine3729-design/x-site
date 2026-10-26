import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { Page } from '@/features/content/api'

/**
 * 后台「资源机 FTP」的接口（docs/06 第 31 节、docs/07 第 2 节，S1-6，契约 service/api/openapi.yaml 的 AdminFTP* schema）。
 * 账号密码不入库也不经过后台：这里只管登记连接信息，账号密码由技术按 cred_env 的两个环境变量名配在 worker 上。
 */

export type TLSMode = 'none' | 'explicit' | 'implicit'
export type FTPStatus = 'active' | 'inactive'

export const TLS_LABELS: Record<TLSMode, string> = {
  none: '不加密',
  explicit: '显式 TLS（FTPS）',
  implicit: '隐式 TLS',
}

export const FTP_STATUS_LABELS: Record<FTPStatus, string> = {
  active: '启用',
  inactive: '停用',
}

/** 测连与打开目录的四个步骤（后端按顺序执行，失败即停）。 */
export const PROBE_STEP_LABELS = {
  resolve: '解析地址',
  connect: '建立连接',
  login: '登录',
  list: '读取目录',
} as const

export type ProbeStepName = keyof typeof PROBE_STEP_LABELS

export const ENTRY_TYPE_LABELS = {
  dir: '目录',
  file: '文件',
  link: '链接',
} as const

export type EntryType = keyof typeof ENTRY_TYPE_LABELS

/** worker 读取账号密码的两个环境变量名（不是账号密码本身）。 */
export type CredEnv = { user: string; password: string }

export type FTPServer = {
  id: number
  name: string
  host: string
  port: number
  tls: TLSMode
  passive: boolean
  cred_ref: string
  cred_env: CredEnv
  status: FTPStatus
  created_at: string
  updated_at: string
}

/** 新建与整体保存：未传的可选字段按新建默认值保存；cred_ref 留空时新建自动生成、保存保持原值。 */
export type FTPServerInput = {
  name: string
  host: string
  port?: number
  tls?: TLSMode
  passive?: boolean
  cred_ref?: string
  status?: FTPStatus
}

export type ProbeStep = { name: ProbeStepName; ok: boolean; ms: number; message: string }

export type FTPEntry = {
  name: string
  type: EntryType
  size: number
  modified_at: string | null
  target: string
}

export type ProbeResult = {
  ok: boolean
  elapsed_ms: number
  steps: ProbeStep[]
  path: string
  entries: FTPEntry[]
  truncated: boolean
}

export type FTPListParams = {
  q?: string
  status?: FTPStatus
  page?: number
  page_size?: number
}

/**
 * 测连与打开目录由 worker 执行，API 最多等 25 秒（jobs/ftp.go 的 ftpProbeWait），
 * 比 http.ts 的默认 15 秒长，必须单独放宽，否则前端先超时、用户看到的是「网络异常」而不是真正的结果。
 */
const PROBE_TIMEOUT_MS = 30_000

export const ftpKeys = {
  all: ['ftp'] as const,
  list: (p: FTPListParams) => ['ftp', 'list', p] as const,
}

export const ftpListQuery = (p: FTPListParams) =>
  queryOptions({
    queryKey: ftpKeys.list(p),
    queryFn: () => http.get<Page<FTPServer>>('/ftp', { query: p }),
    placeholderData: keepPreviousData,
  })

export const ftpApi = {
  create: (input: FTPServerInput) => http.post<FTPServer>('/ftp', input),
  update: (id: number, input: FTPServerInput) => http.patch<FTPServer>(`/ftp/${id}`, input),
  remove: (id: number) => http.delete<null>(`/ftp/${id}`),
  test: (id: number) => http.post<ProbeResult>(`/ftp/${id}/test`, undefined, { timeoutMs: PROBE_TIMEOUT_MS }),
  listDir: (id: number, path: string) =>
    http.post<ProbeResult>(`/ftp/${id}/list`, { path }, { timeoutMs: PROBE_TIMEOUT_MS }),
}

/** 文件大小。目录与 0 字节都显示 —。 */
export function formatSize(bytes: number, type: EntryType): string {
  if (type === 'dir' || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${i === 0 ? value : value.toFixed(value >= 100 ? 0 : 1)} ${units[i]}`
}

/**
 * 拼出子目录的绝对路径。每次打开目录都是一条新的 FTP 连接，相对路径只相对登录目录，
 * 所以进子目录必须用服务器返回的当前路径拼绝对路径。
 */
export function joinPath(current: string, name: string): string {
  const base = current.replace(/\/+$/, '')
  return `${base}/${name}`
}

/** 上一级目录；已在根目录时返回 null。 */
export function parentPath(current: string): string | null {
  const trimmed = current.replace(/\/+$/, '')
  if (trimmed === '' || trimmed === '/') return null
  const cut = trimmed.lastIndexOf('/')
  if (cut < 0) return null
  return cut === 0 ? '/' : trimmed.slice(0, cut)
}
