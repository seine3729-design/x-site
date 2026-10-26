import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Ban, Copy, Plus } from 'lucide-react'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
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
} from '@/components/ui'
import { Badge } from '@/components/ui/badge'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { ResetButton } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  MEMBER_ROLE_LABELS,
  allProjectsQuery,
  feedbackApi,
  feedbackKeys,
  inviteListQuery,
  optionsOf,
  type FbInvite,
  type FbMemberRole,
  type InviteListParams,
} from './api'

const COLUMNS = 8

type StateVariant = 'default' | 'secondary' | 'outline' | 'destructive'

/** 邀请码当前可用状态。nowMs 由调用方在挂载时取一次。 */
function inviteState(i: FbInvite, nowMs: number): { label: string; variant: StateVariant } {
  if (i.revoked_at) return { label: '已作废', variant: 'destructive' }
  if (i.expires_at && Date.parse(i.expires_at) < nowMs) return { label: '已过期', variant: 'outline' }
  if (i.used_count >= i.max_uses) return { label: '已用完', variant: 'outline' }
  return { label: '可用', variant: 'default' }
}


/** 邀请码（feedback/docs/07 第 8.3 节）。注册必须用邀请码，明文只在生成时显示一次。 */
export function InvitesPage() {
  const can = useCan()
  const visible = can('fb_invites.view')
  const search = useSearch({ from: '/_authenticated/feedback/invites' })
  const navigate = useNavigate({ from: '/feedback/invites' })
  const params: InviteListParams = {
    project_id: search.project_id,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...inviteListQuery(params), enabled: visible })
  const projects = useQuery({ ...allProjectsQuery, enabled: visible })
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<FbInvite | null>(null)
  const [fresh, setFresh] = useState<{ code: string; invite: FbInvite } | null>(null)
  // 判断「已过期」需要当前时间：挂载时取一次，不在渲染期调用 Date
  const [nowMs] = useState(() => Date.now())

  if (!visible) return <NoPermission title='邀请码' />
  const editable = can('fb_invites.edit')
  const filtered = !!search.project_id
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='邀请码'
        description='社区注册必须凭邀请码。带项目的邀请码会在注册成功后自动把用户加入该项目。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 生成邀请码
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
                <TableHead className='pl-4'>前 4 位</TableHead>
                <TableHead>项目</TableHead>
                <TableHead>加入角色</TableHead>
                <TableHead>使用情况</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>过期时间</TableHead>
                <TableHead>备注</TableHead>
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
                    {filtered ? '该项目还没有邀请码' : '还没有邀请码，点右上角「生成邀请码」开始'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((i) => {
                  const st = inviteState(i, nowMs)
                  return (
                    <TableRow key={i.id}>
                      <TableCell className='pl-4 font-mono text-xs'>{i.code_hint}••••</TableCell>
                      <TableCell className='text-muted-foreground'>{i.project_name || '不加入项目'}</TableCell>
                      <TableCell className='text-muted-foreground'>{MEMBER_ROLE_LABELS[i.role]}</TableCell>
                      <TableCell className='tabular-nums'>
                        {i.used_count} / {i.max_uses}
                      </TableCell>
                      <TableCell>
                        <Badge variant={st.variant}>{st.label}</Badge>
                      </TableCell>
                      <TableCell className='whitespace-nowrap tabular-nums'>
                        {i.expires_at ? formatDateTime(i.expires_at) : '不过期'}
                      </TableCell>
                      <TableCell className='max-w-40 truncate text-muted-foreground'>{i.note || '—'}</TableCell>
                      <TableCell className='pr-4 text-right'>
                        <WithReason reason={editable ? (i.revoked_at ? '已作废' : null) : '当前角色只能查看'}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable || !!i.revoked_at}
                            aria-label={`作废邀请码 ${i.code_hint}`}
                            onClick={() => setRevoking(i)}
                          >
                            <Ban className='size-4' />
                          </Button>
                        </WithReason>
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
      {creating && (
        <CreateInviteDialog
          projects={projects.data?.list ?? []}
          onClose={() => setCreating(false)}
          onCreated={(code, invite) => setFresh({ code, invite })}
        />
      )}
      {fresh && <CodeDialog code={fresh.code} invite={fresh.invite} onClose={() => setFresh(null)} />}
      {revoking && <RevokeDialog invite={revoking} onClose={() => setRevoking(null)} />}
    </div>
  )
}

function CreateInviteDialog({
  projects,
  onClose,
  onCreated,
}: {
  projects: { id: number; name: string }[]
  onClose: () => void
  onCreated: (code: string, invite: FbInvite) => void
}) {
  const qc = useQueryClient()
  const [projectID, setProjectID] = useState('none')
  const [role, setRole] = useState<FbMemberRole>('member')
  const [maxUses, setMaxUses] = useState('1')
  const [note, setNote] = useState('')
  const [expiresAt, setExpiresAt] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      feedbackApi.createInvite({
        project_id: projectID === 'none' ? undefined : Number(projectID),
        role,
        max_uses: Number(maxUses) || 1,
        note: note.trim(),
        expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
      }),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: feedbackKeys.invites })
      onClose()
      onCreated(d.code, d.invite)
    },
  })

  const uses = Number(maxUses)
  const invalid = !maxUses || Number.isNaN(uses) || uses < 1 || uses > 10000

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>生成邀请码</DialogTitle>
          <DialogDescription>邀请码明文只会显示一次，请生成后立刻复制保存。库里只保存哈希，无法再次查看。</DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='inv-project'>加入项目</Label>
            <Select value={projectID} onValueChange={setProjectID}>
              <SelectTrigger id='inv-project' className='h-9 w-full'>
                <SelectValue placeholder='请选择项目' />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>不加入项目（只注册账号）</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='inv-role'>加入后的角色</Label>
            <Select value={role} onValueChange={(v) => setRole(v as FbMemberRole)}>
              <SelectTrigger id='inv-role' className='h-9 w-full'>
                <SelectValue placeholder='请选择角色' />
              </SelectTrigger>
              <SelectContent>
                {optionsOf(MEMBER_ROLE_LABELS)
                  .filter((o) => o.value !== 'owner')
                  .map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='inv-uses'>可用次数</Label>
            <Input
              id='inv-uses'
              className='h-9'
              inputMode='numeric'
              placeholder='1 到 10000，默认 1 次'
              value={maxUses}
              onChange={(e) => setMaxUses(e.target.value.replace(/\D/g, ''))}
            />
            {invalid && <p className='text-sm text-destructive'>可用次数需在 1 到 10000 之间</p>}
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='inv-expires'>过期时间</Label>
            <Input
              id='inv-expires'
              className='h-9'
              type='datetime-local'
              placeholder='留空表示不过期'
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
          </div>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='inv-note'>备注</Label>
            <Input
              id='inv-note'
              className='h-9'
              maxLength={100}
              placeholder='请输入备注，如 微博渠道第一批'
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button type='button' disabled={invalid || mutation.isPending} onClick={() => mutation.mutate()}>
            {mutation.isPending ? '生成中…' : '生成'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CodeDialog({ code, invite, onClose }: { code: string; invite: FbInvite; onClose: () => void }) {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      toast.success('邀请码已复制')
    } catch {
      toast.error('复制失败，请手动选中后复制')
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>邀请码已生成</DialogTitle>
          <DialogDescription>
            这串明文只显示这一次，关闭后无法再查看。可用 {invite.max_uses} 次
            {invite.project_name ? `，注册后自动加入「${invite.project_name}」` : ''}。
          </DialogDescription>
        </DialogHeader>
        <div className='flex items-center gap-2 rounded-md border bg-muted px-3 py-3'>
          <code className='flex-1 text-center text-lg font-semibold tracking-widest tabular-nums'>{code}</code>
          <Button type='button' variant='secondary' size='icon' aria-label='复制邀请码' onClick={() => void copy()}>
            <Copy className='size-4' />
          </Button>
        </div>
        <DialogFooter>
          <Button type='button' onClick={onClose}>
            我已保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RevokeDialog({ invite, onClose }: { invite: FbInvite; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.revokeInvite(invite.id),
    onSuccess: () => {
      toast.success('邀请码已作废')
      void qc.invalidateQueries({ queryKey: feedbackKeys.invites })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`作废邀请码 ${invite.code_hint}••••`}
      desc='作废后这串邀请码立即不能再用于注册。已经用它注册的账号不受影响。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '作废'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
