import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ListOrdered, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useFieldArray, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { http } from '@/lib/http'
import { cn } from '@/lib/utils'
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
  Label,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
} from '@/components/ui'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Form, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import {
  contentApi,
  contentKeys,
  RULE_SORT_LABELS,
  RULE_WINDOW_LABELS,
  SOURCE_TYPE_LABELS,
  taxonomiesQuery,
  TOPIC_STATUS_LABELS,
  topicListQuery,
  topicQuery,
  type DisplayStatus,
  type Page,
  type RuleSort,
  type RuleWindow,
  type Topic,
  type TopicInput,
} from './api'
import { DramaStatusBadge } from './components/badges'
import { SelectField, SwitchField, TextField } from './components/fields'
import { FilterSelect, ResetButton, SearchBox } from './components/list-toolbar'
import { Pager } from './components/pager'
import { SearchPicker, type PickerOption } from './components/search-picker'
import { SortableList } from './components/sortable-list'
import { searchDramas } from './drama-search'
import { DEFAULT_PAGE_SIZE, withFilter } from './search'
import { searchTags } from './tags-page'
import { lookupReason, moveItem, parseIdList, sameOrder, SLUG_HINT, SLUG_PATTERN } from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'dramas' | 'delete'; topic: Topic }

/** 专题（docs/06 第 14 节）。 */
export function TopicsPage() {
  const can = useCan()
  const visible = can('topics.view')
  const search = useSearch({ from: '/_authenticated/topics' })
  const navigate = useNavigate({ from: '/topics' })
  const params = {
    q: search.q,
    status: search.status,
    source_type: search.source_type,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...topicListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='专题' />
  const editable = can('topics.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status || search.source_type)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='专题'
        description='手选专题按填写的顺序展示剧；规则自动的专题按规则从已上架的剧中选（前台读接口上线后生效）。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建专题
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜标题、简介或 slug'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(TOPIC_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as typeof search.status }) })}
        />
        <FilterSelect
          label='剧来源'
          value={search.source_type}
          options={Object.entries(SOURCE_TYPE_LABELS).map(([value, label]) => ({ value, label }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { source_type: v as typeof search.source_type }) })}
          className='w-40'
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
                <TableHead className='pl-4'>标题</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>剧来源</TableHead>
                <TableHead>列在专题列表页</TableHead>
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
                    {filtered ? '没有符合条件的专题' : '还没有专题'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell className='pl-4'>
                      <div className='flex flex-col'>
                        <span className='font-medium'>{t.title}</span>
                        <code className='text-xs text-muted-foreground'>{t.slug}</code>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={t.status === 'published' ? 'default' : 'secondary'}>{TOPIC_STATUS_LABELS[t.status]}</Badge>
                    </TableCell>
                    <TableCell>{SOURCE_TYPE_LABELS[t.source_type]}</TableCell>
                    <TableCell>{t.list_in_index ? '是' : '否'}</TableCell>
                    <TableCell className='tabular-nums'>{formatDateTime(t.created_at)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={t.source_type === 'auto' ? '规则自动的专题按规则选剧，不能手选' : null}>
                          <Button
                            variant='ghost'
                            size='sm'
                            disabled={t.source_type === 'auto'}
                            onClick={() => setDialog({ type: 'dramas', topic: t })}
                          >
                            <ListOrdered className='size-4' /> 选剧
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`编辑 ${t.title}`}
                            onClick={() => setDialog({ type: 'edit', topic: t })}
                          >
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除 ${t.title}`}
                            onClick={() => setDialog({ type: 'delete', topic: t })}
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

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <TopicDialog topic={dialog.type === 'edit' ? dialog.topic : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'dramas' && <TopicDramasDialog topic={dialog.topic} editable={editable} onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && <DeleteTopicDialog topic={dialog.topic} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 按标题或 slug 搜索专题（页面区块的专题来源与专题组共用）。 */
export async function searchTopics(q: string): Promise<PickerOption[]> {
  const page = await http.get<Page<Topic>>('/topics', { query: { q: q || undefined, page_size: 20 } })
  return page.list.map((t) => ({
    value: t.id,
    label: t.title,
    hint: `#${t.id} · ${t.slug}${t.status === 'draft' ? ' · 草稿' : ''}${t.source_type === 'auto' ? ' · 自动' : ''}`,
  }))
}

