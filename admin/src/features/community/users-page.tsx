import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
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
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  type AdminUser,
  ATTEST_LEVEL_LABELS,
  type Guest,
  GUEST_PLATFORM_LABELS,
  PROFILE_REVIEW_LABELS,
  type ProfileReviewStatus,
  USER_STATUS_HINTS,
  USER_STATUS_LABELS,
  USER_STATUSES,
  type UserStatus,
  guestListQuery,
  userApi,
  userDetailQuery,
  userKeys,
  userListQuery,
} from './users-api'

const STATUS_VARIANT: Record<UserStatus, 'default' | 'secondary' | 'destructive'> = {
  normal: 'default',
  muted: 'secondary',
  banned: 'destructive',
}

const KINDS = [
  { value: 'user', label: '注册用户' },
  { value: 'guest', label: '游客' },
] as const

/**
 * 用户（docs/06 第 26 节）：搜用户名 / 昵称，按类型（注册 / 游客）与账号状态筛选，改账号状态。
 *
 * 封禁会立即下线该账号的全部登录设备（docs/11 S2-7）。游客只读 —— 游客设备没有账号状态，
 * 要限制游客参与互动请用「站点设置」里的游客互动开关。不提供重设用户密码（docs/adr/0001）。
 */
export function UsersPage() {
  const can = useCan()
  const visible = can('users.view')
  const search = useSearch({ from: '/_authenticated/users' })
  const navigate = useNavigate({ from: '/users' })
  const kind = search.kind ?? 'user'
  const page = search.page ?? 1
  const pageSize = search.page_size ?? DEFAULT_PAGE_SIZE

  const users = useQuery({
    ...userListQuery({ q: search.q, status: search.status, page, page_size: pageSize }),
    enabled: visible && kind === 'user',
  })
  const guests = useQuery({
    ...guestListQuery({ q: search.q, page, page_size: pageSize }),
    enabled: visible && kind === 'guest',
  })
  const list = kind === 'user' ? users : guests
  const [editing, setEditing] = useState<AdminUser | null>(null)
  const [detailID, setDetailID] = useState<number | null>(null)

  if (!visible) return <NoPermission title='用户' />
  const editable = can('users.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status || search.kind)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='用户'
        description='注册用户与游客设备。改账号状态即时生效：封禁会把该账号的全部登录设备踢下线，禁言只是发不出评论与弹幕。按产品约定不提供重设用户密码，忘记密码的用户只能重新注册。'
        readonly={!editable}
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder={kind === 'user' ? '请输入用户名或昵称' : '请输入设备编号或并入的用户名'}
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <FilterSelect
          label='类型'
          value={search.kind}
          options={KINDS.map((k) => ({ value: k.value, label: k.label }))}
          onChange={(v) =>
            void navigate({
              // 切类型时顺手清掉状态筛选：游客没有账号状态，带着它请求会被后端拒绝
              search: (prev) => withFilter(prev, { kind: v as 'user' | 'guest' | undefined, status: undefined }),
            })
          }
        />
        {kind === 'user' && (
          <FilterSelect
            label='状态'
            value={search.status}
            options={USER_STATUSES.map((s) => ({ value: s, label: USER_STATUS_LABELS[s] }))}
            onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as UserStatus | undefined }) })}
          />
        )}
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : kind === 'user' ? (
        <UserTable
          rows={users.data?.list}
          pending={users.isPending}
          filtered={filtered}
          editable={editable}
          readonlyReason={readonlyReason}
          onEdit={setEditing}
          onDetail={setDetailID}
        />
      ) : (
        <GuestTable rows={guests.data?.list} pending={guests.isPending} filtered={filtered} />
      )}

      {list.data && (
        <Pager
          page={list.data.page}
          pageSize={list.data.page_size}
          total={list.data.total}
          onChange={(p, size) =>
            void navigate({
              search: (prev) => ({ ...prev, page: p > 1 ? p : undefined, page_size: size === DEFAULT_PAGE_SIZE ? undefined : size }),
            })
          }
        />
      )}

      {editing && <SetStatusDialog user={editing} onClose={() => setEditing(null)} />}
      {detailID !== null && <UserDetailDialog id={detailID} onClose={() => setDetailID(null)} />}
    </div>
  )
}

