import { queryOptions } from '@tanstack/react-query'
import { API_BASE, http } from '@/lib/http'

/**
 * 后台「配置导出导入」的接口（docs/06 第 29 节，S4-4；契约 service/api/openapi.yaml 的 AdminConfig* schema）。
 */

/** 备份正文里各类数据的条数。菜单、静态页、区块的后端还没实现（A3-1 / A3-2），恒为 0。 */
export type ConfigCounts = {
  menus: number
  static_pages: number
  blocks: number
  taxonomies: number
  terms: number
  ads: number
}

export type BackupReason = 'manual' | 'before_import'

export const BACKUP_REASON_LABELS: Record<BackupReason, string> = {
  manual: '手动导出',
  before_import: '导入前自动存档',
}

export type BackupSummary = {
  id: number
  format: number
  reason: BackupReason
  admin_id: number
  note: string
  counts: ConfigCounts
  signed: boolean
  created_at: string
}

export type ImportResult = {
  format: number
  signed: boolean
  backup_id: number
  applied: ConfigCounts
  /** 库里有、备份里没有、因此保留未删的维度与取值数量（删取值会连带删掉剧集的维度归属）。 */
  taxonomies_kept: number
  terms_kept: number
  kept_names: string[]
  /** 广告做了整表替换，原有的曝光与点击计数归零。 */
  ads_replaced: boolean
}

/** 备份文件的结构，导入时原样回传。 */
export type BackupFile = {
  format: number
  exported_at: string
  site: string
  signature: string
  data: unknown
}

export const configKeys = {
  backups: ['platform', 'config', 'backups'] as const,
}

export const backupsQuery = queryOptions({
  queryKey: configKeys.backups,
  queryFn: () => http.get<{ list: BackupSummary[]; total: number }>('/config/backups'),
})

export const configApi = {
  /** 导出当前配置（同时在库里留一条存档）。 */
  exportConfig: () => http.get<BackupFile>('/config/export'),
  /** 导入一份备份文件，覆盖现有配置。 */
  importConfig: (file: BackupFile) => http.post<ImportResult>('/config/import', file),
  /** 取回一份存档的内容（用于下载保存）。 */
  getBackup: (id: number) => http.get<BackupFile>(`/config/backups/${id}`),
  /** 从库里的存档回滚（不验签，数据没离开过系统）。 */
  restore: (id: number) => http.post<ImportResult>(`/config/backups/${id}/restore`),
}

/** 导出地址：直接用 <a download> 让浏览器下载，不经过 fetch。 */
export const exportURL = `${API_BASE}/config/export`

/** 把备份对象存成本地文件。 */
export function downloadBackup(file: BackupFile, name: string) {
  const blob = new Blob([JSON.stringify(file, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}

/** 各类数量的中文摘要，如「维度 2 · 取值 5 · 广告 1」。未实现的三类有内容时才显示。 */
export function countsSummary(c: ConfigCounts): string {
  const parts = [`维度 ${c.taxonomies}`, `取值 ${c.terms}`, `广告 ${c.ads}`]
  if (c.menus > 0) parts.unshift(`菜单 ${c.menus}`)
  if (c.static_pages > 0) parts.unshift(`静态页 ${c.static_pages}`)
  if (c.blocks > 0) parts.unshift(`区块 ${c.blocks}`)
  return parts.join(' · ')
}
