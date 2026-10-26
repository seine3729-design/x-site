import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Info, ShieldAlert } from 'lucide-react'
import { useForm, type Control } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import { Badge, Button, Input, Switch } from '@/components/ui'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SwitchField, TextField } from '@/features/content/components/fields'
import {
  featureApi,
  featureConfigQuery,
  featureKeys,
  rememberLabel,
  type AgeGate,
  type FlagInfo,
} from './features-api'

/**
 * 功能开关与年龄门（docs/06 第 36 节，S3-3 / A3-3）。
 *
 * 关开关会直接影响线上体验，所以每项都写明关闭影响，关闭时还要二次确认。
 * 保存后站点版本 +1，各端几秒内生效。
 */
export function FeaturesPage() {
  const can = useCan()
  const visible = can('features.view')
  const config = useQuery({ ...featureConfigQuery, enabled: visible })

  if (!visible) return <NoPermission title='功能开关与年龄门' />
  const editable = can('features.edit')

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='功能开关与年龄门'
        description='关闭某个功能会立即影响前台，每项都标明了关闭的后果。保存后几秒内各端生效。'
        readonly={!editable}
      />
      {config.isError ? (
        <LoadError onRetry={() => void config.refetch()} />
      ) : (
        <>
          <FlagsSection flags={config.data?.flags} editable={editable} loading={config.isPending} />
          <AgeGateSection gate={config.data?.age_gate} editable={editable} />
        </>
      )}
    </div>
  )
}

// ---------- 功能开关 ----------

