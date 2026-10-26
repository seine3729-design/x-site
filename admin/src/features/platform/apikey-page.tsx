import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { AlertTriangle, Ban, Copy, KeyRound, Pencil, Play, Plus } from 'lucide-react'
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
  Checkbox,
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
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  apiKeyApi,
  apiKeyKeys,
  apiKeyListQuery,
  displayStatusOf,
  scopeTitle,
  type APIKeyRow,
  type APIScopeInfo,
  type KeyStatus,
  type NewAPIKey,
} from './apikey-api'

type DialogState =
  | { type: 'create' }
  | { type: 'edit' | 'disable' | 'enable'; item: APIKeyRow }
  | { type: 'secret'; created: NewAPIKey }

/**
 * API Key（docs/06 第 32 节，A4-3）：给采集程序等服务端调用方发密钥。
 *
 * 完整密钥只在生成那一刻显示一次，系统里只留哈希与开头几位 —— 列表、审计、日志都取不回来，
 * 丢了只能重新生成一把。
 */
export function APIKeyPage() {
  const can = useCan()
  const visible = can('apikeys.view')
  const qc = useQueryClient()
  const search = useSearch({ from: '/_authenticated/apikeys' })
  const navigate = useNavigate({ from: '/apikeys' })
  const params = {
    q: search.q,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...apiKeyListQuery(params), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  const refresh = () => void qc.invalidateQueries({ queryKey: apiKeyKeys.all })

  const setStatus = useMutation({
    mutationFn: ({ item, status }: { item: APIKeyRow; status: KeyStatus }) =>
      status === 'inactive' ? apiKeyApi.disable(item.id) : apiKeyApi.enable(item.id),
    onSuccess: (res) => {
      setDialog(null)
      refresh()
      toast.success(res.status === 'inactive' ? `已停用「${res.name}」，调用方立刻失效` : `已启用「${res.name}」`)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='API Key' />
  const editable = can('apikeys.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const scopes = list.data?.scopes
  const filtered = Boolean(search.q || search.status)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='API Key'
        description='给采集程序等服务端调用方发的密钥。完整密钥只在生成时显示一次，这里只能看到开头几位。'
        readonly={!editable}
      />

      <section className='flex flex-col gap-1 rounded-md border p-4 text-xs text-muted-foreground'>
        <p>
          系统里<span className='font-medium'>只保存密钥的哈希</span>
          与开头几位，后台、审计日志、服务端日志都拿不到完整密钥。调用方丢了密钥只能重新生成一把、换掉旧的。
        </p>
        <p>
          停用<span className='font-medium'>即时生效</span>
          ：下一次调用就会被拒。密钥记录不会被删除，这样「这把密钥曾经存在、被谁用过」始终查得到。
        </p>
        <p>权限范围生成后不可修改。要缩小或扩大权限，重新生成一把交给调用方，再停用旧的。</p>
      </section>

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜用途、使用方或密钥开头'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <FilterSelect
          label='状态'
          value={search.status}
          options={[
            { label: '启用中', value: 'active' },
            { label: '已停用', value: 'inactive' },
          ]}
          onChange={(status) =>
            void navigate({ search: (prev) => withFilter(prev, { status: status as KeyStatus | undefined }) })
          }
        />
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
        <div className='ms-auto'>
          <WithReason reason={readonlyReason}>
            <Button className='h-9' disabled={!editable} onClick={() => setDialog({ type: 'create' })}>
              <Plus className='size-4' aria-hidden /> 生成密钥
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
                <TableHead className='pl-4'>用途</TableHead>
                <TableHead>密钥</TableHead>
                <TableHead>权限范围</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>调用次数</TableHead>
                <TableHead>最近使用</TableHead>
                <TableHead>创建时间</TableHead>
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
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的密钥' : '还没有密钥，点右上角「生成密钥」'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((item) => {
                  const badge = displayStatusOf(item.display_status)
                  return (
                    <TableRow key={item.id}>
                      <TableCell className='pl-4'>
                        <div className='flex flex-col'>
                          <span className='font-medium'>{item.name}</span>
                          {item.owner && <span className='text-xs text-muted-foreground'>{item.owner}</span>}
                        </div>
                      </TableCell>
                      <TableCell>
                        <code className='text-xs whitespace-nowrap'>{item.hint}…</code>
                      </TableCell>
                      <TableCell>
                        <div className='flex flex-wrap gap-1'>
                          {item.scopes.map((s) => (
                            <Badge key={s} variant='outline' className='whitespace-nowrap'>
                              {scopeTitle(s, scopes)}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={badge.variant} className='whitespace-nowrap'>
                          {badge.label}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-sm tabular-nums'>{item.used_count}</TableCell>
                      <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                        {item.last_used_at
                          ? new Date(item.last_used_at).toLocaleString('zh-CN', { hour12: false })
                          : '—'}
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                        {new Date(item.created_at).toLocaleDateString('zh-CN')}
                      </TableCell>
                      <TableCell className='pr-4'>
                        <div className='flex justify-end gap-1'>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`编辑「${item.name}」`}
                              onClick={() => setDialog({ type: 'edit', item })}
                            >
                              <Pencil className='size-4' />
                            </Button>
                          </WithReason>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={
                                item.status === 'active' ? `停用「${item.name}」` : `启用「${item.name}」`
                              }
                              onClick={() =>
                                setDialog({ type: item.status === 'active' ? 'disable' : 'enable', item })
                              }
                            >
                              {item.status === 'active' ? <Ban className='size-4' /> : <Play className='size-4' />}
                            </Button>
                          </WithReason>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })
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

      {dialog?.type === 'create' && (
        <APIKeyDialog
          scopes={scopes ?? []}
          onCreated={(created) => setDialog({ type: 'secret', created })}
          onClose={() => setDialog(null)}
        />
      )}

      {dialog?.type === 'edit' && (
        <APIKeyDialog
          item={dialog.item}
          scopes={scopes ?? []}
          onCreated={() => setDialog(null)}
          onClose={() => setDialog(null)}
        />
      )}

      {dialog?.type === 'secret' && (
        <SecretDialog created={dialog.created} scopes={scopes ?? []} onClose={() => setDialog(null)} />
      )}

      {dialog?.type === 'disable' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          destructive
          title={`停用「${dialog.item.name}」？`}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <div className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-2'>
                <AlertTriangle className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
                <span>
                  <span className='font-medium'>立刻生效。</span>
                  {dialog.item.owner ? `${dialog.item.owner}的` : '调用方的'}下一次请求就会被拒，
                  正在跑的对接会直接中断。
                </span>
              </div>
              <p className='text-muted-foreground'>随时可以再启用，密钥本身不变。</p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={setStatus.isPending ? '停用中…' : '确认停用'}
          isLoading={setStatus.isPending}
          handleConfirm={() => setStatus.mutate({ item: dialog.item, status: 'inactive' })}
        />
      )}

      {dialog?.type === 'enable' && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          title={`启用「${dialog.item.name}」？`}
          desc={
            <p className='text-sm'>
              启用后这把密钥立刻可以继续调用，权限范围是
              {dialog.item.scopes.map((s) => scopeTitle(s, scopes)).join('、')}。
            </p>
          }
          cancelBtnText='取消'
          confirmText={setStatus.isPending ? '启用中…' : '启用'}
          isLoading={setStatus.isPending}
          handleConfirm={() => setStatus.mutate({ item: dialog.item, status: 'active' })}
        />
      )}
    </div>
  )
}

const schema = z.object({
  name: z.string().trim().min(1, '请填写用途（这把密钥做什么用）').max(60, '用途不能超过 60 个字'),
  owner: z.string().trim().max(60, '使用方不能超过 60 个字'),
  scopes: z.array(z.string()).min(1, '至少要选一个权限范围'),
})

type APIKeyForm = z.infer<typeof schema>

/** 生成 / 编辑弹窗。编辑时权限范围只读：改范围等于改已在调用方手里的密钥能做什么。 */
function APIKeyDialog({
  item,
  scopes,
  onCreated,
  onClose,
}: {
  item?: APIKeyRow
  scopes: APIScopeInfo[]
  onCreated: (created: NewAPIKey) => void
  onClose: () => void
}) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<APIKeyForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: item?.name ?? '',
      owner: item?.owner ?? '',
      scopes: item?.scopes ?? [],
    },
  })
  const mutation = useMutation({
    mutationFn: (v: APIKeyForm) =>
      creating
        ? apiKeyApi.create(v)
        : apiKeyApi.update(item.id, v).then((row) => ({ ...row, secret: '' }) as NewAPIKey),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: apiKeyKeys.all })
      if (creating) {
        onCreated(res)
        return
      }
      toast.success('已保存')
      onClose()
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{creating ? '生成 API Key' : `编辑：${item.name}`}</DialogTitle>
          <DialogDescription>
            {creating
              ? '完整密钥只在生成后显示一次，请当场复制交给调用方。'
              : '只能改用途与使用方。权限范围和密钥本身都不可修改。'}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>用途</FormLabel>
                  <FormControl>
                    <Input autoComplete='off' placeholder='请填写这把密钥做什么用，如 剧集采集程序' {...field} />
                  </FormControl>
                  <FormDescription>停用或排查问题时靠它认出是哪一把，最多 60 个字。</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='owner'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>使用方</FormLabel>
                  <FormControl>
                    <Input autoComplete='off' placeholder='选填，谁在用，如 技术部 / 外部合作方 A' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='scopes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>权限范围</FormLabel>
                  <FormDescription>
                    {creating ? '按最小必要来选，生成后不能修改。' : '生成后不可修改，需要调整请重新生成一把。'}
                  </FormDescription>
                  <div className='flex flex-col gap-2'>
                    {scopes.map((s) => {
                      const checked = field.value.includes(s.scope)
                      const id = `scope-${s.scope}`
                      return (
                        <div key={s.scope} className='flex items-start gap-2 rounded-md border p-3'>
                          <Checkbox
                            id={id}
                            className='mt-0.5'
                            checked={checked}
                            disabled={!creating}
                            onCheckedChange={(v) => {
                              // 用 getValues 读最新值，不用闭包里的 field.value：
                              // 连着勾两个框时 React 还没重新渲染，闭包里的还是旧数组，后一次会把前一次覆盖掉
                              const cur = form.getValues('scopes')
                              field.onChange(
                                v === true ? [...cur, s.scope] : cur.filter((x) => x !== s.scope)
                              )
                            }}
                          />
                          <div className='flex flex-col gap-0.5'>
                            <label htmlFor={id} className='cursor-pointer text-sm font-medium'>
                              {s.title}
                            </label>
                            <span className='text-xs text-muted-foreground'>{s.desc}</span>
                            <code className='text-xs text-muted-foreground'>{s.scope}</code>
                          </div>
                        </div>
                      )
                    })}
                    {scopes.length === 0 && (
                      <p className='text-sm text-muted-foreground'>权限范围清单加载中…</p>
                    )}
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '提交中…' : creating ? '生成密钥' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

