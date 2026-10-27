import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Ban, Copy, Pencil, Plus } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
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
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SelectField, TextField } from '@/features/content/components/fields'
import { ResetButton } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  API_KEY_STATUS_LABELS,
  DEFAULT_PAGE_SIZE,
  VISIBILITY_LABELS,
  allProjectsQuery,
  apiKeyListQuery,
  feedbackApi,
  feedbackKeys,
  optionsOf,
  type ApiKeyListParams,
  type FbApiKey,
} from './api'

const COLUMNS = 8

/**
 * 开放接口（feedback/docs/08 第 8.7 节、adr/0009）：给站外网页发一个密钥，让它们往指定项目提交反馈。
 * 密钥只能写入一条反馈，不能读任何数据；明文只在生成时显示一次。
 */
export function ApiKeysPage() {
  const can = useCan()
  const visible = can('fb_api_keys.view')
  const search = useSearch({ from: '/_authenticated/feedback/api-keys' })
  const navigate = useNavigate({ from: '/feedback/api-keys' })
  const params: ApiKeyListParams = {
    project_id: search.project_id,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...apiKeyListQuery(params), enabled: visible })
  const projects = useQuery({ ...allProjectsQuery, enabled: visible })
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<FbApiKey | null>(null)
  const [revoking, setRevoking] = useState<FbApiKey | null>(null)
  const [fresh, setFresh] = useState<{ raw: string; key: FbApiKey } | null>(null)

  if (!visible) return <NoPermission title='开放接口' />
  const editable = can('fb_api_keys.edit')
  const filtered = !!search.project_id
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='开放接口'
        description='给合作站点发一个密钥，它们就能在自己页面上加反馈入口。收到的反馈默认只对项目成员可见，先在「反馈分诊」里确认再决定是否公开。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 生成密钥
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <Select
          value={search.project_id ? String(search.project_id) : 'all'}
          onValueChange={(v) => setFilter({ project_id: v === 'all' ? undefined : Number(v) })}
        >
          <SelectTrigger className='h-9 w-44' aria-label='按项目筛选'>
            <SelectValue placeholder='项目' />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>项目：全部</SelectItem>
            {(projects.data?.list ?? []).map((p) => (
              <SelectItem key={p.id} value={String(p.id)}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
                <TableHead>项目</TableHead>
                <TableHead>密钥</TableHead>
                <TableHead>允许来源</TableHead>
                <TableHead className='text-right'>每日上限</TableHead>
                <TableHead className='text-right'>已收</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={COLUMNS} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={COLUMNS} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '这个项目还没有密钥' : '还没有密钥，点右上角「生成密钥」给第一个合作站点开通'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((k) => (
                  <TableRow key={k.id}>
                    <TableCell className='font-medium'>{k.name}</TableCell>
                    <TableCell className='text-muted-foreground'>{k.project_name}</TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>
                      <code>fbk_…{k.key_hint}</code>
                    </TableCell>
                    <TableCell className='max-w-56'>
                      {k.allowed_origins.length === 0 ? (
                        <span className='text-muted-foreground'>不限</span>
                      ) : (
                        <span className='truncate' title={k.allowed_origins.join('\n')}>
                          {k.allowed_origins[0]}
                          {k.allowed_origins.length > 1 ? ` 等 ${k.allowed_origins.length} 条` : ''}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>{k.daily_limit}</TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {k.used_count}
                      {k.last_used_at && (
                        <span className='block text-xs text-muted-foreground'>{formatDateTime(k.last_used_at)}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {k.revoked_at ? (
                        <Badge variant='destructive'>已作废</Badge>
                      ) : (
                        <Badge variant={k.status === 'active' ? 'default' : 'secondary'}>
                          {API_KEY_STATUS_LABELS[k.status]}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <WithReason reason={k.revoked_at ? '已作废，不能再修改' : null}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!!k.revoked_at}
                            aria-label={`编辑 ${k.name}`}
                            onClick={() => setEditing(k)}
                          >
                            <Pencil className='size-4' />
                          </Button>
                        </WithReason>
                        <WithReason reason={editable ? (k.revoked_at ? '已作废' : null) : '当前角色只能查看'}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable || !!k.revoked_at}
                            aria-label={`作废 ${k.name}`}
                            onClick={() => setRevoking(k)}
                          >
                            <Ban className='size-4' />
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
      {(creating || editing) && (
        <KeyDialog
          apiKey={editing}
          editable={editable}
          projects={projects.data?.list ?? []}
          defaultProjectID={search.project_id}
          onCreated={(raw, key) => setFresh({ raw, key })}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
        />
      )}
      {fresh && <RawKeyDialog raw={fresh.raw} apiKey={fresh.key} onClose={() => setFresh(null)} />}
      {revoking && <RevokeDialog apiKey={revoking} onClose={() => setRevoking(null)} />}
    </div>
  )
}

const schema = z.object({
  project_id: z.coerce.number().int().positive('请选择收集到哪个项目'),
  name: z.string().trim().min(1, '请填写名称').max(60, '最多 60 个字'),
  default_visibility: z.enum(['public', 'members', 'private']),
  daily_limit: z.coerce.number().int().min(1, '至少 1 条').max(100000, '最多 100000 条'),
  status: z.enum(['active', 'inactive']),
  origins: z
    .string()
    .trim()
    .refine(
      (v) =>
        splitOrigins(v).every((o) => /^https?:\/\/[^/\s]+$/.test(o)),
      '每行写一条，形如 https://example.com（不要带路径）',
    )
    .refine((v) => splitOrigins(v).length <= 20, '最多 20 条'),
})

type FormValues = z.infer<typeof schema>

function splitOrigins(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
}

function KeyDialog({
  apiKey,
  editable,
  projects,
  defaultProjectID,
  onCreated,
  onClose,
}: {
  apiKey: FbApiKey | null
  editable: boolean
  projects: { id: number; name: string }[]
  defaultProjectID?: number
  onCreated: (raw: string, key: FbApiKey) => void
  onClose: () => void
}) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof schema>, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      project_id: apiKey?.project_id ?? defaultProjectID ?? projects[0]?.id ?? 0,
      name: apiKey?.name ?? '',
      default_visibility: apiKey?.default_visibility ?? 'members',
      daily_limit: apiKey?.daily_limit ?? 500,
      status: apiKey?.status ?? 'active',
      origins: (apiKey?.allowed_origins ?? []).join('\n'),
    },
  })
  const mutation = useMutation({
    mutationFn: (v: FormValues) => {
      const body = {
        project_id: v.project_id,
        name: v.name,
        default_visibility: v.default_visibility,
        allowed_origins: splitOrigins(v.origins),
        daily_limit: v.daily_limit,
        status: v.status,
      }
      return apiKey
        ? feedbackApi.updateApiKey(apiKey.id, body).then((k) => ({ key: k, raw_key: '' }))
        : feedbackApi.createApiKey(body)
    },
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: feedbackKeys.apiKeys })
      onClose()
      if (d.raw_key) {
        onCreated(d.raw_key, d.key)
      } else {
        toast.success(`已保存「${d.key.name}」`)
      }
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{apiKey ? `编辑「${apiKey.name}」` : '生成开放接口密钥'}</DialogTitle>
          <DialogDescription>
            {apiKey
              ? '不能换项目：换了会让这个密钥已经收到的反馈归属错乱。需要换项目请作废后重建。'
              : '密钥明文只会显示一次，生成后立刻复制给接入方。库里只保存哈希，无法再次查看。'}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='project_id'
                label='收集到哪个项目'
                required
                placeholder='请选择项目'
                items={projects.map((p) => ({ label: p.name, value: String(p.id) }))}
                disabled={!editable || !!apiKey}
              />
              <TextField
                control={form.control}
                name='name'
                label='名称'
                required
                placeholder='请输入便于识别的名称，如 合作站点 A'
                disabled={!editable}
              />
            </div>
            <TextField
              control={form.control}
              name='origins'
              label='允许的来源'
              multiline
              rows={4}
              placeholder={'每行一条，如\nhttps://partner.example.com\nhttps://shop.example.com'}
              description='留空表示不限来源。填了之后：只接受这些站点的页面提交，不带来源信息的请求（例如服务端直接调用）会被拒绝。'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-3'>
              <SelectField
                control={form.control}
                name='default_visibility'
                label='收到后的可见性'
                placeholder='请选择可见性'
                items={optionsOf(VISIBILITY_LABELS)}
                disabled={!editable}
              />
              <TextField
                control={form.control}
                name='daily_limit'
                label='每日上限'
                inputMode='numeric'
                placeholder='1–100000'
                disabled={!editable}
              />
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={optionsOf(API_KEY_STATUS_LABELS)}
                disabled={!editable}
              />
            </div>
            <p className='text-sm text-muted-foreground'>
              除了每日上限，还有固定的每分钟 60 条突发限制，超了会让接入方稍后重试。
            </p>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <WithReason reason={editable ? null : '当前角色只能查看'}>
                <Button type='submit' disabled={!editable || mutation.isPending}>
                  {mutation.isPending ? '保存中…' : apiKey ? '保存' : '生成'}
                </Button>
              </WithReason>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function RawKeyDialog({ raw, apiKey, onClose }: { raw: string; apiKey: FbApiKey; onClose: () => void }) {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(raw)
      toast.success('密钥已复制')
    } catch {
      toast.error('复制失败，请手动选中后复制')
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>「{apiKey.name}」的密钥已生成</DialogTitle>
          <DialogDescription>
            这串明文只显示这一次，关闭后无法再查看。接入方把它放在请求头 X-Api-Key 里，提交到
            /open-api/feedback。
          </DialogDescription>
        </DialogHeader>
        <div className='flex items-center gap-2 rounded-md border bg-muted px-3 py-3'>
          <code className='flex-1 break-all text-sm font-semibold'>{raw}</code>
          <Button type='button' variant='secondary' size='icon' aria-label='复制密钥' onClick={() => void copy()}>
            <Copy className='size-4' />
          </Button>
        </div>
        <p className='text-sm text-muted-foreground'>
          密钥会出现在接入方的页面代码里，等同于公开的提交配额。它只能往「{apiKey.project_name}」提交反馈，读不到任何数据；
          被滥用时在这里作废再重新生成一个。
        </p>
        <DialogFooter>
          <Button type='button' onClick={onClose}>
            我已保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RevokeDialog({ apiKey, onClose }: { apiKey: FbApiKey; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.revokeApiKey(apiKey.id),
    onSuccess: () => {
      toast.success('密钥已作废')
      void qc.invalidateQueries({ queryKey: feedbackKeys.apiKeys })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`作废密钥「${apiKey.name}」`}
      desc='作废后这个密钥立即不能再提交，接入方的页面会收到报错。已经收到的反馈都保留。如果只是想临时停收，把状态改为「停用」。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '作废'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
