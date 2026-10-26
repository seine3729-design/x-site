import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Activity, CircleStop, Flame, Info, Radio } from 'lucide-react'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
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
import {
  bytesLabel,
  DEFAULT_WARM_CONCURRENCY,
  DEFAULT_WARM_SEGMENTS_PER_QUALITY,
  durationLabel,
  scopeLabel,
  warmApi,
  warmHistoryQuery,
  warmKeys,
  warmStartPayload,
  warmStatusVariant,
  type WarmJob,
  type WarmMode,
  type WarmProgress,
} from './resource-warm-api'
import { useWarmProgress } from './use-warm-stream'

/**
 * 资源预热（docs/06 第 10 节，S4-5 / A4-2）：预先拉取清单与前 N 个切片，让边缘缓存住，
 * 减少真实观众的回源等待。
 *
 * 同一时间只允许一轮。可以关掉页面，回来还能看到进度。
 */
export function ResourceWarmPage() {
  const can = useCan()
  const visible = can('resource_warm.view')
  const qc = useQueryClient()
  const progress = useWarmProgress(visible)
  const history = useQuery({ ...warmHistoryQuery, enabled: visible })
  const [dialog, setDialog] = useState<'start' | 'stop' | null>(null)

  const stop = useMutation({
    mutationFn: (id: number) => warmApi.stop(id),
    onSuccess: () => {
      setDialog(null)
      void qc.invalidateQueries({ queryKey: warmKeys.all })
      toast.success('已请求停止，正在收尾（已经热好的算数）')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='资源预热' />
  const editable = can('resource_warm.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const data = progress.data
  const job = data?.job ?? null
  const running = data?.running ?? false

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='资源预热'
        description='提前把清单与起播的几个切片拉一遍，让边缘先缓存住，观众点开就不用等回源。同一时间只能跑一轮。'
        readonly={!editable}
      />

      {data?.note && (
        <div className='flex items-start gap-2 rounded-md border p-3 text-xs text-muted-foreground'>
          <Info className='mt-0.5 size-4 shrink-0' aria-hidden />
          <span>{data.note}</span>
        </div>
      )}

      {progress.isError ? (
        <LoadError onRetry={() => void progress.refetch()} />
      ) : (
        <CurrentRound
          data={data}
          pending={progress.isPending}
          streaming={progress.streaming}
          editable={editable}
          readonlyReason={readonlyReason}
          onStart={() => setDialog('start')}
          onStop={() => setDialog('stop')}
        />
      )}

      <section className='flex flex-col gap-2'>
        <h3 className='text-sm font-medium'>历史记录</h3>
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>开始时间</TableHead>
                <TableHead>范围</TableHead>
                <TableHead>深度</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>集数</TableHead>
                <TableHead>热好 / 有问题</TableHead>
                <TableHead>流量</TableHead>
                <TableHead className='pr-4'>用时</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.isPending ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-20 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : (history.data?.list.length ?? 0) === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-20 text-center text-muted-foreground'>
                    还没有预热过
                  </TableCell>
                </TableRow>
              ) : (
                history.data!.list.map((h) => <HistoryRow key={h.id} job={h} />)
              )}
            </TableBody>
          </Table>
        </div>
      </section>

      {dialog === 'start' && (
        <StartDialog
          onDone={() => {
            progress.retry()
            void qc.invalidateQueries({ queryKey: warmKeys.all })
          }}
          onClose={() => setDialog(null)}
        />
      )}

      {dialog === 'stop' && job && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          title='停止这一轮预热？'
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                <span className='font-medium'>已经热好的算数。</span>
                缓存里的东西不会退回去，只是剩下的不再继续了。
              </p>
              <p className='text-muted-foreground'>
                后台会处理完手上这一集再收尾，所以点完之后状态可能还要几秒才变。 当前已热好 {job.warmed} 个资源、跑完 {job.episodes_done}/
                {job.episodes_total} 集。
              </p>
            </div>
          }
          cancelBtnText='继续预热'
          confirmText={stop.isPending ? '停止中…' : '停止'}
          isLoading={stop.isPending}
          handleConfirm={() => stop.mutate(job.id)}
        />
      )}

      {!running && <StopDisabledHint />}
    </div>
  )
}

