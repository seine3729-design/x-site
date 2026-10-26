import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Pin, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { SortableList } from '@/features/content/components/sortable-list'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import { moveItem, sameOrder } from '@/features/content/utils'
import { HOT_BOARD_SIZE, HOT_SOURCE_LABELS, hotListQuery, opsApi, opsKeys, pinnedHotQuery, type HotWord } from './api'
import { hotPatchMessage, type HotPatch } from './utils'

type DialogState = { type: 'create' } | { type: 'delete'; item: HotWord }

/** 地址栏里的布尔筛选 → 接口参数。 */
const boolParam = (b: boolean | undefined) => (b === undefined ? undefined : b ? ('true' as const) : ('false' as const))

/** 热搜词（docs/06 第 22 节）：热搜榜 = 置顶的词（按顺序）+ 显示中的词按搜索次数倒序，共 10 个。 */
export function HotsearchPage() {
  const can = useCan()
  const visible = can('hotsearch.view')
  const search = useSearch({ from: '/_authenticated/hotsearch' })
  const navigate = useNavigate({ from: '/hotsearch' })
  const params = {
    q: search.q,
    visible: boolParam(search.visible),
    pinned: boolParam(search.pinned),
    missing: search.missing ? ('true' as const) : undefined,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...hotListQuery(params), enabled: visible })
  const pinned = useQuery({ ...pinnedHotQuery(), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const qc = useQueryClient()
  const update = useMutation({
    mutationFn: ({ item, patch }: { item: HotWord; patch: HotPatch }) => opsApi.updateHotWord(item.id, patch),
    onSuccess: (_, { item, patch }) => {
      toast.success(`${hotPatchMessage(item, patch)}，前台立即生效`)
      void qc.invalidateQueries({ queryKey: opsKeys.hotsearch })
    },
  })

  if (!visible) return <NoPermission title='热搜词' />
  const editable = can('hotsearch.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.visible || search.pinned || search.missing)
  const pinnedList = pinned.data?.list ?? []
  const pinFull = pinnedList.length >= HOT_BOARD_SIZE
  const busy = (w: HotWord) => update.isPending && update.variables?.item.id === w.id

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='热搜词'
        description='热搜榜 = 置顶的词（按下面的顺序）+ 显示中的词按搜索次数倒序，共 10 个。用户搜过的词自动收录、默认不显示，搜索次数每小时更新；「片库没有」的次数多，说明该采集这部剧了。修改后前台立即生效。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 手动加词
            </Button>
          )
        }
      />

      {pinned.isError ? (
        <LoadError onRetry={() => void pinned.refetch()} />
      ) : (
        <PinnedCard key={pinnedList.map((w) => w.id).join(',')} items={pinnedList} loading={pinned.isPending} disabledReason={readonlyReason} />
      )}

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜热搜词' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='显示'
          value={boolParam(search.visible)}
          options={[
            { value: 'true', label: '显示中' },
            { value: 'false', label: '不显示' },
          ]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { visible: v === undefined ? undefined : v === 'true' }) })}
        />
        <FilterSelect
          label='置顶'
          value={boolParam(search.pinned)}
          options={[
            { value: 'true', label: '已置顶' },
            { value: 'false', label: '未置顶' },
          ]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { pinned: v === undefined ? undefined : v === 'true' }) })}
        />
        <FilterSelect
          label='片库'
          value={search.missing ? 'true' : undefined}
          className='w-40'
          options={[{ value: 'true', label: '片库没有的词' }]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { missing: v === 'true' ? true : undefined }) })}
        />
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>词</TableHead>
                <TableHead>来源</TableHead>
                <TableHead className='text-right'>搜索次数</TableHead>
                <TableHead className='text-right'>片库没有</TableHead>
                <TableHead>最后搜索</TableHead>
                <TableHead className='text-center'>显示</TableHead>
                <TableHead className='text-center'>置顶</TableHead>
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
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的热搜词' : '还没有热搜词：用户搜索后会自动收录，也可以手动加词'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((w) => {
                  const pinReason = readonlyReason ?? (!w.pinned && pinFull ? `最多置顶 ${HOT_BOARD_SIZE} 个，请先取消别的置顶` : null)
                  return (
                    <TableRow key={w.id}>
                      <TableCell className='pl-4'>
                        <div className='flex items-center gap-2'>
                          {w.pinned && <Pin className='size-4 shrink-0 text-primary' aria-label='已置顶' />}
                          <span className='font-medium break-all'>{w.word}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant='outline'>{HOT_SOURCE_LABELS[w.source]}</Badge>
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>{w.search_count.toLocaleString('zh-CN')}</TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {w.miss_count > 0 ? <span className='font-medium text-destructive'>{w.miss_count.toLocaleString('zh-CN')}</span> : '0'}
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                        {w.last_searched_at ? formatDateTime(w.last_searched_at).slice(0, 16) : '—'}
                      </TableCell>
                      <TableCell className='text-center'>
                        <WithReason reason={readonlyReason}>
                          <Switch
                            checked={w.visible}
                            disabled={!editable || busy(w)}
                            aria-label={`在热搜榜显示「${w.word}」`}
                            onCheckedChange={(on) => update.mutate({ item: w, patch: { visible: on } })}
                          />
                        </WithReason>
                      </TableCell>
                      <TableCell className='text-center'>
                        <WithReason reason={pinReason}>
                          <Switch
                            checked={w.pinned}
                            disabled={!!pinReason || busy(w)}
                            aria-label={`置顶「${w.word}」`}
                            onCheckedChange={(on) => update.mutate({ item: w, patch: { pinned: on } })}
                          />
                        </WithReason>
                      </TableCell>
                      <TableCell className='pr-4'>
                        <div className='flex justify-end'>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`删除热搜词「${w.word}」`}
                              onClick={() => setDialog({ type: 'delete', item: w })}
                            >
                              <Trash2 className='size-4' />
                            </Button>
                          </WithReason>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })
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
              search: (prev) => ({ ...prev, page: page > 1 ? page : undefined, page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size }),
            })
          }
        />
      )}

      {dialog?.type === 'create' && <CreateHotWordDialog onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && <DeleteHotWordDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 置顶词：拖动手柄、方向键或上移 / 下移调整顺序，点「保存顺序」后生效。服务端顺序变化时（key 变化）重新载入。 */
function PinnedCard({ items, loading, disabledReason }: { items: HotWord[]; loading: boolean; disabledReason: string | null }) {
  const qc = useQueryClient()
  const [order, setOrder] = useState(items)
  const changed = !sameOrder(
    order.map((w) => w.id),
    items.map((w) => w.id)
  )
  const save = useMutation({
    mutationFn: () => opsApi.reorderHotWords(order.map((w) => w.id)),
    onSuccess: () => {
      toast.success('已保存置顶顺序，前台立即生效')
      void qc.invalidateQueries({ queryKey: opsKeys.hotsearch })
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          置顶词（{items.length} / {HOT_BOARD_SIZE}）
        </CardTitle>
        <CardDescription>排在热搜榜最前面；在下面的列表里打开「置顶」开关添加。置顶的词一定显示。</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        {loading ? (
          <p className='text-sm text-muted-foreground'>加载中…</p>
        ) : order.length === 0 ? (
          <p className='text-sm text-muted-foreground'>还没有置顶词。</p>
        ) : (
          <SortableList
            items={order}
            getKey={(w) => w.id}
            getLabel={(w) => w.word}
            onMove={(from, to) => setOrder((o) => moveItem(o, from, to))}
            disabled={!!disabledReason || save.isPending}
            renderItem={(w, i) => (
              <div className='flex items-center gap-3 text-sm'>
                <span className='w-6 text-right text-muted-foreground tabular-nums'>{i + 1}</span>
                <span className='font-medium break-all'>{w.word}</span>
                <span className='ms-auto text-xs text-muted-foreground tabular-nums'>搜索 {w.search_count.toLocaleString('zh-CN')} 次</span>
              </div>
            )}
          />
        )}
        {order.length > 1 && (
          <div className='flex justify-end gap-2'>
            <Button variant='outline' disabled={!changed || save.isPending} onClick={() => setOrder(items)}>
              还原
            </Button>
            <WithReason reason={disabledReason}>
              <Button disabled={!!disabledReason || !changed || save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? '保存中…' : '保存顺序'}
              </Button>
            </WithReason>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

const createSchema = z.object({
  word: z.string().trim().min(1, '请输入热搜词').max(50, '热搜词最多 50 个字'),
})

function CreateHotWordDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof createSchema>, unknown, z.output<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: { word: '' },
  })
  const mutation = useMutation({
    mutationFn: (v: z.output<typeof createSchema>) => opsApi.createHotWord(v.word),
    onSuccess: (saved) => {
      toast.success(`已添加「${saved.word}」，前台立即生效`)
      void qc.invalidateQueries({ queryKey: opsKeys.hotsearch })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>手动加词</DialogTitle>
          <DialogDescription>手动加的词默认显示在热搜榜（按搜索次数排位，置顶后排在最前）。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField
              control={form.control}
              name='word'
              label='热搜词'
              required
              placeholder='请输入热搜词，如 霸道总裁'
              description='保存时全角转半角、英文转小写、合并空格，与用户搜索时的处理一致。'
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '添加中…' : '添加'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteHotWordDialog({ item, onClose }: { item: HotWord; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => opsApi.deleteHotWord(item.id),
    onSuccess: () => {
      toast.success(`已删除「${item.word}」`)
      void qc.invalidateQueries({ queryKey: opsKeys.hotsearch })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${item.word}」`}
      desc={`连同搜索次数（${item.search_count.toLocaleString('zh-CN')} 次）一起删除，不可恢复；之后再被搜到会重新自动收录（默认不显示）。只是不想上榜的话，请关掉「显示」。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
