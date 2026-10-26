import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { EyeOff, GitMerge, Pencil, Plus } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { http } from '@/lib/http'
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
import { contentApi, contentKeys, TAG_STATUS_LABELS, tagListQuery, type Page, type Tag } from './api'
import { SelectField, SwitchField, TextField } from './components/fields'
import { FilterSelect, ResetButton, SearchBox } from './components/list-toolbar'
import { Pager } from './components/pager'
import { SearchPicker, type PickerOption } from './components/search-picker'
import { DEFAULT_PAGE_SIZE, withFilter } from './search'
import { SLUG_HINT, SLUG_PATTERN } from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'merge'; tag: Tag }

/** 按名称或 slug 搜索未合并的标签（剧集编辑、批量打标签、合并目标共用）。 */
export async function searchTags(q: string, exclude?: number): Promise<PickerOption[]> {
  const page = await http.get<Page<Tag>>('/tags', { query: { q: q || undefined, merged: 'false', page_size: 50 } })
  return page.list
    .filter((t) => t.id !== exclude)
    .map((t) => ({
      value: t.id,
      label: t.name,
      hint: `#${t.id} · ${t.slug}${t.is_hidden ? ' · 敏感' : ''}${t.status === 'hidden' ? ' · 已隐藏' : ''}`,
    }))
}

/** 标签（docs/06 第 7 节）：不能删除，只能并入其他标签。 */
export function TagsPage() {
  const can = useCan()
  const visible = can('tags.view')
  const search = useSearch({ from: '/_authenticated/tags' })
  const navigate = useNavigate({ from: '/tags' })
  const merged = search.merged === undefined ? undefined : search.merged === 'true'
  const params = {
    q: search.q,
    status: search.status,
    merged,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...tagListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='标签' />
  const editable = can('tags.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status || search.merged)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='标签'
        description='敏感标签过年龄门才显示、不被搜索引擎收录；隐藏的标签不在前台显示。标签不能删除，重复的标签并入另一个。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建标签
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜名称或 slug' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(TAG_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as typeof search.status }) })}
        />
        <FilterSelect
          label='合并'
          value={search.merged}
          options={[
            { label: '未合并', value: 'false' },
            { label: '已并入其他标签', value: 'true' },
          ]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { merged: v as typeof search.merged }) })}
          className='w-44'
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
                <TableHead className='pl-4'>名称</TableHead>
                <TableHead>slug</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>合并</TableHead>
                <TableHead>创建时间</TableHead>
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
                    {filtered ? '没有符合条件的标签' : '还没有标签'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((t) => (
                  <TableRow key={t.id} className={t.merged_into_id ? 'text-muted-foreground' : undefined}>
                    <TableCell className='pl-4'>
                      <div className='flex items-center gap-2'>
                        <span className='font-medium'>{t.name}</span>
                        <span className='text-xs text-muted-foreground tabular-nums'>#{t.id}</span>
                        {t.is_hidden && (
                          <Badge variant='outline'>
                            <EyeOff aria-hidden /> 敏感
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <code className='text-xs'>{t.slug}</code>
                    </TableCell>
                    <TableCell>
                      {t.status === 'active' ? <Badge variant='secondary'>正常</Badge> : <Badge variant='outline'>隐藏</Badge>}
                    </TableCell>
                    <TableCell>
                      {t.merged_into_id ? (
                        <Badge variant='outline'>
                          <GitMerge aria-hidden /> 已并入 #{t.merged_into_id}
                        </Badge>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                    <TableCell className='tabular-nums'>{formatDateTime(t.created_at)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={readonlyReason}>
                          <Button variant='ghost' size='icon' disabled={!editable} aria-label={`编辑 ${t.name}`} onClick={() => setDialog({ type: 'edit', tag: t })}>
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason ?? (t.merged_into_id ? '已并入其他标签' : null)}>
                          <Button
                            variant='ghost'
                            size='sm'
                            disabled={!editable || !!t.merged_into_id}
                            onClick={() => setDialog({ type: 'merge', tag: t })}
                          >
                            <GitMerge className='size-4' /> 并入…
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
            void navigate({ search: (prev) => ({ ...prev, page: page > 1 ? page : undefined, page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size }) })
          }
        />
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <TagDialog tag={dialog.type === 'edit' ? dialog.tag : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'merge' && <MergeTagDialog tag={dialog.tag} onClose={() => setDialog(null)} />}
    </div>
  )
}

const tagSchema = z.object({
  name: z.string().trim().min(1, '请填写名称').max(32, '名称最多 32 个字'),
  slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, SLUG_HINT),
  is_hidden: z.boolean(),
  status: z.enum(['active', 'hidden']),
})

type TagForm = z.infer<typeof tagSchema>

function TagDialog({ tag, onClose }: { tag?: Tag; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !tag
  const form = useForm<z.input<typeof tagSchema>, unknown, TagForm>({
    resolver: zodResolver(tagSchema),
    defaultValues: { name: tag?.name ?? '', slug: tag?.slug ?? '', is_hidden: tag?.is_hidden ?? false, status: tag?.status ?? 'active' },
  })
  const mutation = useMutation({
    mutationFn: (v: TagForm) => (creating ? contentApi.createTag(v) : contentApi.updateTag(tag.id, v)),
    onSuccess: (saved) => {
      toast.success(creating ? `已新建标签「${saved.name}」，slug 为 ${saved.slug}` : '已保存')
      void qc.invalidateQueries({ queryKey: contentKeys.tags })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建标签' : `编辑标签：${tag.name}`}</DialogTitle>
          <DialogDescription>前台标签页地址为 /tag/&lt;slug&gt;。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='名称' required placeholder='请输入标签名称，如 甜宠' />
            <TextField
              control={form.control}
              name='slug'
              label='slug'
              placeholder='选填，留空自动生成，如 sweet'
              description={`${SLUG_HINT}。`}
            />
            <SwitchField control={form.control} name='is_hidden' label='敏感标签' description='过年龄门才显示，不被搜索引擎收录。' />
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              items={[
                { label: '正常', value: 'active' },
                { label: '隐藏（前台不显示）', value: 'hidden' },
              ]}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : creating ? '新建' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function MergeTagDialog({ tag, onClose }: { tag: Tag; onClose: () => void }) {
  const qc = useQueryClient()
  const [target, setTarget] = useState<PickerOption | null>(null)
  const mutation = useMutation({
    mutationFn: (targetId: number) => contentApi.mergeTag(tag.id, targetId),
    onSuccess: (res) => {
      toast.success(`已把「${tag.name}」并入 #${res.merged_into_id}，${res.moved_dramas} 部剧改挂到目标标签`)
      void qc.invalidateQueries({ queryKey: contentKeys.tags })
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`把「${tag.name}」并入其他标签`}
      desc='合并后它的剧改挂到目标标签，原先并入它的标签也改指目标；前台按目标标签展示。合并不能撤销。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '合并中…' : '确认合并'}
      disabled={!target}
      isLoading={mutation.isPending}
      handleConfirm={() => target && mutation.mutate(target.value)}
    >
      <div className='flex flex-col gap-2'>
        <span className='text-sm font-medium' id='merge-target-label'>
          目标标签
        </span>
        <SearchPicker
          queryKey={contentKeys.tags}
          search={(q) => searchTags(q, tag.id)}
          selected={target ? [target.value] : []}
          onSelect={setTarget}
          trigger={target ? `${target.label}（#${target.value}）` : '搜索并选择要并入的标签'}
          placeholder='搜名称或 slug'
          ariaLabel='目标标签'
          className='w-full'
        />
      </div>
    </ConfirmDialog>
  )
}
