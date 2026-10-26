import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Check, Copy, FolderOpen, KeyRound, Pencil, Plug, Plus, Trash2, X } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { errorMessage } from '@/lib/http'
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
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SelectField, TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  FTP_STATUS_LABELS,
  ftpApi,
  ftpKeys,
  ftpListQuery,
  PROBE_STEP_LABELS,
  TLS_LABELS,
  type FTPServer,
  type FTPServerInput,
  type ProbeResult,
  type TLSMode,
} from './api'
import { BrowseDialog } from './browse-dialog'

type DialogState =
  | { type: 'create' }
  | { type: 'edit' | 'delete' | 'cred' | 'browse'; item: FTPServer }

/**
 * 资源机 FTP（docs/06 第 31 节，A1-4）：登记资源机并测连、打开目录。
 * 流程 ①测连 →②打开目录 随 S1-6 完成；③查新剧 ④入库 随 S4-6 开放。
 * 账号密码不在后台填写：由技术按 cred_env 的两个环境变量名配在 worker 上。
 */
export function FTPPage() {
  const can = useCan()
  const visible = can('ftp.view')
  const search = useSearch({ from: '/_authenticated/ftp' })
  const navigate = useNavigate({ from: '/ftp' })
  const params = {
    q: search.q,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...ftpListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [testing, setTesting] = useState<number | null>(null)
  const [result, setResult] = useState<{ server: FTPServer; result: ProbeResult } | null>(null)

  const test = useMutation({
    mutationFn: (s: FTPServer) => ftpApi.test(s.id),
    onMutate: (s) => setTesting(s.id),
    onSuccess: (res, s) => setResult({ server: s, result: res }),
    onError: (err) => toast.error(errorMessage(err)),
    onSettled: () => setTesting(null),
  })

  if (!visible) return <NoPermission title='资源机 FTP' />
  const editable = can('ftp.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='资源机 FTP'
        description='登记片源所在的 FTP 服务器，用于扫描入库。账号密码不在后台填写：由技术按「凭据配置」里的两个环境变量名配在 worker 上。测连与打开目录都由 worker 发起，不暴露 API 节点地址。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 登记资源机
            </Button>
          )
        }
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜名称或地址'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <FilterSelect
          label='状态'
          value={search.status}
          options={Object.entries(FTP_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as typeof search.status }) })}
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
                <TableHead className='pl-4'>名称</TableHead>
                <TableHead>地址</TableHead>
                <TableHead>加密</TableHead>
                <TableHead>凭据标识</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的资源机' : '还没有登记资源机'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className='pl-4 font-medium'>{s.name}</TableCell>
                    <TableCell>
                      <code className='text-xs'>
                        {s.host}:{s.port}
                      </code>
                    </TableCell>
                    <TableCell className='text-sm'>
                      {s.tls === 'explicit' ? (
                        TLS_LABELS.explicit
                      ) : (
                        <Badge variant='outline' className='text-amber-600'>
                          {TLS_LABELS[s.tls]}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-1'>
                        <code className='text-xs'>{s.cred_ref}</code>
                        <Button
                          variant='ghost'
                          size='icon'
                          aria-label={`查看「${s.name}」的凭据配置`}
                          onClick={() => setDialog({ type: 'cred', item: s })}
                        >
                          <KeyRound className='size-4' />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={s.status === 'active' ? 'secondary' : 'outline'}>
                        {FTP_STATUS_LABELS[s.status]}
                      </Badge>
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          variant='ghost'
                          size='icon'
                          aria-label={`测连「${s.name}」`}
                          disabled={testing !== null}
                          onClick={() => test.mutate(s)}
                        >
                          <Plug className={`size-4 ${testing === s.id ? 'animate-pulse' : ''}`} />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon'
                          aria-label={`打开「${s.name}」的目录`}
                          onClick={() => setDialog({ type: 'browse', item: s })}
                        >
                          <FolderOpen className='size-4' />
                        </Button>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`编辑「${s.name}」`}
                            onClick={() => setDialog({ type: 'edit', item: s })}
                          >
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除「${s.name}」`}
                            onClick={() => setDialog({ type: 'delete', item: s })}
                          >
                            <Trash2 className='size-4' />
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
        <FTPServerDialog item={dialog.type === 'edit' ? dialog.item : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && <DeleteDialog item={dialog.item} onClose={() => setDialog(null)} />}
      {dialog?.type === 'cred' && <CredDialog item={dialog.item} onClose={() => setDialog(null)} />}
      {dialog?.type === 'browse' && <BrowseDialog server={dialog.item} onClose={() => setDialog(null)} />}
      {result && <TestResultDialog server={result.server} result={result.result} onClose={() => setResult(null)} />}
    </div>
  )
}

// ---------- 表单 ----------

const ftpSchema = z.object({
  name: z.string().trim().min(1, '请填写名称').max(64, '名称最多 64 个字'),
  host: z
    .string()
    .trim()
    .min(1, '请填写地址')
    .max(253, '地址最多 253 个字符')
    .refine((v) => !/^[a-z]+:\/\//i.test(v) && !v.includes('/') && !v.includes(':'), '只填域名或 IP，不要带协议、端口和路径'),
  port: z.number({ error: '请填写 1–65535 的整数' }).int('请填写整数').min(1, '端口为 1–65535').max(65535, '端口为 1–65535'),
  tls: z.enum(['none', 'explicit', 'implicit']),
  cred_ref: z
    .string()
    .trim()
    .max(32, '凭据标识最多 32 位')
    .refine((v) => v === '' || /^[A-Za-z][A-Za-z0-9_]{0,31}$/.test(v), '只能用字母、数字和下划线，以字母开头'),
  status: z.enum(['active', 'inactive']),
})

type FTPForm = z.infer<typeof ftpSchema>

function FTPServerDialog({ item, onClose }: { item?: FTPServer; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<FTPForm>({
    resolver: zodResolver(ftpSchema),
    defaultValues: {
      name: item?.name ?? '',
      host: item?.host ?? '',
      port: item?.port ?? 21,
      tls: item?.tls ?? 'explicit',
      cred_ref: item?.cred_ref ?? '',
      status: item?.status ?? 'active',
    },
  })

  const save = useMutation({
    mutationFn: (v: FTPForm) => {
      // passive 不传：后端默认 true，且只支持被动模式
      const input: FTPServerInput = {
        name: v.name,
        host: v.host,
        port: v.port,
        tls: v.tls,
        cred_ref: v.cred_ref || undefined,
        status: v.status,
      }
      return creating ? ftpApi.create(input) : ftpApi.update(item.id, input)
    },
    onSuccess: (saved) => {
      void qc.invalidateQueries({ queryKey: ftpKeys.all })
      toast.success(
        creating
          ? `已登记「${saved.name}」。请把 ${saved.cred_env.user} 与 ${saved.cred_env.password} 交给技术在 worker 上配置账号密码。`
          : `已保存「${saved.name}」`
      )
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{creating ? '登记资源机' : `编辑「${item.name}」`}</DialogTitle>
          <DialogDescription>
            只填连接信息。账号密码不入库、也不经过后台，保存后按「凭据配置」里的两个环境变量名交给技术配置。
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => save.mutate(v))}>
            <TextField
              control={form.control}
              name='name'
              label='名称'
              required
              placeholder='请输入便于识别的名称，如 香港资源机 1'
            />
            <div className='grid gap-4 sm:grid-cols-[1fr_8rem]'>
              <TextField
                control={form.control}
                name='host'
                label='地址'
                required
                placeholder='请输入域名或 IP，如 ftp.example.com'
                description='不带协议、端口和路径；中文域名请填 xn-- 开头的编码形式'
              />
              <FormField
                control={form.control}
                name='port'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>端口</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        inputMode='numeric'
                        placeholder='默认 21'
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
            <SelectField
              control={form.control}
              name='tls'
              label='加密方式'
              placeholder='请选择加密方式'
              items={(Object.keys(TLS_LABELS) as TLSMode[]).map((v) => ({ value: v, label: TLS_LABELS[v] }))}
              description='生产环境只允许显式 TLS（FTPS）；选其他方式保存会被拒绝'
            />
            <div className='flex items-center justify-between rounded-md border px-3 py-2'>
              <div className='flex flex-col gap-0.5'>
                <span className='text-sm font-medium'>被动模式</span>
                <span className='text-xs text-muted-foreground'>
                  固定开启。主动模式需要资源机回连 worker，穿不过 NAT，也会暴露 worker 地址。
                </span>
              </div>
              <Badge variant='secondary'>
                <Check aria-hidden /> 已开启
              </Badge>
            </div>
            <TextField
              control={form.control}
              name='cred_ref'
              label='凭据标识'
              placeholder={creating ? '留空自动生成，如 HK1' : '留空保持原值'}
              description={
                creating
                  ? '会拼成 worker 的环境变量名 FTP_<标识>_USER / _PASSWORD。保存时自动转大写。'
                  : '改了标识就要在 worker 上重新配置账号密码，原来的配置会失效。'
              }
            />
            <SelectField
              control={form.control}
              name='status'
              label='状态'
              placeholder='请选择状态'
              items={Object.entries(FTP_STATUS_LABELS).map(([value, label]) => ({ value, label }))}
              description='停用后不参与扫描入库，仍然可以测连与打开目录'
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={save.isPending}>
                {save.isPending ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteDialog({ item, onClose }: { item: FTPServer; onClose: () => void }) {
  const qc = useQueryClient()
  const remove = useMutation({
    mutationFn: () => ftpApi.remove(item.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ftpKeys.all })
      toast.success(`已删除「${item.name}」`)
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除资源机「${item.name}」`}
      desc='设置类数据，不进回收站，删除后不可恢复。worker 上该凭据标识的账号密码配置可以一并清理。'
      cancelBtnText='取消'
      confirmText={remove.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={remove.isPending}
      handleConfirm={() => remove.mutate()}
    />
  )
}

