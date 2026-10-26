import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Copy, HardDriveDownload, MoveRight, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  Badge,
  Button,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import {
  formatBytes,
  manageApi,
  orphanStatsQuery,
  providersQuery,
  REPLICATE_SCOPE_LABELS,
  type CleanupResult,
  type EnqueueResult,
  type ReplicateScope,
  type StorageProvider,
} from './api'

/**
 * 存储运维操作区（docs/12 第 5 节，S1-6f）：手动补副本、迁移存储源、接管历史产物、清理孤立产物。
 * 这些操作都只是入队或扫描，实际搬运与删除由后台任务执行，所以界面上只报告「入队了多少」。
 */
export function MaintenancePanel({ editable }: { editable: boolean }) {
  return (
    <div className='flex flex-col gap-4'>
      <div>
        <h2 className='text-lg font-semibold tracking-tight'>存储运维</h2>
        <p className='text-sm text-muted-foreground'>
          补副本、迁移、接管历史产物与清理。这些操作把任务交给后台执行，界面上看到的是入队数量；进度与结果在
          worker 日志与副本状态里。
        </p>
      </div>
      <div className='grid gap-4 lg:grid-cols-2'>
        <ReplicateCard editable={editable} />
        <MigrateCard editable={editable} />
        <AdoptCard editable={editable} />
        <CleanupCard editable={editable} />
      </div>
    </div>
  )
}

function Card({
  title,
  desc,
  icon,
  children,
}: {
  title: string
  desc: string
  icon: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className='flex flex-col gap-3 rounded-md border p-4'>
      <div className='flex items-start gap-2'>
        <span className='mt-0.5 text-muted-foreground' aria-hidden>
          {icon}
        </span>
        <div className='flex flex-col gap-0.5'>
          <h3 className='font-medium'>{title}</h3>
          <p className='text-xs text-muted-foreground'>{desc}</p>
        </div>
      </div>
      {children}
    </section>
  )
}

/** 入队结果的统一提示。 */
function enqueueToast(label: string, res: EnqueueResult) {
  if (res.objects === 0) {
    toast.info(`${label}：没有需要处理的内容`)
    return
  }
  toast.success(
    `${label}：已入队 ${res.objects} 个对象` +
      (res.capped ? '（达到单次上限，可再次触发继续）' : '') +
      '，进度见后台任务日志'
  )
}

/** 可写的存储源选项（HTTP 源只读、不能作为写入目标）。 */
function writableItems(providers: StorageProvider[] | undefined) {
  return (providers ?? [])
    .filter((p) => p.subtype !== 'http_origin')
    .map((p) => ({ id: p.id, label: `${p.name}（${p.bucket || p.endpoint}）`, status: p.status }))
}

