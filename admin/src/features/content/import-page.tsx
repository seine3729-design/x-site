import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Download, FileUp, Info, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DatePicker } from '@/components/date-picker'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { Pager } from './components/pager'
import {
  actionVariant,
  CSV_TEMPLATE,
  importApi,
  importKeys,
  importLogsQuery,
  type ImportLog,
  type ImportResult,
  type ImportRow,
} from './import-api'
import { DEFAULT_PAGE_SIZE } from './search'

/**
 * 批量导入（docs/06 第 8 节，A4-2 / S4-6）：CSV 批量建剧，先预览再正式导入。
 *
 * CSV 只带剧级信息（剧名、别名、简介、标签），不含分集。
 */
export function ImportPage() {
  const can = useCan()
  const visible = can('import.view')
  const qc = useQueryClient()
  const search = useSearch({ from: '/_authenticated/import' })
  const navigate = useNavigate({ from: '/import' })
  const [file, setFile] = useState<File | null>(null)
  const [source, setSource] = useState('')
  const [preview, setPreview] = useState<ImportResult | null>(null)
  const [confirming, setConfirming] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const logs = useQuery({
    ...importLogsQuery({
      from: search.from ? dayStart(search.from) : undefined,
      to: search.to ? dayEnd(search.to) : undefined,
      page: search.page ?? 1,
      page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
    }),
    enabled: visible,
  })

  const previewMut = useMutation({
    mutationFn: () => importApi.preview(file!, source),
    onSuccess: (res) => {
      setPreview(res)
      toast.success(`预览完成，共 ${res.stats.rows_total} 行`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const runMut = useMutation({
    mutationFn: () => importApi.run(file!, source),
    onSuccess: (res) => {
      setConfirming(false)
      setPreview(res)
      void qc.invalidateQueries({ queryKey: importKeys.all })
      toast.success(
        `导入完成：新增 ${res.stats.dramas_created} 部、更新 ${res.stats.dramas_updated} 部` +
          (res.stats.rejected > 0 ? `，${res.stats.rejected} 行被拒` : '')
      )
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='批量导入' />
  const editable = can('import.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const busy = previewMut.isPending || runMut.isPending

  const pick = (f: File | null) => {
    setFile(f)
    // 换了文件，上一份预览就不再对应当前文件了，必须清掉 ——
    // 否则运营可能看着旧预览点「正式导入」，导进去的却是新文件
    setPreview(null)
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='批量导入'
        description='用 CSV 批量建剧。先预览看清楚每一行会发生什么，再正式导入。'
        readonly={!editable}
      />

      <section className='flex flex-col gap-3 rounded-md border p-4'>
        <div className='flex items-start gap-2 text-xs text-muted-foreground'>
          <Info className='mt-0.5 size-4 shrink-0' aria-hidden />
          <span>
            {preview?.note ??
              'CSV 只带剧级信息（剧名、别名、简介、标签），不含分集。新建的剧是草稿，要自己去上架。'}
          </span>
        </div>

        <div className='flex flex-col gap-3 sm:flex-row sm:items-end'>
          <div className='flex flex-1 flex-col gap-1'>
            <label htmlFor='import-file' className='text-sm font-medium'>
              CSV 文件
            </label>
            <Input
              id='import-file'
              ref={fileInput}
              type='file'
              accept='.csv,text/csv'
              className='h-10 cursor-pointer'
              disabled={!editable || busy}
              onChange={(e) => pick(e.target.files?.[0] ?? null)}
            />
            <span className='text-xs text-muted-foreground'>
              第一行要是表头（剧名、别名、简介、标签）。一次最多 2000 行、1 MB。
            </span>
          </div>
          <div className='flex flex-col gap-1 sm:w-56'>
            <label htmlFor='import-source' className='text-sm font-medium'>
              来源标识
            </label>
            <Input
              id='import-source'
              className='h-10'
              maxLength={60}
              placeholder='选填，如 2026-10-采集批次1'
              value={source}
              disabled={!editable || busy}
              onChange={(e) => setSource(e.target.value)}
            />
            <span className='text-xs text-muted-foreground'>记在剧上，便于以后追溯这批是哪来的。</span>
          </div>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <WithReason reason={readonlyReason}>
            <Button
              className='h-9'
              variant='secondary'
              disabled={!editable || !file || busy}
              onClick={() => previewMut.mutate()}
            >
              <FileUp className='size-4' aria-hidden /> {previewMut.isPending ? '预览中…' : '预览'}
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button className='h-9' disabled={!editable || !file || !preview || busy} onClick={() => setConfirming(true)}>
              <Upload className='size-4' aria-hidden /> 正式导入
            </Button>
          </WithReason>
          {!preview && file && (
            <span className='text-xs text-muted-foreground'>先预览一遍才能导入。</span>
          )}
          <Button className='ms-auto h-9' variant='ghost' onClick={downloadTemplate}>
            <Download className='size-4' aria-hidden /> 下载模板
          </Button>
        </div>
      </section>

      {preview && <PreviewPanel result={preview} />}

      <section className='flex flex-col gap-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <h3 className='text-sm font-medium'>导入记录</h3>
          <span className='text-xs text-muted-foreground'>只记正式导入，预览不留痕。</span>
          <div className='ms-auto flex items-center gap-2'>
            <label htmlFor='import-from' className='text-sm whitespace-nowrap text-muted-foreground'>
              从
            </label>
            <DatePicker
              id='import-from'
              className='h-9 w-36'
              placeholder='开始日期'
              max={search.to || undefined}
              value={search.from ?? ''}
              onChange={(v) => void navigate({ search: (p) => ({ ...p, from: v || undefined, page: undefined }) })}
            />
            <label htmlFor='import-to' className='text-sm whitespace-nowrap text-muted-foreground'>
              到
            </label>
            <DatePicker
              id='import-to'
              className='h-9 w-36'
              placeholder='结束日期'
              min={search.from || undefined}
              value={search.to ?? ''}
              onChange={(v) => void navigate({ search: (p) => ({ ...p, to: v || undefined, page: undefined }) })}
            />
            {(search.from || search.to) && (
              <Button variant='ghost' className='h-9' onClick={() => void navigate({ search: {} })}>
                清除
              </Button>
            )}
          </div>
        </div>

        {logs.isError ? (
          <LoadError onRetry={() => void logs.refetch()} />
        ) : (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>时间</TableHead>
                  <TableHead>文件</TableHead>
                  <TableHead>来源标识</TableHead>
                  <TableHead>总行数</TableHead>
                  <TableHead>新增 / 更新</TableHead>
                  <TableHead>跳过 / 被拒</TableHead>
                  <TableHead>新建标签</TableHead>
                  <TableHead className='pr-4'>操作人</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.isPending ? (
                  <TableRow>
                    <TableCell colSpan={8} className='h-20 text-center text-muted-foreground'>
                      加载中…
                    </TableCell>
                  </TableRow>
                ) : logs.data.list.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className='h-20 text-center text-muted-foreground'>
                      {search.from || search.to ? '这段时间没有导入记录' : '还没有导入过'}
                    </TableCell>
                  </TableRow>
                ) : (
                  logs.data.list.map((l) => <LogRow key={l.id} log={l} />)
                )}
              </TableBody>
            </Table>
          </div>
        )}

        {logs.data && (
          <Pager
            page={logs.data.page}
            pageSize={logs.data.page_size}
            total={logs.data.total}
            onChange={(page, page_size) =>
              void navigate({
                search: (p) => ({
                  ...p,
                  page: page > 1 ? page : undefined,
                  page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size,
                }),
              })
            }
          />
        )}
      </section>

      {confirming && preview && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirming(false)}
          title='正式导入？'
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                会新增 <span className='font-medium'>{preview.stats.dramas_created}</span> 部剧、更新{' '}
                <span className='font-medium'>{preview.stats.dramas_updated}</span> 部
                {preview.stats.tags_created > 0 && `，并自动新建 ${preview.stats.tags_created} 个标签`}。
              </p>
              <p className='text-muted-foreground'>
                新建的剧是<span className='font-medium'>草稿</span>，不会直接对观众可见。
                更新只补充别名与简介，封面、评分、状态都不动，标签只追加不删。
              </p>
              {preview.stats.rejected > 0 && (
                <p className='text-muted-foreground'>
                  其中 {preview.stats.rejected} 行命中屏蔽词，不会被导入。
                </p>
              )}
            </div>
          }
          cancelBtnText='再看看'
          confirmText={runMut.isPending ? '导入中…' : '确认导入'}
          isLoading={runMut.isPending}
          handleConfirm={() => runMut.mutate()}
        />
      )}
    </div>
  )
}

function PreviewPanel({ result }: { result: ImportResult }) {
  const s = result.stats
  return (
    <section className='flex flex-col gap-3 rounded-md border p-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <h3 className='text-sm font-medium'>{result.dry_run ? '预览结果' : '导入结果'}</h3>
        {!result.dry_run && (
          <Badge variant='outline' className='whitespace-nowrap'>
            已写入
          </Badge>
        )}
        <div className='ms-auto flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground'>
          {result.actions.map((a) => (
            <Tooltip key={a.action}>
              <TooltipTrigger asChild>
                <span className='cursor-help underline decoration-dotted underline-offset-2'>{a.title}</span>
              </TooltipTrigger>
              <TooltipContent className='max-w-xs'>{a.desc}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      </div>

      <dl className='grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3 lg:grid-cols-6'>
        <Stat label='总行数' value={s.rows_total} />
        <Stat label='新增剧' value={s.dramas_created} />
        <Stat label='更新剧' value={s.dramas_updated} />
        <Stat label='跳过' value={s.skipped} />
        <Stat label='被拒' value={s.rejected} danger={s.rejected > 0} />
        <Stat label='新建标签' value={s.tags_created} />
      </dl>

      <div className='max-h-96 overflow-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted'>
              <TableHead className='pl-4'>行</TableHead>
              <TableHead>剧名</TableHead>
              <TableHead>处置</TableHead>
              <TableHead>说明</TableHead>
              <TableHead className='pr-4'>标签</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {result.rows.map((r) => (
              <PreviewRow key={r.line} row={r} />
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}

function PreviewRow({ row }: { row: ImportRow }) {
  return (
    <TableRow>
      <TableCell className='pl-4 text-xs tabular-nums text-muted-foreground'>{row.line}</TableCell>
      <TableCell>
        <div className='flex flex-col'>
          <span className='font-medium'>{row.title || <span className='text-muted-foreground'>（空）</span>}</span>
          {row.aliases.length > 0 && (
            <span className='text-xs text-muted-foreground'>别名：{row.aliases.join('、')}</span>
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className='flex items-center gap-1 whitespace-nowrap'>
          <Badge variant={actionVariant(row.action)}>{row.action_title}</Badge>
          {row.in_trash && (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className='cursor-help text-xs text-muted-foreground'>（回收站）</span>
              </TooltipTrigger>
              <TooltipContent>这部剧在回收站里，仍算已存在，不会重建一部同名的。</TooltipContent>
            </Tooltip>
          )}
        </div>
      </TableCell>
      <TableCell className='text-xs text-muted-foreground'>
        {row.reason ||
          (row.action === 'update' && row.matched_by ? (
            row.drama_id > 0 ? (
              <>
                按{row.matched_by}匹配到{' '}
                <Link to='/dramas/$id' params={{ id: String(row.drama_id) }} className='underline'>
                  #{row.drama_id}
                </Link>
              </>
            ) : (
              `按${row.matched_by}匹配到已有的剧`
            )
          ) : row.action === 'create' && row.drama_id > 0 ? (
            <Link to='/dramas/$id' params={{ id: String(row.drama_id) }} className='underline'>
              已建 #{row.drama_id}
            </Link>
          ) : (
            '—'
          ))}
      </TableCell>
      <TableCell className='pr-4'>
        <div className='flex flex-wrap gap-1'>
          {row.tags.map((t) => (
            <Badge key={t} variant={row.new_tags.includes(t) ? 'default' : 'outline'} className='whitespace-nowrap'>
              {t}
              {row.new_tags.includes(t) && '（新）'}
            </Badge>
          ))}
        </div>
      </TableCell>
    </TableRow>
  )
}

function Stat({ label, value, danger }: { label: string; value: number; danger?: boolean }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <dt className='text-xs text-muted-foreground'>{label}</dt>
      <dd className={`font-medium tabular-nums ${danger ? 'text-destructive' : ''}`}>{value}</dd>
    </div>
  )
}

function LogRow({ log }: { log: ImportLog }) {
  return (
    <TableRow>
      <TableCell className='pl-4 text-sm whitespace-nowrap tabular-nums'>
        {new Date(log.created_at).toLocaleString('zh-CN', { hour12: false })}
      </TableCell>
      <TableCell className='max-w-56 truncate text-sm' title={log.file_name}>
        {log.file_name || '—'}
      </TableCell>
      <TableCell className='text-sm'>{log.source || '—'}</TableCell>
      <TableCell className='text-sm tabular-nums'>{log.rows_total}</TableCell>
      <TableCell className='text-sm tabular-nums'>
        {log.dramas_created} / {log.dramas_updated}
      </TableCell>
      <TableCell className='text-sm tabular-nums'>
        {log.skipped} /{' '}
        <span className={log.rejected > 0 ? 'text-destructive' : ''}>{log.rejected}</span>
      </TableCell>
      <TableCell className='text-sm tabular-nums'>{log.tags_created}</TableCell>
      <TableCell className='pr-4 text-sm'>{log.admin_name || `#${log.admin_id}`}</TableCell>
    </TableRow>
  )
}

/** 下载 CSV 模板。带 BOM 让 Excel 正确识别 UTF-8。 */
function downloadTemplate() {
  const blob = new Blob([CSV_TEMPLATE], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = '批量导入模板.csv'
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * 把日期输入框的值换算成带时区的时间戳。
 *
 * 按浏览器所在时区算，这样「今天」是用户的今天，不是服务端 UTC 的今天
 * （和审计日志页的处理一致）。
 */
function dayStart(day: string): string {
  return new Date(`${day}T00:00:00`).toISOString()
}

function dayEnd(day: string): string {
  const d = new Date(`${day}T00:00:00`)
  d.setDate(d.getDate() + 1)
  return d.toISOString()
}
