import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm, useWatch, type Control, type FieldPath, type FieldValues, type UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { Badge, Button, Input } from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SelectField, SwitchField, TextField } from '@/features/content/components/fields'
import {
  brandingApi,
  brandingConfigQuery,
  brandingKeys,
  MAX_COPYRIGHT,
  MAX_IMAGE_KEY,
  MAX_SITE_NAME,
  MAX_SLOGAN,
  MAX_SUPPORT_URL,
  type Brand,
  type BrandingConfig,
  type BrandingOption,
  type BrandingOptions,
  type Layout,
  type Theme,
} from './branding-api'
import {
  cleanColors,
  coverAspect,
  IMAGE_KEY_HINT,
  isHexColorOrEmpty,
  isHttpURLOrEmpty,
  isImageKeyOrEmpty,
  optionLabel,
  previewColors,
  previewScheme,
} from './branding-utils'

/**
 * 品牌与主题（docs/06 第 16 节，S3-3 / A3-3）。
 *
 * 三段分别保存（改配色不会把正在编辑的布局一起写回去），右侧跟着一个实时预览：
 * 配色与布局是「看了才知道对不对」的东西，光看表单很难判断。
 * 下拉的可选值都来自接口，前端不另抄一份枚举。
 */
export function BrandingPage() {
  const can = useCan()
  const visible = can('brand.view')
  const config = useQuery({ ...brandingConfigQuery, enabled: visible })

  if (!visible) return <NoPermission title='品牌与主题' />
  const editable = can('brand.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='品牌与主题'
        description='站名、Logo、配色与布局。三块分别保存，保存后站点配置版本 +1，前台几秒内生效。右侧预览只是示意，实际渲染以前台为准。'
        readonly={!editable}
      />
      {config.isError ? (
        <LoadError onRetry={() => void config.refetch()} />
      ) : config.isPending ? (
        <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]'>
          <div className='flex min-w-0 flex-col gap-4'>
            <BrandSection brand={config.data.brand} disabledReason={readonlyReason} />
            <ThemeSection theme={config.data.theme} options={config.data.options} disabledReason={readonlyReason} />
            <LayoutSection layout={config.data.layout} options={config.data.options} disabledReason={readonlyReason} />
          </div>
          {/* 预览用已保存的配置：编辑中的实时预览在各自的卡片里（表单值拿不到跨卡片） */}
          <Preview config={config.data} />
        </div>
      )}
    </div>
  )
}

// ---------- 品牌 ----------

const brandSchema = z.object({
  site_name: z.string().trim().min(1, '请填写站名').max(MAX_SITE_NAME, `站名最多 ${MAX_SITE_NAME} 个字`),
  slogan: z.string().trim().max(MAX_SLOGAN, `口号最多 ${MAX_SLOGAN} 个字`),
  copyright: z.string().trim().max(MAX_COPYRIGHT, `版权文字最多 ${MAX_COPYRIGHT} 个字`),
  support_url: z.string().trim().max(MAX_SUPPORT_URL, `最多 ${MAX_SUPPORT_URL} 个字符`).refine(isHttpURLOrEmpty, '要填完整的 http(s) 网址'),
  logo_light: z.string().trim().max(MAX_IMAGE_KEY, `最多 ${MAX_IMAGE_KEY} 个字符`).refine(isImageKeyOrEmpty, IMAGE_KEY_HINT),
  favicon: z.string().trim().max(MAX_IMAGE_KEY, `最多 ${MAX_IMAGE_KEY} 个字符`).refine(isImageKeyOrEmpty, IMAGE_KEY_HINT),
})