/** 没有在跑时，「停止」按钮不渲染，这里解释一下为什么看不到它。 */
function StopDisabledHint() {
  return <p className='text-xs text-muted-foreground'>没有在跑的预热。点「开始预热」选好范围与深度就能开一轮；同一时间只能跑一轮。</p>
}

function CurrentRound({
  data,
  pending,
  streaming,
  editable,
  readonlyReason,
  onStart,
  onStop,
}: {
  data: WarmProgress | undefined
  pending: boolean
  streaming: boolean
  editable: boolean
  readonlyReason: string | null
  onStart: () => void
  onStop: () => void
}) {
  const job = data?.job ?? null
  const running = data?.running ?? false

  return (
    <section className='flex flex-col gap-4 rounded-md border p-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <h3 className='text-sm font-medium'>{running ? '正在预热' : job ? '最近一轮' : '还没有预热过'}</h3>
        {job && (
          <Badge variant={warmStatusVariant(job.status)} className='whitespace-nowrap'>
            {job.status_title}
          </Badge>
        )}
        {streaming && (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className='inline-flex cursor-help items-center gap-1 text-xs text-muted-foreground'>
                <Radio className='size-3.5 animate-pulse' aria-hidden /> 实时
              </span>
            </TooltipTrigger>
            <TooltipContent>进度是后台实时推过来的，不用刷新页面。</TooltipContent>
          </Tooltip>
        )}
        <div className='ms-auto flex gap-2'>
          {running ? (
            <WithReason reason={readonlyReason}>
              <Button variant='outline' className='h-9' disabled={!editable} onClick={onStop}>
                <CircleStop className='size-4' aria-hidden /> 停止
              </Button>
            </WithReason>
          ) : (
            <WithReason reason={readonlyReason}>
              <Button className='h-9' disabled={!editable} onClick={onStart}>
                <Flame className='size-4' aria-hidden /> 开始预热
              </Button>
            </WithReason>
          )}
        </div>
      </div>

      {pending ? (
        <p className='text-sm text-muted-foreground'>加载中…</p>
      ) : !job ? (
        <p className='text-sm text-muted-foreground'>还没有跑过预热。新上架的热门剧建议先热一轮，观众点开的体感差别最明显。</p>
      ) : (
        <>
          <div className='flex flex-col gap-1'>
            <div className='flex items-center justify-between text-xs text-muted-foreground'>
              <span>
                {job.episodes_done} / {job.episodes_total} 集
              </span>
              <span className='tabular-nums'>{job.percent}%</span>
            </div>
            <div
              className='h-2 w-full overflow-hidden rounded-full bg-muted'
              role='progressbar'
              aria-valuenow={job.percent}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label='预热进度'
            >
              <div
                className={`h-full rounded-full transition-[width] duration-500 ${job.status === 'interrupted' ? 'bg-destructive' : 'bg-primary'}`}
                style={{ width: `${job.percent}%` }}
              />
            </div>
            {job.current && (
              <p className='flex items-center gap-1 text-xs text-muted-foreground'>
                <Activity className='size-3.5' aria-hidden /> {job.current}
              </p>
            )}
          </div>

          <dl className='grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3 lg:grid-cols-6'>
            <Stat label='范围' value={scopeLabel(job)} />
            <Stat label='深度' value={job.mode_title} />
            <Stat label='热好' value={String(job.warmed)} />
            <Stat
              label='有问题'
              value={String(job.failed)}
              danger={job.failed > 0}
              extra={
                job.failed > 0 ? (
                  <Link to='/resource-checks' search={{ bad_only: true }} className='text-xs underline'>
                    去体检页看
                  </Link>
                ) : undefined
              }
            />
            <Stat label='流量' value={bytesLabel(job.traffic_bytes)} />
            <Stat label='用时' value={durationLabel(job.elapsed_ms)} />
          </dl>

          <p className='text-xs text-muted-foreground'>
            开始于 {new Date(job.started_at).toLocaleString('zh-CN', { hour12: false })}
            {job.mode === 'with_segments' && ` · 每清晰度 ${job.segments_per_quality} 片`}
            {` · 同时 ${job.concurrency} 集`}
            {job.stop_requested && job.status === 'running' && ' · 已请求停止，正在收尾'}
          </p>

          {job.error && <p className='rounded-md border border-destructive/40 bg-destructive/5 p-2 text-xs'>{job.error}</p>}
        </>
      )}
    </section>
  )
}