const topicSchema = z
  .object({
    title: z.string().trim().min(1, '请填写标题').max(100, '标题最多 100 个字'),
    slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, SLUG_HINT),
    summary: z.string().trim().max(2000, '简介最多 2000 个字'),
    status: z.enum(['draft', 'published']),
    source_type: z.enum(['manual', 'auto']),
    term_codes: z.array(z.string()).max(10, '取值编号最多 10 个'),
    tag: z.object({ id: z.number(), label: z.string() }).nullable(),
    sort: z.enum(['latest', 'hot', 'recommend']),
    window: z.enum(['all', '7d', '14d', '30d', '90d']),
    limit: z.string().regex(/^\d+$/, '请填写 1–60 的整数'),
    faq: z
      .array(
        z.object({
          question: z.string().trim().min(1, '请填写问题').max(200, '问题最多 200 个字'),
          answer: z.string().trim().min(1, '请填写回答').max(2000, '回答最多 2000 个字'),
        })
      )
      .max(20, 'FAQ 最多 20 条'),
    list_in_index: z.boolean(),
  })
  .refine((v) => Number(v.limit) >= 1 && Number(v.limit) <= 60, { path: ['limit'], message: '请填写 1–60 的整数' })

type TopicForm = z.infer<typeof topicSchema>

function toInput(v: TopicForm): TopicInput {
  return {
    title: v.title,
    slug: v.slug,
    summary: v.summary,
    status: v.status,
    source_type: v.source_type,
    rule: { term_codes: v.term_codes, tag_id: v.tag?.id ?? null, sort: v.sort, window: v.window, limit: Number(v.limit) },
    faq: v.faq,
    list_in_index: v.list_in_index,
  }
}

