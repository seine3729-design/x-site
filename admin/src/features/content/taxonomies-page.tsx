import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useBlocker, useNavigate, useSearch } from '@tanstack/react-router'
import { Pencil, Plus, Save, Trash2, Undo2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { cn } from '@/lib/utils'
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
  Skeleton,
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { contentApi, contentKeys, taxonomiesQuery, type Taxonomy, type Term } from './api'
import { ActiveBadge } from './components/badges'
import { SelectField, SwitchField, TextField } from './components/fields'
import { SortableList } from './components/sortable-list'
import { CODE_PATTERN, moveItem, sameOrder, SLUG_HINT, SLUG_PATTERN } from './utils'

type DialogState =
  | { type: 'create-taxonomy' }
  | { type: 'edit-taxonomy' | 'delete-taxonomy'; taxonomy: Taxonomy }
  | { type: 'create-term'; taxonomy: Taxonomy }
  | { type: 'edit-term' | 'delete-term'; taxonomy: Taxonomy; term: Term }

/** 筛选维度与取值（docs/06 第 11 节）：左侧维度、右侧所选维度的取值，都可以拖拽调整顺序。 */
export function TaxonomiesPage() {
  const can = useCan()
  const visible = can('taxonomies.view')
  const { t: selectedParam } = useSearch({ from: '/_authenticated/taxonomies' })
  const navigate = useNavigate({ from: '/taxonomies' })
  const taxonomies = useQuery({ ...taxonomiesQuery, enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='筛选维度' />
  const editable = can('taxonomies.edit')
  const list = taxonomies.data ?? []
  const selected = list.find((x) => x.id === selectedParam) ?? list[0]

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='筛选维度'
        description='探索页的分面筛选条。取值停用后前台筛选条不再显示；设为频道的取值有独立频道页 /c/<slug>。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create-taxonomy' })}>
              <Plus /> 新建维度
            </Button>
          )
        }
      />
      {taxonomies.isError ? (
        <LoadError onRetry={() => void taxonomies.refetch()} />
      ) : taxonomies.isPending ? (
        <Skeleton className='h-64 w-full' />
      ) : list.length === 0 ? (
        <Card>
          <CardContent className='py-10 text-center text-sm text-muted-foreground'>
            还没有筛选维度。{editable && '先新建一个维度（如「类型」「地区」），再在右侧添加取值。'}
          </CardContent>
        </Card>
      ) : (
        <div className='grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]'>
          <TaxonomyList
            list={list}
            selectedId={selected?.id}
            editable={editable}
            onSelect={(id) => void navigate({ search: { t: id }, replace: true })}
            onEdit={(taxonomy) => setDialog({ type: 'edit-taxonomy', taxonomy })}
            onDelete={(taxonomy) => setDialog({ type: 'delete-taxonomy', taxonomy })}
          />
          {selected && (
            <TermList
              // 换维度时丢弃上一个维度未保存的排序
              key={selected.id}
              taxonomy={selected}
              editable={editable}
              onCreate={() => setDialog({ type: 'create-term', taxonomy: selected })}
              onEdit={(term) => setDialog({ type: 'edit-term', taxonomy: selected, term })}
              onDelete={(term) => setDialog({ type: 'delete-term', taxonomy: selected, term })}
            />
          )}
        </div>
      )}

      {(dialog?.type === 'create-taxonomy' || dialog?.type === 'edit-taxonomy') && (
        <TaxonomyDialog
          taxonomy={dialog.type === 'edit-taxonomy' ? dialog.taxonomy : undefined}
          onSaved={(id) => void navigate({ search: { t: id }, replace: true })}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'delete-taxonomy' && <DeleteTaxonomyDialog taxonomy={dialog.taxonomy} onClose={() => setDialog(null)} />}
      {(dialog?.type === 'create-term' || dialog?.type === 'edit-term') && (
        <TermDialog taxonomy={dialog.taxonomy} term={dialog.type === 'edit-term' ? dialog.term : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete-term' && <DeleteTermDialog taxonomy={dialog.taxonomy} term={dialog.term} onClose={() => setDialog(null)} />}
    </div>
  )
}

/**
 * 未保存的排序：只记录 ID 的顺序，显示时用最新数据（改了名称也能立即看到）；
 * 新增的项排在最后，删除的项去掉。有改动时离开页面前提醒。
 */
function useOrderDraft<T extends { id: number }>(items: T[], what: string) {
  const [order, setOrder] = useState(() => items.map((x) => x.id))
  const byId = new Map(items.map((x) => [x.id, x]))
  const ids = [...order.filter((id) => byId.has(id)), ...items.filter((x) => !order.includes(x.id)).map((x) => x.id)]
  const dirty = !sameOrder(
    ids,
    items.map((x) => x.id)
  )
  useBlocker({
    shouldBlockFn: () => dirty && !window.confirm(`${what}的新顺序尚未保存，确定离开吗？`),
    enableBeforeUnload: () => dirty,
  })
  return {
    draft: ids.map((id) => byId.get(id) as T),
    dirty,
    move: (from: number, to: number) => setOrder(moveItem(ids, from, to)),
    reset: () => setOrder(items.map((x) => x.id)),
  }
}

function OrderBar({ dirty, saving, onSave, onReset }: { dirty: boolean; saving: boolean; onSave: () => void; onReset: () => void }) {
  if (!dirty) return null
  return (
    <div className='flex items-center justify-between gap-2 rounded-md border border-primary/40 bg-primary/5 px-3 py-2 text-sm'>
      <span>顺序已调整，保存后前台生效。</span>
      <div className='flex gap-2'>
        <Button size='sm' variant='ghost' onClick={onReset} disabled={saving}>
          <Undo2 className='size-4' /> 撤销
        </Button>
        <Button size='sm' onClick={onSave} disabled={saving}>
          <Save className='size-4' /> {saving ? '保存中…' : '保存顺序'}
        </Button>
      </div>
    </div>
  )
}

function TaxonomyList({
  list,
  selectedId,
  editable,
  onSelect,
  onEdit,
  onDelete,
}: {
  list: Taxonomy[]
  selectedId?: number
  editable: boolean
  onSelect: (id: number) => void
  onEdit: (t: Taxonomy) => void
  onDelete: (t: Taxonomy) => void
}) {
  const qc = useQueryClient()
  const order = useOrderDraft(list, '维度')
  const save = useMutation({
    mutationFn: () => contentApi.reorderTaxonomies(order.draft.map((x) => x.id)),
    onSuccess: () => {
      toast.success('已保存维度顺序')
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
    },
  })
  const reason = editable ? null : '当前角色只能查看'

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>维度</CardTitle>
        <CardDescription>点选一个维度查看它的取值。拖动手柄或用上下方向键调整顺序。</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        <OrderBar dirty={order.dirty} saving={save.isPending} onSave={() => save.mutate()} onReset={order.reset} />
        <SortableList
          items={order.draft}
          getKey={(x) => x.id}
          getLabel={(x) => x.name}
          onMove={order.move}
          disabled={!editable || save.isPending}
          renderItem={(x) => (
            <div className='flex items-center gap-2'>
              <button
                type='button'
                className={cn(
                  'flex min-w-0 flex-1 flex-col items-start rounded px-2 py-1 text-start hover:bg-muted',
                  x.id === selectedId && 'bg-muted'
                )}
                aria-current={x.id === selectedId ? 'true' : undefined}
                onClick={() => onSelect(x.id)}
              >
                <span className='flex items-center gap-2 font-medium'>
                  {x.name}
                  <Badge variant='outline'>{x.multi_select ? '多选' : '单选'}</Badge>
                  {!x.show_in_explore && <Badge variant='outline'>不在探索页</Badge>}
                  {x.status === 'inactive' && <ActiveBadge active={false} />}
                </span>
                <span className='text-xs text-muted-foreground'>
                  {x.slug} · {x.terms.length} 个取值
                </span>
              </button>
              <WithReason reason={reason}>
                <Button variant='ghost' size='icon' className='size-8' disabled={!editable} aria-label={`编辑维度 ${x.name}`} onClick={() => onEdit(x)}>
                  <Pencil className='size-4' />
                </Button>
              </WithReason>
              <WithReason reason={reason}>
                <Button variant='ghost' size='icon' className='size-8' disabled={!editable} aria-label={`删除维度 ${x.name}`} onClick={() => onDelete(x)}>
                  <Trash2 className='size-4' />
                </Button>
              </WithReason>
            </div>
          )}
        />
      </CardContent>
    </Card>
  )
}

function TermList({
  taxonomy,
  editable,
  onCreate,
  onEdit,
  onDelete,
}: {
  taxonomy: Taxonomy
  editable: boolean
  onCreate: () => void
  onEdit: (t: Term) => void
  onDelete: (t: Term) => void
}) {
  const qc = useQueryClient()
  const order = useOrderDraft(taxonomy.terms, `「${taxonomy.name}」的取值`)
  const save = useMutation({
    mutationFn: () => contentApi.reorderTerms(taxonomy.id, order.draft.map((x) => x.id)),
    onSuccess: () => {
      toast.success('已保存取值顺序')
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
    },
  })
  const reason = editable ? null : '当前角色只能查看'

  return (
    <Card>
      <CardHeader className='flex flex-row items-start justify-between gap-2'>
        <div className='flex flex-col gap-1.5'>
          <CardTitle className='text-base'>「{taxonomy.name}」的取值</CardTitle>
          <CardDescription>
            {taxonomy.multi_select ? '多选：一部剧可以选多个取值。' : '单选：一部剧最多选一个取值。'}
          </CardDescription>
        </div>
        {editable && (
          <Button size='sm' onClick={onCreate}>
            <Plus className='size-4' /> 新增取值
          </Button>
        )}
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        <OrderBar dirty={order.dirty} saving={save.isPending} onSave={() => save.mutate()} onReset={order.reset} />
        {order.draft.length === 0 ? (
          <p className='py-6 text-center text-sm text-muted-foreground'>这个维度还没有取值。</p>
        ) : (
          <SortableList
            items={order.draft}
            getKey={(x) => x.id}
            getLabel={(x) => x.name}
            onMove={order.move}
            disabled={!editable || save.isPending}
            renderItem={(x) => (
              <div className='flex items-center gap-2'>
                <div className='flex min-w-0 flex-1 flex-col'>
                  <span className='flex flex-wrap items-center gap-2 font-medium'>
                    {x.name}
                    {x.is_channel && <Badge variant='outline'>频道</Badge>}
                    {x.status === 'inactive' && <ActiveBadge active={false} />}
                  </span>
                  <span className='text-xs text-muted-foreground'>
                    {x.slug}
                    {x.code && ` · 编号 ${x.code}`}
                  </span>
                </div>
                <WithReason reason={reason}>
                  <Button variant='ghost' size='icon' className='size-8' disabled={!editable} aria-label={`编辑取值 ${x.name}`} onClick={() => onEdit(x)}>
                    <Pencil className='size-4' />
                  </Button>
                </WithReason>
                <WithReason reason={reason}>
                  <Button variant='ghost' size='icon' className='size-8' disabled={!editable} aria-label={`删除取值 ${x.name}`} onClick={() => onDelete(x)}>
                    <Trash2 className='size-4' />
                  </Button>
                </WithReason>
              </div>
            )}
          />
        )}
      </CardContent>
    </Card>
  )
}

const taxonomySchema = z.object({
  name: z.string().trim().min(1, '请填写名称').max(32, '名称最多 32 个字'),
  slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, SLUG_HINT),
  multi_select: z.boolean(),
  show_in_explore: z.boolean(),
  status: z.enum(['active', 'inactive']),
})

