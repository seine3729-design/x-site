import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Pencil, Plus, Trash2, X } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
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
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
} from '@/components/ui'
import { Form, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { contentKeys, RULE_SORT_LABELS, RULE_WINDOW_LABELS, type RuleSort, type RuleWindow } from '@/features/content/api'
import { SelectField, TextField } from '@/features/content/components/fields'
import { SearchPicker, type PickerOption } from '@/features/content/components/search-picker'
import { SortableList } from '@/features/content/components/sortable-list'
import { searchDramas } from '@/features/content/drama-search'
import { searchTags } from '@/features/content/tags-page'
import { searchTopics, TermCodePicker } from '@/features/content/topics-page'
import { lookupReason, moveItem, parseIdList, sameOrder } from '@/features/content/utils'
import { AD_SLOT_LABELS, AD_SLOTS, RANK_BOARD_LABELS, RANK_BOARDS } from '@/features/ops/api'
import {
  ACTIVE_LABELS,
  ANNOUNCE_KIND_LABELS,
  ANNOUNCE_KINDS,
  BLOCK_SOURCE_LABELS,
  BLOCK_SOURCE_TYPES,
  BLOCK_TYPE_HINTS,
  BLOCK_TYPE_LABELS,
  BLOCK_TYPES,
  blockListQuery,
  blockPagesQuery,
  HOME_PAGE,
  isDramaBlock,
  MAX_BLOCK_COLS,
  MAX_BLOCK_DRAMA_IDS,
  MAX_BLOCK_HTML,
  MAX_BLOCK_ITEMS,
  MAX_BLOCK_SUBTITLE,
  MAX_BLOCK_TAG_IDS,
  MAX_BLOCK_TERM_CODES,
  MAX_BLOCK_TITLE,
  MAX_BLOCK_TOPIC_IDS,
  MAX_BLOCKS_PER_PAGE,
  MENU_DEVICE_LABELS,
  MIN_BLOCK_COLS,
  MIN_BLOCK_ITEMS,
  PERM_BLOCK_CUSTOM_HTML,
  siteApi,
  siteKeys,
  type Block,
  type BlockPageRef,
  type BlockType,
} from './api'
import {
  BLOCK_LINK_HINT,
  blockHasList,
  blockPageLabel,
  blockSourceSummary,
  blockToForm,
  formToBlockInput,
  isLinkOrEmpty,
  type BlockForm,
} from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; item: Block }

/**
 * 页面区块编排（docs/06 第 15 节）：先选页面（首页或某个启用的频道页），再增删改与拖拽调序。
 * 改动后前台立即生效。自定义 HTML 区块另要 blocks.custom_html 权限。
 */