function TopicDialog({ topic, onClose }: { topic?: Topic; onClose: () => void }) {
  const qc = useQueryClient()
  const tagsReason = lookupReason(useCan(), 'tags')
  const creating = !topic
  const form = useForm<z.input<typeof topicSchema>, unknown, TopicForm>({
    resolver: zodResolver(topicSchema),
    defaultValues: {
      title: topic?.title ?? '',
      slug: topic?.slug ?? '',
      summary: topic?.summary ?? '',
      status: topic?.status ?? 'draft',
      source_type: topic?.source_type ?? 'manual',
      term_codes: topic?.rule.term_codes ?? [],
      tag: topic?.rule.tag_id ? { id: topic.rule.tag_id, label: `标签 #${topic.rule.tag_id}` } : null,
      sort: topic?.rule.sort ?? 'latest',
      window: topic?.rule.window ?? 'all',
      limit: String(topic?.rule.limit ?? 24),
      faq: topic?.faq ?? [],
      list_in_index: topic?.list_in_index ?? true,
    },
  })
  const faq = useFieldArray({ control: form.control, name: 'faq' })
  const sourceType = useWatch({ control: form.control, name: 'source_type' })
  const mutation = useMutation({
    mutationFn: (v: TopicForm) => (creating ? contentApi.createTopic(toInput(v)) : contentApi.updateTopic(topic.id, toInput(v))),
    onSuccess: (saved) => {
      toast.success(creating ? `已新建专题「${saved.title}」` : '已保存')
      void qc.invalidateQueries({ queryKey: contentKeys.topics })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建专题' : `编辑专题：${topic.title}`}</DialogTitle>
          <DialogDescription>前台专题页地址为 /topics/&lt;slug&gt;。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='title' label='标题' required placeholder='请输入专题标题' />
              <TextField control={form.control} name='slug' label='slug' placeholder='选填，留空自动生成，如 best-2026' />
            </div>
            <TextField control={form.control} name='summary' label='简介' multiline placeholder='选填，最多 2000 个字' />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={[
                  { label: '草稿', value: 'draft' },
                  { label: '发布', value: 'published' },
                ]}
              />
              <SelectField
                control={form.control}
                name='source_type'
                label='剧来源'
                placeholder='请选择剧来源'
                items={[
                  { label: '手选（保存后点「选剧」）', value: 'manual' },
                  { label: '规则自动', value: 'auto' },
                ]}
              />
            </div>

            {sourceType === 'auto' && (
              <fieldset className='flex flex-col gap-4 rounded-md border p-3'>
                <legend className='px-1 text-sm font-medium'>选剧规则</legend>
                <p className='text-xs text-muted-foreground'>同时满足全部取值与标签的已上架剧，按排序取前若干部。</p>
                <FormField
                  control={form.control}
                  name='term_codes'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>维度取值（按编号）</FormLabel>
                      <TermCodePicker value={field.value} onChange={field.onChange} />
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='tag'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>标签</FormLabel>
                      <div className='flex gap-2'>
                        <WithReason reason={tagsReason}>
                          <SearchPicker
                            queryKey={contentKeys.tags}
                            search={(q) => searchTags(q)}
                            selected={field.value ? [field.value.id] : []}
                            onSelect={(o) => field.onChange({ id: o.value, label: o.label })}
                            trigger={field.value ? field.value.label : '不限（可选一个标签）'}
                            placeholder='搜名称或 slug'
                            ariaLabel='规则标签'
                            disabled={!!tagsReason}
                            className='flex-1'
                          />
                        </WithReason>
                        {field.value && (
                          <Button type='button' variant='ghost' size='icon' aria-label='清除标签' onClick={() => field.onChange(null)}>
                            <X className='size-4' />
                          </Button>
                        )}
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <div className='grid gap-4 sm:grid-cols-3'>
                  <SelectField
                    control={form.control}
                    name='sort'
                    label='排序'
                    placeholder='请选择排序'
                    items={(Object.entries(RULE_SORT_LABELS) as [RuleSort, string][]).map(([value, label]) => ({ value, label }))}
                  />
                  <SelectField
                    control={form.control}
                    name='window'
                    label='上新时间'
                    placeholder='请选择上新时间'
                    items={(Object.entries(RULE_WINDOW_LABELS) as [RuleWindow, string][]).map(([value, label]) => ({ value, label }))}
                  />
                  <TextField control={form.control} name='limit' label='数量' inputMode='numeric' placeholder='1–60，默认 24' />
                </div>
              </fieldset>
            )}

            <fieldset className='flex flex-col gap-3 rounded-md border p-3'>
              <legend className='px-1 text-sm font-medium'>FAQ（{faq.fields.length} / 20）</legend>
              {faq.fields.length === 0 && <p className='text-xs text-muted-foreground'>选填。专题页底部的问答，按顺序显示。</p>}
              {faq.fields.map((f, i) => (
                <div key={f.id} className='flex flex-col gap-2 rounded-md bg-muted/50 p-2'>
                  <div className='flex items-center justify-between'>
                    <span className='text-xs text-muted-foreground'>第 {i + 1} 条</span>
                    <div className='flex'>
                      <Button type='button' variant='ghost' size='sm' disabled={i === 0} onClick={() => faq.move(i, i - 1)}>
                        上移
                      </Button>
                      <Button type='button' variant='ghost' size='sm' onClick={() => faq.remove(i)}>
                        删除
                      </Button>
                    </div>
                  </div>
                  <TextField control={form.control} name={`faq.${i}.question`} label='问题' placeholder='如：在哪里可以看全集？' />
                  <TextField control={form.control} name={`faq.${i}.answer`} label='回答' multiline rows={2} placeholder='请输入回答（纯文本）' />
                </div>
              ))}
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='self-start'
                disabled={faq.fields.length >= 20}
                onClick={() => faq.append({ question: '', answer: '' })}
              >
                <Plus className='size-4' /> 添加一条
              </Button>
            </fieldset>

            <SwitchField control={form.control} name='list_in_index' label='列在专题列表页' description='关闭后只能通过链接访问。' />
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

/** 选择规则引用的取值编号：列出所有设置了编号的取值（按维度分组）。 */
/** 按编号选维度取值（自动专题与页面区块的自动来源共用）。 */
export function TermCodePicker({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  // 列出取值要用「筛选维度」页面的读接口；没有查看权限时只显示已选的编号，不能新增
  const termsReason = lookupReason(useCan(), 'taxonomies')
  const taxonomies = useQuery({ ...taxonomiesQuery, enabled: !termsReason })
  const [open, setOpen] = useState(false)
  const options = (taxonomies.data ?? []).flatMap((x) =>
    x.terms.filter((t) => t.code).map((t) => ({ code: t.code, label: `${x.name} · ${t.name}`, inactive: t.status === 'inactive' }))
  )
  const labelOf = (code: string) =>
    options.find((o) => o.code === code)?.label ?? (taxonomies.isSuccess ? `编号 ${code}（已不存在）` : `编号 ${code}`)
  const toggle = (code: string) => onChange(value.includes(code) ? value.filter((c) => c !== code) : [...value, code])

  return (
    <div className='flex flex-col gap-2'>
      {value.length > 0 && (
        <div className='flex flex-wrap gap-1'>
          {value.map((code) => (
            <Badge key={code} variant='secondary' className='gap-1'>
              {labelOf(code)}
              <button type='button' aria-label={`移除 ${labelOf(code)}`} onClick={() => toggle(code)}>
                <X className='size-3' />
              </button>
            </Badge>
          ))}
        </div>
      )}
      <WithReason reason={termsReason}>
        <Popover modal open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button type='button' variant='outline' disabled={!!termsReason} className='justify-start font-normal text-muted-foreground'>
              {termsReason || options.length > 0 || taxonomies.isPending
                ? '选择取值（可多选，不选为不限）'
                : '还没有设置编号的取值（在「筛选维度」中给取值填编号）'}
            </Button>
          </PopoverTrigger>
          <PopoverContent className='w-80 p-0' align='start'>
            <Command>
              <CommandInput placeholder='搜维度或取值' />
              <CommandList>
                <CommandEmpty>没有找到</CommandEmpty>
                <CommandGroup>
                  {options.map((o) => (
                    <CommandItem key={o.code} value={`${o.label} ${o.code}`} onSelect={() => toggle(o.code)}>
                      <span className={cn('size-4 rounded-sm border', value.includes(o.code) && 'bg-primary')} aria-hidden />
                      <span className='flex-1'>{o.label}</span>
                      <code className='text-xs text-muted-foreground'>{o.code}</code>
                      {o.inactive && <span className='text-xs text-muted-foreground'>已停用</span>}
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </WithReason>
    </div>
  )
}

/** 选剧列表中的一项；粘贴编号添加的剧在保存前不知道剧名。 */
type PickedDrama = { id: number; title?: string; display_status?: DisplayStatus; publish_at?: string | null }

/** 手选专题的剧：按剧名或编号添加，拖拽排序；保存时整体替换（未列出的移出专题）。 */
function TopicDramasDialog({ topic, editable, onClose }: { topic: Topic; editable: boolean; onClose: () => void }) {
  const detail = useQuery(topicQuery(topic.id))
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>选剧：{topic.title}</DialogTitle>
          <DialogDescription>按列表顺序展示；保存后未列出的剧移出专题。回收站中的剧不显示，还原后回到原位置。</DialogDescription>
        </DialogHeader>
        {detail.isError ? (
          <LoadError onRetry={() => void detail.refetch()} />
        ) : detail.isPending ? (
          <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
        ) : (
          <TopicDramasEditor
            topic={topic}
            initial={detail.data.dramas.map((d) => ({ id: d.id, title: d.title, display_status: d.display_status, publish_at: d.publish_at }))}
            editable={editable}
            onClose={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function TopicDramasEditor({ topic, initial, editable, onClose }: { topic: Topic; initial: PickedDrama[]; editable: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const dramasReason = lookupReason(useCan(), 'dramas')
  const [items, setItems] = useState(initial)
  const [paste, setPaste] = useState('')
  const dirty = !sameOrder(
    items.map((d) => d.id),
    initial.map((d) => d.id)
  )
  const add = (list: PickedDrama[]) => setItems((cur) => [...cur, ...list.filter((d) => !cur.some((c) => c.id === d.id))])
  const save = useMutation({
    mutationFn: () =>
      contentApi.setTopicDramas(
        topic.id,
        items.map((d) => d.id)
      ),
    onSuccess: (res) => {
      toast.success(`已保存，专题中有 ${res.drama_ids.length} 部剧`)
      void qc.invalidateQueries({ queryKey: contentKeys.topic(topic.id) })
      onClose()
    },
  })
  const close = () => {
    if (dirty && !window.confirm('选剧的修改尚未保存，确定关闭吗？')) return
    onClose()
  }
  const pasted = parseIdList(paste)

  return (
    <div className='flex flex-col gap-4'>
      {editable && (
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='flex flex-col gap-2'>
            <Label>按剧名或编号添加</Label>
            {/* 搜剧要用「剧集」页面的读接口；没有查看权限时仍可粘贴编号追加 */}
            <WithReason reason={dramasReason}>
              <SearchPicker
                queryKey={contentKeys.dramas}
                search={searchDramas}
                selected={items.map((d) => d.id)}
                onSelect={(o: PickerOption) => add([{ id: o.value, title: o.label }])}
                trigger='搜索剧名、别名或 #编号'
                placeholder='输入剧名或 #编号'
                multiple
                ariaLabel='添加剧'
                disabled={!!dramasReason}
              />
            </WithReason>
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='topic-paste'>粘贴剧编号</Label>
            <div className='flex gap-2'>
              <Textarea
                id='topic-paste'
                rows={1}
                className='min-h-9'
                placeholder='如 12, 15, #18（按顺序追加）'
                value={paste}
                onChange={(e) => setPaste(e.target.value)}
              />
              <Button
                type='button'
                variant='secondary'
                disabled={pasted.ids.length === 0}
                onClick={() => {
                  add(pasted.ids.map((id) => ({ id })))
                  setPaste('')
                }}
              >
                追加
              </Button>
            </div>
            {pasted.invalid.length > 0 && <p className='text-xs text-destructive'>无法识别：{pasted.invalid.join('、')}</p>}
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>专题里还没有剧。</p>
      ) : (
        <SortableList
          items={items}
          getKey={(d) => d.id}
          getLabel={(d) => d.title ?? `#${d.id}`}
          onMove={(from, to) => setItems(moveItem(items, from, to))}
          disabled={!editable || save.isPending}
          renderItem={(d, i) => (
            <div className='flex items-center gap-3'>
              <span className='w-6 text-right text-xs text-muted-foreground tabular-nums'>{i + 1}</span>
              <div className='flex min-w-0 flex-1 flex-col'>
                <span className='truncate font-medium'>{d.title ?? '（保存时校验是否存在）'}</span>
                <span className='text-xs text-muted-foreground tabular-nums'>#{d.id}</span>
              </div>
              {d.display_status && <DramaStatusBadge status={d.display_status} publishAt={d.publish_at ?? null} />}
              {editable && (
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='size-8'
                  aria-label={`移出 ${d.title ?? `#${d.id}`}`}
                  onClick={() => setItems(items.filter((x) => x.id !== d.id))}
                >
                  <X className='size-4' />
                </Button>
              )}
            </div>
          )}
        />
      )}

      <DialogFooter>
        <Button type='button' variant='outline' onClick={close}>
          {editable ? '取消' : '关闭'}
        </Button>
        {editable && (
          <Button type='button' disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? '保存中…' : `保存（${items.length} 部）`}
          </Button>
        )}
      </DialogFooter>
    </div>
  )
}

function DeleteTopicDialog({ topic, onClose }: { topic: Topic; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => contentApi.deleteTopic(topic.id),
    onSuccess: () => {
      toast.success(`已删除专题「${topic.title}」`)
      void qc.invalidateQueries({ queryKey: contentKeys.topics })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除专题「${topic.title}」`}
      desc='删除后不可恢复（不进回收站），专题里的剧不受影响。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
