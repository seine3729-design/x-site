import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Copy, Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
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
import { ImageUploadControl } from '@/features/media/upload-field'
import { SelectField, SwitchField, TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import { SLUG_HINT, SLUG_PATTERN } from '@/features/content/utils'
import {
  MAX_PAGE_BODY,
  MAX_PAGE_SEO_DESC,
  MAX_PAGE_SEO_TITLE,
  MAX_PAGE_TITLE,
  PAGE_STATUS_LABELS,
  pageListQuery,
  siteApi,
  siteKeys,
  type StaticPage,
} from './api'
import { RichTextField } from './rich-text-field'
import { bodySummary, bodyWasSanitized, formToPageInput, pagePath, pageToForm, type StaticPageForm } from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; item: StaticPage }

/** 复制到剪贴板。非安全上下文（非 localhost 的 http）下 clipboard 不可用，这时提示手动复制。 */
async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(`已复制${label}`)
  } catch {
    toast.error('当前浏览器不允许自动复制，请手动选中后复制')
  }
}

/** 静态页（docs/06 第 13 节）：条款、隐私、FAQ 等，前台地址 /p/<slug>。正文保存时按白名单清洗。 */
export function PagesPage() {
  const can = useCan()
  const visible = can('pages.view')
  const search = useSearch({ from: '/_authenticated/pages' })
  const navigate = useNavigate({ from: '/pages' })
  const params = {
    q: search.q,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...pageListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='静态页' />
  const editable = can('pages.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='静态页'
        description='条款、隐私、FAQ 等页面，前台地址是 /p/<slug>。草稿不对前台显示；标记为页脚页的已发布页会出现在前台页脚「帮助与条款」。正文保存时按白名单清洗，脚本、样式表、iframe 与事件属性会被去掉。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建静态页
            </Button>
          )
        }
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜标题或 slug' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(PAGE_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as typeof search.status }) })}
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
                <TableHead>正文摘要</TableHead>
                <TableHead className='text-center'>页脚页</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>最后修改</TableHead>
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
                    {filtered ? '没有符合条件的静态页' : '还没有静态页：新建后填上条款、隐私等内容，并在「前台菜单」里加入口'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className='pl-4'>
                      <div className='flex flex-col gap-1'>
                        <span className='font-medium break-all'>{p.title}</span>
                        <div className='flex items-center gap-1'>
                          <span className='text-xs break-all text-muted-foreground'>{pagePath(p.slug)}</span>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='size-6'
                            aria-label={`复制「${p.title}」的前台地址`}
                            onClick={() => void copyText(pagePath(p.slug), '前台地址')}
                          >
                            <Copy className='size-3' />
                          </Button>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className='text-sm text-muted-foreground'>
                      <span className='line-clamp-2 max-w-sm wrap-break-word'>{bodySummary(p.body)}</span>
                    </TableCell>
                    <TableCell className='text-center text-sm'>{p.is_footer ? '是' : '—'}</TableCell>
                    <TableCell>
                      <Badge variant={p.status === 'published' ? 'default' : 'secondary'}>{PAGE_STATUS_LABELS[p.status]}</Badge>
                    </TableCell>
                    <TableCell className='text-sm whitespace-nowrap tabular-nums'>{formatDateTime(p.updated_at).slice(0, 16)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`编辑「${p.title}」`}
                            onClick={() => setDialog({ type: 'edit', item: p })}
                          >
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除「${p.title}」`}
                            onClick={() => setDialog({ type: 'delete', item: p })}
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

      {dialog?.type === 'create' && <PageDialog onClose={() => setDialog(null)} />}
      {dialog?.type === 'edit' && <PageDialog item={dialog.item} onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && <DeletePageDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

const pageSchema = z.object({
  title: z.string().trim().min(1, '请填写标题').max(MAX_PAGE_TITLE, `标题最多 ${MAX_PAGE_TITLE} 个字`),
  slug: z
    .string()
    .trim()
    .refine((v) => v !== '' && SLUG_PATTERN.test(v.toLowerCase()), `请填写 slug：${SLUG_HINT}`),
  body: z.string().trim().min(1, '请填写正文').max(MAX_PAGE_BODY, `正文最多 ${MAX_PAGE_BODY} 个字符`),
  status: z.enum(['draft', 'published']),
  seo_title: z.string().trim().max(MAX_PAGE_SEO_TITLE, `SEO 标题最多 ${MAX_PAGE_SEO_TITLE} 个字`),
  seo_desc: z.string().trim().max(MAX_PAGE_SEO_DESC, `SEO 简介最多 ${MAX_PAGE_SEO_DESC} 个字`),
  is_footer: z.boolean(),
})

function PageDialog({ item, onClose }: { item?: StaticPage; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<z.input<typeof pageSchema>, unknown, StaticPageForm>({
    resolver: zodResolver(pageSchema),
    defaultValues: pageToForm(item),
  })
  const slug = useWatch({ control: form.control, name: 'slug' })
  const mutation = useMutation({
    mutationFn: (v: StaticPageForm) => {
      const body = formToPageInput(v)
      return creating ? siteApi.createPage(body) : siteApi.updatePage(item.id, body)
    },
    onSuccess: (saved, v) => {
      toast.success(creating ? `已新建静态页 #${saved.id}` : '已保存')
      if (bodyWasSanitized(v.body, saved.body)) {
        toast.info('正文已按白名单清洗，不允许的标签或属性已去掉，可再次打开查看保存结果')
      }
      void qc.invalidateQueries({ queryKey: siteKeys.pages })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建静态页' : `编辑静态页 #${item.id}`}</DialogTitle>
          <DialogDescription>
            正文保存时按白名单清洗（只保留常见排版标签、链接与图片），保存后这里显示的是清洗结果。需要图片时使用下方上传组件插入 img/ 路径。
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='title' label='标题' required placeholder='请输入标题，如 用户协议' />
              <TextField
                control={form.control}
                name='slug'
                label='slug'
                required
                placeholder='请输入 slug，如 terms'
                description={slug ? `前台地址：${pagePath(String(slug))}` : SLUG_HINT}
              />
            </div>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                required
                placeholder='请选择状态'
                items={Object.entries(PAGE_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
                description='草稿不对前台显示。'
              />
            </div>
            <ImageUploadControl
              purpose='static_page_image'
              label='插入正文图片'
              onChange={(key) => {
                const current = form.getValues('body')
                const separator = current.trim() ? '\n' : ''
                form.setValue('body', `${current}${separator}<img src="${key}" alt="">`, { shouldDirty: true, shouldValidate: true })
                toast.success('图片路径已插入正文')
              }}
              description='上传完成后会自动追加一段 <img> 标签；可在正文中补充 alt 文本。'
            />
            {/* 正文统一用富文本编辑器；后端仍按白名单清洗，站外链接自动新窗口打开（docs/06 第 13 节） */}
            <RichTextField
              control={form.control}
              name='body'
              label='正文'
              required
              description='用上面的工具栏排版：段落样式、加粗、列表、引用、链接。粘贴的内容会转成纯文本，避免编辑时看到的和保存结果不一致；保存时后端按白名单再清洗一次。'
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='seo_title' label='SEO 标题' placeholder='选填，留空时用上面的标题' />
              <TextField control={form.control} name='seo_desc' label='SEO 简介' placeholder='选填，搜索结果里显示的描述' />
            </div>
            <SwitchField
              control={form.control}
              name='is_footer'
              label='标记为页脚页'
              description='已发布且打开此项的页面会出现在前台页脚「帮助与条款」。'
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

function DeletePageDialog({ item, onClose }: { item: StaticPage; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => siteApi.deletePage(item.id),
    onSuccess: () => {
      toast.success(`已删除「${item.title}」`)
      void qc.invalidateQueries({ queryKey: siteKeys.pages })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${item.title}」`}
      desc={`删除后不可恢复，前台 ${pagePath(item.slug)} 立即打不开，指向它的前台菜单项会自动隐藏。只是暂时不想显示的话，请把状态改为草稿。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