export function BlocksPage() {
  const can = useCan()
  const visible = can('blocks.view')
  const search = useSearch({ from: '/_authenticated/blocks' })
  const navigate = useNavigate({ from: '/blocks' })
  const page = search.page ?? HOME_PAGE
  const pages = useQuery({ ...blockPagesQuery(), enabled: visible })
  const list = useQuery({ ...blockListQuery(page), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='页面区块' />
  const editable = can('blocks.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const items = list.data?.list ?? []
  const options = pages.data?.list ?? [{ value: HOME_PAGE, label: '首页' }]
  const full = items.length >= MAX_BLOCKS_PER_PAGE
  const createReason = readonlyReason ?? (full ? `单个页面最多 ${MAX_BLOCKS_PER_PAGE} 个区块` : null)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='页面区块'
        description='首页与各频道页由区块拼装：选好页面后在这里增删改、拖拽调序，前台立即生效。停用的区块与当前取不到内容的列表区块前台不显示。'
        readonly={!editable}
      />

      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex flex-col gap-2'>
          <Label htmlFor='block-page'>编辑的页面</Label>
          <Select value={page} onValueChange={(next) => void navigate({ search: { page: next === HOME_PAGE ? undefined : next } })}>
            <SelectTrigger id='block-page' className='h-9 w-64'>
              <SelectValue placeholder='请选择页面' />
            </SelectTrigger>
            <SelectContent>
              {options.map((p: BlockPageRef) => (
                <SelectItem key={p.value} value={p.value}>
                  {p.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <p className='text-xs text-muted-foreground'>
          共 {items.length} 个区块（启用 {items.filter((b) => b.status === 'active').length} 个）。频道页只能选启用中的频道。
        </p>
      </div>

      {pages.isError ? (
        <LoadError onRetry={() => void pages.refetch()} />
      ) : list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : list.isPending ? (
        <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <BlockListCard
          // 服务端数据有任何变化（增删、改类型、改状态）都重新载入本地顺序
          key={`${page}:${JSON.stringify(items)}`}
          page={page}
          pageLabel={blockPageLabel(page, options)}
          items={items}
          disabledReason={readonlyReason}
          createReason={createReason}
          onCreate={() => setDialog({ type: 'create' })}
          onEdit={(item) => setDialog({ type: 'edit', item })}
          onDelete={(item) => setDialog({ type: 'delete', item })}
        />
      )}

      {dialog?.type === 'create' && <BlockDialog page={page} pages={options} onClose={() => setDialog(null)} />}
      {dialog?.type === 'edit' && <BlockDialog item={dialog.item} pages={options} onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && (
        <DeleteBlockDialog item={dialog.item} pageLabel={blockPageLabel(page, options)} onClose={() => setDialog(null)} />
      )}
    </div>
  )
}

/** 一个页面的区块：按前台显示顺序排列，拖动手柄、方向键或上移 / 下移调整，点「保存顺序」后生效。 */
function BlockListCard({
  page,
  pageLabel,
  items,
  disabledReason,
  createReason,
  onCreate,
  onEdit,
  onDelete,
}: {
  page: string
  pageLabel: string
  items: Block[]
  disabledReason: string | null
  createReason: string | null
  onCreate: () => void
  onEdit: (item: Block) => void
  onDelete: (item: Block) => void
}) {
  const qc = useQueryClient()
  const [order, setOrder] = useState(items)
  const changed = !sameOrder(
    order.map((b) => b.id),
    items.map((b) => b.id)
  )
  const save = useMutation({
    mutationFn: () =>
      siteApi.reorderBlocks(
        page,
        order.map((b) => b.id)
      ),
    onSuccess: () => {
      toast.success(`已保存「${pageLabel}」的区块顺序，前台立即生效`)
      void qc.invalidateQueries({ queryKey: siteKeys.blocks })
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>{pageLabel}的区块（从上到下就是前台的顺序）</CardTitle>
        <CardDescription>
          剧类区块可以手选剧、用某个专题或按规则自动选；广告位与公告条只占位，素材与内容由前台另取。调序时如果别人刚增删过，会提示刷新后重来。
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        {order.length === 0 ? (
          <p className='rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground'>这个页面还没有区块。</p>
        ) : (
          <SortableList
            items={order}
            getKey={(b) => b.id}
            getLabel={(b) => b.title || BLOCK_TYPE_LABELS[b.type]}
            onMove={(from, to) => setOrder((o) => moveItem(o, from, to))}
            disabled={!!disabledReason || save.isPending}
            renderItem={(b, i) => (
              <div className='flex flex-1 flex-wrap items-center gap-2 text-sm'>
                <span className='w-6 text-right text-xs text-muted-foreground tabular-nums'>{i + 1}</span>
                <Badge variant='secondary'>{BLOCK_TYPE_LABELS[b.type]}</Badge>
                <span className='font-medium break-all'>{b.title || '（没有标题）'}</span>
                <span className='text-xs break-all text-muted-foreground'>{blockSourceSummary(b)}</span>
                {blockHasList(b.type) && <span className='text-xs text-muted-foreground tabular-nums'>最多 {b.item_limit} 项</span>}
                {b.device !== 'all' && <Badge variant='outline'>{MENU_DEVICE_LABELS[b.device]}</Badge>}
                {b.status === 'inactive' && <Badge variant='outline'>已停用</Badge>}
                <div className='ms-auto flex items-center gap-1'>
                  <WithReason reason={disabledReason}>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-8'
                      disabled={!!disabledReason}
                      aria-label={`编辑「${b.title || BLOCK_TYPE_LABELS[b.type]}」`}
                      onClick={() => onEdit(b)}
                    >
                      <Pencil className='size-4' />
                    </Button>
                  </WithReason>
                  <WithReason reason={disabledReason}>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-8'
                      disabled={!!disabledReason}
                      aria-label={`删除「${b.title || BLOCK_TYPE_LABELS[b.type]}」`}
                      onClick={() => onDelete(b)}
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  </WithReason>
                </div>
              </div>
            )}
          />
        )}
        <div className='flex flex-wrap justify-end gap-2'>
          {changed && (
            <Button variant='outline' disabled={save.isPending} onClick={() => setOrder(items)}>
              还原顺序
            </Button>
          )}
          {order.length > 1 && (
            <WithReason reason={disabledReason}>
              <Button variant='secondary' disabled={!!disabledReason || !changed || save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? '保存中…' : '保存顺序'}
              </Button>
            </WithReason>
          )}
          <WithReason reason={createReason}>
            <Button disabled={!!createReason} onClick={onCreate}>
              <Plus /> 新建区块
            </Button>
          </WithReason>
        </div>
      </CardContent>
    </Card>
  )
}

const intField = (label: string, min: number, max: number) =>
  z
    .string()
    .trim()
    .regex(/^\d+$/, `${label}请填 ${min}–${max} 的整数`)
    .refine((v) => Number(v) >= min && Number(v) <= max, `${label}请填 ${min}–${max} 的整数`)

const blockSchema = z
  .object({
    page: z.string().min(1, '请选择页面'),
    type: z.enum(BLOCK_TYPES),
    title: z.string().trim().max(MAX_BLOCK_TITLE, `标题最多 ${MAX_BLOCK_TITLE} 个字`),
    subtitle: z.string().trim().max(MAX_BLOCK_SUBTITLE, `副标题最多 ${MAX_BLOCK_SUBTITLE} 个字`),
    item_limit: intField('数量', MIN_BLOCK_ITEMS, MAX_BLOCK_ITEMS),
    status: z.enum(['active', 'inactive']),
    more_link: z.string().trim().refine(isLinkOrEmpty, '请填站内路径（以 / 开头）或 http(s) 完整地址'),
    cols_desktop: intField('电脑每行个数', MIN_BLOCK_COLS, MAX_BLOCK_COLS),
    device: z.enum(['all', 'mobile', 'desktop']),
    source_type: z.enum(BLOCK_SOURCE_TYPES),
    drama_ids: z.array(z.object({ id: z.number(), title: z.string().optional() })).max(MAX_BLOCK_DRAMA_IDS, `手选最多 ${MAX_BLOCK_DRAMA_IDS} 部`),
    topic: z.object({ id: z.number(), label: z.string() }).nullable(),
    term_codes: z.array(z.string()).max(MAX_BLOCK_TERM_CODES, `取值编号最多 ${MAX_BLOCK_TERM_CODES} 个`),
    tag: z.object({ id: z.number(), label: z.string() }).nullable(),
    sort: z.enum(['latest', 'hot', 'recommend']),
    window: z.enum(['all', '7d', '14d', '30d', '90d']),
    rank_board: z.string(),
    ad_slot: z.string(),
    announce_kind: z.enum(ANNOUNCE_KINDS),
    topic_ids: z.array(z.object({ id: z.number(), label: z.string() })).max(MAX_BLOCK_TOPIC_IDS, `最多 ${MAX_BLOCK_TOPIC_IDS} 个专题`),
    tag_ids: z.array(z.object({ id: z.number(), label: z.string() })).max(MAX_BLOCK_TAG_IDS, `最多 ${MAX_BLOCK_TAG_IDS} 个标签`),
    custom_html: z.string().max(MAX_BLOCK_HTML, `自定义 HTML 最多 ${MAX_BLOCK_HTML} 个字符`),
  })
  .superRefine((v, ctx) => {
    // 按类型校验来源（与后端一致，先在前端拦下明显的缺项）
    if (isDramaBlock(v.type)) {
      if (v.source_type === 'manual' && v.drama_ids.length === 0) {
        ctx.addIssue({ code: 'custom', path: ['drama_ids'], message: '请至少选一部剧' })
      }
      if (v.source_type === 'topic' && !v.topic) {
        ctx.addIssue({ code: 'custom', path: ['topic'], message: '请选择专题' })
      }
    }
    if (v.type === 'rank' && !RANK_BOARDS.includes(v.rank_board as (typeof RANK_BOARDS)[number])) {
      ctx.addIssue({ code: 'custom', path: ['rank_board'], message: '请选择榜单' })
    }
    if (v.type === 'ad' && !AD_SLOTS.includes(v.ad_slot as (typeof AD_SLOTS)[number])) {
      ctx.addIssue({ code: 'custom', path: ['ad_slot'], message: '请选择广告位置' })
    }
    if (v.type === 'custom_html' && v.custom_html.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['custom_html'], message: '请填写自定义 HTML' })
    }
  })

function BlockDialog({ item, page, pages, onClose }: { item?: Block; page?: string; pages: readonly BlockPageRef[]; onClose: () => void }) {
  const qc = useQueryClient()
  const can = useCan()
  const creating = !item
  const canHTML = can(PERM_BLOCK_CUSTOM_HTML)
  // 原本是自定义 HTML 的区块，没有权限连改成别的类型都不行（后端同样拦）
  const lockedHTML = !canHTML && item?.type === 'custom_html'
  const form = useForm<z.input<typeof blockSchema>, unknown, BlockForm>({
    resolver: zodResolver(blockSchema),
    defaultValues: blockToForm(item, page),
  })
  const [type, sourceType] = useWatch({ control: form.control, name: ['type', 'source_type'] })
  const mutation = useMutation({
    mutationFn: (v: BlockForm) => {
      const body = formToBlockInput(v)
      return creating ? siteApi.createBlock(body) : siteApi.updateBlock(item.id, body)
    },
    onSuccess: (saved) => {
      toast.success(creating ? '已新建区块，前台立即生效' : '已保存，前台立即生效')
      if (saved.type === 'custom_html' && saved.custom_html.trim() !== form.getValues('custom_html').trim()) {
        toast.info('自定义 HTML 已按白名单清洗，保存的是清洗后的内容')
      }
      void qc.invalidateQueries({ queryKey: siteKeys.blocks })
      onClose()
    },
  })
  const typeItems = BLOCK_TYPES.filter((t) => t !== 'custom_html' || canHTML).map((value) => ({ value, label: BLOCK_TYPE_LABELS[value] }))
  const dramaBlock = isDramaBlock(type as BlockType)

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建区块' : `编辑区块 #${item.id}`}</DialogTitle>
          <DialogDescription>新建或改页面时排在该页面的末尾，顺序随后在列表里拖动调整。</DialogDescription>
        </DialogHeader>
        {lockedHTML ? (
          <div className='flex flex-col gap-4'>
            <p className='text-sm text-muted-foreground'>
              这是自定义 HTML 区块，修改需要「自定义 HTML」权限（默认只有超级管理员）。当前角色可以调序或删除它。
            </p>
            <pre className='max-h-60 overflow-auto rounded-md bg-muted p-3 text-xs break-all whitespace-pre-wrap'>{item.custom_html}</pre>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                关闭
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <Form {...form}>
            <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
              <div className='grid gap-4 sm:grid-cols-2'>
                <SelectField
                  control={form.control}
                  name='page'
                  label='页面'
                  required
                  placeholder='请选择页面'
                  items={pages.map((p) => ({ value: p.value, label: p.label }))}
                  description='只能选首页或启用中的频道页。'
                />
                <SelectField
                  control={form.control}
                  name='type'
                  label='类型'
                  required
                  placeholder='请选择类型'
                  items={typeItems}
                  description={BLOCK_TYPE_HINTS[type as BlockType]}
                />
              </div>
              <div className='grid gap-4 sm:grid-cols-2'>
                <TextField control={form.control} name='title' label='标题' placeholder='选填，如 热播推荐' />
                <TextField control={form.control} name='subtitle' label='副标题' placeholder='选填，显示在标题下方' />
              </div>

              {dramaBlock && (
                <SelectField
                  control={form.control}
                  name='source_type'
                  label='剧来源'
                  required
                  placeholder='请选择剧来源'
                  items={BLOCK_SOURCE_TYPES.map((value) => ({ value, label: BLOCK_SOURCE_LABELS[value] }))}
                />
              )}
              {dramaBlock && sourceType === 'manual' && (
                <FormField
                  control={form.control}
                  name='drama_ids'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        手选剧（按这个顺序显示，{(field.value ?? []).length} / {MAX_BLOCK_DRAMA_IDS}）
                      </FormLabel>
                      <DramaPicker value={field.value ?? []} onChange={field.onChange} />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {dramaBlock && sourceType === 'topic' && (
                <FormField
                  control={form.control}
                  name='topic'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>专题</FormLabel>
                      <OnePicker
                        kind='topic'
                        value={field.value ?? null}
                        onChange={field.onChange}
                        placeholder='搜索专题标题或 slug'
                        empty='请选择专题'
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {dramaBlock && sourceType === 'auto' && (
                <fieldset className='flex flex-col gap-4 rounded-md border p-3'>
                  <legend className='px-1 text-sm font-medium'>选剧规则</legend>
                  <p className='text-xs text-muted-foreground'>同时满足全部取值与标签的已上架剧，按排序取前若干部（与自动专题同一套规则）。</p>
                  <FormField
                    control={form.control}
                    name='term_codes'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>维度取值（按编号）</FormLabel>
                        <TermCodePicker value={field.value ?? []} onChange={field.onChange} />
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
                        <OnePicker
                          kind='tag'
                          value={field.value ?? null}
                          onChange={field.onChange}
                          placeholder='搜名称或 slug'
                          empty='不限（可选一个标签）'
                        />
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <div className='grid gap-4 sm:grid-cols-2'>
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
                  </div>
                </fieldset>
              )}

              {type === 'rank' && (
                <SelectField
                  control={form.control}
                  name='rank_board'
                  label='榜单'
                  required
                  placeholder='请选择榜单'
                  items={RANK_BOARDS.map((value) => ({ value, label: RANK_BOARD_LABELS[value] }))}
                  description='剧由榜单服务提供，后台每 5 分钟重新排名。'
                />
              )}
              {type === 'ad' && (
                <SelectField
                  control={form.control}
                  name='ad_slot'
                  label='广告位置'
                  required
                  placeholder='请选择广告位置'
                  items={AD_SLOTS.map((value) => ({ value, label: AD_SLOT_LABELS[value] }))}
                  description='这里只占位，素材在「广告位」里配；前台按设备与频控取。'
                />
              )}
              {type === 'announce' && (
                <SelectField
                  control={form.control}
                  name='announce_kind'
                  label='公告方式'
                  placeholder='请选择公告方式'
                  items={ANNOUNCE_KINDS.map((value) => ({ value, label: ANNOUNCE_KIND_LABELS[value] }))}
                  description='这里只占位，内容在「公告」里配。'
                />
              )}
              {type === 'topic_group' && (
                <FormField
                  control={form.control}
                  name='topic_ids'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        专题（按这个顺序显示，{(field.value ?? []).length} / {MAX_BLOCK_TOPIC_IDS}）
                      </FormLabel>
                      <ManyPicker
                        kind='topic'
                        value={field.value ?? []}
                        onChange={field.onChange}
                        placeholder='搜索专题标题或 slug'
                        empty='不指定时取「列在专题列表页」的已发布专题'
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {type === 'tag_cloud' && (
                <FormField
                  control={form.control}
                  name='tag_ids'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        标签（按这个顺序显示，{(field.value ?? []).length} / {MAX_BLOCK_TAG_IDS}）
                      </FormLabel>
                      <ManyPicker
                        kind='tag'
                        value={field.value ?? []}
                        onChange={field.onChange}
                        placeholder='搜名称或 slug'
                        empty='不指定时取热门标签'
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {type === 'custom_html' && (
                <TextField
                  control={form.control}
                  name='custom_html'
                  label='自定义 HTML'
                  required
                  multiline
                  rows={6}
                  placeholder='只允许常见排版标签、链接与图片；脚本、样式表、iframe 与事件属性会被去掉'
                  description='保存时按白名单清洗，清洗后为空会被拒绝。前台在沙箱中渲染。'
                />
              )}

              <div className='grid gap-4 sm:grid-cols-3'>
                {blockHasList(type as BlockType) && (
                  <TextField control={form.control} name='item_limit' label='最多显示' inputMode='numeric' placeholder='1–60，默认 24' />
                )}
                <TextField control={form.control} name='cols_desktop' label='电脑每行个数' inputMode='numeric' placeholder='2–8，默认 6' />
                <SelectField
                  control={form.control}
                  name='device'
                  label='设备'
                  placeholder='请选择设备'
                  items={Object.entries(MENU_DEVICE_LABELS).map(([value, label]) => ({ value, label }))}
                />
                <SelectField
                  control={form.control}
                  name='status'
                  label='状态'
                  placeholder='请选择状态'
                  items={Object.entries(ACTIVE_LABELS).map(([value, label]) => ({ value, label }))}
                />
              </div>
              <TextField
                control={form.control}
                name='more_link'
                label='查看全部链接'
                placeholder='如 /explore 或 https://example.com'
                description={BLOCK_LINK_HINT}
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
        )}
      </DialogContent>
    </Dialog>
  )
}

/** 手选剧：搜索添加或粘贴编号追加，拖拽调整显示顺序（与专题选剧一致）。 */
function DramaPicker({ value, onChange }: { value: { id: number; title?: string }[]; onChange: (v: { id: number; title?: string }[]) => void }) {
  const dramasReason = lookupReason(useCan(), 'dramas')
  const [paste, setPaste] = useState('')
  const pasted = parseIdList(paste)
  const add = (list: { id: number; title?: string }[]) => onChange([...value, ...list.filter((d) => !value.some((c) => c.id === d.id))])

  return (
    <div className='flex flex-col gap-2'>
      <div className='grid gap-2 sm:grid-cols-2'>
        {/* 搜剧要用「剧集」页面的读接口；没有查看权限时仍可粘贴编号 */}
        <WithReason reason={dramasReason}>
          <SearchPicker
            queryKey={contentKeys.dramas}
            search={searchDramas}
            selected={value.map((d) => d.id)}
            onSelect={(o: PickerOption) => add([{ id: o.value, title: o.label }])}
            trigger='搜索剧名、别名或 #编号'
            placeholder='输入剧名或 #编号'
            multiple
            ariaLabel='添加剧'
            disabled={!!dramasReason}
          />
        </WithReason>
        <div className='flex gap-2'>
          <Textarea
            rows={1}
            className='min-h-9'
            aria-label='粘贴剧编号'
            placeholder='粘贴编号，如 12, 15, #18'
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
      </div>
      {pasted.invalid.length > 0 && <p className='text-xs text-destructive'>无法识别：{pasted.invalid.join('、')}</p>}
      {value.length === 0 ? (
        <p className='rounded-md border border-dashed py-4 text-center text-xs text-muted-foreground'>还没有选剧。</p>
      ) : (
        <SortableList
          items={value}
          getKey={(d) => d.id}
          getLabel={(d) => d.title ?? `#${d.id}`}
          onMove={(from, to) => onChange(moveItem(value, from, to))}
          renderItem={(d, i) => (
            <div className='flex flex-1 items-center gap-3 text-sm'>
              <span className='w-6 text-right text-xs text-muted-foreground tabular-nums'>{i + 1}</span>
              <span className='flex-1 truncate'>{d.title ?? '（保存时校验是否存在）'}</span>
              <span className='text-xs text-muted-foreground tabular-nums'>#{d.id}</span>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-8'
                aria-label={`移出 ${d.title ?? `#${d.id}`}`}
                onClick={() => onChange(value.filter((x) => x.id !== d.id))}
              >
                <X className='size-4' />
              </Button>
            </div>
          )}
        />
      )}
    </div>
  )
}

type Picked = { id: number; label: string }

/** 专题 / 标签的搜索与权限：没有对应页面的查看权限时只显示已选的编号。 */
function usePickerSource(kind: 'topic' | 'tag') {
  const can = useCan()
  const reason = lookupReason(can, kind === 'topic' ? 'topics' : 'tags')
  return {
    reason,
    queryKey: kind === 'topic' ? contentKeys.topics : contentKeys.tags,
    search: kind === 'topic' ? searchTopics : (q: string) => searchTags(q),
  }
}

/** 选一个（专题来源、规则标签）。 */
function OnePicker({
  kind,
  value,
  onChange,
  placeholder,
  empty,
}: {
  kind: 'topic' | 'tag'
  value: Picked | null
  onChange: (v: Picked | null) => void
  placeholder: string
  empty: string
}) {
  const { reason, queryKey, search } = usePickerSource(kind)
  return (
    <div className='flex gap-2'>
      <WithReason reason={reason}>
        <SearchPicker
          queryKey={queryKey}
          search={search}
          selected={value ? [value.id] : []}
          onSelect={(o: PickerOption) => onChange({ id: o.value, label: o.label })}
          trigger={value ? value.label : empty}
          placeholder={placeholder}
          ariaLabel={kind === 'topic' ? '选择专题' : '选择标签'}
          disabled={!!reason}
          className='flex-1'
        />
      </WithReason>
      {value && (
        <Button type='button' variant='ghost' size='icon' aria-label='清除选择' onClick={() => onChange(null)}>
          <X className='size-4' />
        </Button>
      )}
    </div>
  )
}

/** 选多个并排序（专题组、标签云）。 */
function ManyPicker({
  kind,
  value,
  onChange,
  placeholder,
  empty,
}: {
  kind: 'topic' | 'tag'
  value: Picked[]
  onChange: (v: Picked[]) => void
  placeholder: string
  empty: string
}) {
  const { reason, queryKey, search } = usePickerSource(kind)
  return (
    <div className='flex flex-col gap-2'>
      <WithReason reason={reason}>
        <SearchPicker
          queryKey={queryKey}
          search={search}
          selected={value.map((v) => v.id)}
          onSelect={(o: PickerOption) => !value.some((v) => v.id === o.value) && onChange([...value, { id: o.value, label: o.label }])}
          trigger={kind === 'topic' ? '搜索并添加专题' : '搜索并添加标签'}
          placeholder={placeholder}
          multiple
          ariaLabel={kind === 'topic' ? '添加专题' : '添加标签'}
          disabled={!!reason}
        />
      </WithReason>
      {value.length === 0 ? (
        <p className='text-xs text-muted-foreground'>{empty}</p>
      ) : (
        <SortableList
          items={value}
          getKey={(v) => v.id}
          getLabel={(v) => v.label}
          onMove={(from, to) => onChange(moveItem(value, from, to))}
          renderItem={(v, i) => (
            <div className='flex flex-1 items-center gap-3 text-sm'>
              <span className='w-6 text-right text-xs text-muted-foreground tabular-nums'>{i + 1}</span>
              <span className='flex-1 truncate'>{v.label}</span>
              <span className='text-xs text-muted-foreground tabular-nums'>#{v.id}</span>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-8'
                aria-label={`移出 ${v.label}`}
                onClick={() => onChange(value.filter((x) => x.id !== v.id))}
              >
                <X className='size-4' />
              </Button>
            </div>
          )}
        />
      )}
    </div>
  )
}

function DeleteBlockDialog({ item, pageLabel, onClose }: { item: Block; pageLabel: string; onClose: () => void }) {
  const qc = useQueryClient()
  const name = item.title || BLOCK_TYPE_LABELS[item.type]
  const mutation = useMutation({
    mutationFn: () => siteApi.deleteBlock(item.id),
    onSuccess: () => {
      toast.success(`已删除「${name}」`)
      void qc.invalidateQueries({ queryKey: siteKeys.blocks })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除区块「${name}」`}
      desc={`删除后不可恢复，${pageLabel}立即不再显示这一块。只是暂时不显示的话，请改为停用。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
