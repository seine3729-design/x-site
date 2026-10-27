import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useBlocker } from '@tanstack/react-router'
import { ChevronRight, Save, Undo2 } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button, Card, CardContent, CardHeader, CardTitle } from '@/components/ui'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Form } from '@/components/ui/form'
import { WithReason } from '@/features/access/shared'
import { ImageUploadField } from '@/features/media/upload-field'
import { contentApi, contentKeys, type DramaDetail } from './api'
import { BadgeField } from './components/badge-field'
import { DateField, SelectField, SwitchField, TextField } from './components/fields'
import { dramaToForm, formToDramaInput, fromLocalInput, SLUG_HINT, SLUG_PATTERN, splitList, type DramaForm } from './utils'

const profileSchema = z.object({
  title: z.string().trim().min(1, '请填写剧名').max(200, '剧名最多 200 个字'),
  aliases: z
    .string()
    .refine((v) => splitList(v).length <= 20, '别名最多 20 个')
    .refine((v) => splitList(v).every((s) => s.length <= 100), '每个别名最多 100 个字'),
  slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, SLUG_HINT),
  summary: z.string().max(5000, '简介最多 5000 个字'),
  cover_portrait: z.string().max(500, '最多 500 个字符'),
  cover_landscape: z.string().max(500, '最多 500 个字符'),
  badge: z.string().trim().max(16, '角标最多 16 个字'),
  score: z
    .string()
    .trim()
    .regex(/^(\d(\.\d)?)?$/, '0–9.9，最多一位小数'),
  year: z
    .string()
    .trim()
    .regex(/^(\d{4})?$/, '请填写四位年份')
    .refine((v) => v === '' || (Number(v) >= 1900 && Number(v) <= 2100), '年份为 1900–2100'),
  status: z.enum(['draft', 'published', 'offline']),
  publish_at: z.string(),
  is_finished: z.boolean(),
  sort_weight: z
    .string()
    .trim()
    .regex(/^(-?\d{1,9})?$/, '请填写整数'),
  view_base: z
    .string()
    .trim()
    .regex(/^\d{0,12}$/, '请填写非负整数'),
  seo_title: z.string().max(200, '最多 200 个字'),
  seo_desc: z.string().max(500, '最多 500 个字'),
}) satisfies z.ZodType<DramaForm>