type TaxonomyForm = z.infer<typeof taxonomySchema>

function TaxonomyDialog({ taxonomy, onSaved, onClose }: { taxonomy?: Taxonomy; onSaved: (id: number) => void; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !taxonomy
  const form = useForm<z.input<typeof taxonomySchema>, unknown, TaxonomyForm>({
    resolver: zodResolver(taxonomySchema),
    defaultValues: {
      name: taxonomy?.name ?? '',
      slug: taxonomy?.slug ?? '',
      multi_select: taxonomy?.multi_select ?? false,
      show_in_explore: taxonomy?.show_in_explore ?? true,
      status: taxonomy?.status ?? 'active',
    },
  })
  const mutation = useMutation({
    mutationFn: (v: TaxonomyForm) => (creating ? contentApi.createTaxonomy(v) : contentApi.updateTaxonomy(taxonomy.id, v)),
    onSuccess: (saved) => {
      toast.success(creating ? `已新建维度「${saved.name}」` : '已保存')
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onSaved(saved.id)
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建维度' : `编辑维度：${taxonomy.name}`}</DialogTitle>
          <DialogDescription>探索页按「维度 slug + 取值 slug」筛选。新维度排在最后。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='名称' required placeholder='请输入维度名称，如 类型' />
            <TextField control={form.control} name='slug' label='slug' placeholder='选填，留空自动生成，如 genre' description={`${SLUG_HINT}。`} />
            <SwitchField
              control={form.control}
              name='multi_select'
              label='可多选'
              description='关闭为单选：一部剧最多选一个取值。已有剧选了多个取值时不能改为单选。'
            />
            <SwitchField control={form.control} name='show_in_explore' label='显示在探索页' description='关闭后只用于频道与区块，不出现在探索页筛选条。' />
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              items={[
                { label: '启用', value: 'active' },
                { label: '停用', value: 'inactive' },
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

const termSchema = z.object({
  name: z.string().trim().min(1, '请填写名称').max(32, '名称最多 32 个字'),
  slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, SLUG_HINT),
  status: z.enum(['active', 'inactive']),
  is_channel: z.boolean(),
  code: z.string().trim().regex(CODE_PATTERN, '字母、数字、下划线和连字符，最多 32 个字符'),
})

type TermForm = z.infer<typeof termSchema>

function TermDialog({ taxonomy, term, onClose }: { taxonomy: Taxonomy; term?: Term; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !term
  const form = useForm<z.input<typeof termSchema>, unknown, TermForm>({
    resolver: zodResolver(termSchema),
    defaultValues: {
      name: term?.name ?? '',
      slug: term?.slug ?? '',
      status: term?.status ?? 'active',
      is_channel: term?.is_channel ?? false,
      code: term?.code ?? '',
    },
  })
  const mutation = useMutation({
    mutationFn: (v: TermForm) =>
      creating ? contentApi.createTerm(taxonomy.id, v) : contentApi.updateTerm(taxonomy.id, term.id, v),
    onSuccess: (saved) => {
      toast.success(creating ? `已新增取值「${saved.name}」` : '已保存')
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{creating ? `在「${taxonomy.name}」中新增取值` : `编辑取值：${term.name}`}</DialogTitle>
          <DialogDescription>停用后前台筛选条不再显示该取值，频道页返回 404。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='名称' required placeholder='请输入取值名称，如 都市' />
            <TextField
              control={form.control}
              name='slug'
              label='slug'
              placeholder='选填，留空自动生成，如 city'
              description={`在本维度内唯一；设为频道时全站唯一。${SLUG_HINT}。`}
            />
            <SwitchField control={form.control} name='is_channel' label='设为频道' description='有独立频道页 /c/<slug>。' />
            <TextField
              control={form.control}
              name='code'
              label='编号'
              placeholder='选填，供区块与专题规则引用，如 city'
              description='填写时全站唯一。'
            />
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              items={[
                { label: '启用', value: 'active' },
                { label: '停用', value: 'inactive' },
              ]}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : creating ? '新增' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteTaxonomyDialog({ taxonomy, onClose }: { taxonomy: Taxonomy; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => contentApi.deleteTaxonomy(taxonomy.id),
    onSuccess: () => {
      toast.success(`已删除维度「${taxonomy.name}」`)
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除维度「${taxonomy.name}」`}
      desc={`它的 ${taxonomy.terms.length} 个取值与剧的关联一并删除，不可恢复。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function DeleteTermDialog({ taxonomy, term, onClose }: { taxonomy: Taxonomy; term: Term; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => contentApi.deleteTerm(taxonomy.id, term.id),
    onSuccess: () => {
      toast.success(`已删除取值「${term.name}」`)
      void qc.invalidateQueries({ queryKey: contentKeys.taxonomies })
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除取值「${term.name}」`}
      desc='剧与它的关联一并删除，不可恢复。只想让前台不显示时，可以改为停用。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
