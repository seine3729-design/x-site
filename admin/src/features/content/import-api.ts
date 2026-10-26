import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { http, postForm } from '@/lib/http'
import type { Page } from './api'

/**
 * 后台「批量导入」的接口（docs/06 第 8 节，A4-2 / S4-6；契约 service/api/openapi.yaml 的 AdminImport* schema）。
 *
 * CSV 只带剧级信息（剧名、别名、简介、标签），不含分集 —— 分集靠资源机扫描或开放平台推送带入。
 */

/** 一行的处置结果。 */
export type ImportAction = 'create' | 'update' | 'skip' | 'reject'

export type ImportActionInfo = {
  action: ImportAction
  title: string
  desc: string
}

export type ImportRow = {
  /** 在文件里的行号（含表头，从 1 开始）。 */
  line: number
  title: string
  aliases: string[]
  summary: string
  tags: string[]
  action: ImportAction
  action_title: string
  /** update 时是已存在的那部剧；create 时预览为 0、正式导入后是新建的编号。 */
  drama_id: number
  /** 按什么判定为已存在的（剧名 / 别名）。 */
  matched_by: string
  /** 匹配到的剧在回收站里 —— 它仍算已存在，不会重建一部同名的。 */
  in_trash: boolean
  /** 这一行会自动新建的标签名。 */
  new_tags: string[]
  /** 跳过或被拒的原因。 */
  reason: string
}

export type ImportStats = {
  rows_total: number
  dramas_created: number
  dramas_updated: number
  /** CSV 场景恒为 0：CSV 的列里没有分集信息。 */
  episodes_created: number
  episodes_updated: number
  skipped: number
  rejected: number
  tags_created: number
}

export type ImportResult = {
  dry_run: boolean
  stats: ImportStats
  rows: ImportRow[]
  actions: ImportActionInfo[]
  /** 正式导入后的记录编号；预览时为 0。 */
  log_id: number
  /** 说明 CSV 能带什么、不能带什么，由后端给出。 */
  note: string
}

export type ImportLog = {
  id: number
  source: string
  file_name: string
  rows_total: number
  dramas_created: number
  dramas_updated: number
  episodes_created: number
  episodes_updated: number
  skipped: number
  rejected: number
  tags_created: number
  admin_id: number
  admin_name: string
  created_at: string
}

export type ImportLogParams = {
  /** 带时区的 RFC3339（按浏览器时区换算），让「今天」是用户的今天。 */
  from?: string
  to?: string
  page?: number
  page_size?: number
}

export const importKeys = {
  all: ['content', 'import'] as const,
  logs: (p: ImportLogParams) => ['content', 'import', 'logs', p] as const,
}

export const importLogsQuery = (p: ImportLogParams) =>
  queryOptions({
    queryKey: importKeys.logs(p),
    queryFn: () => http.get<Page<ImportLog>>('/import/logs', { query: p }),
    placeholderData: keepPreviousData,
  })

/**
 * 上传 CSV 做预览或正式导入。
 *
 * 用 multipart 直接提交而不是走上传会话：CSV 是几十到几百 KB 的小文件，
 * 为它走「建会话 → 传内容 → 完成」三步没有收益。
 */
function postCSV(path: string, file: File, source: string): Promise<ImportResult> {
  const form = new FormData()
  form.append('file', file)
  form.append('source', source)
  return postForm<ImportResult>(path, form)
}

export const importApi = {
  preview: (file: File, source: string) => postCSV('/import/preview', file, source),
  run: (file: File, source: string) => postCSV('/import/run', file, source),
}

const ACTION_VARIANTS: Record<ImportAction, 'default' | 'outline' | 'secondary' | 'destructive'> = {
  create: 'default',
  update: 'outline',
  skip: 'secondary',
  reject: 'destructive',
}

export function actionVariant(a: ImportAction) {
  return ACTION_VARIANTS[a] ?? 'outline'
}

/** CSV 模板，给运营下载用。带 BOM 让 Excel 正确识别 UTF-8。 */
export const CSV_TEMPLATE =
  '\ufeff剧名,别名,简介,标签\n' +
  '霸道总裁爱上我,总裁来了|霸总,一个关于总裁的故事,都市、言情\n' +
  '重生之我是赘婿,,穿越回古代的故事,古装、穿越\n'
