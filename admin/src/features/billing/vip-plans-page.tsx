import { useEffect } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CreditCard, Info } from 'lucide-react'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge, Button, Checkbox, Input, Switch } from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { SelectDropdown } from '@/components/select-dropdown'
import { LoadError, PageHeading } from '@/features/access/shared'
import {
  billingApi,
  billingKeys,
  CHANNEL_LABELS,
  CLIENT_LABELS,
  PLAN_CODE_LABELS,
  vipPlansQuery,
  type AdminVIPPlan,
  type AdminVIPPlanInput,
  type PaymentChannel,
  type VIPClient,
} from './api'

const schema = z.object({
  duration_days: z.number({ error: '请输入 1–3660 的整数' }).int().min(1, '最小 1 天').max(3660, '最大 3660 天'),
  price_fen: z.number({ error: '请输入非负整数' }).int().min(0, '不能为负数'),
  list_price_fen: z.number({ error: '请输入非负整数' }).int().min(0, '不能为负数'),
  price_micro_usdt: z.number({ error: '请输入非负整数' }).int().min(0, '不能为负数'),
  badge_key: z.string().max(64, '最多 64 个字符'),
  recommended: z.boolean(),
  channels: z.array(z.enum(['alipay', 'wechat', 'usdt', 'iap_apple', 'iap_google'])),
  clients: z.array(z.enum(['web_pc', 'web_mobile', 'app_official', 'app_store'])),
  sort: z.number({ error: '请输入 0–9999 的整数' }).int().min(0, '最小 0').max(9999, '最大 9999'),
  status: z.enum(['on', 'off']),
})

type PlanForm = z.infer<typeof schema>

const channelOptions = Object.entries(CHANNEL_LABELS).map(([value, label]) => ({ value: value as PaymentChannel, label }))
const clientOptions = Object.entries(CLIENT_LABELS).map(([value, label]) => ({ value: value as VIPClient, label }))

function formOf(plan: AdminVIPPlan): PlanForm {
  return {
    duration_days: plan.duration_days,
    price_fen: plan.price_fen,
    list_price_fen: plan.list_price_fen,
    price_micro_usdt: plan.price_micro_usdt,
    badge_key: plan.badge_key,
    recommended: plan.recommended,
    channels: plan.channels,
    clients: plan.clients,
    sort: plan.sort,
    status: plan.status,
  }
}

function toInput(values: PlanForm): AdminVIPPlanInput {
  return values
}

export function VIPPlansPage() {
  const can = useCan()
  const visible = can('vip_plans.view')
  const plans = useQuery({ ...vipPlansQuery, enabled: visible })

  if (!visible) return <NoPermission title='会员套餐' />
  if (plans.isError) return <LoadError onRetry={() => void plans.refetch()} />

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading title='会员套餐' description='固定五种卡。保存后立即清除前台套餐缓存；已创建订单仍使用下单时的商品快照。' readonly={!can('vip_plans.edit')} />
      <Alert>
        <Info />
        <AlertTitle>支付渠道当前仅用于模拟/预配置</AlertTitle>
        <AlertDescription>真实支付渠道凭据、收款和到账尚未启用。请勿将套餐上架误认为已可真实收款。</AlertDescription>
      </Alert>
      {plans.isPending || !plans.data ? (
        <div className='grid gap-4 xl:grid-cols-2'>
          {Array.from({ length: 5 }, (_, index) => <div key={index} className='h-96 animate-pulse rounded-md border' />)}
        </div>
      ) : (
        <div className='grid gap-4 xl:grid-cols-2'>
          {plans.data.map((plan) => <PlanCard key={plan.id} plan={plan} editable={can('vip_plans.edit')} />)}
        </div>
      )}
    </div>
  )
}

