import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Plus, Trash2 } from 'lucide-react'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SelectField, TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  BLOCK_ACTION_HINTS,
  BLOCK_ACTION_LABELS,
  BLOCK_ACTIONS,
  type BlockAction,
  type Blockword,
  blockwordGroupsQuery,
  blockwordListQuery,
  communityApi,
  communityKeys,
} from './api'

type DialogState = { type: 'create' } | { type: 'delete'; item: Blockword }

const ACTION_VARIANT: Record<BlockAction, 'destructive' | 'secondary' | 'outline'> = {
  block: 'destructive',
  mask: 'secondary',
  review: 'outline',
}

/** 屏蔽词（docs/06 第 27 节）：进程内 Aho-Corasick 自动机匹配，改后前台立即生效。改处理办法 = 先删再加。 */
export function BlockwordsPage() {
  const can = useCan()
  const visible = can('blockwords.view')
  const search = useSearch({ from: '/_authenticated/blockwords' })
  const navigate = useNavigate({ from: '/blockwords' })
  const params = {
    q: search.q,
    group: search.group,
    action: search.action,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...blockwordListQuery(params), enabled: visible })
  const groups = useQuery({ ...blockwordGroupsQuery(), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='屏蔽词' />
  const editable = can('blockwords.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.group || search.action)
  const groupOptions = (groups.data?.groups ?? []).map((g) => ({ value: g, label: g }))

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='屏蔽词'
        description='命中屏蔽词的处理：直接拦下 / 变成 *** / 先送审核。适用于评论、弹幕、昵称、个人简介与采集剧名。词按规范化（全角转半角、小写、去零宽字符）后匹配；改后前台立即生效。改处理办法请先删掉再重新添加。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 添加屏蔽词
            </Button>
          )
        }
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜屏蔽词' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='分组'
          value={search.group}
          options={groupOptions}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { group: v }) })}
        />
        <FilterSelect
          label='处理办法'
          value={search.action}
          options={BLOCK_ACTIONS.map((a) => ({ value: a, label: BLOCK_ACTION_LABELS[a] }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { action: v as BlockAction | undefined }) })}
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
                <TableHead>分组</TableHead>
                <TableHead>处理办法</TableHead>
                <TableHead>加入时间</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的屏蔽词' : '还没有屏蔽词：点右上角「添加屏蔽词」'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((w) => (
                  <TableRow key={w.id}>
                    <TableCell className='pl-4 font-medium break-all'>{w.word}</TableCell>
                    <TableCell>{w.group ? <Badge variant='outline'>{w.group}</Badge> : <span className='text-muted-foreground'>—</span>}</TableCell>
                    <TableCell>
                      <Badge variant={ACTION_VARIANT[w.action]}>{BLOCK_ACTION_LABELS[w.action]}</Badge>
                    </TableCell>
                    <TableCell className='text-sm whitespace-nowrap tabular-nums'>{formatDateTime(w.created_at).slice(0, 16)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end'>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除屏蔽词「${w.word}」`}
                            onClick={() => setDialog({ type: 'delete', item: w })}
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
              search: (prev) => ({ ...prev, page: page > 1 ? page : undefined, page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size }),
            })
          }
        />
      )}

      {dialog?.type === 'create' && <CreateBlockwordDialog onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && <DeleteBlockwordDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

const createSchema = z.object({
  word: z.string().trim().min(1, '请输入屏蔽词').max(100, '屏蔽词最多 100 个字'),
  group: z.string().trim().max(50, '分组最多 50 个字'),
  action: z.enum(['block', 'mask', 'review']),
})

function CreateBlockwordDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof createSchema>, unknown, z.output<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: { word: '', group: '', action: 'block' },
  })
  const action = form.watch('action')
  const mutation = useMutation({
    mutationFn: (v: z.output<typeof createSchema>) => communityApi.createBlockword(v),
    onSuccess: (saved) => {
      toast.success(`已添加「${saved.word}」，前台立即生效`)
      void qc.invalidateQueries({ queryKey: communityKeys.blockwords })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>添加屏蔽词</DialogTitle>
          <DialogDescription>一次加一个词，不分大小写。保存时全角转半角、英文转小写、去零宽字符，与匹配时的处理一致。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField
              control={form.control}
              name='word'
              label='屏蔽词'
              required
              placeholder='请输入一个屏蔽词，如 代开发票'
            />
            <TextField
              control={form.control}
              name='group'
              label='分组'
              placeholder='可留空；如 引流、辱骂、政治'
              description='「引流」分组用于识别简介与评论中的联系方式。'
            />
            <SelectField
              control={form.control}
              name='action'
              label='处理办法'
              required
              placeholder='请选择处理办法'
              items={BLOCK_ACTIONS.map((a) => ({ value: a, label: BLOCK_ACTION_LABELS[a] }))}
              description={BLOCK_ACTION_HINTS[action as BlockAction]}
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

function DeleteBlockwordDialog({ item, onClose }: { item: Blockword; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => communityApi.deleteBlockword(item.id),
    onSuccess: () => {
      toast.success(`已删除「${item.word}」`)
      void qc.invalidateQueries({ queryKey: communityKeys.blockwords })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${item.word}」`}
      desc='删除后该词不再拦截，前台立即生效。想改处理办法的话，删掉后重新添加即可。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
