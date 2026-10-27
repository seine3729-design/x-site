import { useRef, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, ShieldAlert, X } from 'lucide-react'
import { useFieldArray, useForm, useWatch, type Control, type FieldPath, type FieldValues } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { Badge, Button, Input, Textarea } from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SwitchField, TextField } from '@/features/content/components/fields'
import { ImageRefField } from '@/features/media/image-preview'
import { IMAGE_KEY_HINT, isImageKeyOrEmpty, optionLabel } from './branding-utils'
import {
  analyticsQuery,
  MAX_ANALYTICS,
  MAX_OG_IMAGE,
  MAX_ROBOTS_EXTRA,
  MAX_VERIFICATIONS,
  MAX_VERIFY_CONTENT,
  MAX_VERIFY_NAME,
  PERM_ANALYTICS_CODE,
  seoApi,
  seoConfigQuery,
  seoKeys,
  type AnalyticsCode,
  type SEO,
  type SEOConfig,
} from './seo-api'
import { cleanTemplates, cleanVerifications, insertVariable, isVerifyName, previewTemplate, unknownVariables } from './seo-utils'
import type { BrandingOption } from './branding-api'

/**
 * SEO 与统计（docs/06 第 17 节，S3-3 / A3-3）。
 *
 * 模板里的变量由后端登记，这里给出「点击插入」与示例预览——变量名写错会让前台把 {dramaTitle}
 * 原样显示在标题里，所以前端也标出未知变量。统计代码另有权限点，没有权限时整张卡片不显示。
 */
export function SEOPage() {
  const can = useCan()
  const visible = can('seo.view')
  const config = useQuery({ ...seoConfigQuery, enabled: visible })
  const canAnalytics = can(PERM_ANALYTICS_CODE)

  if (!visible) return <NoPermission title='SEO 与统计' />
  const editable = can('seo.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <div className='flex flex-col gap-6'>
      <PageHeading
        title='SEO 与统计'
        description='分页面的标题与描述模板、收录与分享设置、站长验证。保存后站点配置版本 +1，前台几秒内生效。'
        readonly={!editable}
      />
      {config.isError ? (
        <LoadError onRetry={() => void config.refetch()} />
      ) : config.isPending ? (
        <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <SEOSections config={config.data} disabledReason={readonlyReason} />
      )}
      {canAnalytics ? (
        <AnalyticsSection disabledReason={readonlyReason} />
      ) : (
        <section className='flex flex-col gap-2'>
          <h2 className='font-medium'>统计代码</h2>
          <p className='rounded-md border border-dashed p-4 text-sm text-muted-foreground'>
            统计代码会被原样注入每个页面，需要「统计代码」权限（默认只有超级管理员）才能查看与修改。
          </p>
        </section>
      )}
    </div>
  )
}

// ---------- SEO ----------

function seoSchema(config: SEOConfig) {
  const template = z
    .string()
    .trim()
    .max(config.max_template, `最多 ${config.max_template} 个字`)
    // 变量名写错时直接指出是哪个：真漏到线上，前台会把 {dramaTitle} 原样显示在标题里给用户看
    .refine((v) => unknownVariables(v, config.variables).length === 0, {
      error: (iss) => `有未知变量 {${unknownVariables(String(iss.input ?? ''), config.variables)[0]}}，请从上面的变量里选`,
    })
  return z.object({
    title_templates: z.record(z.string(), template),
    desc_templates: z.record(z.string(), template),
    og_image: z.string().trim().max(MAX_OG_IMAGE, `最多 ${MAX_OG_IMAGE} 个字符`).refine(isImageKeyOrEmpty, IMAGE_KEY_HINT),
    robots_allow: z.boolean(),
    robots_extra: z.string().trim().max(MAX_ROBOTS_EXTRA, `最多 ${MAX_ROBOTS_EXTRA} 个字符`),
    sitemap: z.boolean(),
    structured_data: z.boolean(),
    verifications: z
      .array(
        z
          .object({
            name: z.string().trim().max(MAX_VERIFY_NAME, `名称最多 ${MAX_VERIFY_NAME} 个字符`),
            content: z.string().trim().max(MAX_VERIFY_CONTENT, `内容最多 ${MAX_VERIFY_CONTENT} 个字符`),
          })
          .superRefine((v, ctx) => {
            if (v.name === '' && v.content === '') return // 整行留空的提交时会丢掉
            if (v.name === '' || v.content === '') {
              ctx.addIssue({ code: 'custom', path: ['content'], message: '名称与内容都要填' })
              return
            }
            if (!isVerifyName(v.name)) {
              ctx.addIssue({ code: 'custom', path: ['name'], message: '只能用字母、数字、点、下划线与连字符' })
            }
          })
      )
      .max(MAX_VERIFICATIONS, `最多 ${MAX_VERIFICATIONS} 组`),
  })
}

