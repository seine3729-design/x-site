import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { AlertTriangle } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { SelectDropdown } from '@/components/select-dropdown'
import { Badge, Button, Input, Switch } from '@/components/ui'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { providersQuery, settingsApi, settingsQuery, type StorageProvider, type StorageSettings } from './api'
import { MaintenancePanel } from './maintenance-panel'

export function StorageSettingsPage() {
  const can = useCan()
  const visible = can('storage_settings.view')
  const settings = useQuery({ ...settingsQuery, enabled: visible })
  if (!visible) return <NoPermission title='存储设置' />
  if (settings.isError) {
    return (
      <div role='alert' className='rounded-md border border-destructive/40 p-4 text-sm'>
        数据加载失败。
        <button type='button' className='ms-2 font-medium text-primary underline-offset-4 hover:underline' onClick={() => void settings.refetch()}>
          重试
        </button>
      </div>
    )
  }
  if (settings.isPending || !settings.data) {
    return <div className='h-48 animate-pulse rounded-md border' />
  }
  return <SettingsForm settings={settings.data} editable={can('storage_settings.edit')} />
}

/**
 * DeliveryAlert 显示交付自检结果：主存储上没有可用线路时，上传、转码、复制都显示成功，
 * 但前台选源拿不到线路（播放返回 503）。这里把后端算出的原因与修复入口摆在设置页上。
 */
function DeliveryAlert({ delivery }: { delivery: StorageSettings['delivery'] }) {
  if (!delivery || delivery.ok) return null
  return (
    <Alert variant='destructive'>
      <AlertTriangle />
      <AlertTitle>上传的资源前台取不到</AlertTitle>
      <AlertDescription>
        <ul className='list-disc space-y-1 ps-4'>
          {delivery.issues.map((issue) => (
            <li key={`${issue.kind}-${issue.code}`}>{issue.message}</li>
          ))}
        </ul>
        <Link to='/storage/lines' className='font-medium text-primary underline-offset-4 hover:underline'>
          去配置存储线路
        </Link>
      </AlertDescription>
    </Alert>
  )
}

const schema = z.object({
  candidate_count: z.number({ error: '请填写 1–5 的整数' }).int().min(1, '最小 1').max(5, '最大 5'),
  default_ttl: z.number({ error: '请填写 60–604800 的整数' }).int().min(60, '最小 60 秒').max(604800, '最大 604800 秒'),
  smart_routing: z.boolean(),
  primary_video_provider_id: z.number().int().min(0),
  primary_image_provider_id: z.number().int().min(0),
  replicate_on_upload: z.boolean(),
})

type SettingsForm = z.infer<typeof schema>

function SettingsForm({ settings, editable }: { settings: StorageSettings; editable: boolean }) {
  const qc = useQueryClient()
  const providers = useQuery(providersQuery)
  const form = useForm<SettingsForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      candidate_count: settings.candidate_count,
      default_ttl: settings.default_ttl,
      smart_routing: settings.smart_routing,
      primary_video_provider_id: settings.primary_video_provider_id,
      primary_image_provider_id: settings.primary_image_provider_id,
      replicate_on_upload: settings.replicate_on_upload,
    },
  })
  const mutation = useMutation({
    mutationFn: (v: SettingsForm) => settingsApi.update(v),
    onSuccess: () => {
      toast.success('已保存存储设置')
      void qc.invalidateQueries({ queryKey: ['storage', 'settings'] })
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const providerItems = (kinds: StorageProvider['purpose'][]) => [
    { label: '不设置', value: '0' },
    ...(providers.data ?? [])
      .filter((p) => kinds.includes(p.purpose))
      .map((p) => ({ label: `${p.name}（${p.bucket || p.endpoint}）`, value: String(p.id) })),
  ]

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex items-center gap-2'>
        <h1 className='text-2xl font-semibold tracking-tight'>存储设置</h1>
        {!editable && <Badge variant='secondary'>只读</Badge>}
      </div>
      <p className='text-sm text-muted-foreground'>选线候选数量、默认链接有效期、智能选线、视频 / 图片主存储、上传后自动复制。</p>

      <DeliveryAlert delivery={settings.delivery} />

      <Form {...form}>
        <form className='flex max-w-2xl flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
          <div className='grid grid-cols-2 gap-4'>
            <FormField
              control={form.control}
              name='candidate_count'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>候选线路数量</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      inputMode='numeric'
                      placeholder='1–5，默认 3'
                      disabled={!editable}
                      name={field.name}
                      ref={field.ref}
                      onBlur={field.onBlur}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(e) => field.onChange(e.target.value === '' ? Number.NaN : Number(e.target.value))}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='default_ttl'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>默认链接有效期（秒）</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      inputMode='numeric'
                      placeholder='60–604800，默认 600'
                      disabled={!editable}
                      name={field.name}
                      ref={field.ref}
                      onBlur={field.onBlur}
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(e) => field.onChange(e.target.value === '' ? Number.NaN : Number(e.target.value))}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='primary_video_provider_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>视频主存储</FormLabel>
                <SelectDropdown
                  isControlled
                  defaultValue={String(field.value)}
                  onValueChange={(v) => field.onChange(Number(v))}
                  disabled={!editable}
                  placeholder='请选择视频主存储'
                  items={providerItems(['video', 'both'])}
                />
                <FormDescription>新视频内容写入这个存储源。</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='primary_image_provider_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>图片主存储</FormLabel>
                <SelectDropdown
                  isControlled
                  defaultValue={String(field.value)}
                  onValueChange={(v) => field.onChange(Number(v))}
                  disabled={!editable}
                  placeholder='请选择图片主存储'
                  items={providerItems(['image', 'both'])}
                />
                <FormDescription>封面、头像等图片写入这个存储源。</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='smart_routing'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between rounded-md border p-3'>
                <div className='flex flex-col gap-0.5'>
                  <FormLabel>智能选线</FormLabel>
                  <FormDescription>按地区、健康分自动排序线路；关闭后只按优先级与权重。</FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} disabled={!editable} />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='replicate_on_upload'
            render={({ field }) => (
              <FormItem className='flex items-center justify-between rounded-md border p-3'>
                <div className='flex flex-col gap-0.5'>
                  <FormLabel>上传后自动复制</FormLabel>
                  <FormDescription>内容写入主存储后，自动复制到镜像存储源。</FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} disabled={!editable} />
                </FormControl>
              </FormItem>
            )}
          />

          {editable && (
            <div>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '保存设置'}
              </Button>
            </div>
          )}
        </form>
      </Form>

      <hr className='my-2' />
      <MaintenancePanel editable={editable} />
    </div>
  )
}
