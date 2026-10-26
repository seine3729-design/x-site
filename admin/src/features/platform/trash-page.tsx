import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle, RotateCcw, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  expiryLabel,
  statusLabel,
  trashApi,
  trashKeys,
  trashListQuery,
  trashOverviewQuery,
  type TrashCategory,
  type TrashItem,
} from './trash-api'

type DialogState = { type: 'restore' | 'purge'; item: TrashItem }

/**
 * 回收站（docs/06 第 35 节，A4-3 / S4-7）：按分类查看已删除的内容，30 天内可还原，也可以提前彻底删除。
 * 设置类数据不进回收站（菜单、广告、专题等），误删看审计日志重建。
 */
export function TrashPage() {
  const can = useCan()
  const visible = can('trash.view')
  const qc = useQueryClient()
  const search = useSearch({ from: '/_authenticated/trash' })
  const navigate = useNavigate({ from: '/trash' })
  const category: TrashCategory = 'dramas'
  const overview = useQuery({ ...trashOverviewQuery, enabled: visible })
  const params = {
    category,
    q: search.q,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...trashListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  const refresh = () => void qc.invalidateQueries({ queryKey: trashKeys.all })

  const restore = useMutation({
    mutationFn: (item: TrashItem) => trashApi.restore(category, item.id),
    onSuccess: (res) => {
      setDialog(null)
      refresh()
      toast.success(`已还原「${res.title}」，恢复为${statusLabel(res.status)}`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const purge = useMutation({
    mutationFn: (item: TrashItem) => trashApi.purge(category, item.id),
    onSuccess: (res) => {
      setDialog(null)
      refresh()
      toast.success(
        res.extra > 0
          ? `已彻底删除「${res.title}」，连带 ${res.extra} 个分集`
          : `已彻底删除「${res.title}」`
      )
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='回收站' />
  const editable = can('trash.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const info = overview.data?.categories.find((c) => c.category === category)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='回收站'
        description={
          overview.data
            ? `删除的内容在这里保留 ${overview.data.retention_days} 天，期间可以还原；过期后由后台任务彻底删除，不可恢复。`
            : '删除的内容在这里保留一段时间，期间可以还原。'
        }
        readonly={!editable}
      />

      <section className='flex flex-col gap-2 rounded-md border p-4 text-sm'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='font-medium'>{info?.title ?? '剧集'}</span>
          <Badge variant='outline' className='tabular-nums'>
            {info?.count ?? 0} 条
          </Badge>
          {overview.data?.oldest_expires_at && (info?.count ?? 0) > 0 && (
            <span className='text-xs text-muted-foreground'>
              最近一条在 {new Date(overview.data.oldest_expires_at).toLocaleDateString('zh-CN')} 到期
            </span>
          )}
        </div>
        {info && <p className='text-xs text-muted-foreground'>{info.desc}</p>}
        <p className='text-xs text-muted-foreground'>
          设置类数据（前台菜单、广告、专题、线路、资源机等）<span className='font-medium'>不进回收站</span>
          ，误删请在「审计日志」里查改动前的内容重建。
        </p>
      </section>

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜剧名或标识'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <ResetButton disabled={!search.q} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>名称</TableHead>
                <TableHead>删除前状态</TableHead>
                <TableHead>附带内容</TableHead>
                <TableHead>删除时间</TableHead>
                <TableHead>到期</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                    {search.q ? '没有符合条件的内容' : '回收站是空的'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className='pl-4'>
                      <div className='flex flex-col'>
                        <span className='font-medium'>{item.title}</span>
                        {item.slug && <code className='text-xs text-muted-foreground'>{item.slug}</code>}
                      </div>
                    </TableCell>
                    <TableCell className='text-sm'>{statusLabel(item.status)}</TableCell>
                    <TableCell className='text-sm tabular-nums'>
                      {item.extra > 0 ? `${item.extra} 个分集` : '—'}
                    </TableCell>
                    <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                      {new Date(item.deleted_at).toLocaleString('zh-CN', { hour12: false })}
                    </TableCell>
                    <TableCell>
                      <Badge variant={item.days_left <= 3 ? 'destructive' : 'outline'} className='whitespace-nowrap'>
                        {expiryLabel(item.days_left)}
                      </Badge>
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`还原「${item.title}」`}
                            onClick={() => setDialog({ type: 'restore', item })}
                          >
                            <RotateCcw className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`彻底删除「${item.title}」`}
                            onClick={() => setDialog({ type: 'purge', item })}
                          >
                            <Trash2 className='size-4' />
                          </Button>
                        </WithReason>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {list.data && (
        <Pager
          page={list.data.page}
          pageSize={list.data.page_size}
          total={list.data.total}
          onChange={(page, page_size) =>
            void navigate({
              search: (prev) => ({
                ...prev,
                page: page > 1 ? page : undefined,
                page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size,
              }),
            })
          }
        />
      )}

      {dialog?.type === 'restore' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          title={`还原「${dialog.item.title}」？`}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                会恢复为<span className='font-medium'>{statusLabel(dialog.item.status)}</span>
                {dialog.item.extra > 0 && `，连带 ${dialog.item.extra} 个分集一起回来`}。
              </p>
              <p className='text-muted-foreground'>
                标签、维度取值与专题关联一直保留着，还原后内容是完整的。
              </p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={restore.isPending ? '还原中…' : '还原'}
          isLoading={restore.isPending}
          handleConfirm={() => restore.mutate(dialog.item)}
        />
      )}

      {dialog?.type === 'purge' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          destructive
          title={`彻底删除「${dialog.item.title}」？`}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <div className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-2'>
                <AlertTriangle className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
                <span>
                  <span className='font-medium'>不可恢复。</span>
                  {dialog.item.extra > 0 && `${dialog.item.extra} 个分集`}
                  以及标签、维度取值、专题关联会一并消失。
                </span>
              </div>
              <p className='text-muted-foreground'>
                这条内容本来会在 {expiryLabel(dialog.item.days_left)}时由后台任务自动删除。
                转码产物不在这一步删除，由孤立产物清理任务按自己的保留期回收。
              </p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={purge.isPending ? '删除中…' : '确认彻底删除'}
          isLoading={purge.isPending}
          handleConfirm={() => purge.mutate(dialog.item)}
        />
      )}
    </div>
  )
}
