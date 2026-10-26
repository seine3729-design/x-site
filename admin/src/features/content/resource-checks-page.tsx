import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle, ExternalLink, Pencil, Play, Stethoscope, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Checkbox,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
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
import { FilterSelect, ResetButton, SearchBox } from './components/list-toolbar'
import {
  checkApi,
  checkKeys,
  checkListQuery,
  elapsedLabel,
  fileTypeLabel,
  resultVariant,
  type CheckOverview,
  type CheckRow,
} from './resource-check-api'
import { withFilter } from './search'

type DialogState = { type: 'run' } | { type: 'clear' }

/**
 * 资源体检（docs/06 第 9 节，S4-5 / A4-2）：批量探测剧集的视频产物与封面是否可达。
 *
 * 这里查的是**资源**在不在（一个一个对象去读），不是线路好不好 ——
 * 整条线路挂了这里会全红，那种情况该去「选线预览与健康」页看。
 */
export function ResourceChecksPage() {
  const can = useCan()
  const visible = can('resource_checks.view')
  const qc = useQueryClient()
  const search = useSearch({ from: '/_authenticated/resource-checks' })
  const navigate = useNavigate({ from: '/resource-checks' })
  const params = {
    drama_id: search.drama_id,
    bad_only: search.bad_only,
    q: search.q,
  }
  const list = useQuery({ ...checkListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  const refresh = () => void qc.invalidateQueries({ queryKey: checkKeys.all })

  const clear = useMutation({
    mutationFn: (dramaID?: number) => checkApi.clear(dramaID),
    onSuccess: (res) => {
      setDialog(null)
      refresh()
      toast.success(res.deleted > 0 ? `已清空 ${res.deleted} 条结果` : '没有结果需要清空')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='资源体检' />
  const editable = can('resource_checks.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const data = list.data
  const filtered = Boolean(search.q || search.drama_id || search.bad_only)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='资源体检'
        description='逐个读一遍剧集的视频产物与封面，看哪些打不开。只读不改，发现问题后去剧集页修路径或重新转封装。'
        readonly={!editable}
      />

      {data?.line_warning && (
        <div className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm'>
          <AlertTriangle className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
          <span>{data.line_warning}</span>
        </div>
      )}

      <Summary data={data} />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜剧名或剧编号'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <DramaIDFilter
          value={search.drama_id}
          onSubmit={(drama_id) => void navigate({ search: (prev) => withFilter(prev, { drama_id }) })}
        />
        <FilterSelect
          label='范围'
          value={search.bad_only ? 'bad' : undefined}
          options={[{ label: '只看打不开', value: 'bad' }]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { bad_only: v === 'bad' ? true : undefined }) })}
        />
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
        <div className='ms-auto flex gap-2'>
          <WithReason reason={readonlyReason}>
            <Button className='h-9' disabled={!editable} onClick={() => setDialog({ type: 'run' })}>
              <Stethoscope className='size-4' aria-hidden /> 开始体检
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button
              variant='outline'
              className='h-9'
              disabled={!editable || (data?.summary.total ?? 0) === 0}
              onClick={() => setDialog({ type: 'clear' })}
            >
              <Trash2 className='size-4' aria-hidden /> 清空结果
            </Button>
          </WithReason>
        </div>
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>剧集</TableHead>
                <TableHead>资源</TableHead>
                <TableHead>结果</TableHead>
                <TableHead>说明</TableHead>
                <TableHead>线路</TableHead>
                <TableHead>耗时</TableHead>
                <TableHead>体检时间</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : data!.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                    {filtered
                      ? search.bad_only
                        ? '没有打不开的资源'
                        : '没有符合条件的结果'
                      : '还没有体检过，点右上角「开始体检」'}
                  </TableCell>
                </TableRow>
              ) : (
                data!.list.map((row) => <ResultRow key={row.id} row={row} />)
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {data && data.total > data.shown && (
        <p className='text-xs text-muted-foreground'>
          符合条件的有 {data.total} 条，这里只显示前 {data.shown} 条。筛「只看打不开」或填剧编号来缩小范围。
        </p>
      )}

      {dialog?.type === 'run' && <RunDialog onDone={refresh} onClose={() => setDialog(null)} />}

      {dialog?.type === 'clear' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          title={search.drama_id ? `清空 ${search.drama_id} 号剧的体检结果？` : '清空全部体检结果？'}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                <span className='font-medium'>只清体检表，源文件一个都不会动。</span>
                清掉的只是「上次探测的结论」，重新体检就会再长出来。
              </p>
              <p className='text-muted-foreground'>
                当前共 {data?.summary.total ?? 0} 条结果
                {(data?.summary.bad ?? 0) > 0 && `，其中 ${data!.summary.bad} 条打不开`}。
              </p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={clear.isPending ? '清空中…' : '清空'}
          isLoading={clear.isPending}
          handleConfirm={() => clear.mutate(search.drama_id)}
        />
      )}
    </div>
  )
}

/**
 * 剧编号筛选。包在 form 里，回车就能提交 —— 和旁边的搜索框一个用法。
 * 失焦也提交，免得填完一个数字愣在那里不知道还要按什么。
 */
function DramaIDFilter({
  value,
  onSubmit,
}: {
  value: number | undefined
  onSubmit: (dramaID: number | undefined) => void
}) {
  // key 让地址栏变化（如点了重置）时输入框跟着回到同步状态
  return <DramaIDFilterInner key={value ?? ''} value={value} onSubmit={onSubmit} />
}

function DramaIDFilterInner({
  value,
  onSubmit,
}: {
  value: number | undefined
  onSubmit: (dramaID: number | undefined) => void
}) {
  const [draft, setDraft] = useState(value ? String(value) : '')
  const submit = () => {
    const v = draft.trim()
    const n = v === '' ? undefined : Number(v)
    const next = n !== undefined && Number.isInteger(n) && n > 0 ? n : undefined
    if (next !== value) onSubmit(next)
  }
  return (
    <form
      className='flex items-center gap-2'
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <label htmlFor='rc-drama-id' className='text-sm whitespace-nowrap text-muted-foreground'>
        剧编号
      </label>
      <Input
        id='rc-drama-id'
        className='h-9 w-28'
        inputMode='numeric'
        maxLength={12}
        placeholder='如 12，回车筛选'
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={submit}
      />
    </form>
  )
}

function Summary({ data }: { data: CheckOverview | undefined }) {
  const s = data?.summary
  return (
    <section className='flex flex-col gap-3 rounded-md border p-4'>
      <div className='flex flex-wrap items-center gap-x-6 gap-y-2 text-sm'>
        <span>
          已体检 <span className='font-medium tabular-nums'>{s?.total ?? 0}</span> 个资源
        </span>
        <span>
          打不开{' '}
          <span className={`font-medium tabular-nums ${(s?.bad ?? 0) > 0 ? 'text-destructive' : ''}`}>
            {s?.bad ?? 0}
          </span>{' '}
          个
        </span>
        <span className='text-muted-foreground'>
          最近体检：
          {s?.last_checked_at ? new Date(s.last_checked_at).toLocaleString('zh-CN', { hour12: false }) : '还没有'}
        </span>
      </div>
      {data && data.results.length > 0 && (
        <div className='flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground'>
          {data.results.map((r) => (
            <Tooltip key={r.result}>
              <TooltipTrigger asChild>
                <span className='cursor-help underline decoration-dotted underline-offset-2'>{r.title}</span>
              </TooltipTrigger>
              <TooltipContent className='max-w-xs'>{r.desc}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      )}
    </section>
  )
}

function ResultRow({ row }: { row: CheckRow }) {
  return (
    <TableRow>
      <TableCell className='pl-4'>
        <div className='flex flex-col'>
          <Link
            to='/dramas/$id'
            params={{ id: String(row.drama_id) }}
            className='font-medium hover:underline'
          >
            {row.drama_title}
          </Link>
          <span className='text-xs text-muted-foreground tabular-nums'>
            #{row.drama_id}
            {row.episode_number > 0 && ` · 第 ${row.episode_number} 集`}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <div className='flex flex-col'>
          <span className='text-sm'>
            {fileTypeLabel(row.file_type)} · {row.field_title}
          </span>
          {row.url && (
            <code className='max-w-88 truncate text-xs text-muted-foreground' title={row.url}>
              {row.url}
            </code>
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className='flex items-center gap-1 whitespace-nowrap'>
          <Badge variant={resultVariant(row.result)}>{row.result_title}</Badge>
          {row.status_code > 0 && (
            <span className='text-xs text-muted-foreground tabular-nums'>{row.status_code}</span>
          )}
        </div>
      </TableCell>
      <TableCell className='max-w-[18rem] text-xs text-muted-foreground'>{row.message || '—'}</TableCell>
      <TableCell className='text-xs whitespace-nowrap'>{row.line_name || '直连地址'}</TableCell>
      <TableCell className='text-xs whitespace-nowrap tabular-nums'>{elapsedLabel(row.elapsed_ms)}</TableCell>
      <TableCell className='text-xs whitespace-nowrap tabular-nums'>
        {new Date(row.checked_at).toLocaleString('zh-CN', { hour12: false })}
      </TableCell>
      <TableCell className='pr-4'>
        <div className='flex justify-end gap-1'>
          <Button variant='ghost' size='icon' asChild aria-label={`编辑「${row.drama_title}」`}>
            <Link to='/dramas/$id' params={{ id: String(row.drama_id) }}>
              <Pencil className='size-4' />
            </Link>
          </Button>
          {row.url && (
            <Button variant='ghost' size='icon' asChild aria-label='在新窗口打开这个地址'>
              <a href={row.url} target='_blank' rel='noreferrer noopener'>
                <ExternalLink className='size-4' />
              </a>
            </Button>
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}

/** 发起体检：选范围。 */
function RunDialog({ onDone, onClose }: { onDone: () => void; onClose: () => void }) {
  const [scope, setScope] = useState<'all' | 'one'>('all')
  const [dramaID, setDramaID] = useState('')
  const [includeUnpublished, setIncludeUnpublished] = useState(false)

  const run = useMutation({
    mutationFn: () =>
      checkApi.run(
        scope === 'one'
          ? { drama_id: Number(dramaID) }
          : { include_unpublished: includeUnpublished }
      ),
    onSuccess: (res) => {
      onDone()
      onClose()
      toast.success(
        res.dramas === 1
          ? '已开始体检这部剧，稍后刷新看结果'
          : `已开始体检 ${res.dramas} 部剧，稍后刷新看结果`
      )
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const idInvalid = scope === 'one' && !/^\d+$/.test(dramaID.trim())

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>开始体检</DialogTitle>
          <DialogDescription>
            由后台任务按剧逐部跑，不用守着这个页面。只读源站，不会改动任何文件。
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-3'>
          <div className='flex flex-col gap-2'>
            <span className='text-sm font-medium'>范围</span>
            <label className='flex items-center gap-2 text-sm' htmlFor='rc-scope-all'>
              <input
                id='rc-scope-all'
                type='radio'
                name='rc-scope'
                className='size-4'
                checked={scope === 'all'}
                onChange={() => setScope('all')}
              />
              全部剧
            </label>
            <label className='flex items-center gap-2 text-sm' htmlFor='rc-scope-one'>
              <input
                id='rc-scope-one'
                type='radio'
                name='rc-scope'
                className='size-4'
                checked={scope === 'one'}
                onChange={() => setScope('one')}
              />
              指定一部剧
            </label>
          </div>

          {scope === 'one' ? (
            <div className='flex flex-col gap-1'>
              <label htmlFor='rc-run-id' className='text-sm font-medium'>
                剧编号
              </label>
              <Input
                id='rc-run-id'
                className='h-10'
                inputMode='numeric'
                placeholder='请输入剧编号，如 12'
                value={dramaID}
                onChange={(e) => setDramaID(e.target.value)}
              />
              <span className='text-xs text-muted-foreground'>在剧集列表里每行编号前有 # 标记。</span>
            </div>
          ) : (
            <div className='flex items-start gap-2 rounded-md border p-3'>
              <Checkbox
                id='rc-include-unpublished'
                className='mt-0.5'
                checked={includeUnpublished}
                onCheckedChange={(v) => setIncludeUnpublished(v === true)}
              />
              <div className='flex flex-col gap-0.5'>
                <label htmlFor='rc-include-unpublished' className='cursor-pointer text-sm font-medium'>
                  含未上架的剧
                </label>
                <span className='text-xs text-muted-foreground'>
                  默认只检已上架的 —— 那些是观众现在就能点开的。草稿与已下架的剧资源没准备好是正常的。
                </span>
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button type='button' disabled={run.isPending || idInvalid} onClick={() => run.mutate()}>
            {run.isPending ? '提交中…' : '开始'}
            {!run.isPending && <Play className='size-4' aria-hidden />}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
