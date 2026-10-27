import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CircleCheck, CircleOff, History, Pencil, Plus, Trash2 } from 'lucide-react'
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
import { SelectField, TextField } from '@/features/content/components/fields'
import { ImageThumb } from '@/features/media/image-preview'
import { ImageUploadField, VideoUploadField } from '@/features/media/upload-field'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  AD_DEVICE_LABELS,
  AD_DISPLAY_LABELS,
  AD_SLOT_HINTS,
  AD_SLOT_LABELS,
  AD_SLOTS,
  AD_STATUS_LABELS,
  adListQuery,
  CREATIVE_LABELS,
  LOCALE_LABELS,
  LOCALES,
  opsApi,
  opsKeys,
  type Ad,
  type AdDisplayStatus,
  type AdSlot,
  type CreativeType,
} from './api'
import { CheckboxGroupField, DateTimeField } from './components/fields'
import {
  adToForm,
  creativeSummary,
  ctr,
  DEFAULT_AD_WEIGHT,
  formToAdInput,
  isIntInRange,
  isLinkOrEmpty,
  isMediaRefOrEmpty,
  LINK_HINT,
  MEDIA_REF_HINT,
  localesSummary,
  type AdForm,
} from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; item: Ad }

const HTML_REASON = '自定义 HTML 素材需要「自定义 HTML」权限'