function BrandSection({ brand, disabledReason }: { brand: Brand; disabledReason: string | null }) {
  const form = useForm<z.infer<typeof brandSchema>>({ resolver: zodResolver(brandSchema), defaultValues: brand })
  const save = useSegmentSave(
    '品牌',
    (v: Brand) => brandingApi.setBrand(v),
    (cfg) => cfg.brand,
    form.reset
  )
  const editable = !disabledReason

  return (
    <Section
      title='品牌'
      hint='站名会用在页头、浏览器标题与各处兜底文案；Logo 留空时前台显示站名文字。'
      form={form}
      disabledReason={disabledReason}
      pending={save.isPending}
      onSubmit={(v) => save.mutate(v)}
    >
      <div className='grid gap-4 sm:grid-cols-2'>
        <TextField control={form.control} name='site_name' label='站名' required placeholder='请输入站名，如 短剧站' disabled={!editable} />
        <TextField control={form.control} name='slogan' label='口号' placeholder='选填，如 每天追好剧' disabled={!editable} />
      </div>
      <TextField control={form.control} name='copyright' label='版权文字' placeholder='选填，显示在页脚，如 © 2026 短剧站' disabled={!editable} />
      <div className='grid gap-4 sm:grid-cols-2'>
        <TextField control={form.control} name='support_url' label='客服链接' placeholder='选填，如 https://t.me/support' disabled={!editable} />
        <TextField
          control={form.control}
          name='logo_light'
          label='头部 Logo'
          placeholder='选填，如 img/logo.webp'
          description={IMAGE_KEY_HINT + '；留空显示站名文字'}
          disabled={!editable}
        />
        <TextField control={form.control} name='favicon' label='favicon' placeholder='选填，如 img/favicon.png' disabled={!editable} />
      </div>
    </Section>
  )
}

// ---------- 配色 ----------

/** 配色表单：六个颜色键都在表单里，空值表示「用预设色」。 */
type ThemeForm = Omit<Theme, 'colors'> & { colors: Record<string, string> }

function themeSchema(options: BrandingOptions) {
  return z.object({
    preset: z.string().min(1, '请选择皮肤预设'),
    color_scheme: z.string().min(1, '请选择默认深浅色'),
    allow_scheme_toggle: z.boolean(),
    colors: z.record(z.string(), z.string().refine(isHexColorOrEmpty, '要填 #RGB 或 #RRGGBB 形式的色值')),
    radius: z
      .number({ error: `请填写 0–${options.max_radius} 的整数` })
      .int('请填写整数')
      .min(0, '不能是负数')
      .max(options.max_radius, `最多 ${options.max_radius} 像素`),
    cover_ratio: z.string().min(1, '请选择封面比例'),
    font: z.string().min(1, '请选择字体'),
  })
}

function ThemeSection({ theme, options, disabledReason }: { theme: Theme; options: BrandingOptions; disabledReason: string | null }) {
  const schema = themeSchema(options)
  const form = useForm<ThemeForm>({
    resolver: zodResolver(schema),
    // 把六个颜色键都放进表单，空值表示「用预设色」
    defaultValues: { ...theme, colors: Object.fromEntries(options.colors.map((c) => [c.value, theme.colors?.[c.value] ?? ''])) },
  })
  const save = useSegmentSave(
    '配色',
    (v: Theme) => brandingApi.setTheme({ ...v, colors: cleanColors(v.colors) }),
    (cfg) => ({ ...cfg.theme, colors: Object.fromEntries(options.colors.map((c) => [c.value, cfg.theme.colors?.[c.value] ?? ''])) }),
    form.reset
  )
  const editable = !disabledReason
  const current = useWatch({ control: form.control }) as ThemeForm

  return (
    <Section
      title='配色'
      hint='先选皮肤预设，再按需覆盖单个颜色（留空就用预设色）。改动会立刻反映在下面的小预览里。'
      form={form}
      disabledReason={disabledReason}
      pending={save.isPending}
      onSubmit={(v) => save.mutate(v)}
    >
      <div className='grid gap-4 sm:grid-cols-3'>
        <SelectField
          control={form.control}
          name='preset'
          label='皮肤预设'
          required
          placeholder='请选择皮肤预设'
          items={toItems(options.presets)}
          disabled={!editable}
        />
        <SelectField
          control={form.control}
          name='color_scheme'
          label='默认深浅色'
          required
          placeholder='请选择默认深浅色'
          items={toItems(options.color_schemes)}
          disabled={!editable}
        />
        <SelectField
          control={form.control}
          name='cover_ratio'
          label='封面比例'
          required
          placeholder='请选择封面比例'
          items={toItems(options.cover_ratios)}
          disabled={!editable}
        />
        <SelectField
          control={form.control}
          name='font'
          label='字体'
          required
          placeholder='请选择字体'
          items={toItems(options.fonts)}
          disabled={!editable}
        />
        <NumberField
          control={form.control}
          name='radius'
          label='卡片圆角'
          placeholder={`0–${options.max_radius}`}
          description={`单位像素，最多 ${options.max_radius}`}
          disabled={!editable}
        />
      </div>
      <SwitchField
        control={form.control}
        name='allow_scheme_toggle'
        label='允许访客切换深浅色'
        description='关闭后前台不显示切换按钮，始终用上面选的默认值。'
        disabled={!editable}
      />
      <fieldset className='flex flex-col gap-3 rounded-md border p-3' disabled={!editable}>
        <legend className='px-1 text-sm font-medium'>颜色</legend>
        <p className='text-xs text-muted-foreground'>留空表示用预设色。色值形如 #ff0033 或 #f03。</p>
        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
          {options.colors.map((c) => (
            <ColorField key={c.value} control={form.control} name={`colors.${c.value}` as FieldPath<ThemeForm>} option={c} disabled={!editable} />
          ))}
        </div>
      </fieldset>
      <ThemePreview theme={current} />
    </Section>
  )
}

