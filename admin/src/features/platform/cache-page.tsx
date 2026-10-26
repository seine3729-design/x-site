import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Database, Eraser, Hash, Lock, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
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
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import {
  cacheApi,
  cacheKeys,
  cacheOverviewQuery,
  formatBytes,
  type CacheScopeInfo,
  type PurgeScope,
} from './api'

/**
 * 缓存（docs/06 第 28 节，S4-4）：按范围清除前台的读穿缓存，并展示暂存量与键前缀。
 * 只清白名单内的缓存键；会话、限流计数、尚未落库的统计与榜单本体在页面上标为不可清。
 */
export function CachePage() {
  const can = useCan()
  const visible = can('cache.view')
  const qc = useQueryClient()
  const overview = useQuery({ ...cacheOverviewQuery, enabled: visible })
  const [scope, setScope] = useState<PurgeScope | ''>('')
  const [dramaID, setDramaID] = useState('')
  const [confirm, setConfirm] = useState(false)

  const purge = useMutation({
    mutationFn: () => cacheApi.purge(scope as PurgeScope, selected?.needs_drama ? Number(dramaID) : undefined),
    onSuccess: (res) => {
      setConfirm(false)
      void qc.invalidateQueries({ queryKey: cacheKeys.overview })
      const title = selected?.title ?? res.scope
      const version = res.site_version ? `，站点版本已升到 ${res.site_version}` : ''
      toast.success(
        res.keys === 0
          ? `已清「${title}」：当前没有暂存内容${version}`
          : `已清「${title}」：删除 ${res.keys} 个缓存键${version}`
      )
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='缓存' />
  const editable = can('cache.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'

  const data = overview.data
  const selected: CacheScopeInfo | undefined = data?.scopes.find((s) => s.scope === scope)
  const ready = !!selected && (!selected.needs_drama || Number(dramaID) > 0)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='缓存'
        description='清除前台的读穿缓存。改了后台设置但前台没变化时，清对应范围即可；清「站点设置」还会让站点版本 +1，作废浏览器与 CDN 上的旧配置。'
        readonly={!editable}
        actions={
          <Button variant='outline' disabled={overview.isFetching} onClick={() => void overview.refetch()}>
            <RefreshCw className={overview.isFetching ? 'animate-spin' : ''} /> 刷新
          </Button>
        }
      />

      {overview.isError ? (
        <LoadError onRetry={() => void overview.refetch()} />
      ) : (
        <>
          <div className='grid gap-3 sm:grid-cols-3'>
            <StatCard
              icon={<Hash className='size-4' />}
              label='站点版本'
              value={data ? String(data.site_version) : '…'}
              hint='前台配置的 ETag 由它派生'
            />
            <StatCard
              icon={<Database className='size-4' />}
              label='暂存量'
              value={data ? `${data.total_keys} 个键` : '…'}
              hint='整个 Valkey 库，含会话等有状态数据'
            />
            <StatCard
              icon={<Database className='size-4' />}
              label='内存占用'
              value={data?.memory_text || '—'}
              hint={data ? formatBytes(data.memory_used) : ''}
            />
          </div>

          <section className='flex flex-col gap-3 rounded-md border p-4'>
            <div className='flex flex-col gap-0.5'>
              <h2 className='font-medium'>清除缓存</h2>
              <p className='text-xs text-muted-foreground'>
                清除后下一次请求会回源重建，期间会有一小段响应变慢。不会影响登录状态与尚未落库的统计。
              </p>
            </div>
            <div className='flex flex-wrap items-end gap-3'>
              <div className='flex flex-col gap-2'>
                <label htmlFor='cache-scope' className='text-sm text-muted-foreground'>
                  范围
                </label>
                <Select
                  value={scope}
                  onValueChange={(v) => {
                    setScope(v as PurgeScope)
                    setDramaID('')
                  }}
                  disabled={!editable || !data}
                >
                  <SelectTrigger id='cache-scope' className='w-56'>
                    <SelectValue placeholder='请选择要清除的范围' />
                  </SelectTrigger>
                  <SelectContent>
                    {(data?.scopes ?? []).map((s) => (
                      <SelectItem key={s.scope} value={s.scope}>
                        {s.title}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {selected?.needs_drama && (
                <div className='flex flex-col gap-2'>
                  <label htmlFor='cache-drama' className='text-sm text-muted-foreground'>
                    剧集编号
                  </label>
                  <Input
                    id='cache-drama'
                    type='number'
                    inputMode='numeric'
                    className='w-40'
                    placeholder='请输入剧集编号，如 1024'
                    value={dramaID}
                    onChange={(e) => setDramaID(e.target.value)}
                    disabled={!editable}
                  />
                </div>
              )}
              <WithReason reason={readonlyReason}>
                <Button
                  variant={scope === 'all' || scope === 'site_config' ? 'destructive' : 'default'}
                  disabled={!editable || !ready || purge.isPending}
                  onClick={() => setConfirm(true)}
                >
                  <Eraser /> {purge.isPending ? '清除中…' : '清除'}
                </Button>
              </WithReason>
            </div>
            {selected && <p className='text-sm text-muted-foreground'>{selected.desc}</p>}
          </section>

          <section className='flex flex-col gap-2'>
            <h2 className='font-medium'>键前缀</h2>
            <p className='text-xs text-muted-foreground'>
              下半部分是有状态数据，清缓存不会动它们：清掉会让人掉线、重置限流额度或丢掉尚未落库的统计。
            </p>
            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow className='bg-muted'>
                    <TableHead className='pl-4'>前缀</TableHead>
                    <TableHead>用途</TableHead>
                    <TableHead className='w-24 text-right'>键数</TableHead>
                    <TableHead className='w-56 pr-4'>是否可清</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {overview.isPending ? (
                    <TableRow>
                      <TableCell colSpan={4} className='h-24 text-center text-muted-foreground'>
                        加载中…
                      </TableCell>
                    </TableRow>
                  ) : (
                    (data?.prefixes ?? []).map((p) => (
                      <TableRow key={p.prefix}>
                        <TableCell className='pl-4'>
                          <code className='text-xs'>{p.prefix}</code>
                        </TableCell>
                        <TableCell className='text-sm'>{p.title}</TableCell>
                        <TableCell className='text-right text-sm tabular-nums'>{p.keys}</TableCell>
                        <TableCell className='pr-4'>
                          {p.purgeable ? (
                            <Badge variant='secondary'>可清</Badge>
                          ) : (
                            <div className='flex items-start gap-1.5'>
                              <Lock className='mt-0.5 size-3.5 shrink-0 text-muted-foreground' aria-hidden />
                              <span className='text-xs text-muted-foreground'>{p.reason}</span>
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </section>
        </>
      )}

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        destructive={scope === 'all' || scope === 'site_config'}
        title={`确认清除「${selected?.title ?? ''}」缓存？`}
        desc={
          <div className='flex flex-col gap-2 text-sm'>
            <p>{selected?.desc}</p>
            {selected?.needs_drama && <p>剧集编号：{dramaID}</p>}
            {(scope === 'site_config' || scope === 'all') && (
              <p className='text-muted-foreground'>
                这个范围会让<span className='font-medium'>站点版本 +1</span>
                ，所有客户端与 CDN 上的配置缓存一并作废、下次访问重新拉取。
              </p>
            )}
            <p className='text-muted-foreground'>
              清除后下一次请求回源重建，期间响应会慢一些。登录状态、限流计数与尚未落库的统计不受影响。
            </p>
          </div>
        }
        cancelBtnText='取消'
        confirmText={purge.isPending ? '清除中…' : '确认清除'}
        isLoading={purge.isPending}
        handleConfirm={() => purge.mutate()}
      />
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  hint,
}: {
  icon: React.ReactNode
  label: string
  value: string
  hint?: string
}) {
  return (
    <div className='flex flex-col gap-1 rounded-md border p-4'>
      <div className='flex items-center gap-2 text-sm text-muted-foreground'>
        <span aria-hidden>{icon}</span>
        {label}
      </div>
      <span className='text-2xl font-semibold tabular-nums'>{value}</span>
      {hint && <span className='text-xs text-muted-foreground'>{hint}</span>}
    </div>
  )
}
