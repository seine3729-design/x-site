import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type ColumnDef } from '@tanstack/react-table'
import { Check, Pencil, Plug, Plus, SlidersHorizontal, Trash2, X } from 'lucide-react'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
} from '@/components/ui'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import {
  ACCESS_MODE_LABELS,
  EFFECT_LABELS,
  LINE_KIND_LABELS,
  LINE_STATUS_LABELS,
  REGION_SCOPE_LABELS,
  SIGN_TYPE_LABELS,
  lineApi,
  lineKeys,
  linesQuery,
  providersQuery,
  type LineInput,
  type LineProbeResult,
  type RuleInput,
  type StorageLine,
} from './api'

type DialogState =
  | { type: 'create' }
  | { type: 'edit' | 'delete' | 'rules'; line: StorageLine }

export function StorageLinesPage() {
  const can = useCan()
  const visible = can('storage_lines.view')
  const qc = useQueryClient()
  const providers = useQuery({ ...providersQuery, enabled: visible })
  const [providerId, setProviderId] = useState(0)
  const lines = useQuery(linesQuery(providerId))
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [testResult, setTestResult] = useState<{ line: StorageLine; result: LineProbeResult } | null>(null)

  const testMutation = useMutation({
    mutationFn: (l: StorageLine) => lineApi.test(l.id),
    onSuccess: (res, l) => setTestResult({ line: l, result: res }),
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='线路' />
  const editable = can('storage_lines.edit')

  const columns: ColumnDef<StorageLine, unknown>[] = [
    {
      accessorKey: 'name',
      header: '线路',
      cell: ({ row }) => (
        <div className='flex flex-col'>
          <span className='font-medium'>{row.original.name || `线路 ${row.original.id}`}</span>
          <code className='text-xs text-muted-foreground'>{row.original.base_url || '直连存储'}</code>
        </div>
      ),
    },
    { accessorKey: 'kind', header: '类型', cell: ({ row }) => LINE_KIND_LABELS[row.original.kind] },
    { accessorKey: 'access_mode', header: '访问方式', cell: ({ row }) => ACCESS_MODE_LABELS[row.original.access_mode] },
    { accessorKey: 'sign_type', header: '签名', cell: ({ row }) => SIGN_TYPE_LABELS[row.original.sign_type] },
    { accessorKey: 'priority', header: '优先级', cell: ({ row }) => <span className='tabular-nums'>{row.original.priority}</span> },
    { accessorKey: 'weight', header: '权重', cell: ({ row }) => <span className='tabular-nums'>{row.original.weight}</span> },
    {
      id: 'rules',
      header: '地区规则',
      cell: ({ row }) =>
        row.original.region_scope === 'global' ? (
          <Badge variant='outline'>全局兜底</Badge>
        ) : (
          <span className='text-xs text-muted-foreground'>{row.original.rules.length} 条规则</span>
        ),
    },
    {
      accessorKey: 'status',
      header: '状态',
      cell: ({ row }) => {
        const s = row.original.status
        return <Badge variant={s === 'active' ? 'secondary' : s === 'watch' ? 'outline' : 'destructive'}>{LINE_STATUS_LABELS[s]}</Badge>
      },
    },
    {
      id: 'actions',
      header: () => <span className='sr-only'>操作</span>,
      cell: ({ row }) => {
        const l = row.original
        return (
          <div className='flex items-center justify-end gap-1'>
            <Button variant='ghost' size='sm' disabled={testMutation.isPending} aria-label={`测试 ${l.name}`} onClick={() => testMutation.mutate(l)}>
              <Plug className='size-4' /> 测试
            </Button>
            <Button variant='ghost' size='icon' disabled={!editable} aria-label='地区规则' title={editable ? '地区规则' : '当前角色只能查看'} onClick={() => setDialog({ type: 'rules', line: l })}>
              <SlidersHorizontal className='size-4' />
            </Button>
            <Button variant='ghost' size='icon' disabled={!editable} aria-label={`编辑 ${l.name}`} title={editable ? undefined : '当前角色只能查看'} onClick={() => setDialog({ type: 'edit', line: l })}>
              <Pencil className='size-4' />
            </Button>
            <Button variant='ghost' size='icon' disabled={!editable} aria-label={`删除 ${l.name}`} title={editable ? undefined : '当前角色只能查看'} onClick={() => setDialog({ type: 'delete', line: l })}>
              <Trash2 className='size-4' />
            </Button>
          </div>
        )
      },
    },
  ]

  const providerItems = (providers.data ?? []).map((p) => ({ label: `${p.name}（${p.bucket || p.endpoint}）`, value: String(p.id) }))

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <div className='flex items-center gap-2'>
            <h1 className='text-2xl font-semibold tracking-tight'>线路</h1>
            {!editable && <Badge variant='secondary'>只读</Badge>}
          </div>
          <p className='text-sm text-muted-foreground'>一个存储源可挂多条线路（CDN 域名 / 直连 / 自定义域名），按地区规则决定给哪些用户。</p>
        </div>
        {editable && providerId > 0 && (
          <Button onClick={() => setDialog({ type: 'create' })}>
            <Plus /> 新建线路
          </Button>
        )}
      </div>

      <div className='flex items-center gap-2'>
        <span className='text-sm text-muted-foreground'>存储源</span>
        <Select value={providerId ? String(providerId) : undefined} onValueChange={(v) => setProviderId(Number(v))}>
          <SelectTrigger className='w-80'>
            <SelectValue placeholder='请选择要查看线路的存储源' />
          </SelectTrigger>
          <SelectContent>
            {providerItems.map((it) => (
              <SelectItem key={it.value} value={it.value}>
                {it.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {providerId === 0 ? (
        <div className='rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground'>请先在上方选择一个存储源，再查看或新建它的线路。</div>
      ) : (
        <DataTable data={lines.data ?? []} columns={columns} loading={lines.isPending} enableRowSelection={false} hidePagination />
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <LineDialog providerId={providerId} line={dialog.type === 'edit' ? dialog.line : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'rules' && <RulesDialog line={dialog.line} onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && (
        <DeleteLineDialog line={dialog.line} onClose={() => setDialog(null)} onDeleted={() => void qc.invalidateQueries({ queryKey: lineKeys.all })} />
      )}
      {testResult && <LineTestDialog line={testResult.line} result={testResult.result} onClose={() => setTestResult(null)} />}
    </div>
  )
}

// ---------- 线路表单 ----------

const lineSchema = z
  .object({
    kind: z.enum(['video', 'image']),
    name: z.string().trim().max(64, '名称最多 64 个字'),
    base_url: z.string().trim(),
    access_mode: z.enum(['presign', 'cdn', 'public']),
    sign_type: z.enum(['s3_presign', 'hmac_token', 'cdn_vendor', 'none']),
    sign_key_ref: z.string().trim(),
    token_ttl: z.number({ error: '请填写 60–604800 的整数' }).int().min(60, '最小 60 秒').max(604800, '最大 604800 秒'),
    priority: z.number({ error: '请填写 0–10000 的整数' }).int().min(0).max(10000),
    weight: z.number({ error: '请填写 1–100 的整数' }).int().min(1, '最小 1').max(100, '最大 100'),
    region_scope: z.enum(['rules', 'global']),
    video_process: z.enum(['standard', 'text_to_image']),
    probe_path: z.string().trim(),
    status: z.enum(['active', 'watch', 'disabled']),
    note: z.string().trim().max(200, '备注最多 200 个字'),
  })
  .superRefine((v, ctx) => {
    if (v.access_mode !== 'presign' && v.base_url === '') {
      ctx.addIssue({ code: 'custom', path: ['base_url'], message: '访问方式不是预签名直连时必须填写访问域名' })
    }
  })

type LineForm = z.infer<typeof lineSchema>

function LineDialog({ providerId, line, onClose }: { providerId: number; line?: StorageLine; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !line
  const form = useForm<LineForm>({
    resolver: zodResolver(lineSchema),
    defaultValues: {
      kind: line?.kind ?? 'video',
      name: line?.name ?? '',
      base_url: line?.base_url ?? '',
      access_mode: line?.access_mode ?? 'cdn',
      sign_type: line?.sign_type ?? 'hmac_token',
      sign_key_ref: line?.sign_key_ref ?? '',
      token_ttl: line?.token_ttl ?? 600,
      priority: line?.priority ?? 0,
      weight: line?.weight ?? 100,
      region_scope: line?.region_scope ?? 'global',
      video_process: line?.video_process ?? 'standard',
      probe_path: line?.probe_path ?? '/_probe/256k.bin',
      status: line?.status ?? 'active',
      note: line?.note ?? '',
    },
  })
  const mutation = useMutation({
    mutationFn: (v: LineForm) => {
      const body: LineInput = { ...v, provider_id: creating ? providerId : undefined }
      return creating ? lineApi.create(body).then(() => undefined) : lineApi.update(line.id, body).then(() => undefined)
    },
    onSuccess: () => {
      toast.success(creating ? '已新建线路' : '已保存')
      void qc.invalidateQueries({ queryKey: lineKeys.all })
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const numberField = (name: 'token_ttl' | 'priority' | 'weight', label: string, placeholder: string) => (
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
              placeholder={placeholder}
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
  )

  const selectField = (name: keyof LineForm, label: string, items: { label: string; value: string }[], placeholder: string) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <SelectDropdown isControlled defaultValue={String(field.value)} onValueChange={field.onChange} placeholder={placeholder} items={items} />
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建线路' : `编辑线路：${line.name || line.id}`}</DialogTitle>
          <DialogDescription>配置访问域名与签名方式；地区规则在列表的「地区规则」按钮里单独编辑。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid grid-cols-2 gap-4'>
              {selectField('kind', '类型', [{ label: LINE_KIND_LABELS.video, value: 'video' }, { label: LINE_KIND_LABELS.image, value: 'image' }], '请选择类型')}
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>名称</FormLabel>
                    <FormControl>
                      <Input placeholder='选填，如 线路 1 · 电信优化' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <div className='grid grid-cols-2 gap-4'>
              {selectField('access_mode', '访问方式', [
                { label: ACCESS_MODE_LABELS.presign, value: 'presign' },
                { label: ACCESS_MODE_LABELS.cdn, value: 'cdn' },
                { label: ACCESS_MODE_LABELS.public, value: 'public' },
              ], '请选择访问方式')}
              {selectField('sign_type', '签名方式', [
                { label: SIGN_TYPE_LABELS.s3_presign, value: 's3_presign' },
                { label: SIGN_TYPE_LABELS.hmac_token, value: 'hmac_token' },
                { label: SIGN_TYPE_LABELS.cdn_vendor, value: 'cdn_vendor' },
                { label: SIGN_TYPE_LABELS.none, value: 'none' },
              ], '请选择签名方式')}
            </div>
            <FormField
              control={form.control}
              name='base_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>访问域名 Base URL</FormLabel>
                  <FormControl>
                    <Input placeholder='如 https://v1.example.net；预签名直连可留空（由存储源生成）' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className='grid grid-cols-2 gap-4'>
              <FormField
                control={form.control}
                name='sign_key_ref'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>签名密钥引用</FormLabel>
                    <FormControl>
                      <Input placeholder='选填，密钥管理中的名字' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {numberField('token_ttl', '链接有效期（秒）', '60–604800，默认 600')}
            </div>
            <div className='grid grid-cols-3 gap-4'>
              {numberField('priority', '优先级', '0–10000')}
              {numberField('weight', '权重', '1–100')}
              {selectField('status', '状态', [
                { label: LINE_STATUS_LABELS.active, value: 'active' },
                { label: LINE_STATUS_LABELS.watch, value: 'watch' },
                { label: LINE_STATUS_LABELS.disabled, value: 'disabled' },
              ], '请选择状态')}
            </div>
            <div className='grid grid-cols-2 gap-4'>
              {selectField('region_scope', '地区范围', [
                { label: REGION_SCOPE_LABELS.rules, value: 'rules' },
                { label: REGION_SCOPE_LABELS.global, value: 'global' },
              ], '请选择地区范围')}
              <FormField
                control={form.control}
                name='probe_path'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>探测路径</FormLabel>
                    <FormControl>
                      <Input placeholder='默认 /_probe/256k.bin' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <FormField
              control={form.control}
              name='note'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>备注</FormLabel>
                  <FormControl>
                    <Textarea rows={2} placeholder='选填，最多 200 个字' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
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

// ---------- 地区规则编辑器 ----------

function RulesDialog({ line, onClose }: { line: StorageLine; onClose: () => void }) {
  const qc = useQueryClient()
  const [rules, setRules] = useState<RuleInput[]>(
    line.rules.map((r) => ({ country: r.country, province: r.province, isp: r.isp, asn: r.asn, effect: r.effect, boost: r.boost }))
  )
  const mutation = useMutation({
    mutationFn: () => lineApi.setRules(line.id, rules),
    onSuccess: () => {
      toast.success('已保存地区规则')
      void qc.invalidateQueries({ queryKey: lineKeys.all })
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const update = (i: number, patch: Partial<RuleInput>) => setRules((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  const addRule = () => setRules((rs) => [...rs, { country: '*', province: '*', isp: '*', asn: '', effect: 'include', boost: 0 }])
  const removeRule = (i: number) => setRules((rs) => rs.filter((_, idx) => idx !== i))

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>地区规则：{line.name || `线路 ${line.id}`}</DialogTitle>
          <DialogDescription>命中任意一条规则即视为「本地区线路」。国家填 ISO 两位字母（如 CN、HK）或 * ；省份、运营商填 * 表示不限。</DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <div className='grid grid-cols-[1fr_1fr_1fr_90px_90px_32px] items-center gap-2 text-xs text-muted-foreground'>
            <span>国家</span>
            <span>省份</span>
            <span>运营商</span>
            <span>效果</span>
            <span>加分</span>
            <span />
          </div>
          {rules.length === 0 && <p className='py-2 text-sm text-muted-foreground'>还没有规则。全局兜底线路可以不加规则；否则点下方「添加规则」。</p>}
          {rules.map((r, i) => (
            <div key={i} className='grid grid-cols-[1fr_1fr_1fr_90px_90px_32px] items-center gap-2'>
              <Input value={r.country} placeholder='CN 或 *' onChange={(e) => update(i, { country: e.target.value })} />
              <Input value={r.province} placeholder='广东 或 *' onChange={(e) => update(i, { province: e.target.value })} />
              <Input value={r.isp} placeholder='电信 或 *' onChange={(e) => update(i, { isp: e.target.value })} />
              <Select value={r.effect} onValueChange={(v) => update(i, { effect: v as RuleInput['effect'] })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='include'>{EFFECT_LABELS.include}</SelectItem>
                  <SelectItem value='exclude'>{EFFECT_LABELS.exclude}</SelectItem>
                </SelectContent>
              </Select>
              <Input
                type='number'
                inputMode='numeric'
                value={Number.isNaN(r.boost) ? '' : r.boost}
                placeholder='0–100'
                onChange={(e) => update(i, { boost: e.target.value === '' ? 0 : Number(e.target.value) })}
              />
              <Button variant='ghost' size='icon' aria-label='删除规则' onClick={() => removeRule(i)}>
                <X className='size-4' />
              </Button>
            </div>
          ))}
          <div>
            <Button variant='outline' size='sm' onClick={addRule}>
              <Plus className='size-4' /> 添加规则
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
            {mutation.isPending ? '保存中…' : '保存规则'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DeleteLineDialog({ line, onClose, onDeleted }: { line: StorageLine; onClose: () => void; onDeleted: () => void }) {
  const mutation = useMutation({
    mutationFn: () => lineApi.remove(line.id),
    onSuccess: () => {
      toast.success(`已删除线路「${line.name || line.id}」`)
      onDeleted()
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除线路「${line.name || line.id}」`}
      desc='删除后不可恢复，该线路的地区规则一并删除。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function LineTestDialog({ line, result, onClose }: { line: StorageLine; result: LineProbeResult; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            线路测试：{line.name || `线路 ${line.id}`}
            {result.ok ? <Badge variant='secondary'>可达</Badge> : <Badge variant='destructive'>不可达</Badge>}
          </DialogTitle>
          <DialogDescription>{result.message}</DialogDescription>
        </DialogHeader>
        <dl className='grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm'>
          <dt className='text-muted-foreground'>探测地址</dt>
          <dd className='break-all'>{result.url || '—'}</dd>
          <dt className='text-muted-foreground'>HTTP 状态</dt>
          <dd>{result.http_status || '—'}</dd>
          <dt className='text-muted-foreground'>Content-Type</dt>
          <dd className='break-all'>{result.content_type || '—'}</dd>
          <dt className='text-muted-foreground'>耗时</dt>
          <dd>{result.elapsed_ms} ms</dd>
          <dt className='text-muted-foreground'>CORS</dt>
          <dd className='flex items-center gap-1'>
            {result.cors ? <Check className='size-4 text-emerald-600' /> : <X className='size-4 text-destructive' />}
            <span className='break-all text-muted-foreground'>{result.cors_origin || '未返回 Access-Control-Allow-Origin'}</span>
          </dd>
        </dl>
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