/** 一个颜色输入：色块 + 文本框。原生 color 选择器不能表达「留空用预设色」，所以用文本框为主。 */
function ColorField<T extends FieldValues>({
  control,
  name,
  option,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  option: BrandingOption
  disabled: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {option.label}
            <code className='ms-1 text-xs text-muted-foreground'>{option.value}</code>
          </FormLabel>
          <div className='flex items-center gap-2'>
            <span
              className='size-8 shrink-0 rounded-md border'
              style={{ background: isHexColorOrEmpty(field.value ?? '') && field.value ? field.value : 'transparent' }}
              aria-hidden
            />
            <FormControl>
              <Input placeholder='留空用预设色' maxLength={7} disabled={disabled} {...field} />
            </FormControl>
          </div>
          {option.hint && <FormDescription>{option.hint}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** 配色卡片里的小预览：改一个颜色能立刻看到效果。 */
function ThemePreview({ theme }: { theme: ThemeForm }) {
  const scheme = previewScheme(theme.color_scheme)
  const c = previewColors(theme, scheme)
  return (
    <div className='flex flex-col gap-2'>
      <span className='text-sm font-medium'>效果示意</span>
      <div className='flex items-center gap-3 rounded-md border p-3' style={{ background: c.bg, color: c.text }}>
        <div
          className='w-16 shrink-0 overflow-hidden border'
          style={{ aspectRatio: coverAspect(theme.cover_ratio), background: c.surface, borderRadius: theme.radius }}
        />
        <div className='flex min-w-0 flex-col gap-1'>
          <span className='truncate text-sm font-medium'>剧名示意</span>
          <span className='truncate text-xs' style={{ color: c.muted }}>
            更新至 12 集
          </span>
          <div className='flex gap-2'>
            <span className='px-2 py-0.5 text-xs' style={{ background: c.primary, color: '#fff', borderRadius: theme.radius }}>
              立即观看
            </span>
            <span className='px-2 py-0.5 text-xs' style={{ background: c.accent, color: '#111', borderRadius: theme.radius }}>
              HOT
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

// ---------- 布局 ----------

const layoutSchema = z.object({
  header_style: z.string().min(1, '请选择页头样式'),
  card_style: z.string().min(1, '请选择卡片样式'),
  play_mode: z.string().min(1, '请选择播放形态'),
  mobile_tabbar: z.boolean(),
})

function LayoutSection({ layout, options, disabledReason }: { layout: Layout; options: BrandingOptions; disabledReason: string | null }) {
  const form = useForm<z.infer<typeof layoutSchema>>({ resolver: zodResolver(layoutSchema), defaultValues: layout })
  const save = useSegmentSave(
    '布局',
    (v: Layout) => brandingApi.setLayout(v),
    (cfg) => cfg.layout,
    form.reset
  )
  const editable = !disabledReason

  return (
    <Section
      title='布局'
      hint='页头与卡片样式影响所有列表页；播放形态决定点开一部剧是进沉浸式还是详情页。'
      form={form}
      disabledReason={disabledReason}
      pending={save.isPending}
      onSubmit={(v) => save.mutate(v)}
    >
      <div className='grid gap-4 sm:grid-cols-3'>
        <SelectField
          control={form.control}
          name='header_style'
          label='页头样式'
          required
          placeholder='请选择页头样式'
          items={toItems(options.header_styles)}
          disabled={!editable}
        />
        <SelectField
          control={form.control}
          name='card_style'
          label='卡片样式'
          required
          placeholder='请选择卡片样式'
          items={toItems(options.card_styles)}
          disabled={!editable}
        />
        <SelectField
          control={form.control}
          name='play_mode'
          label='播放形态'
          required
          placeholder='请选择播放形态'
          items={toItems(options.play_modes)}
          disabled={!editable}
        />
      </div>
      <SwitchField
        control={form.control}
        name='mobile_tabbar'
        label='手机显示底部标签栏'
        description='关闭后手机端只保留顶部导航与侧边抽屉。'
        disabled={!editable}
      />
    </Section>
  )
}

// ---------- 预览 ----------

/** 右侧预览：用已保存的配置画一个页头 + 两张卡片，让人直观看到站名、配色、页头与卡片样式。 */
function Preview({ config }: { config: BrandingConfig }) {
  const { brand, theme, layout, options } = config
  const scheme = previewScheme(theme.color_scheme)
  const c = previewColors(theme, scheme)
  const header = layout.header_style
  const overlay = layout.card_style === 'overlay'

  return (
    <aside className='flex flex-col gap-2 xl:sticky xl:top-4 xl:h-fit'>
      <div className='flex items-center justify-between'>
        <span className='text-sm font-medium'>前台预览</span>
        <Badge variant='outline'>{optionLabel(options.color_schemes, theme.color_scheme)}</Badge>
      </div>
      <div className='overflow-hidden rounded-lg border' style={{ background: c.bg, color: c.text }}>
        {/* 页头 */}
        <div
          className={`flex items-center gap-2 border-b px-3 py-2 text-xs ${header === 'centered' ? 'flex-col' : ''}`}
          style={{ borderColor: c.surface, background: header === 'pill' ? 'transparent' : c.surface }}
        >
          <div className={`flex items-center gap-2 ${header === 'centered' ? '' : 'me-auto'}`}>
            {brand.logo_light ? (
              <span className='rounded px-1.5 py-0.5 text-[10px]' style={{ background: c.primary, color: '#fff' }}>
                LOGO
              </span>
            ) : (
              <span className='font-semibold'>{brand.site_name || '站名'}</span>
            )}
            {brand.slogan && (
              <span className='truncate text-[10px]' style={{ color: c.muted }}>
                {brand.slogan}
              </span>
            )}
          </div>
          <nav
            className={`flex gap-1 ${header === 'pill' ? 'rounded-full px-2 py-1' : ''}`}
            style={header === 'pill' ? { background: c.surface } : undefined}
          >
            {['首页', '探索', '排行'].map((t) => (
              <span key={t} className='px-1.5' style={{ color: c.muted }}>
                {t}
              </span>
            ))}
          </nav>
        </div>
        {/* 卡片 */}
        <div className='grid grid-cols-3 gap-2 p-3'>
          {['剧名一', '剧名二', '剧名三'].map((title, i) => (
            <div key={title} className='flex flex-col gap-1'>
              <div
                className='relative overflow-hidden border'
                style={{ aspectRatio: coverAspect(theme.cover_ratio), background: c.surface, borderColor: c.surface, borderRadius: theme.radius }}
              >
                {i === 0 && (
                  <span
                    className='absolute inset-e-1 top-1 px-1 text-[9px]'
                    style={{ background: c.accent, color: '#111', borderRadius: theme.radius }}
                  >
                    HOT
                  </span>
                )}
                {overlay && (
                  <span
                    className='absolute inset-x-0 bottom-0 truncate px-1 py-0.5 text-[10px]'
                    style={{ background: 'linear-gradient(transparent, rgba(0,0,0,.75))', color: '#fff' }}
                  >
                    {title}
                  </span>
                )}
              </div>
              {!overlay && <span className='truncate text-[10px]'>{title}</span>}
            </div>
          ))}
        </div>
        {/* 底栏与页脚 */}
        {layout.mobile_tabbar && (
          <div className='flex justify-around border-t px-2 py-1 text-[10px]' style={{ borderColor: c.surface, background: c.surface }}>
            {['首页', '刷剧', '我的'].map((t, i) => (
              <span key={t} style={{ color: i === 0 ? c.primary : c.muted }}>
                {t}
              </span>
            ))}
          </div>
        )}
        <div className='px-3 py-2 text-[10px]' style={{ color: c.muted }}>
          {brand.copyright || '页脚版权文字'}
        </div>
      </div>
      <p className='text-xs text-muted-foreground'>
        示意图：{optionLabel(options.header_styles, layout.header_style)} 页头 · {optionLabel(options.card_styles, layout.card_style)} ·{' '}
        {optionLabel(options.play_modes, layout.play_mode)}。真实渲染以前台为准。
      </p>
    </aside>
  )
}

// ---------- 公共部件 ----------

/** 一段配置的卡片：标题 + 说明 + 表单 + 右下角的保存按钮（没有改动时禁用）。 */
function Section<T extends FieldValues>({
  title,
  hint,
  form,
  disabledReason,
  pending,
  onSubmit,
  children,
}: {
  title: string
  hint: string
  form: UseFormReturn<T>
  disabledReason: string | null
  pending: boolean
  onSubmit: (v: T) => void
  children: React.ReactNode
}) {
  const dirty = form.formState.isDirty
  return (
    <section className='flex flex-col gap-3'>
      <div className='flex flex-col gap-0.5'>
        <h2 className='font-medium'>{title}</h2>
        <p className='text-xs text-muted-foreground'>{hint}</p>
      </div>
      <Form {...form}>
        <form className='flex flex-col gap-4 rounded-md border p-4' onSubmit={form.handleSubmit((v) => onSubmit(v))} noValidate>
          {children}
          <div className='flex flex-wrap items-center justify-end gap-2'>
            {dirty && <span className='me-auto text-sm text-muted-foreground'>有未保存的修改</span>}
            <Button type='button' variant='outline' disabled={!dirty || pending} onClick={() => form.reset()}>
              还原
            </Button>
            <WithReason reason={disabledReason}>
              <Button type='submit' disabled={!!disabledReason || !dirty || pending}>
                {pending ? '保存中…' : `保存${title}`}
              </Button>
            </WithReason>
          </div>
        </form>
      </Form>
    </section>
  )
}

/**
 * 一段配置的保存：成功后用响应替换本地缓存并重置表单（这样「有未保存的修改」会消失），
 * 失败时提示后端的原话（校验细则在后端，前端只拦明显的格式问题）。
 */
function useSegmentSave<T>(
  name: string,
  save: (v: T) => Promise<BrandingConfig>,
  pick: (cfg: BrandingConfig) => unknown,
  reset: (values?: any) => void
) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: save,
    onSuccess: (res) => {
      qc.setQueryData(brandingKeys.config, res)
      reset(pick(res))
      toast.success(`已保存${name}，前台几秒内生效`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
}

/** 数字输入：react-hook-form 默认给字符串，这里转成数字再交给 zod。 */
function NumberField<T extends FieldValues>({
  control,
  name,
  label,
  placeholder,
  description,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  placeholder: string
  description?: string
  disabled?: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              inputMode='numeric'
              name={field.name}
              ref={field.ref}
              onBlur={field.onBlur}
              placeholder={placeholder}
              disabled={disabled}
              // 清空时显示空串而不是 NaN（显示 NaN 会让再次输入变成 "NaN9"），只收数字
              value={Number.isFinite(field.value) ? String(field.value) : ''}
              onChange={(e) => {
                const v = e.target.value.replace(/[^\d]/g, '')
                field.onChange(v === '' ? Number.NaN : Number(v))
              }}
            />
          </FormControl>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function toItems(options: BrandingOption[]): { value: string; label: string }[] {
  return options.map((o) => ({ value: o.value, label: o.hint ? `${o.label}（${o.hint}）` : o.label }))
}