function Stat({ label, value, danger, extra }: { label: string; value: string; danger?: boolean; extra?: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <dt className='text-xs text-muted-foreground'>{label}</dt>
      <dd className={`font-medium tabular-nums ${danger ? 'text-destructive' : ''}`}>{value}</dd>
      {extra}
    </div>
  )
}

function HistoryRow({ job }: { job: WarmJob }) {
  return (
    <TableRow>
      <TableCell className='pl-4 text-sm whitespace-nowrap tabular-nums'>
        {new Date(job.started_at).toLocaleString('zh-CN', { hour12: false })}
      </TableCell>
      <TableCell className='text-sm'>{scopeLabel(job)}</TableCell>
      <TableCell className='text-sm whitespace-nowrap'>{job.mode_title}</TableCell>
      <TableCell>
        <Badge variant={warmStatusVariant(job.status)} className='whitespace-nowrap'>
          {job.status_title}
        </Badge>
      </TableCell>
      <TableCell className='text-sm tabular-nums'>
        {job.episodes_done} / {job.episodes_total}
      </TableCell>
      <TableCell className='text-sm tabular-nums'>
        {job.warmed}
        {job.failed > 0 && <span className='text-destructive'> / {job.failed}</span>}
      </TableCell>
      <TableCell className='text-sm whitespace-nowrap tabular-nums'>{bytesLabel(job.traffic_bytes)}</TableCell>
      <TableCell className='pr-4 text-sm whitespace-nowrap tabular-nums'>{durationLabel(job.elapsed_ms)}</TableCell>
    </TableRow>
  )
}