/** 剧资料（docs/06 第 5 节「剧资料」）：整体保存；有未保存的修改时离开或切换分区前提醒。 */
export function ProfileSection({ drama, editable }: { drama: DramaDetail; editable: boolean }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof profileSchema>, unknown, DramaForm>({
    resolver: zodResolver(profileSchema),
    defaultValues: dramaToForm(drama),
  })
  const dirty = form.formState.isDirty
  const [moreOpen, setMoreOpen] = useState(
    () => drama.aliases.length > 0 || !!drama.publish_at || !!drama.slug || !!drama.seo_title || drama.sort_weight !== 0
  )
  const status = useWatch({ control: form.control, name: 'status' })
  const publishAt = useWatch({ control: form.control, name: 'publish_at' })
  // 判断定时是否已到点用的「现在」：打开页面时的时间，提示文字够用
  const [openedAt] = useState(() => Date.now())

  useBlocker({
    shouldBlockFn: () => dirty && !window.confirm('剧资料的修改尚未保存，确定离开吗？'),
    enableBeforeUnload: () => dirty,
  })

  const save = useMutation({
    mutationFn: (v: DramaForm) => contentApi.updateDrama(drama.id, formToDramaInput(v)),
    onSuccess: (saved) => {
      toast.success('已保存剧资料')
      qc.setQueryData<DramaDetail>(contentKeys.drama(drama.id), (old) => (old ? { ...old, ...saved } : old))
      void qc.invalidateQueries({ queryKey: [...contentKeys.dramas, 'list'] })
      form.reset(dramaToForm(saved))
    },
  })

  const disabled = !editable || save.isPending
  const scheduleHint = (() => {
    if (status !== 'published') return '定时上架只在状态为「已上架」时生效。'
    const at = fromLocalInput(publishAt)
    if (!at) return '留空：保存后立即上架（写入当前时间作为上架时间）。'
    return Date.parse(at) > openedAt ? `到 ${formatDateTime(at)} 才在前台可见（定时未到点）。` : '上架时间已过，保存后前台可见。'
  })()

  return (
    <Form {...form}>
      <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
        <Card>
          <CardHeader>
            <CardTitle className='text-base'>基本资料</CardTitle>
          </CardHeader>
          <CardContent className='grid gap-4 md:grid-cols-2'>
            <div className='md:col-span-2'>
              <TextField control={form.control} name='title' label='剧名' required placeholder='请输入剧名' disabled={disabled} />
            </div>
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              disabled={disabled}
              items={[
                { label: '草稿', value: 'draft' },
                { label: '已上架', value: 'published' },
                { label: '已下架', value: 'offline' },
              ]}
            />
            <BadgeField control={form.control} name='badge' disabled={disabled} />
            <TextField control={form.control} name='score' label='评分' inputMode='decimal' placeholder='选填，0–9.9，如 9.5' disabled={disabled} />
            <SwitchField
              control={form.control}
              name='is_finished'
              label='已完结'
              description='显示「全 N 集」，否则显示「更新至 N 集」。'
              disabled={disabled}
            />
            <div className='md:col-span-2'>
              <TextField
                control={form.control}
                name='summary'
                label='简介'
                multiline
                rows={4}
                placeholder='选填，最多 5000 个字'
                disabled={disabled}
              />
            </div>
            <div className='md:col-span-2'>
              <ImageUploadField
                control={form.control}
                name='cover_portrait'
                label='竖封面'
                purpose='drama_cover'
                description='列表、海报网格使用；上传前在浏览器压缩为 WebP。'
                disabled={disabled}
              />
            </div>
          </CardContent>
        </Card>

        <Collapsible open={moreOpen} onOpenChange={setMoreOpen}>
          <Card>
            <CardHeader>
              <CollapsibleTrigger asChild>
                <button type='button' className='flex items-center gap-2 text-start text-base font-semibold' aria-expanded={moreOpen}>
                  <ChevronRight className={cn('size-4 transition-transform', moreOpen && 'rotate-90')} aria-hidden />
                  更多资料
                  <span className='text-sm font-normal text-muted-foreground'>别名、年份、横封面、排序值、初始播放量、定时上架、slug、SEO</span>
                </button>
              </CollapsibleTrigger>
            </CardHeader>
            <CollapsibleContent>
              <CardContent className='grid gap-4 md:grid-cols-2'>
                <div className='md:col-span-2'>
                  <TextField
                    control={form.control}
                    name='aliases'
                    label='别名'
                    placeholder='选填，多个用逗号或顿号隔开'
                    description='搜索与去重时也会匹配别名。'
                    disabled={disabled}
                  />
                </div>
                <TextField control={form.control} name='year' label='年份' inputMode='numeric' placeholder='选填，如 2026' disabled={disabled} />
                <ImageUploadField
                  control={form.control}
                  name='cover_landscape'
                  label='横封面'
                  purpose='drama_cover'
                  description='详情页横幅使用；上传前在浏览器压缩为 WebP。'
                  disabled={disabled}
                />
                <TextField
                  control={form.control}
                  name='sort_weight'
                  label='排序值'
                  inputMode='numeric'
                  placeholder='默认 0'
                  description='「推荐」排序时越大越靠前。'
                  disabled={disabled}
                />
                <TextField
                  control={form.control}
                  name='view_base'
                  label='初始播放量'
                  inputMode='numeric'
                  placeholder='默认 0'
                  description='「最热」排序时与真实播放量相加。'
                  disabled={disabled}
                />
                <DateField
                  control={form.control}
                  name='publish_at'
                  label='定时上架'
                  mode='datetime'
                  placeholder='选填，请选择上架时间'
                  description={scheduleHint}
                  disabled={disabled}
                />
                <TextField
                  control={form.control}
                  name='slug'
                  label='slug'
                  placeholder='选填，可留空，如 ceo-love'
                  description={`${SLUG_HINT}；前台按 /d/<编号> 访问，可以不填。`}
                  disabled={disabled}
                />
                <TextField control={form.control} name='seo_title' label='SEO 标题' placeholder='选填，留空时用剧名' disabled={disabled} />
                <TextField
                  control={form.control}
                  name='seo_desc'
                  label='SEO 描述'
                  multiline
                  rows={2}
                  placeholder='选填，留空时用简介'
                  disabled={disabled}
                />
              </CardContent>
            </CollapsibleContent>
          </Card>
        </Collapsible>

        <p className='text-xs text-muted-foreground tabular-nums'>
          真实播放量 {drama.view_count} · 热度 {drama.hot_score}（初始 + 真实）{drama.source && ` · 来源 ${drama.source}`} · 创建于{' '}
          {formatDateTime(drama.created_at)} · 修改于 {formatDateTime(drama.updated_at)}
        </p>

        <div className='sticky bottom-4 z-10 flex justify-end gap-2 rounded-lg border bg-background/95 p-2 shadow-sm backdrop-blur'>
          {dirty && <span className='me-auto self-center ps-2 text-sm text-muted-foreground'>有未保存的修改</span>}
          <Button type='button' variant='ghost' disabled={!dirty || save.isPending} onClick={() => form.reset(dramaToForm(drama))}>
            <Undo2 className='size-4' /> 撤销修改
          </Button>
          <WithReason reason={editable ? null : '当前角色只能查看'}>
            <Button type='submit' disabled={!editable || !dirty || save.isPending}>
              <Save className='size-4' /> {save.isPending ? '保存中…' : '保存剧资料'}
            </Button>
          </WithReason>
        </div>
      </form>
    </Form>
  )
}