function PlanCard({ plan, editable }: { plan: AdminVIPPlan; editable: boolean }) {
  const queryClient = useQueryClient()
  const form = useForm<PlanForm>({ resolver: zodResolver(schema), defaultValues: formOf(plan) })

  useEffect(() => {
    form.reset(formOf(plan))
  }, [form, plan])

  const save = useMutation({
    mutationFn: (values: PlanForm) => billingApi.updateVIPPlan(plan.id, toInput(values)),
    onSuccess: (updated) => {
      queryClient.setQueryData<AdminVIPPlan[]>(billingKeys.vipPlans, (previous) => previous?.map((item) => (item.id === updated.id ? updated : item)) ?? [updated])
      form.reset(formOf(updated))
      toast.success(`已保存${PLAN_CODE_LABELS[updated.code]}`)
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const toggleChannel = (channel: PaymentChannel, checked: boolean) => {
    const current = form.getValues('channels')
    form.setValue('channels', checked ? [...current, channel] : current.filter((value) => value !== channel), { shouldDirty: true, shouldValidate: true })
  }
  const toggleClient = (client: VIPClient, checked: boolean) => {
    const current = form.getValues('clients')
    form.setValue('clients', checked ? [...current, client] : current.filter((value) => value !== client), { shouldDirty: true, shouldValidate: true })
  }

  return (
    <section className='rounded-md border bg-card p-5' aria-labelledby={`vip-plan-${plan.id}`}>
      <div className='mb-5 flex flex-wrap items-center gap-2'>
        <CreditCard className='size-5 text-primary' aria-hidden='true' />
        <h2 id={`vip-plan-${plan.id}`} className='text-lg font-semibold'>{PLAN_CODE_LABELS[plan.code]}</h2>
        <Badge variant={plan.status === 'on' ? 'default' : 'secondary'}>{plan.status === 'on' ? '上架' : '下架'}</Badge>
        {plan.recommended && <Badge variant='outline'>推荐</Badge>}
        <span className='ms-auto text-xs text-muted-foreground'>编码：{plan.code}</span>
      </div>

      <Form {...form}>
        <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((values) => save.mutate(values))} noValidate>
          <div className='grid gap-4 sm:grid-cols-2'>
            <NumberField form={form} name='duration_days' label='时长（天）' placeholder='1–3660，如 30' disabled={!editable} />
            <NumberField form={form} name='sort' label='排序' placeholder='0–9999，越小越靠前' disabled={!editable} />
            <NumberField form={form} name='price_fen' label='人民币售价（分）' placeholder='如 1990 表示 ¥19.90' disabled={!editable} />
            <NumberField form={form} name='list_price_fen' label='人民币划线价（分）' placeholder='0 表示不展示，如 2990' disabled={!editable} />
            <NumberField form={form} name='price_micro_usdt' label='USDT 价格（微单位）' placeholder='如 19900000 表示 19.9 USDT' disabled={!editable} />
            <FormField
              control={form.control}
              name='status'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>上架状态</FormLabel>
                  <SelectDropdown
                    defaultValue={field.value}
                    onValueChange={field.onChange}
                    isControlled
                    items={[{ value: 'off', label: '下架' }, { value: 'on', label: '上架' }]}
                    placeholder='请选择上架状态'
                    disabled={!editable}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='badge_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>角标文案键</FormLabel>
                <FormControl>
                  <Input placeholder='如 vip.badge.recommended；留空表示无角标' disabled={!editable} {...field} />
                </FormControl>
                <FormDescription>仅保存用户端 i18n 键，不在后台填写面向用户的文案。</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='recommended'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-md border p-3'>
                <div className='space-y-1'>
                  <FormLabel>推荐套餐</FormLabel>
                  <FormDescription>前台可突出展示，套餐 code 仍固定不变。</FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} disabled={!editable} aria-label='推荐套餐' />
                </FormControl>
              </FormItem>
            )}
          />

          <CheckboxGroup
            title='允许的支付渠道'
            description='当前仅保存预配置；没有真实渠道适配时不会向用户开放付款。'
            values={form.watch('channels')}
            options={channelOptions}
            disabled={!editable}
            onChange={toggleChannel}
          />
          <CheckboxGroup
            title='允许的客户端'
            description='上架套餐至少选择一个客户端；商店包的实际支付仍需走内购。'
            values={form.watch('clients')}
            options={clientOptions}
            disabled={!editable}
            onChange={toggleClient}
          />

          <div className='flex justify-end'>
            <Button type='submit' className='h-10' disabled={!editable || save.isPending || !form.formState.isDirty}>
              {save.isPending ? '保存中…' : '保存套餐'}
            </Button>
          </div>
        </form>
      </Form>
    </section>
  )
}

function NumberField({
  form,
  name,
  label,
  placeholder,
  disabled,
}: {
  form: UseFormReturn<PlanForm>
  name: 'duration_days' | 'sort' | 'price_fen' | 'list_price_fen' | 'price_micro_usdt'
  label: string
  placeholder: string
  disabled: boolean
}) {
  return (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              inputMode='numeric'
              min='0'
              placeholder={placeholder}
              disabled={disabled}
              name={field.name}
              ref={field.ref}
              onBlur={field.onBlur}
              value={Number.isNaN(field.value) ? '' : field.value}
              onChange={(event) => field.onChange(event.target.value === '' ? Number.NaN : Number(event.target.value))}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function CheckboxGroup<T extends string>({
  title,
  description,
  values,
  options,
  disabled,
  onChange,
}: {
  title: string
  description: string
  values: T[]
  options: Array<{ value: T; label: string }>
  disabled: boolean
  onChange: (value: T, checked: boolean) => void
}) {
  return (
    <fieldset className='rounded-md border p-3'>
      <legend className='px-1 text-sm font-medium'>{title}</legend>
      <p className='mb-3 text-xs text-muted-foreground'>{description}</p>
      <div className='grid gap-2 sm:grid-cols-2'>
        {options.map((option) => {
          const id = `${title}-${option.value}`
          return (
            <div key={option.value} className='flex items-center gap-2'>
              <Checkbox id={id} checked={values.includes(option.value)} onCheckedChange={(checked) => onChange(option.value, checked === true)} disabled={disabled} />
              <label htmlFor={id} className='text-sm text-foreground'>{option.label}</label>
            </div>
          )
        })}
      </div>
    </fieldset>
  )
}
