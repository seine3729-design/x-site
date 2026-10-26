import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Eye, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
  Checkbox,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import {
  contentApi,
  contentKeys,
  DISPLAY_STATUS_LABELS,
  DRAMA_SORT_LABELS,
  dramaListQuery,
  taxonomiesQuery,
  type DisplayStatus,
  type Drama,
  type DramaListParams,
  type DramaSort,
} from './api'
import { DramaStatusBadge } from './components/badges'
import { TextField } from './components/fields'
import { FilterSelect, ResetButton, SearchBox } from './components/list-toolbar'
import { Pager } from './components/pager'
import { SearchPicker } from './components/search-picker'
import { BulkBar } from './drama-bulk'
import { DEFAULT_PAGE_SIZE, withFilter } from './search'
import { searchTags } from './tags-page'
import { episodeSummary, joinList, lookupReason } from './utils'

/** 剧集列表（docs/06 第 4 节）。 */
export function DramasPage() {
  const can = useCan()
  const visible = can('dramas.view')
  const search = useSearch({ from: '/_authenticated/dramas/' })
  const navigate = useNavigate({ from: '/dramas/' })
  const params: DramaListParams = {
    q: search.q,
    status: search.status,
    tag_id: search.tag_id,
    term_id: search.term_id,
    sort: search.sort,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const tagsReason = lookupReason(can, 'tags')
  const termsReason = lookupReason(can, 'taxonomies')
  const list = useQuery({ ...dramaListQuery(params), enabled: visible })
  const taxonomies = useQuery({ ...taxonomiesQuery, enabled: visible && !termsReason })
  const [tagLabel, setTagLabel] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  if (!visible) return <NoPermission title='剧集' />
  const editable = can('dramas.edit')
  const filtered = !!(search.q || search.status || search.tag_id || search.term_id || search.sort)
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='剧集'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 新建剧集
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜剧名、别名或 #编号' onSubmit={(q) => setFilter({ q })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={(Object.entries(DISPLAY_STATUS_LABELS) as [DisplayStatus, string][]).map(([value, label]) => ({ value, label }))}
          onChange={(v) => setFilter({ status: v as DisplayStatus | undefined })}
          className='w-40'
        />
        <div className='flex items-center gap-1'>
          <WithReason reason={tagsReason}>
            <SearchPicker
              queryKey={contentKeys.tags}
              search={(q) => searchTags(q)}
              selected={search.tag_id ? [search.tag_id] : []}
              onSelect={(o) => {
                setTagLabel(o.label)
                setFilter({ tag_id: o.value })
              }}
              trigger={search.tag_id ? `标签：${tagLabel ?? `#${search.tag_id}`}` : '标签：全部'}
              placeholder='搜标签名称或 slug'
              ariaLabel='按标签筛选'
              disabled={!!tagsReason}
              className='h-9 w-44'
            />
          </WithReason>
          {search.tag_id && (
            <Button variant='ghost' size='icon' className='size-9' aria-label='清除标签筛选' onClick={() => setFilter({ tag_id: undefined })}>
              <X className='size-4' />
            </Button>
          )}
        </div>
        <Select
          value={search.term_id ? String(search.term_id) : 'all'}
          onValueChange={(v) => setFilter({ term_id: v === 'all' ? undefined : Number(v) })}
          disabled={!!termsReason}
        >
          <WithReason reason={termsReason}>
            <SelectTrigger className='h-9 w-44' aria-label='按维度取值筛选'>
              <SelectValue placeholder='维度取值' />
            </SelectTrigger>
          </WithReason>
          <SelectContent>
            <SelectItem value='all'>维度取值：全部</SelectItem>
            {(taxonomies.data ?? []).map((x) => (
              <SelectGroup key={x.id}>
                <SelectLabel>{x.name}</SelectLabel>
                {x.terms.map((t) => (
                  <SelectItem key={t.id} value={String(t.id)}>
                    {t.name}
                    {t.status === 'inactive' ? '（已停用）' : ''}
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
        <Select value={search.sort ?? 'default'} onValueChange={(v) => setFilter({ sort: v === 'default' ? undefined : (v as DramaSort) })}>
          <SelectTrigger className='h-9 w-48' aria-label='排序'>
            <SelectValue placeholder='排序' />
          </SelectTrigger>
          <SelectContent>
            {Object.entries(DRAMA_SORT_LABELS).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                排序：{label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        // 换页、换筛选条件时清空多选
        <DramaTable key={JSON.stringify(params)} rows={list.data?.list} loading={list.isPending} filtered={filtered} editable={editable} />
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
      {creating && <CreateDramaDialog onClose={() => setCreating(false)} />}
    </div>
  )
}

function DramaTable({
  rows,
  loading,
  filtered,
  editable,
}: {
  rows: Drama[] | undefined
  loading: boolean
  filtered: boolean
  editable: boolean
}) {
  const [selected, setSelected] = useState<number[]>([])
  const [deleting, setDeleting] = useState<Drama | null>(null)
  const list = rows ?? []
  const allChecked = list.length > 0 && list.every((d) => selected.includes(d.id))
  const someChecked = !allChecked && list.some((d) => selected.includes(d.id))
  const toggle = (id: number) => setSelected((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted'>
              {editable && (
                <TableHead className='w-10 pl-4'>
                  <Checkbox
                    aria-label='全选本页'
                    checked={allChecked ? true : someChecked ? 'indeterminate' : false}
                    onCheckedChange={() => setSelected(allChecked ? [] : list.map((d) => d.id))}
                  />
                </TableHead>
              )}
              <TableHead className={editable ? undefined : 'pl-4'}>编号</TableHead>
              <TableHead>剧名</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>集数</TableHead>
              <TableHead className='text-right'>排序值</TableHead>
              <TableHead className='text-right'>热度</TableHead>
              <TableHead>上架时间</TableHead>
              <TableHead>修改时间</TableHead>
              <TableHead className='pr-4'>
                <span className='sr-only'>操作</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={10} className='h-24 text-center text-muted-foreground'>
                  加载中…
                </TableCell>
              </TableRow>
            ) : list.length === 0 ? (
              <TableRow>
                <TableCell colSpan={10} className='h-24 text-center text-muted-foreground'>
                  {filtered ? '没有符合条件的剧' : '还没有剧，点右上角「新建剧集」开始录入'}
                </TableCell>
              </TableRow>
            ) : (
              list.map((d) => (
                <TableRow key={d.id} data-state={selected.includes(d.id) ? 'selected' : undefined}>
                  {editable && (
                    <TableCell className='pl-4'>
                      <Checkbox aria-label={`选择 ${d.title}`} checked={selected.includes(d.id)} onCheckedChange={() => toggle(d.id)} />
                    </TableCell>
                  )}
                  <TableCell className={editable ? 'tabular-nums' : 'pl-4 tabular-nums'}>#{d.id}</TableCell>
                  <TableCell>
                    <div className='flex max-w-xs flex-col'>
                      <Link to='/dramas/$id' params={{ id: String(d.id) }} className='truncate font-medium hover:underline'>
                        {d.title}
                      </Link>
                      {d.aliases.length > 0 && <span className='truncate text-xs text-muted-foreground'>别名：{joinList(d.aliases)}</span>}
                    </div>
                  </TableCell>
                  <TableCell>
                    <DramaStatusBadge status={d.display_status} publishAt={d.publish_at} />
                  </TableCell>
                  <TableCell className='whitespace-nowrap'>{episodeSummary(d)}</TableCell>
                  <TableCell className='text-right tabular-nums'>{d.sort_weight}</TableCell>
                  <TableCell className='text-right tabular-nums'>{d.hot_score}</TableCell>
                  <TableCell className='whitespace-nowrap tabular-nums'>{d.publish_at ? formatDateTime(d.publish_at) : '—'}</TableCell>
                  <TableCell className='whitespace-nowrap tabular-nums'>{formatDateTime(d.updated_at)}</TableCell>
                  <TableCell className='pr-4'>
                    <div className='flex justify-end gap-1'>
                      <Button asChild variant='ghost' size='icon' aria-label={`${editable ? '编辑' : '查看'} ${d.title}`}>
                        <Link to='/dramas/$id' params={{ id: String(d.id) }}>
                          {editable ? <Pencil className='size-4' /> : <Eye className='size-4' />}
                        </Link>
                      </Button>
                      <WithReason reason={readonlyReason}>
                        <Button variant='ghost' size='icon' disabled={!editable} aria-label={`删除 ${d.title}`} onClick={() => setDeleting(d)}>
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
      {editable && <BulkBar ids={selected} onClear={() => setSelected([])} />}
      {deleting && <DeleteDramaDialog drama={deleting} onClose={() => setDeleting(null)} />}
    </>
  )
}

const createSchema = z.object({
  title: z.string().trim().min(1, '请填写剧名').max(200, '剧名最多 200 个字'),
  summary: z.string().trim().max(5000, '简介最多 5000 个字'),
})

type CreateForm = z.infer<typeof createSchema>

function CreateDramaDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const form = useForm<z.input<typeof createSchema>, unknown, CreateForm>({
    resolver: zodResolver(createSchema),
    defaultValues: { title: '', summary: '' },
  })
  const mutation = useMutation({
    mutationFn: (v: CreateForm) => contentApi.createDrama(v),
    onSuccess: (d) => {
      toast.success(`已新建草稿 #${d.id}「${d.title}」，继续填写资料`)
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
      void navigate({ to: '/dramas/$id', params: { id: String(d.id) } })
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>新建剧集</DialogTitle>
          <DialogDescription>先填剧名与简介建成草稿，之后在编辑页补充封面、标签与分集。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='title' label='剧名' required placeholder='请输入剧名' />
            <TextField control={form.control} name='summary' label='简介' multiline rows={4} placeholder='选填，最多 5000 个字' />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '新建草稿'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

/** 放进回收站（列表与编辑页共用）。 */
export function DeleteDramaDialog({ drama, onClose, onDeleted }: { drama: Pick<Drama, 'id' | 'title'>; onClose: () => void; onDeleted?: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => contentApi.deleteDrama(drama.id),
    onSuccess: () => {
      toast.success(`已把 #${drama.id}「${drama.title}」放进回收站，30 天内可还原`)
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
      onDeleted?.()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`把「${drama.title}」放进回收站`}
      desc='前台立即不可见，分集与关联保留；30 天内可在「回收站」还原，之后彻底删除。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '放进回收站'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