function ReplicateCard({ editable }: { editable: boolean }) {
  const providers = useQuery(providersQuery)
  const [scope, setScope] = useState<ReplicateScope>('all')
  const [dramaID, setDramaID] = useState('')
  const [providerID, setProviderID] = useState('')

  const run = useMutation({
    mutationFn: () =>
      manageApi.replicate({
        scope,
        drama_id: scope === 'drama' ? Number(dramaID) : undefined,
        provider_id: scope === 'provider' ? Number(providerID) : undefined,
      }),
    onSuccess: (res) => enqueueToast('补副本', res),
    onError: (err) => toast.error(errorMessage(err)),
  })

  const ready = scope === 'all' || (scope === 'drama' ? Number(dramaID) > 0 : Number(providerID) > 0)

  return (
    <Card
      title='手动补副本'
      desc='把内容复制到全部镜像存储源。任务幂等，已就绪的镜像会跳过，可重复触发。'
      icon={<Copy className='size-4' />}
    >
      <div className='flex flex-col gap-2'>
        <label htmlFor='repl-scope' className='text-sm text-muted-foreground'>
          范围
        </label>
        <Select value={scope} onValueChange={(v) => setScope(v as ReplicateScope)} disabled={!editable}>
          <SelectTrigger id='repl-scope' className='w-full'>
            <SelectValue placeholder='请选择范围' />
          </SelectTrigger>
          <SelectContent>
            {(Object.keys(REPLICATE_SCOPE_LABELS) as ReplicateScope[]).map((s) => (
              <SelectItem key={s} value={s}>
                {REPLICATE_SCOPE_LABELS[s]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {scope === 'drama' && (
          <>
            <label htmlFor='repl-drama' className='text-sm text-muted-foreground'>
              剧集编号
            </label>
            <Input
              id='repl-drama'
              type='number'
              inputMode='numeric'
              placeholder='请输入剧集编号，如 1024'
              value={dramaID}
              onChange={(e) => setDramaID(e.target.value)}
              disabled={!editable}
            />
          </>
        )}
        {scope === 'provider' && (
          <>
            <label htmlFor='repl-provider' className='text-sm text-muted-foreground'>
              存储源
            </label>
            <Select value={providerID} onValueChange={setProviderID} disabled={!editable}>
              <SelectTrigger id='repl-provider' className='w-full'>
                <SelectValue placeholder='请选择存储源' />
              </SelectTrigger>
              <SelectContent>
                {writableItems(providers.data).map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        )}
      </div>
      {editable && (
        <Button className='self-start' onClick={() => run.mutate()} disabled={run.isPending || !ready}>
          {run.isPending ? '入队中…' : '开始补副本'}
        </Button>
      )}
    </Card>
  )
}

function MigrateCard({ editable }: { editable: boolean }) {
  const providers = useQuery(providersQuery)
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [confirm, setConfirm] = useState(false)

  const run = useMutation({
    mutationFn: () => manageApi.migrate({ from_provider_id: Number(from), to_provider_id: Number(to) }),
    onSuccess: (res) => {
      setConfirm(false)
      enqueueToast('迁移', res)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const items = writableItems(providers.data)
  const ready = Number(from) > 0 && Number(to) > 0 && from !== to
  const fromName = items.find((p) => String(p.id) === from)?.label ?? from
  const toName = items.find((p) => String(p.id) === to)?.label ?? to

  return (
    <Card
      title='迁移存储源'
      desc='先复制到迁入源、确认就绪后再撤掉迁出源的副本记录。不删迁出源上的文件，所以可逆；副本清零后该存储源才能删除。'
      icon={<MoveRight className='size-4' />}
    >
      <div className='flex flex-col gap-2'>
        <label htmlFor='mig-from' className='text-sm text-muted-foreground'>
          迁出存储源（建议先设为停写）
        </label>
        <Select value={from} onValueChange={setFrom} disabled={!editable}>
          <SelectTrigger id='mig-from' className='w-full'>
            <SelectValue placeholder='请选择迁出的存储源' />
          </SelectTrigger>
          <SelectContent>
            {items.map((p) => (
              <SelectItem key={p.id} value={String(p.id)}>
                {p.label}
                {p.status === 'draining' ? '（停写）' : ''}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <label htmlFor='mig-to' className='text-sm text-muted-foreground'>
          迁入存储源（必须启用中）
        </label>
        <Select value={to} onValueChange={setTo} disabled={!editable}>
          <SelectTrigger id='mig-to' className='w-full'>
            <SelectValue placeholder='请选择迁入的存储源' />
          </SelectTrigger>
          <SelectContent>
            {items
              .filter((p) => p.status === 'active' && String(p.id) !== from)
              .map((p) => (
                <SelectItem key={p.id} value={String(p.id)}>
                  {p.label}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
      </div>
      {editable && (
        <Button className='self-start' onClick={() => setConfirm(true)} disabled={run.isPending || !ready}>
          {run.isPending ? '入队中…' : '开始迁移'}
        </Button>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title='确认迁移存储源？'
        desc={
          <div className='flex flex-col gap-2 text-sm'>
            <p>
              把 <span className='font-medium'>{fromName}</span> 上的副本迁到{' '}
              <span className='font-medium'>{toName}</span>。
            </p>
            <p className='text-muted-foreground'>
              迁移会先复制到迁入源，确认就绪后撤掉迁出源的副本记录。
              <span className='font-medium'>不会删除迁出源上的文件</span>，所以这一步可逆；
              文件由你确认后自行清桶。建议先把迁出源设为停写（draining），避免迁移过程中又写入新内容。
            </p>
          </div>
        }
        confirmText={run.isPending ? '入队中…' : '确认迁移'}
        handleConfirm={() => run.mutate()}
        disabled={run.isPending}
      />
    </Card>
  )
}

function AdoptCard({ editable }: { editable: boolean }) {
  const providers = useQuery(providersQuery)
  const [providerID, setProviderID] = useState('')

  const run = useMutation({
    mutationFn: () => manageApi.adopt({ provider_id: Number(providerID) }),
    onSuccess: (res) => enqueueToast('接管历史产物', res),
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Card
      title='接管历史产物'
      desc='给还没有副本记录的旧产物补登记。后台会先校验文件在该存储源上真实存在，校验不通过的跳过。'
      icon={<HardDriveDownload className='size-4' />}
    >
      <div className='flex flex-col gap-2'>
        <label htmlFor='adopt-provider' className='text-sm text-muted-foreground'>
          产物所在的存储源
        </label>
        <Select value={providerID} onValueChange={setProviderID} disabled={!editable}>
          <SelectTrigger id='adopt-provider' className='w-full'>
            <SelectValue placeholder='请选择产物实际所在的存储源' />
          </SelectTrigger>
          <SelectContent>
            {writableItems(providers.data).map((p) => (
              <SelectItem key={p.id} value={String(p.id)}>
                {p.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {editable && (
        <Button
          className='self-start'
          onClick={() => run.mutate()}
          disabled={run.isPending || Number(providerID) <= 0}
        >
          {run.isPending ? '入队中…' : '开始接管'}
        </Button>
      )}
    </Card>
  )
}

function CleanupCard({ editable }: { editable: boolean }) {
  const qc = useQueryClient()
  const stats = useQuery(orphanStatsQuery)
  const [preview, setPreview] = useState<CleanupResult | null>(null)
  const [confirm, setConfirm] = useState(false)

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ['storage', 'orphans'] })
  }

  const dryRun = useMutation({
    mutationFn: () => manageApi.cleanup({ dry_run: true }),
    onSuccess: (res) => {
      setPreview(res)
      if (res.deleted === 0) toast.info('预览：当前没有可清理的产物')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const run = useMutation({
    mutationFn: () => manageApi.cleanup({ dry_run: false }),
    onSuccess: (res) => {
      setConfirm(false)
      setPreview(null)
      invalidate()
      toast.success(
        res.deleted === 0
          ? '清理完成：没有需要删除的产物'
          : `清理完成：删除 ${res.deleted} 个对象、${res.files} 个文件，释放 ${formatBytes(res.bytes)}` +
              (res.kept > 0 ? `；${res.kept} 个跳过（重新被引用或删除失败，下轮再试）` : '')
      )
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const s = stats.data

  return (
    <Card
      title='清理孤立产物'
      desc='删除分集删除后、重新转封装后不再被引用的产物与各存储源上的文件。平时每天自动执行，这里用于提前触发。'
      icon={<Trash2 className='size-4' />}
    >
      <div className='flex flex-col gap-1 text-sm'>
        {stats.isError ? (
          <span className='text-destructive'>{errorMessage(stats.error)}</span>
        ) : s ? (
          <>
            <div className='flex flex-wrap items-center gap-2'>
              <span className='text-muted-foreground'>已标记</span>
              <Badge variant='outline' className='tabular-nums'>
                {s.total} 个
              </Badge>
              <span className='text-muted-foreground'>其中已过保留期</span>
              <Badge variant={s.expired > 0 ? 'secondary' : 'outline'} className='tabular-nums'>
                {s.expired} 个
              </Badge>
              <span className='tabular-nums text-muted-foreground'>占用 {formatBytes(s.bytes)}</span>
            </div>
            <span className='text-xs text-muted-foreground'>
              保留期 {s.retention_hours} 小时（约 {Math.round(s.retention_hours / 24)} 天），从首次发现不再被引用算起
            </span>
          </>
        ) : (
          <span className='text-muted-foreground'>读取中…</span>
        )}
        {preview && (
          <div className='mt-1 flex items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-2 text-xs'>
            <AlertTriangle className='size-4 shrink-0 text-amber-600' aria-hidden />
            <span>
              预览：会删除 {preview.deleted} 个对象，释放约 {formatBytes(preview.bytes)}。确认后不可恢复。
            </span>
          </div>
        )}
      </div>
      {editable && (
        <div className='flex flex-wrap gap-2'>
          <Button variant='outline' onClick={() => dryRun.mutate()} disabled={dryRun.isPending}>
            {dryRun.isPending ? '预览中…' : '预览'}
          </Button>
          <Button
            variant='destructive'
            onClick={() => setConfirm(true)}
            disabled={run.isPending || (s?.expired ?? 0) === 0}
          >
            {run.isPending ? '清理中…' : '立即清理'}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        destructive
        title='确认清理孤立产物？'
        desc={
          <div className='flex flex-col gap-2 text-sm'>
            <p>
              将删除 <span className='font-medium'>{preview?.deleted ?? s?.expired ?? 0}</span>{' '}
              个已过保留期的产物，连同各存储源上的物理文件。
            </p>
            <p className='text-muted-foreground'>
              这一步<span className='font-medium'>不可恢复</span>。删除前会再检查一次引用，仍被分集引用的会跳过；
              某个存储源上的文件删不掉时会保留记录、下轮再试。建议先点「预览」确认数量。
            </p>
          </div>
        }
        confirmText={run.isPending ? '清理中…' : '确认删除'}
        handleConfirm={() => run.mutate()}
        disabled={run.isPending}
      />
    </Card>
  )
}
