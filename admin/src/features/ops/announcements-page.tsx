import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CircleCheck, CircleOff, Clock, History, Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
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
import { ImageThumb } from '@/features/media/image-preview'
import { ImageUploadField } from '@/features/media/upload-field'
import { SelectField, TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  ACTIVE_LABELS,
  ANNOUNCEMENT_DISPLAY_LABELS,
  ANNOUNCEMENT_KIND_LABELS,
  ANNOUNCEMENT_PAGE_LABELS,
  ANNOUNCEMENT_PAGES,
  announcementListQuery,
  FREQUENCY_LABELS,
  LOCALE_LABELS,
  LOCALES,
  opsApi,
  opsKeys,
  type Announcement,
  type AnnouncementDisplayStatus,
} from './api'
import { CheckboxGroupField, DateTimeField } from './components/fields'
import {
  announcementToForm,
  formToAnnouncementInput,
  isLinkOrEmpty,
  isMediaRefOrEmpty,
  LINK_HINT,
  MEDIA_REF_HINT,
  localesSummary,
  pagesSummary,
  windowSummary,
  type AnnouncementForm,
} from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; item: Announcement }

/** 公告（docs/06 第 19 节）：顶部滚动条与弹窗；新建、修改、删除后前台立即生效。 */
export function AnnouncementsPage() {
  const can = useCan()
  const visible = can('announcements.view')
  const search = useSearch({ from: '/_authenticated/announcements' })
  const navigate = useNavigate({ from: '/announcements' })
  const params = {
    q: search.q,
    kind: search.kind,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...announcementListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='公告' />
  const editable = can('announcements.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.kind || search.status)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='公告'
        description='顶部滚动条与弹窗。新建、修改、停用后前台立即生效；同一页面有多条弹窗时只弹最新的一条。需要多语言时按语言各建一条。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建公告
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜内容或跳转网址' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='方式'
          value={search.kind}
          options={[
            { value: 'topbar', label: ANNOUNCEMENT_KIND_LABELS.topbar },
            { value: 'popup', label: ANNOUNCEMENT_KIND_LABELS.popup },
          ]}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { kind: v as typeof search.kind }) })}
        />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(ACTIVE_LABELS).map(([value, label]) => ({ value, label }))}
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
                <TableHead className='pl-4'>方式</TableHead>
                <TableHead>内容</TableHead>
                <TableHead>出现页面</TableHead>
                <TableHead>投放语言</TableHead>
                <TableHead>频率</TableHead>
                <TableHead>时间</TableHead>
                <TableHead>状态</TableHead>
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
                    {filtered ? '没有符合条件的公告' : '还没有公告'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className='pl-4'>
                      <Badge variant='outline'>{ANNOUNCEMENT_KIND_LABELS[a.kind]}</Badge>
                    </TableCell>
                    <TableCell>
                      <div className='flex max-w-sm items-start gap-2'>
                        {a.image && <ImageThumb value={a.image} alt='公告配图' className='h-10 w-16' />}
                        <div className='flex min-w-0 flex-col'>
                          <span className='line-clamp-2'>{a.content}</span>
                          {a.click_url && <code className='truncate text-xs text-muted-foreground'>→ {a.click_url}</code>}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className='max-w-40 text-sm'>{pagesSummary(a.pages)}</TableCell>
                    <TableCell className='max-w-40 text-sm'>{localesSummary(a.locales)}</TableCell>
                    <TableCell className='text-sm'>{FREQUENCY_LABELS[a.frequency]}</TableCell>
                    <TableCell className='text-sm whitespace-nowrap tabular-nums'>{windowSummary(a.start_at, a.end_at)}</TableCell>
                    <TableCell>
                      <DisplayBadge status={a.display_status} />
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`编辑公告 #${a.id}`}
                            onClick={() => setDialog({ type: 'edit', item: a })}
                          >
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除公告 #${a.id}`}
                            onClick={() => setDialog({ type: 'delete', item: a })}
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
        <AnnouncementDialog item={dialog.type === 'edit' ? dialog.item : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && <DeleteAnnouncementDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 显示状态：图标 + 文字（不只靠颜色区分）。 */
function DisplayBadge({ status }: { status: AnnouncementDisplayStatus }) {
  const label = ANNOUNCEMENT_DISPLAY_LABELS[status]
  switch (status) {
    case 'live':
      return (
        <Badge variant='default'>
          <CircleCheck aria-hidden /> {label}
        </Badge>
      )
    case 'scheduled':
      return (
        <Badge variant='outline'>
          <Clock aria-hidden /> {label}
        </Badge>
      )
    case 'ended':
      return (
        <Badge variant='outline' className='text-muted-foreground'>
          <History aria-hidden /> {label}
        </Badge>
      )
    default:
      return (
        <Badge variant='secondary'>
          <CircleOff aria-hidden /> {label}
        </Badge>
      )
  }
}

const announcementSchema = z
  .object({
    kind: z.enum(['topbar', 'popup']),
    content: z.string().trim().min(1, '请填写公告内容').max(500, '公告内容最多 500 个字'),
    click_url: z.string().max(500, '最多 500 个字符').refine(isLinkOrEmpty, `跳转网址需为${LINK_HINT}`),
    frequency: z.enum(['always', 'once', 'daily']),
    pages: z.array(z.enum(ANNOUNCEMENT_PAGES)),
    locales: z.array(z.enum(LOCALES)),
    image: z.string().max(500, '最多 500 个字符').refine(isMediaRefOrEmpty, `配图需为${MEDIA_REF_HINT}`),
    start_at: z.string(),
    end_at: z.string(),
    status: z.enum(['active', 'inactive']),
  })
  .refine((v) => !v.start_at || !v.end_at || v.end_at > v.start_at, { path: ['end_at'], message: '结束时间要晚于开始时间' })

function AnnouncementDialog({ item, onClose }: { item?: Announcement; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<z.input<typeof announcementSchema>, unknown, AnnouncementForm>({
    resolver: zodResolver(announcementSchema),
    defaultValues: announcementToForm(item),
  })
  const kind = useWatch({ control: form.control, name: 'kind' })
  const mutation = useMutation({
    mutationFn: (v: AnnouncementForm) => {
      const body = formToAnnouncementInput(v)
      return creating ? opsApi.createAnnouncement(body) : opsApi.updateAnnouncement(item.id, body)
    },
    onSuccess: (saved) => {
      toast.success(creating ? `已新建公告 #${saved.id}，前台立即生效` : '已保存，前台立即生效')
      void qc.invalidateQueries({ queryKey: opsKeys.announcements })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建公告' : `编辑公告 #${item.id}`}</DialogTitle>
          <DialogDescription>弹窗配图使用上传组件；图片会在浏览器压缩为 WebP 再上传。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='kind'
                label='方式'
                required
                placeholder='请选择公告方式'
                items={[
                  { label: '顶部滚动条', value: 'topbar' },
                  { label: '弹窗', value: 'popup' },
                ]}
                description='站内信暂未开放。'
              />
              <SelectField
                control={form.control}
                name='frequency'
                label='频率'
                placeholder='请选择显示频率'
                items={Object.entries(FREQUENCY_LABELS).map(([value, label]) => ({ value, label }))}
                description='「每人一次」「每人每天一次」记在用户本机。'
              />
            </div>
            <TextField control={form.control} name='content' label='内容' required multiline placeholder='请输入公告内容，最多 500 个字' />
            <TextField
              control={form.control}
              name='click_url'
              label='跳转网址'
              placeholder='选填，站内路径如 /vip，或 https:// 开头的网址'
              description='留空表示点击不跳转。'
            />
            {kind === 'popup' && (
              <ImageUploadField
                control={form.control}
                name='image'
                label='弹窗配图'
                purpose='announcement_image'
                description='建议约 16:9；上传时会压缩，最长边不超过 2400px。'
              />
            )}
            <CheckboxGroupField
              control={form.control}
              name='pages'
              label='出现页面'
              options={ANNOUNCEMENT_PAGES.map((value) => ({ value, label: ANNOUNCEMENT_PAGE_LABELS[value] }))}
              description='不选表示全部页面。'
            />
            <CheckboxGroupField
              control={form.control}
              name='locales'
              label='投放语言'
              options={LOCALES.map((value) => ({ value, label: LOCALE_LABELS[value] }))}
              description='不选表示全部语言；需要多语言时按语言各建一条。'
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <DateTimeField control={form.control} name='start_at' label='开始时间' description='不填表示立即开始。' />
              <DateTimeField control={form.control} name='end_at' label='结束时间' description='不填表示一直显示。' />
            </div>
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              items={[
                { label: '启用（到了时间就显示）', value: 'active' },
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

function DeleteAnnouncementDialog({ item, onClose }: { item: Announcement; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => opsApi.deleteAnnouncement(item.id),
    onSuccess: () => {
      toast.success(`已删除公告 #${item.id}`)
      void qc.invalidateQueries({ queryKey: opsKeys.announcements })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除公告 #${item.id}`}
      desc='删除后不可恢复，前台立即不再显示。只是暂时不想显示的话，请改为停用。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
