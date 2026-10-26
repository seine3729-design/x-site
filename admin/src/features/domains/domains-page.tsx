import { type ReactNode, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Copy, Download, Pencil, Plus, RotateCcw, Send, ShieldPlus, Trash2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { Pager } from '@/features/content/components/pager'
import {
  CERT_STATE_LABELS,
  CERT_STATE_VARIANTS,
  KEY_TYPE_LABELS,
  ROLE_LABELS,
  STATUS_LABELS,
  certDaysLabel,
  domainApi,
  domainKeys,
  domainsQuery,
  healthQuery,
  releasesQuery,
  type DomainEntry,
  type DomainInput,
  type DomainRole,
  type DomainStatus,
  type GeneratedCert,
  type KeyType,
  type ReleasePreview,
} from './api'

const ROLES: DomainRole[] = ['web_primary', 'web_backup', 'nav', 'api', 'media', 'admin']
const STATUSES: DomainStatus[] = ['active', 'watch', 'disabled']

/**
 * 域名与证书（docs/06 第 38 节，A4-4）：入口域名池、手动生成 / 上传 SSL 证书、签名清单发布与回滚、健康看板。
 * 仅超管可改（domains.edit 为高敏权限点）。入口证书由部署层 certbot 自动签发，这里的自签证书用于源站 mTLS 等。
 */
export function DomainsPage() {
  const can = useCan()
  const visible = can('domains.view')
  if (!visible) return <NoPermission title='域名与证书' />
  const editable = can('domains.edit')

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='域名与证书'
        description='维护入口域名池与证书到期；生成或上传 SSL 证书；发布离线签名的域名清单。公网入口证书由部署层自动签发。'
        readonly={!editable}
      />
      <Tabs defaultValue='pool'>
        <TabsList>
          <TabsTrigger value='pool'>域名池</TabsTrigger>
          <TabsTrigger value='releases'>签名清单</TabsTrigger>
          <TabsTrigger value='health'>健康看板</TabsTrigger>
        </TabsList>
        <TabsContent value='pool' className='mt-4'>
          <DomainPoolTab editable={editable} />
        </TabsContent>
        <TabsContent value='releases' className='mt-4'>
          <ReleasesTab editable={editable} />
        </TabsContent>
        <TabsContent value='health' className='mt-4'>
          <HealthTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// ---------- 域名池 ----------

type PoolDialog =
  | { type: 'create' }
  | { type: 'edit'; entry: DomainEntry }
  | { type: 'delete'; entry: DomainEntry }
  | { type: 'upload'; entry: DomainEntry }
  | { type: 'generate' }

function DomainPoolTab({ editable }: { editable: boolean }) {
  const qc = useQueryClient()
  const [status, setStatus] = useState<DomainStatus | 'all'>('all')
  const [role, setRole] = useState<DomainRole | 'all'>('all')
  const [q, setQ] = useState('')
  const [dialog, setDialog] = useState<PoolDialog | null>(null)

  const params = {
    status: status === 'all' ? undefined : status,
    role: role === 'all' ? undefined : role,
    q: q.trim() || undefined,
  }
  const list = useQuery(domainsQuery(params))
  const refresh = () => void qc.invalidateQueries({ queryKey: domainKeys.all })
  const readonlyReason = editable ? null : '当前角色只能查看'

  const remove = useMutation({
    mutationFn: (id: number) => domainApi.remove(id),
    onSuccess: () => {
      setDialog(null)
      refresh()
      toast.success('已删除域名')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const data = list.data
  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          className='h-9 w-56'
          placeholder='搜域名或备注'
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <FilterSelect label='状态' value={status} onChange={(v) => setStatus(v as DomainStatus | 'all')} options={STATUSES.map((s) => ({ value: s, label: STATUS_LABELS[s] }))} />
        <FilterSelect label='角色' value={role} onChange={(v) => setRole(v as DomainRole | 'all')} options={ROLES.map((r) => ({ value: r, label: ROLE_LABELS[r] }))} />
        <div className='ms-auto flex gap-2'>
          <WithReason reason={readonlyReason}>
            <Button variant='outline' className='h-9' disabled={!editable} onClick={() => setDialog({ type: 'generate' })}>
              <ShieldPlus className='size-4' aria-hidden /> 生成证书
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button className='h-9' disabled={!editable} onClick={() => setDialog({ type: 'create' })}>
              <Plus className='size-4' aria-hidden /> 新建域名
            </Button>
          </WithReason>
        </div>
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>域名</TableHead>
                <TableHead>角色</TableHead>
                <TableHead>权重</TableHead>
                <TableHead>地区</TableHead>
                <TableHead>CDN / DNS</TableHead>
                <TableHead>证书</TableHead>
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
              ) : (data?.list.length ?? 0) === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                    还没有域名，点右上角「新建域名」
                  </TableCell>
                </TableRow>
              ) : (
                data!.list.map((d) => (
                  <DomainRow
                    key={d.id}
                    entry={d}
                    editable={editable}
                    onEdit={() => setDialog({ type: 'edit', entry: d })}
                    onDelete={() => setDialog({ type: 'delete', entry: d })}
                    onUpload={() => setDialog({ type: 'upload', entry: d })}
                  />
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <DomainFormDialog
          entry={dialog.type === 'edit' ? dialog.entry : null}
          onDone={refresh}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'generate' && <GenerateCertDialog onClose={() => setDialog(null)} />}
      {dialog?.type === 'upload' && (
        <UploadCertDialog entry={dialog.entry} onDone={refresh} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          title={`删除域名「${dialog.entry.host}」？`}
          desc='设置类数据，不进回收站，删除不可恢复。停用的域名仍会从下一版清单中移除，若只是暂时下线建议改状态为「停用」。'
          destructive
          cancelBtnText='取消'
          confirmText={remove.isPending ? '删除中…' : '删除'}
          isLoading={remove.isPending}
          handleConfirm={() => remove.mutate(dialog.entry.id)}
        />
      )}
    </div>
  )
}

function DomainRow({
  entry,
  editable,
  onEdit,
  onDelete,
  onUpload,
}: {
  entry: DomainEntry
  editable: boolean
  onEdit: () => void
  onDelete: () => void
  onUpload: () => void
}) {
  return (
    <TableRow>
      <TableCell className='pl-4 font-medium'>{entry.host}</TableCell>
      <TableCell>
        <Badge variant='outline'>{entry.role_title}</Badge>
      </TableCell>
      <TableCell className='tabular-nums'>{entry.weight}</TableCell>
      <TableCell className='text-sm'>{entry.region}</TableCell>
      <TableCell className='text-xs text-muted-foreground'>
        {entry.cdn || '—'}
        {entry.dns_provider ? ` / ${entry.dns_provider}` : ''}
      </TableCell>
      <TableCell>
        <div className='flex flex-col gap-0.5'>
          <Badge variant={CERT_STATE_VARIANTS[entry.cert_state]} className='w-fit'>
            {CERT_STATE_LABELS[entry.cert_state]}
          </Badge>
          <span className='text-xs text-muted-foreground'>{certDaysLabel(entry.cert_state, entry.cert_days_left)}</span>
        </div>
      </TableCell>
      <TableCell>
        <Badge variant={entry.status === 'disabled' ? 'secondary' : 'outline'}>{entry.status_title}</Badge>
      </TableCell>
      <TableCell className='pr-4'>
        <div className='flex justify-end gap-1'>
          <Button variant='ghost' size='icon' disabled={!editable} onClick={onUpload} aria-label='上传证书回填到期日'>
            <Upload className='size-4' />
          </Button>
          <Button variant='ghost' size='icon' disabled={!editable} onClick={onEdit} aria-label={`编辑「${entry.host}」`}>
            <Pencil className='size-4' />
          </Button>
          <Button variant='ghost' size='icon' disabled={!editable} onClick={onDelete} aria-label={`删除「${entry.host}」`}>
            <Trash2 className='size-4' />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className='h-9 w-32' aria-label={label}>
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value='all'>{label}：全部</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

// ---------- 域名表单 ----------

function DomainFormDialog({ entry, onDone, onClose }: { entry: DomainEntry | null; onDone: () => void; onClose: () => void }) {
  const editing = entry !== null
  const [host, setHost] = useState(entry?.host ?? '')
  const [role, setRole] = useState<DomainRole>(entry?.role ?? 'api')
  const [weight, setWeight] = useState(String(entry?.weight ?? 100))
  const [region, setRegion] = useState(entry?.region ?? '*')
  const [cdn, setCdn] = useState(entry?.cdn ?? '')
  const [dns, setDns] = useState(entry?.dns_provider ?? '')
  const [status, setStatus] = useState<DomainStatus>(entry?.status ?? 'active')
  const [note, setNote] = useState(entry?.note ?? '')
  const [certDate, setCertDate] = useState(entry?.cert_expires_at ? entry.cert_expires_at.slice(0, 10) : '')

  const save = useMutation({
    mutationFn: (body: DomainInput) => (editing ? domainApi.update(entry.id, body) : domainApi.create(body)),
    onSuccess: () => {
      onDone()
      onClose()
      toast.success(editing ? '已保存域名' : '已新建域名')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const weightNum = Number(weight)
  const invalid = host.trim() === '' || !Number.isInteger(weightNum) || weightNum < 1 || weightNum > 100

  const submit = () => {
    save.mutate({
      host: host.trim(),
      role,
      weight: weightNum,
      region: region.trim() || '*',
      cdn: cdn.trim(),
      dns_provider: dns.trim(),
      cert_expires_at: certDate ? new Date(`${certDate}T00:00:00Z`).toISOString() : null,
      status,
      note: note.trim(),
    })
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{editing ? `编辑域名：${entry.host}` : '新建域名'}</DialogTitle>
          <DialogDescription>域名角色与权重决定它在签名清单里的分发方式；证书到期日也可由「上传证书」自动回填。</DialogDescription>
        </DialogHeader>
        <div className='grid grid-cols-2 gap-3'>
          <Field className='col-span-2' label='域名' required>
            <Input placeholder='如 a1.example.net（只填主机名）' value={host} onChange={(e) => setHost(e.target.value)} />
          </Field>
          <Field label='角色'>
            <Select value={role} onValueChange={(v) => setRole(v as DomainRole)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((r) => (
                  <SelectItem key={r} value={r}>
                    {ROLE_LABELS[r]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label='状态'>
            <Select value={status} onValueChange={(v) => setStatus(v as DomainStatus)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {STATUS_LABELS[s]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label='权重（1–100）'>
            <Input inputMode='numeric' value={weight} onChange={(e) => setWeight(e.target.value.replace(/\D/g, ''))} />
          </Field>
          <Field label='适用地区'>
            <Input placeholder='* 或 cn / us…' value={region} onChange={(e) => setRegion(e.target.value)} />
          </Field>
          <Field label='所属 CDN'>
            <Input placeholder='可留空' value={cdn} onChange={(e) => setCdn(e.target.value)} />
          </Field>
          <Field label='DNS 服务商'>
            <Input placeholder='可留空' value={dns} onChange={(e) => setDns(e.target.value)} />
          </Field>
          <Field label='证书到期日'>
            <Input type='date' value={certDate} onChange={(e) => setCertDate(e.target.value)} />
          </Field>
          <Field className='col-span-2' label='备注'>
            <Textarea rows={2} placeholder='可留空，≤200 字' value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button disabled={save.isPending || invalid} onClick={submit}>
            {save.isPending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, required, className, children }: { label: string; required?: boolean; className?: string; children: ReactNode }) {
  return (
    <div className={`flex flex-col gap-1.5 ${className ?? ''}`}>
      <Label className='text-sm'>
        {label}
        {required && <span className='text-destructive'> *</span>}
      </Label>
      {children}
    </div>
  )
}

// ---------- 生成自签证书 ----------

function GenerateCertDialog({ onClose }: { onClose: () => void }) {
  const [cn, setCn] = useState('')
  const [sans, setSans] = useState('')
  const [days, setDays] = useState('825')
  const [keyType, setKeyType] = useState<KeyType>('ec_p256')
  const [result, setResult] = useState<GeneratedCert | null>(null)

  const gen = useMutation({
    mutationFn: () =>
      domainApi.generateCert({
        common_name: cn.trim(),
        sans: sans
          .split(/[\s,]+/)
          .map((s) => s.trim())
          .filter(Boolean),
        days: Number(days) || 0,
        key_type: keyType,
      }),
    onSuccess: (res) => {
      setResult(res)
      toast.success('已生成证书，请立即下载私钥')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const daysNum = Number(days)
  const invalid = cn.trim() === '' || !Number.isInteger(daysNum) || daysNum < 1 || daysNum > 3650

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>手动生成 SSL 证书</DialogTitle>
          <DialogDescription>
            现场签出自签证书（浏览器不信任），用于源站回源 mTLS、本地 / 测试 HTTPS 或 certbot 应急占位。
            私钥只在这里显示一次，服务端不保存，请立即下载保存。
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <CertResult result={result} />
        ) : (
          <div className='flex flex-col gap-3'>
            <Field label='证书主题（Common Name）' required>
              <Input placeholder='如 origin.example.net 或 *.example.net' value={cn} onChange={(e) => setCn(e.target.value)} />
            </Field>
            <Field label='附加 SAN（可留空，逗号或空格分隔）'>
              <Textarea rows={2} placeholder='如 a.example.net b.example.net' value={sans} onChange={(e) => setSans(e.target.value)} />
            </Field>
            <div className='grid grid-cols-2 gap-3'>
              <Field label='有效期（天，1–3650）'>
                <Input inputMode='numeric' value={days} onChange={(e) => setDays(e.target.value.replace(/\D/g, ''))} />
              </Field>
              <Field label='密钥类型'>
                <Select value={keyType} onValueChange={(v) => setKeyType(v as KeyType)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(Object.keys(KEY_TYPE_LABELS) as KeyType[]).map((k) => (
                      <SelectItem key={k} value={k}>
                        {KEY_TYPE_LABELS[k]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            {result ? '关闭' : '取消'}
          </Button>
          {!result && (
            <Button disabled={gen.isPending || invalid} onClick={() => gen.mutate()}>
              {gen.isPending ? '生成中…' : '生成'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function downloadPem(name: string, content: string) {
  const url = URL.createObjectURL(new Blob([content], { type: 'application/x-pem-file' }))
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  URL.revokeObjectURL(url)
}

function CertResult({ result }: { result: GeneratedCert }) {
  return (
    <div className='flex flex-col gap-3'>
      <div className='rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm'>
        私钥 <span className='font-medium'>只显示这一次</span>，关闭后无法再取回。请立即下载 key.pem 并妥善保管。
      </div>
      <div className='grid grid-cols-2 gap-x-6 gap-y-1 text-sm'>
        <span className='text-muted-foreground'>有效期至</span>
        <span className='tabular-nums'>{new Date(result.not_after).toLocaleString('zh-CN', { hour12: false })}</span>
        <span className='text-muted-foreground'>SAN</span>
        <span className='break-all'>{result.sans.join('、')}</span>
        <span className='text-muted-foreground'>指纹</span>
        <span className='break-all font-mono text-xs'>{result.fingerprint}</span>
      </div>
      <PemBlock label='证书 cert.pem' value={result.cert_pem} onDownload={() => downloadPem('cert.pem', result.cert_pem)} />
      <PemBlock label='私钥 key.pem（仅此一次）' value={result.key_pem} onDownload={() => downloadPem('key.pem', result.key_pem)} />
    </div>
  )
}

function PemBlock({ label, value, onDownload }: { label: string; value: string; onDownload: () => void }) {
  return (
    <div className='flex flex-col gap-1'>
      <div className='flex items-center justify-between'>
        <Label className='text-sm'>{label}</Label>
        <div className='flex gap-1'>
          <Button variant='ghost' size='icon' aria-label='复制' onClick={() => void navigator.clipboard?.writeText(value).then(() => toast.success('已复制'))}>
            <Copy className='size-4' />
          </Button>
          <Button variant='ghost' size='icon' aria-label='下载' onClick={onDownload}>
            <Download className='size-4' />
          </Button>
        </div>
      </div>
      <Textarea readOnly rows={4} className='font-mono text-xs' value={value} />
    </div>
  )
}

// ---------- 上传证书回填 ----------

function UploadCertDialog({ entry, onDone, onClose }: { entry: DomainEntry; onDone: () => void; onClose: () => void }) {
  const [pem, setPem] = useState('')
  const upload = useMutation({
    mutationFn: () => domainApi.uploadCert(entry.id, pem),
    onSuccess: (res) => {
      onDone()
      onClose()
      toast.success(`已回填到期日：${new Date(res.cert.not_after).toLocaleDateString('zh-CN')}`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>为「{entry.host}」上传证书</DialogTitle>
          <DialogDescription>粘贴一张已签发的 PEM 证书，解析它的到期日回填到该域名（只读取证书公开信息，不涉及私钥）。</DialogDescription>
        </DialogHeader>
        <Textarea
          rows={8}
          className='font-mono text-xs'
          placeholder={'-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----'}
          value={pem}
          onChange={(e) => setPem(e.target.value)}
        />
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button disabled={upload.isPending || pem.trim() === ''} onClick={() => upload.mutate()}>
            {upload.isPending ? '解析中…' : '解析并回填'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------- 签名清单 ----------

function ReleasesTab({ editable }: { editable: boolean }) {
  const qc = useQueryClient()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [preview, setPreview] = useState<{ fromVersion?: number } | null>(null)
  const list = useQuery(releasesQuery(page, pageSize))
  const refresh = () => void qc.invalidateQueries({ queryKey: domainKeys.all })
  const readonlyReason = editable ? null : '当前角色只能查看'
  const data = list.data

  return (
    <div className='flex flex-col gap-4'>
      <Card>
        <CardHeader className='pb-2'>
          <CardTitle className='text-base'>当前线上清单</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-wrap items-center gap-x-8 gap-y-2 text-sm'>
          {data?.current ? (
            <>
              <span>
                版本 <span className='font-medium tabular-nums'>{data.current.version}</span>
              </span>
              <span className='text-muted-foreground'>
                发布于 {new Date(data.current.published_at).toLocaleString('zh-CN', { hour12: false })}
                {data.current.published_by_name && ` · ${data.current.published_by_name}`}
              </span>
              <span className='text-muted-foreground'>
                有效期至 {new Date(data.current.payload.expires_at).toLocaleDateString('zh-CN')}
              </span>
            </>
          ) : (
            <span className='text-muted-foreground'>还没有发布过清单。</span>
          )}
          <div className='ms-auto'>
            <WithReason reason={readonlyReason}>
              <Button className='h-9' disabled={!editable} onClick={() => setPreview({})}>
                <Send className='size-4' aria-hidden /> 生成新版清单
              </Button>
            </WithReason>
          </div>
        </CardContent>
      </Card>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>版本</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>发布人</TableHead>
                <TableHead>发布时间</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : (data?.list.length ?? 0) === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    还没有发布记录
                  </TableCell>
                </TableRow>
              ) : (
                data!.list.map((r) => (
                  <TableRow key={r.version}>
                    <TableCell className='pl-4 font-medium tabular-nums'>{r.version}</TableCell>
                    <TableCell className='max-w-[20rem] text-sm text-muted-foreground'>{r.note || '—'}</TableCell>
                    <TableCell className='text-sm'>{r.published_by_name || '—'}</TableCell>
                    <TableCell className='text-xs whitespace-nowrap tabular-nums'>
                      {new Date(r.published_at).toLocaleString('zh-CN', { hour12: false })}
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end'>
                        <WithReason reason={readonlyReason}>
                          <Button variant='ghost' size='sm' disabled={!editable} onClick={() => setPreview({ fromVersion: r.version })}>
                            <RotateCcw className='size-4' aria-hidden /> 回滚到此版本
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

      {data && data.total > 0 && (
        <Pager
          page={page}
          pageSize={pageSize}
          total={data.total}
          onChange={(p, s) => {
            setPage(p)
            setPageSize(s)
          }}
        />
      )}

      {preview && <PreviewDialog fromVersion={preview.fromVersion} onDone={refresh} onClose={() => setPreview(null)} />}
    </div>
  )
}

/** 生成待签清单预览 → 展示 JSON 供离线签名 → 粘贴签名发布。 */
function PreviewDialog({ fromVersion, onDone, onClose }: { fromVersion?: number; onDone: () => void; onClose: () => void }) {
  const [prev, setPrev] = useState<ReleasePreview | null>(null)
  const [signature, setSignature] = useState('')
  const [note, setNote] = useState('')

  const load = useQuery({
    queryKey: ['domains', 'preview', fromVersion ?? 0],
    queryFn: () => domainApi.previewRelease(fromVersion),
  })
  // useQuery 的结果同步进本地（展示用，可编辑签名）
  if (load.data && prev === null) setPrev(load.data)

  const payloadText = prev ? JSON.stringify(prev.payload, null, 2) : ''

  const publish = useMutation({
    mutationFn: () => domainApi.publishRelease({ payload: prev!.payload, signature: signature.trim(), note: note.trim() }),
    onSuccess: (rel) => {
      onDone()
      onClose()
      toast.success(`已发布第 ${rel.version} 版清单`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            生成新版清单{fromVersion ? `（基于第 ${fromVersion} 版内容）` : ''}
          </DialogTitle>
          <DialogDescription>
            下面是待签名的清单正文（版本号 v 已是下一个版本）。用离线签名工具（keytool）对它签名，把签名粘到下方后发布；后台不保存签名私钥。
          </DialogDescription>
        </DialogHeader>

        {load.isError ? (
          <LoadError onRetry={() => void load.refetch()} />
        ) : !prev ? (
          <p className='py-6 text-center text-muted-foreground'>生成中…</p>
        ) : (
          <div className='flex flex-col gap-3'>
            <div className='flex items-center justify-between text-sm'>
              <span>
                版本 <span className='font-medium tabular-nums'>{prev.version}</span> · 共 {prev.hosts} 个主机
              </span>
              <Button
                variant='ghost'
                size='sm'
                onClick={() => void navigator.clipboard?.writeText(payloadText).then(() => toast.success('已复制待签内容'))}
              >
                <Copy className='size-4' aria-hidden /> 复制待签内容
              </Button>
            </div>
            <Textarea readOnly rows={8} className='font-mono text-xs' value={payloadText} />
            <Field label='离线签名（base64）' required>
              <Textarea
                rows={2}
                className='font-mono text-xs'
                placeholder='把 keytool 对上面内容签出的签名粘到这里'
                value={signature}
                onChange={(e) => setSignature(e.target.value)}
              />
            </Field>
            <Field label='备注'>
              <Input placeholder='可留空，如「下线 a3 域名」' value={note} onChange={(e) => setNote(e.target.value)} />
            </Field>
          </div>
        )}

        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button disabled={!prev || publish.isPending || signature.trim() === ''} onClick={() => publish.mutate()}>
            {publish.isPending ? '发布中…' : '发布'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------- 健康看板 ----------

function HealthTab() {
  const q = useQuery(healthQuery())
  if (q.isError) return <LoadError onRetry={() => void q.refetch()} />
  if (q.isPending) return <p className='text-sm text-muted-foreground'>加载中…</p>
  const data = q.data
  if (data.list.length === 0) {
    return (
      <div className='flex items-start gap-2 rounded-md border p-4 text-sm text-muted-foreground'>
        <Activity className='mt-0.5 size-4 shrink-0' aria-hidden />
        <span>{data.note || '暂无健康数据。'}</span>
      </div>
    )
  }
  return (
    <div className='flex flex-col gap-3'>
      <p className='text-sm text-muted-foreground'>最近 {data.since_days} 天，按域名与地区聚合。</p>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted'>
              <TableHead className='pl-4'>域名</TableHead>
              <TableHead>地区</TableHead>
              <TableHead>探测成功率</TableHead>
              <TableHead>切换次数</TableHead>
              <TableHead className='pr-4'>失败原因</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.list.map((r) => (
              <TableRow key={`${r.host}:${r.region}`}>
                <TableCell className='pl-4 font-medium'>{r.host}</TableCell>
                <TableCell className='text-sm'>{r.region}</TableCell>
                <TableCell className='tabular-nums'>
                  {r.success_rate === null ? '—' : `${(r.success_rate * 100).toFixed(1)}%`}
                </TableCell>
                <TableCell className='tabular-nums'>{r.failover_count}</TableCell>
                <TableCell className='pr-4 text-xs text-muted-foreground'>
                  {Object.keys(r.fail_reasons).length === 0
                    ? '—'
                    : Object.entries(r.fail_reasons)
                        .map(([k, v]) => `${k}:${v}`)
                        .join('、')}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