function FlagsSection({
  flags,
  editable,
  loading,
}: {
  flags: FlagInfo[] | undefined
  editable: boolean
  loading: boolean
}) {
  const qc = useQueryClient()
  const [confirm, setConfirm] = useState<FlagInfo | null>(null)

  // 接口是整体保存，但提示要说清改的是哪一项，所以把目标开关一起带进 mutation。
  // （不能靠「比对返回值与提交值」反推：整体保存时全部开关都一致，第一项就会被误当成改动项。）
  const save = useMutation({
    mutationFn: ({ next }: { next: Record<string, boolean>; target: FlagInfo; on: boolean }) =>
      featureApi.setFlags(next),
    onSuccess: (res, { target, on }) => {
      qc.setQueryData(featureKeys.config, res)
      setConfirm(null)
      toast.success(`已${on ? '开启' : '关闭'}「${target.title}」`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  /** 整体保存：把当前全部开关发上去，只改目标那一项。 */
  const apply = (target: FlagInfo, on: boolean) => {
    const next: Record<string, boolean> = {}
    for (const f of flags ?? []) next[f.flag] = f.flag === target.flag ? on : f.on
    save.mutate({ next, target, on })
  }

  const toggle = (f: FlagInfo, on: boolean) => {
    // 关闭是有后果的操作，先确认；开启直接生效
    if (!on) {
      setConfirm(f)
      return
    }
    apply(f, true)
  }

  return (
    <section className='flex flex-col gap-3'>
      <h2 className='font-medium'>功能开关</h2>
      {loading ? (
        <p className='text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <div className='grid gap-3 lg:grid-cols-2'>
          {(flags ?? []).map((f) => (
            <div key={f.flag} className='flex items-start justify-between gap-4 rounded-md border p-4'>
              <div className='flex flex-col gap-1'>
                <div className='flex items-center gap-2'>
                  <span className='font-medium'>{f.title}</span>
                  {!f.on && <Badge variant='destructive'>已关闭</Badge>}
                </div>
                <div className='flex items-start gap-1.5 text-xs text-muted-foreground'>
                  <Info className='mt-0.5 size-3.5 shrink-0' aria-hidden />
                  <span>{f.impact}</span>
                </div>
                <code className='text-xs text-muted-foreground'>{f.flag}</code>
              </div>
              <WithReason reason={editable ? null : '当前角色只能查看'}>
                <Switch
                  checked={f.on}
                  disabled={!editable || save.isPending}
                  aria-label={`${f.title}开关`}
                  onCheckedChange={(on) => toggle(f, on)}
                />
              </WithReason>
            </div>
          ))}
        </div>
      )}

      {confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirm(null)}
          destructive
          title={`确认关闭「${confirm.title}」？`}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>{confirm.impact}</p>
              <p className='text-muted-foreground'>保存后几秒内前台生效。随时可以再打开。</p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={save.isPending ? '关闭中…' : '确认关闭'}
          isLoading={save.isPending}
          handleConfirm={() => apply(confirm, false)}
        />
      )}
    </section>
  )
}

// ---------- 年龄确认门 ----------

const ageGateSchema = z
  .object({
    enabled: z.boolean(),
    title: z.string().trim().max(60, '标题最多 60 个字'),
    desc: z.string().trim().max(500, '说明文字最多 500 个字'),
    confirm_text: z.string().trim().max(20, '按钮文字最多 20 个字'),
    deny_text: z.string().trim().max(20, '按钮文字最多 20 个字'),
    deny_url: z
      .string()
      .trim()
      .max(500, '最多 500 个字符')
      .refine((v) => v === '' || /^https?:\/\/.+/.test(v), '要填完整的 http(s) 网址'),
    remember_days: z
      .number({ error: '请填写 0–365 的整数' })
      .int('请填写整数')
      .min(0, '不能是负数')
      .max(365, '最多 365 天'),
  })
  .superRefine((v, ctx) => {
    // 和后端同一条规则：开着的年龄门挡在所有访客前面，文案缺失会让人看到无法操作的弹层
    if (!v.enabled) return
    for (const [field, label] of [
      ['title', '标题'],
      ['confirm_text', '确认按钮文字'],
      ['deny_text', '拒绝按钮文字'],
    ] as const) {
      if (!v[field]) {
        ctx.addIssue({ code: 'custom', path: [field], message: `启用时必须填写${label}` })
      }
    }
  })

type AgeGateForm = z.infer<typeof ageGateSchema>

function AgeGateSection({ gate, editable }: { gate: AgeGate | undefined; editable: boolean }) {
  const qc = useQueryClient()
  const form = useForm<AgeGateForm>({
    resolver: zodResolver(ageGateSchema),
    defaultValues: {
      enabled: false,
      title: '',
      desc: '',
      confirm_text: '',
      deny_text: '',
      deny_url: '',
      remember_days: 30,
    },
  })

  // 读到配置后回填（首次加载时 gate 还是 undefined）
  useEffect(() => {
    if (gate) form.reset(gate)
  }, [gate, form])

  const save = useMutation({
    mutationFn: (v: AgeGateForm) => featureApi.setAgeGate(v),
    onSuccess: (res) => {
      qc.setQueryData(featureKeys.config, res)
      form.reset(res.age_gate)
      toast.success(res.age_gate.enabled ? '年龄确认门已启用' : '年龄确认门已关闭')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const enabled = form.watch('enabled')
  const days = form.watch('remember_days')

  return (
    <section className='flex flex-col gap-3'>
      <div className='flex flex-col gap-0.5'>
        <h2 className='font-medium'>年龄确认门</h2>
        <p className='text-xs text-muted-foreground'>
          开启后访客首次打开站点要先确认年龄。文案留空会让访客看到无法操作的弹层，所以启用时标题与两个按钮文字必填。
        </p>
      </div>
      <Form {...form}>
        <form
          className='flex flex-col gap-4 rounded-md border p-4'
          onSubmit={form.handleSubmit((v) => save.mutate(v))}
        >
          <SwitchField
            control={form.control}
            name='enabled'
            label='启用年龄确认门'
            description={enabled ? '访客需要先确认才能浏览' : '关闭时下面的文案仍会保存，可以先准备好再启用'}
            disabled={!editable}
          />
          <div className='grid gap-4 sm:grid-cols-2'>
            <TextField
              control={form.control}
              name='title'
              label='标题'
              required={enabled}
              placeholder='请输入弹层标题，如 内容分级提示'
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='deny_url'
              label='拒绝后跳转'
              placeholder='请输入完整网址，留空则停留在当前页'
              disabled={!editable}
            />
          </div>
          <TextField
            control={form.control}
            name='desc'
            label='说明文字'
            multiline
            rows={3}
            placeholder='请输入说明文字，如 本站内容可能包含不适合未成年人的情节，请确认您已满 18 周岁'
            disabled={!editable}
          />
          <div className='grid gap-4 sm:grid-cols-3'>
            <TextField
              control={form.control}
              name='confirm_text'
              label='确认按钮文字'
              required={enabled}
              placeholder='如 我已满 18 周岁'
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='deny_text'
              label='拒绝按钮文字'
              required={enabled}
              placeholder='如 未满 18 周岁'
              disabled={!editable}
            />
            <AgeGateDaysField control={form.control} disabled={!editable} hint={rememberLabel(days)} />
          </div>
          {enabled && (
            <div className='flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-3 text-xs'>
              <ShieldAlert className='mt-0.5 size-4 shrink-0 text-amber-600' aria-hidden />
              <span>
                年龄门会挡在所有前台页面之前。启用后请在前台实际确认一次弹层显示与跳转是否正常。
              </span>
            </div>
          )}
          {editable && (
            <div className='flex items-center gap-2'>
              <Button type='submit' disabled={save.isPending || !form.formState.isDirty}>
                {save.isPending ? '保存中…' : '保存'}
              </Button>
              {form.formState.isDirty && (
                <Button type='button' variant='ghost' onClick={() => gate && form.reset(gate)}>
                  放弃修改
                </Button>
              )}
            </div>
          )}
        </form>
      </Form>
    </section>
  )
}

/**
 * 重复间隔：数字输入 + 中文说明。
 *
 * 没用 TextField：它的值是字符串，而这里要整数（空输入要变成 NaN 好让 zod 报错，
 * 不能变成 0 —— 0 在这里是「每次都确认」的有效取值）。写法与存储源页的「预签名有效期」一致。
 */
function AgeGateDaysField({
  control,
  disabled,
  hint,
}: {
  control: Control<AgeGateForm>
  disabled: boolean
  hint: string
}) {
  return (
    <FormField
      control={control}
      name='remember_days'
      render={({ field }) => (
        <FormItem>
          <FormLabel>重复间隔（天）</FormLabel>
          <FormControl>
            <Input
              type='number'
              inputMode='numeric'
              placeholder='0–365，0 表示每次都确认'
              disabled={disabled}
              name={field.name}
              ref={field.ref}
              onBlur={field.onBlur}
              value={Number.isNaN(field.value) ? '' : field.value}
              onChange={(e) => field.onChange(e.target.value === '' ? Number.NaN : Number(e.target.value))}
            />
          </FormControl>
          <FormDescription>{hint}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