type SEOForm = z.infer<ReturnType<typeof seoSchema>>

function SEOSections({ config, disabledReason }: { config: SEOConfig; disabledReason: string | null }) {
  const qc = useQueryClient()
  const schema = seoSchema(config)
  // 把每个页面都放进表单（空值表示用前台默认），这样页面清单变化时表单结构跟着接口走
  const form = useForm<SEOForm>({
    resolver: zodResolver(schema),
    defaultValues: toForm(config),
  })
  const save = useMutation({
    mutationFn: (v: SEOForm) =>
      seoApi.setSEO({
        ...v,
        title_templates: cleanTemplates(v.title_templates),
        desc_templates: cleanTemplates(v.desc_templates),
        verifications: cleanVerifications(v.verifications),
      } as SEO),
    onSuccess: (res) => {
      qc.setQueryData(seoKeys.config, res)
      form.reset(toForm(res))
      toast.success('已保存 SEO 设置，前台几秒内生效')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const verifications = useFieldArray({ control: form.control, name: 'verifications' })
  const editable = !disabledReason
  const dirty = form.formState.isDirty

  return (
    <Form {...form}>
      <form className='flex flex-col gap-6' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
        <section className='flex flex-col gap-3'>
          <div className='flex flex-col gap-0.5'>
            <h2 className='font-medium'>标题与描述模板</h2>
            <p className='text-xs text-muted-foreground'>留空的页面用前台默认模板。点变量按钮插到光标处；下面会按示例数据预览最终效果。</p>
          </div>
          <VariableBar variables={config.variables} />
          <div className='flex flex-col gap-4 rounded-md border p-4'>
            {config.pages.map((page) => (
              <PageTemplates key={page.value} page={page} control={form.control} disabled={!editable} />
            ))}
          </div>
        </section>

        <section className='flex flex-col gap-3'>
          <div className='flex flex-col gap-0.5'>
            <h2 className='font-medium'>收录与分享</h2>
            <p className='text-xs text-muted-foreground'>关掉「允许收录」后整站输出 noindex，搜索引擎会逐步移除已收录的页面。</p>
          </div>
          <div className='flex flex-col gap-4 rounded-md border p-4'>
            <SwitchField
              control={form.control}
              name='robots_allow'
              label='允许搜索引擎收录'
              description='关闭后整站输出 noindex，并让 robots.txt 拒绝抓取。'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SwitchField control={form.control} name='sitemap' label='提供 sitemap.xml' disabled={!editable} />
              <SwitchField control={form.control} name='structured_data' label='输出结构化数据' disabled={!editable} />
            </div>
            <ImageRefField
              control={form.control}
              name='og_image'
              label='默认分享图'
              placeholder='选填，如 img/og.webp'
              description={IMAGE_KEY_HINT}
              disabled={!editable}
              thumbClassName='h-16 w-auto max-w-56 object-contain'
            />
            <TextField
              control={form.control}
              name='robots_extra'
              label='robots.txt 追加内容'
              multiline
              rows={3}
              placeholder={'选填，如\nDisallow: /search'}
              description='原样追加到 robots.txt 末尾。'
              disabled={!editable}
            />
          </div>
        </section>

        <section className='flex flex-col gap-3'>
          <div className='flex flex-col gap-0.5'>
            <h2 className='font-medium'>站长验证</h2>
            <p className='text-xs text-muted-foreground'>
              输出成 <code>&lt;meta name=… content=…&gt;</code>，用于各搜索引擎的站点归属验证。名称与内容都要填。
            </p>
          </div>
          <fieldset className='flex flex-col gap-3 rounded-md border p-4' disabled={!editable}>
            <legend className='px-1 text-sm font-medium'>
              验证标签（{verifications.fields.length} / {MAX_VERIFICATIONS}）
            </legend>
            {verifications.fields.length === 0 && <p className='text-xs text-muted-foreground'>还没有添加。</p>}
            {verifications.fields.map((f, i) => (
              <div key={f.id} className='flex items-start gap-2'>
                <div className='grid flex-1 gap-2 sm:grid-cols-[16rem_1fr]'>
                  <TextField
                    control={form.control}
                    name={`verifications.${i}.name`}
                    label={`名称 ${i + 1}`}
                    placeholder='如 google-site-verification'
                    disabled={!editable}
                  />
                  <TextField
                    control={form.control}
                    name={`verifications.${i}.content`}
                    label={`内容 ${i + 1}`}
                    placeholder='搜索引擎给的校验串'
                    disabled={!editable}
                  />
                </div>
                {editable && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='mt-7'
                    aria-label={`删除验证 ${i + 1}`}
                    onClick={() => verifications.remove(i)}
                  >
                    <X className='size-4' />
                  </Button>
                )}
              </div>
            ))}
            {editable && (
              <Button
                type='button'
                variant='secondary'
                size='sm'
                className='self-start'
                disabled={verifications.fields.length >= MAX_VERIFICATIONS}
                onClick={() => verifications.append({ name: '', content: '' })}
              >
                <Plus className='size-4' /> 添加验证标签
              </Button>
            )}
          </fieldset>
        </section>

        <div className='flex flex-wrap items-center justify-end gap-2'>
          {dirty && <span className='me-auto text-sm text-muted-foreground'>有未保存的修改</span>}
          <Button type='button' variant='outline' disabled={!dirty || save.isPending} onClick={() => form.reset()}>
            还原
          </Button>
          <WithReason reason={disabledReason}>
            <Button type='submit' disabled={!editable || !dirty || save.isPending}>
              {save.isPending ? '保存中…' : '保存 SEO 设置'}
            </Button>
          </WithReason>
        </div>
      </form>
    </Form>
  )
}

/** 把接口数据转成表单值：每个页面都要有键，否则新增页面时表单里缺字段。 */
function toForm(config: SEOConfig): SEOForm {
  const byPage = (templates: Record<string, string>) => Object.fromEntries(config.pages.map((p) => [p.value, templates?.[p.value] ?? '']))
  return {
    title_templates: byPage(config.seo.title_templates),
    desc_templates: byPage(config.seo.desc_templates),
    og_image: config.seo.og_image ?? '',
    robots_allow: config.seo.robots_allow,
    robots_extra: config.seo.robots_extra ?? '',
    sitemap: config.seo.sitemap,
    structured_data: config.seo.structured_data,
    verifications: config.seo.verifications ?? [],
  }
}

/** 变量清单：点一下把 {变量} 插到最后一个聚焦的模板输入框里。 */
function VariableBar({ variables }: { variables: BrandingOption[] }) {
  return (
    <div className='flex flex-wrap items-center gap-1.5 rounded-md border bg-muted/40 p-2'>
      <span className='text-xs text-muted-foreground'>可用变量：</span>
      {variables.map((v) => (
        <Badge key={v.value} variant='secondary' className='font-normal'>
          {v.label}
          <code className='ms-1 text-[10px] opacity-70'>{`{${v.value}}`}</code>
        </Badge>
      ))}
      <span className='text-xs text-muted-foreground'>点下面输入框右侧的「插入」选变量。</span>
    </div>
  )
}

/** 一个页面的标题与描述模板 + 示例预览。 */
function PageTemplates({ page, control, disabled }: { page: BrandingOption; control: Control<SEOForm>; disabled: boolean }) {
  const title = useWatch({ control, name: `title_templates.${page.value}` as FieldPath<SEOForm> }) as string | undefined
  const desc = useWatch({ control, name: `desc_templates.${page.value}` as FieldPath<SEOForm> }) as string | undefined

  return (
    <div className='flex flex-col gap-2 border-b pb-4 last:border-b-0 last:pb-0'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-sm font-medium'>{page.label}</span>
        <code className='text-xs text-muted-foreground'>{page.value}</code>
        {page.hint && <span className='text-xs text-muted-foreground'>{page.hint}</span>}
      </div>
      <div className='grid gap-3 lg:grid-cols-2'>
        <TemplateField control={control} name={`title_templates.${page.value}` as FieldPath<SEOForm>} label='标题模板' disabled={disabled} />
        <TemplateField control={control} name={`desc_templates.${page.value}` as FieldPath<SEOForm>} label='描述模板' disabled={disabled} />
      </div>
      {(title || desc) && (
        <p className='text-xs text-muted-foreground'>
          预览：<span className='text-foreground'>{previewTemplate(title ?? '') || '（用前台默认）'}</span>
          {desc ? ` · ${previewTemplate(desc)}` : ''}
        </p>
      )}
    </div>
  )
}

/** 模板输入框：右侧带一个「插入」下拉，把变量插到光标处。 */
function TemplateField<T extends FieldValues>({
  control,
  name,
  label,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  disabled: boolean
}) {
  const ref = useRef<HTMLInputElement>(null)
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <div className='flex items-start gap-2'>
            <FormControl>
              {/* 自己留一份 ref 用来定位光标，同时把元素交给 react-hook-form（它用 ref 做聚焦与滚动） */}
              <Input
                placeholder='留空用前台默认'
                disabled={disabled}
                {...field}
                value={field.value ?? ''}
                ref={(el) => {
                  ref.current = el
                  field.ref(el)
                }}
              />
            </FormControl>
            {!disabled && (
              <InsertMenu
                onPick={(variable) => {
                  const el = ref.current
                  const { value, cursor } = insertVariable(
                    String(field.value ?? ''),
                    variable,
                    el?.selectionStart ?? undefined,
                    el?.selectionEnd ?? undefined
                  )
                  field.onChange(value)
                  requestAnimationFrame(() => {
                    el?.focus()
                    el?.setSelectionRange(cursor, cursor)
                  })
                }}
              />
            )}
          </div>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** 「插入」按钮：展开可用变量，点一下插入。用原生 details 以免为一个小菜单引入更多组件。 */
function InsertMenu({ onPick }: { onPick: (variable: string) => void }) {
  const config = useQuery(seoConfigQuery)
  const [open, setOpen] = useState(false)
  const variables = config.data?.variables ?? []
  return (
    <details className='relative' open={open} onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}>
      <summary className='flex h-9 cursor-pointer items-center rounded-md border px-2 text-xs whitespace-nowrap select-none hover:bg-muted'>
        插入
      </summary>
      <div className='absolute inset-e-0 z-20 mt-1 flex w-56 flex-col rounded-md border bg-popover p-1 shadow-md'>
        {variables.map((v) => (
          <button
            key={v.value}
            type='button'
            className='flex items-center justify-between gap-2 rounded px-2 py-1 text-start text-sm hover:bg-muted'
            onClick={() => {
              onPick(v.value)
              setOpen(false)
            }}
          >
            {v.label}
            <code className='text-[10px] text-muted-foreground'>{`{${v.value}}`}</code>
          </button>
        ))}
      </div>
    </details>
  )
}

// ---------- 统计代码 ----------

const analyticsSchema = z.object({
  head_snippet: z.string().max(MAX_ANALYTICS, `最多 ${MAX_ANALYTICS} 个字符`),
  body_snippet: z.string().max(MAX_ANALYTICS, `最多 ${MAX_ANALYTICS} 个字符`),
})

type AnalyticsForm = z.infer<typeof analyticsSchema>

/**
 * 统计代码：只有 seo.analytics_code 才能看到这张卡片。
 * 这段代码原样注入页面，等于给站点注入任意脚本，所以界面上也要把风险说清楚。
 */
function AnalyticsSection({ disabledReason }: { disabledReason: string | null }) {
  const qc = useQueryClient()
  const code = useQuery(analyticsQuery)
  const form = useForm<AnalyticsForm>({
    resolver: zodResolver(analyticsSchema),
    defaultValues: { head_snippet: '', body_snippet: '' },
    values: code.data ? { head_snippet: code.data.head_snippet, body_snippet: code.data.body_snippet } : undefined,
  })
  const save = useMutation({
    mutationFn: (v: AnalyticsForm) => seoApi.setAnalytics(v as AnalyticsCode),
    onSuccess: (res) => {
      qc.setQueryData(seoKeys.analytics, res)
      form.reset(res)
      toast.success('已保存统计代码，前台几秒内生效')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  const editable = !disabledReason
  const dirty = form.formState.isDirty

  return (
    <section className='flex flex-col gap-3'>
      <div className='flex flex-col gap-0.5'>
        <h2 className='font-medium'>统计代码</h2>
        <p className='text-xs text-muted-foreground'>第三方统计或埋点代码，保存后原样注入每个页面。</p>
      </div>
      {code.isError ? (
        <LoadError onRetry={() => void code.refetch()} />
      ) : (
        <Form {...form}>
          <form className='flex flex-col gap-4 rounded-md border p-4' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
            <p className='flex items-start gap-2 rounded-md border border-dashed p-3 text-xs text-muted-foreground'>
              <ShieldAlert className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
              <span>
                这里的内容<strong className='text-foreground'>不会被清洗</strong>
                ，会原样出现在每个页面上：填错或粘到不可信的代码等于让第三方在站点里执行脚本。 只放你确认过来源的统计代码；每次修改都会记入审计日志。
              </span>
            </p>
            <SnippetField control={form.control} name='head_snippet' label='head 代码' hint='注入 <head>，适合统计初始化脚本' disabled={!editable} />
            <SnippetField
              control={form.control}
              name='body_snippet'
              label='body 代码'
              hint='注入 </body> 前，适合 noscript 像素等'
              disabled={!editable}
            />
            <div className='flex flex-wrap items-center justify-end gap-2'>
              {dirty && <span className='me-auto text-sm text-muted-foreground'>有未保存的修改</span>}
              <Button type='button' variant='outline' disabled={!dirty || save.isPending} onClick={() => form.reset()}>
                还原
              </Button>
              <WithReason reason={disabledReason}>
                <Button type='submit' disabled={!editable || !dirty || save.isPending}>
                  {save.isPending ? '保存中…' : '保存统计代码'}
                </Button>
              </WithReason>
            </div>
          </form>
        </Form>
      )}
    </section>
  )
}

/** 代码输入框：等宽字体 + 字符计数（上限 20000，接近时要能看出来）。 */
function SnippetField<T extends FieldValues>({
  control,
  name,
  label,
  hint,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  hint: string
  disabled: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => {
        const length = String(field.value ?? '').length
        return (
          <FormItem>
            <FormLabel>
              {label}
              <span className='ms-2 text-xs font-normal text-muted-foreground tabular-nums'>
                {length} / {MAX_ANALYTICS}
              </span>
            </FormLabel>
            <FormControl>
              <Textarea
                rows={6}
                spellCheck={false}
                className='font-mono text-xs'
                placeholder='选填，粘贴第三方统计代码'
                disabled={disabled}
                {...field}
                value={field.value ?? ''}
              />
            </FormControl>
            <FormDescription>{hint}</FormDescription>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}

export { optionLabel }