/** 开一轮：选范围与深度。 */
function StartDialog({ onDone, onClose }: { onDone: () => void; onClose: () => void }) {
  const [scope, setScope] = useState<'all' | 'one'>('all')
  const [dramaID, setDramaID] = useState('')
  const [includeUnpublished, setIncludeUnpublished] = useState(false)
  const [mode, setMode] = useState<WarmMode>('with_segments')
  const [segments, setSegments] = useState(String(DEFAULT_WARM_SEGMENTS_PER_QUALITY))
  const [concurrency, setConcurrency] = useState(String(DEFAULT_WARM_CONCURRENCY))

  const start = useMutation({
    mutationFn: () =>
      warmApi.start(
        warmStartPayload({
          scope,
          dramaID,
          includeUnpublished,
          mode,
          segments,
          concurrency,
        })
      ),
    onSuccess: (job) => {
      onDone()
      onClose()
      toast.success(`已开始预热 ${job.episodes_total} 集，可以关掉页面，回来还能看进度`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const idInvalid = scope === 'one' && !/^\d+$/.test(dramaID.trim())
  const numInvalid =
    !/^\d+$/.test(segments.trim()) ||
    Number(segments) < 1 ||
    Number(segments) > 20 ||
    !/^\d+$/.test(concurrency.trim()) ||
    Number(concurrency) < 1 ||
    Number(concurrency) > 16

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>开始预热</DialogTitle>
          <DialogDescription>由后台任务跑，可以关掉这个页面。同一时间只能跑一轮，跑的过程中可以随时停止。</DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <span className='text-sm font-medium'>范围</span>
            <label className='flex items-center gap-2 text-sm' htmlFor='rw-scope-all'>
              <input id='rw-scope-all' type='radio' name='rw-scope' className='size-4' checked={scope === 'all'} onChange={() => setScope('all')} />
              全部剧
            </label>
            <label className='flex items-center gap-2 text-sm' htmlFor='rw-scope-one'>
              <input id='rw-scope-one' type='radio' name='rw-scope' className='size-4' checked={scope === 'one'} onChange={() => setScope('one')} />
              指定一部剧
            </label>
          </div>

          {scope === 'one' ? (
            <div className='flex flex-col gap-1'>
              <label htmlFor='rw-drama-id' className='text-sm font-medium'>
                剧编号
              </label>
              <Input
                id='rw-drama-id'
                className='h-10'
                inputMode='numeric'
                placeholder='请输入剧编号，如 12'
                value={dramaID}
                onChange={(e) => setDramaID(e.target.value)}
              />
            </div>
          ) : (
            <div className='flex items-start gap-2 rounded-md border p-3'>
              <Checkbox
                id='rw-include-unpublished'
                className='mt-0.5'
                checked={includeUnpublished}
                onCheckedChange={(v) => setIncludeUnpublished(v === true)}
              />
              <div className='flex flex-col gap-0.5'>
                <label htmlFor='rw-include-unpublished' className='cursor-pointer text-sm font-medium'>
                  含未上架的剧
                </label>
                <span className='text-xs text-muted-foreground'>默认只热已上架的。明天要上的剧可以提前热好，上架那一刻观众点开就是热的。</span>
              </div>
            </div>
          )}

          <div className='flex flex-col gap-1'>
            <label htmlFor='rw-mode' className='text-sm font-medium'>
              深度
            </label>
            <Select value={mode} onValueChange={(v) => setMode(v as WarmMode)}>
              <SelectTrigger id='rw-mode' className='h-10 w-full'>
                <SelectValue placeholder='请选择预热深度' />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='with_segments'>连切片一起拉（真正的边缘预热）</SelectItem>
                <SelectItem value='playlist_only'>只拉清单（流量极小）</SelectItem>
              </SelectContent>
            </Select>
            <span className='text-xs text-muted-foreground'>
              {mode === 'with_segments'
                ? '拉清单和起播的几个切片。流量按「片数 × 清晰度数 × 集数」增长，全站预热前先估一下。'
                : '只拉清单，热的是服务端的清单缓存。切片仍要观众回源，起播体感改善有限。'}
            </span>
          </div>

          <div className='grid grid-cols-2 gap-4'>
            <div className='flex flex-col gap-1'>
              <label htmlFor='rw-segments' className='text-sm font-medium'>
                每清晰度片数
              </label>
              <Input
                id='rw-segments'
                className='h-10'
                inputMode='numeric'
                placeholder='1–20，默认 2'
                value={segments}
                disabled={mode !== 'with_segments'}
                onChange={(e) => setSegments(e.target.value)}
              />
              <span className='text-xs text-muted-foreground'>2 片约 8 秒，覆盖起播与 ABR 稳定前的窗口。</span>
            </div>
            <div className='flex flex-col gap-1'>
              <label htmlFor='rw-concurrency' className='text-sm font-medium'>
                同时几集
              </label>
              <Input
                id='rw-concurrency'
                className='h-10'
                inputMode='numeric'
                placeholder='1–16，默认 4'
                value={concurrency}
                onChange={(e) => setConcurrency(e.target.value)}
              />
              <span className='text-xs text-muted-foreground'>调大更快，但对源站的压力也更大。</span>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button type='button' disabled={start.isPending || idInvalid || numInvalid} onClick={() => start.mutate()}>
            {start.isPending ? '提交中…' : '开始'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
