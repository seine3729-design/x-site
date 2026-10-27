import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type ColumnDef } from '@tanstack/react-table'
import { Check, Pencil, Plug, Plus, Trash2, X } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { DataTable } from '@/components/common/data-table'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import { SelectDropdown } from '@/components/select-dropdown'
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Separator,
  Switch,
} from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import {
  PROBE_STEP_LABELS,
  PURPOSE_LABELS,
  ROLE_LABELS,
  STATUS_LABELS,
  SUBTYPE_LABELS,
  TYPE_LABELS,
  type ProbeResult,
  type StorageProvider,
  type StorageProviderInput,
  providersQuery,
  storageApi,
  storageKeys,
} from './api'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; provider: StorageProvider }

export function StorageProvidersPage() {
  const can = useCan()
  const visible = can('storage_providers.view')
  const qc = useQueryClient()
  const providers = useQuery({ ...providersQuery, enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [testing, setTesting] = useState<number | null>(null)
  const [result, setResult] = useState<{ provider: StorageProvider; result: ProbeResult } | null>(null)

  const testMutation = useMutation({
    mutationFn: (p: StorageProvider) => storageApi.test(p.id),
    onMutate: (p) => setTesting(p.id),
    onSuccess: (res, p) => setResult({ provider: p, result: res }),
    onError: (err) => toast.error(errorMessage(err)),
    onSettled: () => setTesting(null),
  })

  if (!visible) return <NoPermission title='存储源' />
  const editable = can('storage_providers.edit')

  const columns: ColumnDef<StorageProvider, unknown>[] = [
    {
      accessorKey: 'name',
      header: '名称',
      cell: ({ row }) => (
        <div className='flex flex-col'>
          <span className='font-medium'>{row.original.name}</span>
          <code className='text-xs text-muted-foreground'>{row.original.endpoint || row.original.bucket || '—'}</code>
        </div>
      ),
    },
    {
      accessorKey: 'type',
      header: '类型',
      cell: ({ row }) => (
        <div className='flex flex-col gap-0.5'>
          <span>{TYPE_LABELS[row.original.type]}</span>
          {row.original.subtype && (
            <span className='text-xs text-muted-foreground'>{SUBTYPE_LABELS[row.original.subtype]}</span>
          )}
        </div>
      ),
    },
    { accessorKey: 'purpose', header: '用途', cell: ({ row }) => PURPOSE_LABELS[row.original.purpose] },
    {
      accessorKey: 'role',
      header: '角色',
      cell: ({ row }) => <Badge variant={row.original.role === 'primary' ? 'default' : 'secondary'}>{ROLE_LABELS[row.original.role]}</Badge>,
    },
    { accessorKey: 'bucket', header: '桶', cell: ({ row }) => <code className='text-xs'>{row.original.bucket || '—'}</code> },
    {
      id: 'cred',
      header: '凭据',
      cell: ({ row }) =>
        row.original.has_write_cred ? (
          <code className='text-xs'>****{row.original.cred_tail}</code>
        ) : (
          <span className='text-xs text-muted-foreground'>未配置</span>
        ),
    },
    {
      accessorKey: 'status',
      header: '状态',
      cell: ({ row }) => {
        const s = row.original.status
        return <Badge variant={s === 'active' ? 'secondary' : 'destructive'}>{STATUS_LABELS[s]}</Badge>
      },
    },
    {
      id: 'actions',
      header: () => <span className='sr-only'>操作</span>,
      cell: ({ row }) => {
        const p = row.original
        return (
          <div className='flex items-center justify-end gap-1'>
            <Button
              variant='ghost'
              size='sm'
              disabled={testing === p.id}
              aria-label={`测试 ${p.name}`}
              onClick={() => testMutation.mutate(p)}
            >
              <Plug className='size-4' /> {testing === p.id ? '测试中…' : '测试'}
            </Button>
            <Button
              variant='ghost'
              size='icon'
              disabled={!editable}
              aria-label={`编辑 ${p.name}`}
              title={editable ? undefined : '当前角色只能查看'}
              onClick={() => setDialog({ type: 'edit', provider: p })}
            >
              <Pencil className='size-4' />
            </Button>
            <Button
              variant='ghost'
              size='icon'
              disabled={!editable}
              aria-label={`删除 ${p.name}`}
              title={editable ? undefined : '当前角色只能查看'}
              onClick={() => setDialog({ type: 'delete', provider: p })}
            >
              <Trash2 className='size-4' />
            </Button>
          </div>
        )
      },
    },
  ]

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <div className='flex items-center gap-2'>
            <h1 className='text-2xl font-semibold tracking-tight'>存储源</h1>
            {!editable && <Badge variant='secondary'>只读</Badge>}
          </div>
          <p className='text-sm text-muted-foreground'>
            视频、图片资源的存储后端（S3 / Cloudflare R2 / 自定义）。凭据加密保存、只写不读，保存后可一键测试连通性。
          </p>
        </div>
        {editable && (
          <Button onClick={() => setDialog({ type: 'create' })}>
            <Plus /> 新建存储源
          </Button>
        )}
      </div>

      {providers.isError ? (
        <div role='alert' className='rounded-md border border-destructive/40 p-4 text-sm'>
          数据加载失败。
          <button type='button' className='ms-2 font-medium text-primary underline-offset-4 hover:underline' onClick={() => void providers.refetch()}>
            重试
          </button>
        </div>
      ) : (
        <DataTable data={providers.data ?? []} columns={columns} loading={providers.isPending} enableRowSelection={false} hidePagination />
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <ProviderDialog provider={dialog.type === 'edit' ? dialog.provider : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && (
        <DeleteProviderDialog
          provider={dialog.provider}
          onClose={() => setDialog(null)}
          onDeleted={() => void qc.invalidateQueries({ queryKey: storageKeys.providers })}
        />
      )}
      {result && <TestResultDialog provider={result.provider} result={result.result} onClose={() => setResult(null)} />}
    </div>
  )
}

// ---------- 表单 ----------

const providerSchema = z
  .object({
    name: z.string().trim().min(1, '请填写名称').max(64, '名称最多 64 个字'),
    type: z.enum(['s3', 'r2', 'custom']),
    subtype: z.enum(['', 's3_compatible', 'http_origin']),
    purpose: z.enum(['video', 'image', 'both']),
    role: z.enum(['primary', 'mirror', 'readonly']),
    endpoint: z.string().trim(),
    region: z.string().trim(),
    bucket: z.string().trim(),
    account_id: z.string().trim(),
    path_style: z.boolean(),
    path_prefix: z.string().trim().max(256, '前缀最多 256 个字'),
    max_presign_ttl: z
      .number({ error: '请填写 60–604800 的整数' })
      .int('请填写整数')
      .min(60, '最小 60 秒')
      .max(604800, '最大 604800 秒（7 天）'),
    status: z.enum(['active', 'draining', 'disabled']),
    write_access: z.string().trim(),
    write_secret: z.string().trim(),
    read_access: z.string().trim(),
    read_secret: z.string().trim(),
  })
  .superRefine((v, ctx) => {
    const needBucket = v.type === 's3' || v.type === 'r2' || (v.type === 'custom' && v.subtype === 's3_compatible')
    if (v.type === 'custom' && v.subtype === '') {
      ctx.addIssue({ code: 'custom', path: ['subtype'], message: '请选择自定义类型的子类型' })
    }
    if (v.type === 's3' && v.region === '') {
      ctx.addIssue({ code: 'custom', path: ['region'], message: 'AWS S3（兼容）需要填写 Region' })
    }
    if (v.type === 'r2' && !/^[0-9a-f]{32}$/.test(v.account_id)) {
      ctx.addIssue({ code: 'custom', path: ['account_id'], message: 'R2 Account ID 为 32 位十六进制' })
    }
    if ((v.type === 'custom' && (v.subtype === 's3_compatible' || v.subtype === 'http_origin')) && v.endpoint === '') {
      ctx.addIssue({ code: 'custom', path: ['endpoint'], message: '请填写端点地址' })
    }
    if (needBucket && v.bucket === '') {
      ctx.addIssue({ code: 'custom', path: ['bucket'], message: '请填写桶名' })
    }
    // 凭据：每组要么都填要么都空
    if (!!v.write_access !== !!v.write_secret) {
      ctx.addIssue({ code: 'custom', path: ['write_secret'], message: 'Access Key 与 Secret Key 要同时填写' })
    }
    if (!!v.read_access !== !!v.read_secret) {
      ctx.addIssue({ code: 'custom', path: ['read_secret'], message: 'Access Key 与 Secret Key 要同时填写' })
    }
  })

type ProviderForm = z.infer<typeof providerSchema>

function ProviderDialog({ provider, onClose }: { provider?: StorageProvider; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !provider
  const form = useForm<ProviderForm>({
    resolver: zodResolver(providerSchema),
    defaultValues: {
      name: provider?.name ?? '',
      type: provider?.type ?? 's3',
      subtype: provider?.subtype ?? '',
      purpose: provider?.purpose ?? 'both',
      role: provider?.role ?? 'primary',
      endpoint: provider?.endpoint ?? '',
      region: provider?.region ?? '',
      bucket: provider?.bucket ?? '',
      account_id: provider?.account_id ?? '',
      path_style: provider?.path_style ?? false,
      path_prefix: provider?.path_prefix ?? '',
      max_presign_ttl: provider?.max_presign_ttl ?? 600,
      status: provider?.status ?? 'active',
      write_access: '',
      write_secret: '',
      read_access: '',
      read_secret: '',
    },
  })
  const type = form.watch('type')
  const subtype = form.watch('subtype')
  const httpOrigin = type === 'custom' && subtype === 'http_origin'
  const showSubtype = type === 'custom'
  const showAccountId = type === 'r2'
  const showRegion = type === 's3' || (type === 'custom' && subtype === 's3_compatible')
  const showBucket = type === 's3' || type === 'r2' || (type === 'custom' && subtype === 's3_compatible')
  const showEndpoint = type === 's3' || type === 'custom'
  const showPathStyle = type === 's3' || (type === 'custom' && subtype === 's3_compatible')

  const mutation = useMutation({
    mutationFn: (v: ProviderForm) => {
      const body: StorageProviderInput = {
        name: v.name,
        type: v.type,
        subtype: v.type === 'custom' ? v.subtype : '',
        purpose: v.purpose,
        role: v.role,
        endpoint: showEndpoint ? v.endpoint : '',
        region: showRegion ? v.region : '',
        bucket: showBucket ? v.bucket : '',
        account_id: v.type === 'r2' ? v.account_id : '',
        path_style: showPathStyle ? v.path_style : false,
        path_prefix: v.path_prefix,
        max_presign_ttl: v.max_presign_ttl,
        status: v.status,
        write_cred: v.write_access || v.write_secret ? { access_key_id: v.write_access, secret_access_key: v.write_secret } : undefined,
        read_cred: v.read_access || v.read_secret ? { access_key_id: v.read_access, secret_access_key: v.read_secret } : undefined,
      }
      return creating ? storageApi.create(body).then(() => undefined) : storageApi.update(provider.id, body).then(() => undefined)
    },
    onSuccess: () => {
      toast.success(creating ? '已新建存储源' : '已保存')
      void qc.invalidateQueries({ queryKey: storageKeys.providers })
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const onSubmit = form.handleSubmit((v) => {
    // 新建时（HTTP 源除外）必须提供读写凭据
    if (creating && !httpOrigin && !(v.write_access && v.write_secret)) {
      form.setError('write_secret', { message: '新建时必须填写读写凭据' })
      return
    }
    mutation.mutate(v)
  })

  const credPlaceholder = (field: 'access' | 'secret', tail?: string) => {
    if (creating) return field === 'access' ? '请输入 Access Key ID' : '请输入 Secret Access Key'
    return tail ? `留空表示不修改（已配置 ****${tail}）` : '留空表示不修改'
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建存储源' : `编辑存储源：${provider.name}`}</DialogTitle>
          <DialogDescription>按类型填写连接信息；凭据加密保存、只写不读，保存后可在列表里测试连通性。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={onSubmit} noValidate>
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>名称</FormLabel>
                  <FormControl>
                    <Input placeholder='请输入显示名，如 R2-主存储' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='type'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>类型</FormLabel>
                    <SelectDropdown
                      isControlled
                      defaultValue={field.value}
                      onValueChange={(val) => {
                        field.onChange(val)
                        if (val !== 'custom') form.setValue('subtype', '')
                      }}
                      placeholder='请选择存储源类型'
                      items={[
                        { label: TYPE_LABELS.s3, value: 's3' },
                        { label: TYPE_LABELS.r2, value: 'r2' },
                        { label: TYPE_LABELS.custom, value: 'custom' },
                      ]}
                    />
                    <FormMessage />
                  </FormItem>
                )}
              />
              {showSubtype && (
                <FormField
                  control={form.control}
                  name='subtype'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>子类型</FormLabel>
                      <SelectDropdown
                        isControlled
                        defaultValue={field.value}
                        onValueChange={field.onChange}
                        placeholder='请选择子类型'
                        items={[
                          { label: SUBTYPE_LABELS.s3_compatible, value: 's3_compatible' },
                          { label: SUBTYPE_LABELS.http_origin, value: 'http_origin' },
                        ]}
                      />
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </div>

            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='purpose'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>用途</FormLabel>
                    <SelectDropdown
                      isControlled
                      defaultValue={field.value}
                      onValueChange={field.onChange}
                      placeholder='请选择用途'
                      items={[
                        { label: PURPOSE_LABELS.video, value: 'video' },
                        { label: PURPOSE_LABELS.image, value: 'image' },
                        { label: PURPOSE_LABELS.both, value: 'both' },
                      ]}
                    />
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='role'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>角色</FormLabel>
                    <SelectDropdown
                      isControlled
                      defaultValue={field.value}
                      onValueChange={field.onChange}
                      placeholder='请选择角色'
                      items={[
                        { label: ROLE_LABELS.primary, value: 'primary' },
                        { label: ROLE_LABELS.mirror, value: 'mirror' },
                        { label: ROLE_LABELS.readonly, value: 'readonly' },
                      ]}
                    />
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            {showAccountId && (
              <FormField
                control={form.control}
                name='account_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Account ID</FormLabel>
                    <FormControl>
                      <Input placeholder='请输入 R2 Account ID（32 位十六进制）' {...field} />
                    </FormControl>
                    <FormDescription>端点会自动生成为 https://&lt;account_id&gt;.r2.cloudflarestorage.com</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {showEndpoint && (
              <FormField
                control={form.control}
                name='endpoint'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{httpOrigin ? 'Base URL' : `Endpoint${type === 's3' ? '（可选）' : ''}`}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={
                          httpOrigin
                            ? '请输入只读 HTTP 源地址，如 https://img.example.com/media'
                            : type === 's3'
                              ? 'AWS 官方留空；S3 兼容服务填写，如 https://oss-cn-hangzhou.aliyuncs.com'
                              : '请输入 S3 端点，如 https://minio.example.com:9000'
                        }
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            <div className='grid grid-cols-2 gap-4'>
              {showBucket && (
                <FormField
                  control={form.control}
                  name='bucket'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>桶 Bucket</FormLabel>
                      <FormControl>
                        <Input placeholder='请输入桶名，如 duanju666' {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {showRegion && (
                <FormField
                  control={form.control}
                  name='region'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Region</FormLabel>
                      <FormControl>
                        <Input placeholder='请输入区域，如 us-east-1' {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </div>

            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='path_prefix'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>路径前缀（选填）</FormLabel>
                    <FormControl>
                      <Input placeholder='如 media/，便于多环境共用一个桶' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='max_presign_ttl'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>预签名有效期（秒）</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        inputMode='numeric'
                        placeholder='60–604800，默认 600'
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

            {showPathStyle && (
              <FormField
                control={form.control}
                name='path_style'
                render={({ field }) => (
                  <FormItem className='flex items-center justify-between rounded-md border p-3'>
                    <div className='flex flex-col gap-0.5'>
                      <FormLabel>路径风格寻址（Path-style）</FormLabel>
                      <FormDescription>自建 S3 兼容服务通常需要开启</FormDescription>
                    </div>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
                    </FormControl>
                  </FormItem>
                )}
              />
            )}

            <FormField
              control={form.control}
              name='status'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>状态</FormLabel>
                  <SelectDropdown
                    isControlled
                    defaultValue={field.value}
                    onValueChange={field.onChange}
                    placeholder='请选择状态'
                    items={[
                      { label: STATUS_LABELS.active, value: 'active' },
                      { label: STATUS_LABELS.draining, value: 'draining' },
                      { label: STATUS_LABELS.disabled, value: 'disabled' },
                    ]}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />

            <Separator />
            <p className='text-sm font-medium'>
              读写凭据{httpOrigin ? '（HTTP 源可留空）' : ''}
              {!creating && <span className='ms-1 font-normal text-muted-foreground'>（留空表示不修改）</span>}
            </p>
            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='write_access'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Access Key ID</FormLabel>
                    <FormControl>
                      <Input autoComplete='off' placeholder={credPlaceholder('access')} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='write_secret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Secret Access Key</FormLabel>
                    <FormControl>
                      <Input type='password' autoComplete='off' placeholder={credPlaceholder('secret', provider?.cred_tail)} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <p className='text-sm font-medium'>
              只读凭据（选填，用于预签名）
              {!creating && provider?.has_read_cred && <span className='ms-1 font-normal text-muted-foreground'>（留空表示不修改）</span>}
            </p>
            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='read_access'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Access Key ID</FormLabel>
                    <FormControl>
                      <Input autoComplete='off' placeholder='选填，不填则预签名用读写凭据' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='read_secret'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Secret Access Key</FormLabel>
                    <FormControl>
                      <Input type='password' autoComplete='off' placeholder={credPlaceholder('secret', provider?.read_cred_tail)} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
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

function DeleteProviderDialog({ provider, onClose, onDeleted }: { provider: StorageProvider; onClose: () => void; onDeleted: () => void }) {
  const mutation = useMutation({
    mutationFn: () => storageApi.remove(provider.id),
    onSuccess: () => {
      toast.success(`已删除存储源「${provider.name}」`)
      onDeleted()
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除存储源「${provider.name}」`}
      desc='删除后不可恢复。有媒体副本引用的存储源不能直接删除，需要先停写并迁移。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function TestResultDialog({ provider, result, onClose }: { provider: StorageProvider; result: ProbeResult; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            连通性测试：{provider.name}
            {result.ok ? <Badge variant='secondary'>通过</Badge> : <Badge variant='destructive'>未通过</Badge>}
          </DialogTitle>
          <DialogDescription>共耗时 {result.elapsed_ms} 毫秒，按步骤依次执行，某一步失败后不再继续。</DialogDescription>
        </DialogHeader>
        <ol className='flex flex-col gap-2'>
          {result.steps.map((s) => (
            <li key={s.name} className='flex items-start gap-2 text-sm'>
              {s.ok ? <Check className='mt-0.5 size-4 text-emerald-600' /> : <X className='mt-0.5 size-4 text-destructive' />}
              <div className='flex flex-col'>
                <span className='font-medium'>
                  {PROBE_STEP_LABELS[s.name]} <span className='text-xs text-muted-foreground'>{s.ms}ms</span>
                </span>
                <span className='text-muted-foreground'>{s.message}</span>
              </div>
            </li>
          ))}
        </ol>
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