function UserTable({
  rows,
  pending,
  filtered,
  editable,
  readonlyReason,
  onEdit,
  onDetail,
}: {
  rows: AdminUser[] | undefined
  pending: boolean
  filtered: boolean
  editable: boolean
  readonlyReason: string | null
  onEdit: (u: AdminUser) => void
  onDetail: (id: number) => void
}) {
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted'>
            <TableHead className='pl-4'>用户名</TableHead>
            <TableHead>昵称</TableHead>
            <TableHead>简介</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>注册时间</TableHead>
            <TableHead className='pr-4'>
              <span className='sr-only'>操作</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {pending ? (
            <TableRow>
              <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                加载中…
              </TableCell>
            </TableRow>
          ) : !rows || rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                {filtered ? '没有符合条件的用户' : '还没有注册用户'}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((u) => (
              <TableRow key={u.id}>
                <TableCell className='pl-4 font-medium'>{u.username}</TableCell>
                <TableCell>{u.nickname || <span className='text-muted-foreground'>—</span>}</TableCell>
                <TableCell className='max-w-xs'>
                  {u.bio ? (
                    <div className='truncate' title={u.bio}>
                      {u.bio}
                    </div>
                  ) : (
                    <span className='text-muted-foreground'>—</span>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant={STATUS_VARIANT[u.status]}>{USER_STATUS_LABELS[u.status]}</Badge>
                </TableCell>
                <TableCell className='text-sm whitespace-nowrap tabular-nums'>{formatDateTime(u.created_at).slice(0, 16)}</TableCell>
                <TableCell className='pr-4'>
                  <div className='flex justify-end gap-2'>
                    <Button variant='ghost' size='sm' onClick={() => onDetail(u.id)}>
                      详情
                    </Button>
                    <WithReason reason={readonlyReason}>
                      <Button variant='outline' size='sm' disabled={!editable} onClick={() => onEdit(u)}>
                        改状态
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
  )
}

function GuestTable({ rows, pending, filtered }: { rows: Guest[] | undefined; pending: boolean; filtered: boolean }) {
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted'>
            <TableHead className='pl-4'>设备编号</TableHead>
            <TableHead>平台</TableHead>
            <TableHead>来源渠道</TableHead>
            <TableHead>设备校验</TableHead>
            <TableHead>已并入账号</TableHead>
            <TableHead className='pr-4'>首次出现</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {pending ? (
            <TableRow>
              <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                加载中…
              </TableCell>
            </TableRow>
          ) : !rows || rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                {filtered ? '没有符合条件的游客设备' : '还没有游客设备'}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((g) => (
              <TableRow key={g.id}>
                <TableCell className='pl-4 font-mono text-xs'>{g.id}</TableCell>
                <TableCell>{GUEST_PLATFORM_LABELS[g.platform] ?? g.platform}</TableCell>
                <TableCell>{g.source_channel || <span className='text-muted-foreground'>—</span>}</TableCell>
                <TableCell>
                  <Badge variant='outline'>{ATTEST_LEVEL_LABELS[g.attest_level] ?? g.attest_level}</Badge>
                </TableCell>
                <TableCell>
                  {g.merged_user_id ? (
                    g.merged_username || `#${g.merged_user_id}`
                  ) : (
                    <span className='text-muted-foreground'>未并入</span>
                  )}
                </TableCell>
                <TableCell className='pr-4 text-sm whitespace-nowrap tabular-nums'>{formatDateTime(g.first_seen).slice(0, 16)}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}

function SetStatusDialog({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const qc = useQueryClient()
  const [status, setStatus] = useState<UserStatus>(user.status)
  const mutation = useMutation({
    mutationFn: () => userApi.setStatus(user.id, { status }),
    onSuccess: (saved) => {
      toast.success(
        saved.status === 'banned'
          ? `已封禁 ${saved.username}，其登录设备已全部下线`
          : `${saved.username} 的状态已改为${USER_STATUS_LABELS[saved.status]}`,
      )
      void qc.invalidateQueries({ queryKey: userKeys.all })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>改账号状态</DialogTitle>
          <DialogDescription>
            {user.username}
            {user.nickname && `（${user.nickname}）`}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <label htmlFor='user-status' className='text-sm font-medium'>
            账号状态
          </label>
          <Select value={status} onValueChange={(v) => setStatus(v as UserStatus)}>
            <SelectTrigger id='user-status' className='h-10 w-full'>
              <SelectValue placeholder='请选择账号状态' />
            </SelectTrigger>
            <SelectContent>
              {USER_STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {USER_STATUS_LABELS[s]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className='text-sm text-muted-foreground'>{USER_STATUS_HINTS[status]}</p>
        </div>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            取消
          </Button>
          <Button
            type='submit'
            variant={status === 'banned' ? 'destructive' : 'default'}
            disabled={mutation.isPending || status === user.status}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? '保存中…' : '保存'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function UserDetailDialog({ id, onClose }: { id: number; onClose: () => void }) {
  const detail = useQuery(userDetailQuery(id))
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>用户详情</DialogTitle>
          <DialogDescription>{detail.data ? `${detail.data.username}（编号 ${detail.data.id}）` : '加载中…'}</DialogDescription>
        </DialogHeader>
        {detail.isError ? (
          <LoadError onRetry={() => void detail.refetch()} />
        ) : detail.data ? (
          <div className='flex flex-col gap-4 text-sm'>
            <dl className='grid grid-cols-2 gap-3'>
              <Item label='昵称' value={detail.data.nickname || '—'} />
              <Item label='账号状态' value={USER_STATUS_LABELS[detail.data.status]} />
              <Item label='个人简介' value={detail.data.bio || '—'} />
              <Item label='界面语言' value={detail.data.locale || '未设置'} />
              <Item label='昵称审核' value={reviewLabel(detail.data.nickname_status)} />
              <Item label='简介审核' value={reviewLabel(detail.data.bio_status)} />
              <Item label='头像审核' value={reviewLabel(detail.data.avatar_status)} />
              <Item label='看剧历史' value={detail.data.history_paused ? '已暂停记录' : '正常记录'} />
              <Item label='注册时间' value={formatDateTime(detail.data.created_at)} />
              <Item label='验证时间' value={detail.data.verified_at ? formatDateTime(detail.data.verified_at) : '未验证'} />
            </dl>
            <div>
              <div className='mb-2 font-medium'>社区足迹</div>
              <dl className='grid grid-cols-3 gap-3'>
                <Item label='评论' value={detail.data.stats.comment_count} />
                <Item label='弹幕' value={detail.data.stats.danmaku_count} />
                <Item label='追剧' value={detail.data.stats.follow_count} />
                <Item label='发出的举报' value={detail.data.stats.report_sent} />
                <Item label='被举报' value={detail.data.stats.report_against} />
                <Item label='在线设备' value={detail.data.stats.session_count} />
              </dl>
            </div>
            <p className='text-xs text-muted-foreground'>
              会员状态、等级成长值、金币余额、邀请关系与风控标记要等会员支付（S3-10）与福利体系（M6）上线后接入。
            </p>
          </div>
        ) : (
          <div className='py-8 text-center text-muted-foreground'>加载中…</div>
        )}
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function reviewLabel(s: ProfileReviewStatus) {
  return PROFILE_REVIEW_LABELS[s] ?? s
}

function Item({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt className='text-xs text-muted-foreground'>{label}</dt>
      <dd className='tabular-nums'>{value}</dd>
    </div>
  )
}