/** 复制到剪贴板。非安全上下文（非 localhost 的 http）下 clipboard 不可用，这时提示手动复制。 */
async function copyText(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(`已复制${label}`)
  } catch {
    toast.error('当前浏览器不允许自动复制，请手动选中后复制')
  }
}

/** 生成成功后一次性展示完整密钥。关掉这个弹窗之后谁也取不回来了。 */
function SecretDialog({
  created,
  scopes,
  onClose,
}: {
  created: NewAPIKey
  scopes: APIScopeInfo[]
  onClose: () => void
}) {
  const [confirmed, setConfirmed] = useState(false)
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <KeyRound className='size-5' aria-hidden /> 密钥已生成
          </DialogTitle>
          <DialogDescription>
            「{created.name}」的权限范围是
            {created.scopes.map((s) => scopeTitle(s, scopes)).join('、')}。
          </DialogDescription>
        </DialogHeader>

        <div className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm'>
          <AlertTriangle className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
          <span>
            <span className='font-medium'>现在复制走。</span>
            关掉这个弹窗之后，完整密钥就再也看不到了 —— 系统里只留哈希，列表里只有开头几位。
            丢了只能重新生成一把。
          </span>
        </div>

        <div className='flex flex-col gap-2'>
          <label htmlFor='new-secret' className='text-sm font-medium'>
            完整密钥
          </label>
          <div className='flex items-center gap-2'>
            <Input
              id='new-secret'
              readOnly
              className='h-10 font-mono text-xs'
              value={created.secret}
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button
              type='button'
              variant='secondary'
              className='h-10 shrink-0'
              onClick={() => void copyText(created.secret, '密钥')}
            >
              <Copy className='size-4' aria-hidden /> 复制
            </Button>
          </div>
          <p className='text-xs text-muted-foreground'>
            调用方在请求头里带上 <code>Authorization: Bearer &lt;密钥&gt;</code>。密钥等同于口令，
            只走服务端、不要放进前端代码或代码仓库。
          </p>
        </div>

        <div className='flex items-center gap-2'>
          <Checkbox
            id='secret-confirmed'
            checked={confirmed}
            onCheckedChange={(v) => setConfirmed(v === true)}
          />
          <label htmlFor='secret-confirmed' className='cursor-pointer text-sm'>
            我已经把密钥保存好了
          </label>
        </div>

        <DialogFooter>
          <Button type='button' disabled={!confirmed} onClick={onClose}>
            完成
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