// ---------- 凭据配置 ----------

/** 复制到剪贴板。非安全上下文（非 localhost 的 http）下 clipboard 不可用，这时提示手动复制。 */
async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(`已复制${label}`)
  } catch {
    toast.error('当前浏览器不允许自动复制，请手动选中后复制')
  }
}

function CredDialog({ item, onClose }: { item: FTPServer; onClose: () => void }) {
  const both = `${item.cred_env.user}=\n${item.cred_env.password}=`
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>凭据配置：{item.name}</DialogTitle>
          <DialogDescription>
            账号密码不入库、不经过后台。把下面两个环境变量名发给技术，由技术在 worker 的密钥管理中填入实际账号密码。
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <EnvRow label='账号' name={item.cred_env.user} />
          <EnvRow label='密码' name={item.cred_env.password} />
        </div>
        <p className='text-xs text-muted-foreground'>
          凭据标识 <code>{item.cred_ref}</code>。改了标识后这两个名字会跟着变，需要在 worker 上重新配置；
          测连时报登录失败，通常就是这两项没配或配错了。
        </p>
        <DialogFooter>
          <Button variant='outline' onClick={() => void copyText(both, '两个环境变量名')}>
            <Copy /> 复制两项
          </Button>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function EnvRow({ label, name }: { label: string; name: string }) {
  return (
    <div className='flex items-center gap-2 rounded-md border px-3 py-2'>
      <span className='w-8 shrink-0 text-sm text-muted-foreground'>{label}</span>
      <code className='flex-1 truncate text-sm'>{name}</code>
      <Button variant='ghost' size='icon' aria-label={`复制${label}的环境变量名`} onClick={() => void copyText(name, name)}>
        <Copy className='size-4' />
      </Button>
    </div>
  )
}

// ---------- 测连结果 ----------

function TestResultDialog({
  server,
  result,
  onClose,
}: {
  server: FTPServer
  result: ProbeResult
  onClose: () => void
}) {
  const failed = result.steps.find((s) => !s.ok)
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            测连：{server.name}
            {result.ok ? <Badge variant='secondary'>通过</Badge> : <Badge variant='destructive'>未通过</Badge>}
          </DialogTitle>
          <DialogDescription>
            由 worker 发起，共耗时 {result.elapsed_ms} 毫秒。按步骤依次执行，某一步失败后不再继续。
          </DialogDescription>
        </DialogHeader>
        <ol className='flex flex-col gap-2'>
          {result.steps.map((s) => (
            <li key={s.name} className='flex items-start gap-2 text-sm'>
              {s.ok ? (
                <Check className='mt-0.5 size-4 shrink-0 text-emerald-600' aria-hidden />
              ) : (
                <X className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
              )}
              <div className='flex flex-col'>
                <span className='font-medium'>
                  {PROBE_STEP_LABELS[s.name]} <span className='text-xs text-muted-foreground tabular-nums'>{s.ms}ms</span>
                </span>
                <span className='break-all text-muted-foreground'>{s.message}</span>
              </div>
            </li>
          ))}
        </ol>
        {failed?.name === 'login' && (
          <p className='rounded-md border border-amber-500/40 bg-amber-500/5 p-2 text-xs'>
            登录失败多半是 worker 上没配账号密码。把 <code>{server.cred_env.user}</code> 和{' '}
            <code>{server.cred_env.password}</code> 发给技术配置后重试。
          </p>
        )}
        {result.ok && result.path && (
          <p className='truncate font-mono text-xs text-muted-foreground' title={result.path}>
            登录目录：{result.path}
          </p>
        )}
        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