/** 广告位（docs/06 第 18 节）：按位置投放广告；新建、修改、停用后前台立即生效，曝光与点击每分钟更新。 */
export function AdsPage() {
  const can = useCan()
  const visible = can('ads.view')
  const search = useSearch({ from: '/_authenticated/ads' })
  const navigate = useNavigate({ from: '/ads' })
  const params = {
    q: search.q,
    slot: search.slot,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...adListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='广告位' />
  const editable = can('ads.edit')
  const canHTML = can('ads.custom_html')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.slot || search.status)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='广告位'
        description='每个位置按出现机会（权重）随机挑选，可只投手机或电脑、限定投放语言与每台设备每天的次数。新建、修改、停用后前台立即生效；曝光与点击每分钟更新。多数位置要在「页面区块」里加「广告位」区块并选好位置才会显示。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建广告
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜广告名或跳转网址' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='位置'
          value={search.slot}
          className='w-44'
          options={AD_SLOTS.map((value) => ({
            value,
            label: AD_SLOT_LABELS[value],
          }))}
          onChange={(v) =>
            void navigate({
              search: (prev) => withFilter(prev, { slot: v as AdSlot | undefined }),
            })
          }
        />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(AD_STATUS_LABELS).map(([value, label]) => ({
            value,
            label,
          }))}
          onChange={(v) =>
            void navigate({
              search: (prev) => withFilter(prev, { status: v as typeof search.status }),
            })
          }
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
                <TableHead className='pl-4'>广告</TableHead>
                <TableHead>位置</TableHead>
                <TableHead>素材</TableHead>
                <TableHead>投给谁</TableHead>
                <TableHead className='text-right'>出现机会</TableHead>
                <TableHead>每天上限</TableHead>
                <TableHead className='text-right'>曝光 / 点击</TableHead>
                <TableHead>结束时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={10} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={10} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的广告' : '还没有广告'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((a) => {
                  const editReason = readonlyReason ?? (a.creative_type === 'html' && !canHTML ? HTML_REASON : null)
                  return (
                    <TableRow key={a.id}>
                      <TableCell className='pl-4'>
                        <div className='flex max-w-56 flex-col'>
                          <span className='font-medium'>{a.name}</span>
                          <span className='text-xs text-muted-foreground'>
                            #{a.id}
                            {a.click_url && <code className='ms-2'>→ {a.click_url}</code>}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap'>{AD_SLOT_LABELS[a.slot]}</TableCell>
                      <TableCell>
                        <div className='flex max-w-64 items-start gap-2'>
                          {/* image 字段同时承载图片素材与视频广告的封面，有值就给缩略图 */}
                          {a.image && <ImageThumb value={a.image} alt={`${a.name}素材`} className='h-10 w-16' />}
                          <div className='flex min-w-0 flex-col items-start gap-1'>
                            <Badge variant='outline'>{CREATIVE_LABELS[a.creative_type]}</Badge>
                            <span className='line-clamp-2 text-xs break-all text-muted-foreground'>{creativeSummary(a)}</span>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='text-sm'>
                        <div className='flex flex-col'>
                          <span className='whitespace-nowrap'>{AD_DEVICE_LABELS[a.device]}</span>
                          <span className='text-xs text-muted-foreground'>{localesSummary(a.locales)}</span>
                        </div>
                      </TableCell>
                      <TableCell className='text-right text-sm tabular-nums'>{a.weight}</TableCell>
                      <TableCell className='text-sm whitespace-nowrap'>{a.freq_cap > 0 ? `每台设备 ${a.freq_cap} 次` : '不限'}</TableCell>
                      <TableCell className='text-right text-sm whitespace-nowrap tabular-nums'>
                        <div className='flex flex-col'>
                          <span>
                            {a.imp_count.toLocaleString('zh-CN')} / {a.click_count.toLocaleString('zh-CN')}
                          </span>
                          <span className='text-xs text-muted-foreground'>点击率 {ctr(a.imp_count, a.click_count)}</span>
                        </div>
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                        {a.end_at ? formatDateTime(a.end_at).slice(0, 16) : '一直投放'}
                      </TableCell>
                      <TableCell>
                        <DisplayBadge status={a.display_status} />
                      </TableCell>
                      <TableCell className='pr-4'>
                        <div className='flex justify-end gap-1'>
                          <WithReason reason={editReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!!editReason}
                              aria-label={`编辑广告 #${a.id}`}
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
                              aria-label={`删除广告 #${a.id}`}
                              onClick={() => setDialog({ type: 'delete', item: a })}
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
              search: (prev) => ({
                ...prev,
                page: page > 1 ? page : undefined,
                page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size,
              }),
            })
          }
        />
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <AdDialog item={dialog.type === 'edit' ? dialog.item : undefined} canHTML={canHTML} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && <DeleteAdDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 投放状态：图标 + 文字（不只靠颜色区分）。 */
function DisplayBadge({ status }: { status: AdDisplayStatus }) {
  const label = AD_DISPLAY_LABELS[status]
  switch (status) {
    case 'live':
      return (
        <Badge variant='default'>
          <CircleCheck aria-hidden /> {label}
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

const link = (label: string) => z.string().max(500, '最多 500 个字符').refine(isLinkOrEmpty, `${label}需为${LINK_HINT}`)
const mediaRef = (label: string) => z.string().max(500, '最多 500 个字符').refine(isMediaRefOrEmpty, `${label}需为${MEDIA_REF_HINT}`)

const adSchema = z
  .object({
    name: z.string().trim().min(1, '请填写广告名').max(50, '广告名最多 50 个字'),
    slot: z.string().min(1, '请选择广告位置'),
    creative_type: z.enum(['image', 'video', 'text', 'html']),
    image: mediaRef('图片素材'),
    video_url: mediaRef('视频素材'),
    text: z.string().max(200, '文字最多 200 个字'),
    html: z.string().max(10000, '自定义 HTML 最多 10000 个字符'),
    alt: z.string().max(100, '说明最多 100 个字'),
    click_url: link('跳转网址'),
    device: z.enum(['all', 'mobile', 'desktop']),
    locales: z.array(z.enum(LOCALES)),
    weight: z.string().refine((v) => isIntInRange(v, 1, 100), '请输入 1–100 的整数'),
    freq_cap: z.string().refine((v) => isIntInRange(v, 0, 1000), '请输入 0–1000 的整数，0 表示不限'),
    end_at: z.string(),
    status: z.enum(['active', 'inactive']),
  })
  .superRefine((v, ctx) => {
    // 素材按类型必填（与后端一致）
    const required: Partial<Record<CreativeType, [keyof AdForm, string]>> = {
      image: ['image', '请上传图片素材'],
      video: ['video_url', '请上传视频素材'],
      text: ['text', '请填写文字内容'],
      html: ['html', '请填写自定义 HTML'],
    }
    const rule = required[v.creative_type]
    if (rule && !String(v[rule[0]]).trim()) ctx.addIssue({ code: 'custom', path: [rule[0]], message: rule[1] })
  })

function AdDialog({ item, canHTML, onClose }: { item?: Ad; canHTML: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<z.input<typeof adSchema>, unknown, AdForm>({
    resolver: zodResolver(adSchema),
    defaultValues: adToForm(item),
  })
  const [slot, type] = useWatch({
    control: form.control,
    name: ['slot', 'creative_type'],
  })
  const mutation = useMutation({
    mutationFn: (v: AdForm) => {
      const body = formToAdInput(v)
      return creating ? opsApi.createAd(body) : opsApi.updateAd(item.id, body)
    },
    onSuccess: (saved, v) => {
      toast.success(creating ? `已新建广告 #${saved.id}，前台立即生效` : '已保存，前台立即生效')
      if (saved.creative_type === 'html' && saved.html !== v.html.trim()) {
        toast.info('自定义 HTML 已按白名单清洗，不允许的标签或属性已去掉，可再次打开查看保存结果')
      }
      void qc.invalidateQueries({ queryKey: opsKeys.ads })
      onClose()
    },
  })
  const typeItems = (Object.keys(CREATIVE_LABELS) as CreativeType[])
    .filter((t) => t !== 'html' || canHTML)
    .map((value) => ({ value, label: CREATIVE_LABELS[value] }))

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建广告' : `编辑广告 #${item.id}`}</DialogTitle>
          <DialogDescription>图片在上传前自动压缩为 WebP；视频上传后自动转为 HLS m3u8。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='广告名' required placeholder='请输入广告名，后台识别用，如 首页顶部-会员 5 折' />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='slot'
                label='位置'
                required
                placeholder='请选择广告位置'
                items={AD_SLOTS.map((value) => ({
                  value,
                  label: AD_SLOT_LABELS[value],
                }))}
                description={slot ? AD_SLOT_HINTS[slot as AdSlot] : '选择位置后显示建议尺寸。'}
              />
              <SelectField
                control={form.control}
                name='creative_type'
                label='素材类型'
                required
                placeholder='请选择素材类型'
                items={typeItems}
                description={
                  canHTML ? '自定义 HTML 保存时按白名单清洗，前台在沙箱中显示。' : '自定义 HTML 需要「自定义 HTML」权限（默认只有超级管理员）。'
                }
              />
            </div>
            {(type === 'image' || type === 'video') && (
              <div className='grid gap-4 sm:grid-cols-2'>
                {type === 'video' && <VideoUploadField control={form.control} name='video_url' label='视频素材' required />}
                <ImageUploadField
                  control={form.control}
                  name='image'
                  label={type === 'video' ? '视频封面' : '图片素材'}
                  purpose={type === 'video' ? 'ad_poster' : 'ad_image'}
                  required={type === 'image'}
                  description={type === 'video' ? '选填；建议上传与视频比例一致的封面。' : AD_SLOT_HINTS[slot as AdSlot]}
                />
                <TextField
                  control={form.control}
                  name='alt'
                  label={type === 'video' ? '视频说明' : '图片说明'}
                  placeholder='选填，读屏软件会读出，如 会员 5 折'
                />
              </div>
            )}
            {type === 'text' && (
              <TextField
                control={form.control}
                name='text'
                label='文字内容'
                required
                multiline
                rows={2}
                placeholder='请输入广告文字，最多 200 个字'
              />
            )}
            {type === 'html' && (
              <TextField
                control={form.control}
                name='html'
                label='自定义 HTML'
                required
                multiline
                rows={6}
                placeholder='请输入 HTML，如 <a href="https://example.com"><img src="/ads/a.webp" alt="活动"></a>'
                description='只保留常见排版标签、链接、图片与少量内联样式；脚本、样式表、iframe、表单与事件属性会被去掉。'
              />
            )}
            <TextField
              control={form.control}
              name='click_url'
              label='跳转网址'
              placeholder='选填，站内路径如 /vip，或 https:// 开头的网址'
              description='留空表示点击不跳转。'
            />
            <div className='grid gap-4 sm:grid-cols-3'>
              <SelectField
                control={form.control}
                name='device'
                label='投给谁'
                placeholder='请选择投放的端'
                items={Object.entries(AD_DEVICE_LABELS).map(([value, label]) => ({ value, label }))}
              />
              <TextField
                control={form.control}
                name='weight'
                label='出现机会'
                required
                inputMode='numeric'
                placeholder={`1–100，默认 ${DEFAULT_AD_WEIGHT}`}
                description='同一位置内按权重随机。'
              />
              <TextField
                control={form.control}
                name='freq_cap'
                label='每天次数上限'
                required
                inputMode='numeric'
                placeholder='0–1000，0 表示不限'
                description='每台设备每天（北京时间）最多曝光几次。'
              />
            </div>
            <CheckboxGroupField
              control={form.control}
              name='locales'
              label='投放语言'
              options={LOCALES.map((value) => ({
                value,
                label: LOCALE_LABELS[value],
              }))}
              description='不选表示全部语言；需要多语言时按语言各建一条。'
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <DateTimeField control={form.control} name='end_at' label='结束时间' description='不填表示一直投放。' />
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={[
                  { label: '投放中', value: 'active' },
                  { label: '停用', value: 'inactive' },
                ]}
              />
            </div>
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

function DeleteAdDialog({ item, onClose }: { item: Ad; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => opsApi.deleteAd(item.id),
    onSuccess: () => {
      toast.success(`已删除广告 #${item.id}`)
      void qc.invalidateQueries({ queryKey: opsKeys.ads })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除广告 #${item.id}`}
      desc='删除后不可恢复，曝光、点击累计一起删除，前台立即不再投放。只是暂时不投放的话，请改为停用。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
