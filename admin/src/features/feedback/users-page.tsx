import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
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
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  USER_STATUS_LABELS,
  feedbackApi,
  feedbackKeys,
  optionsOf,
  userListQuery,
  type FbUser,
  type FbUserStatus,
  type UserListParams,
} from './api'
import { UserStatusBadge } from './components/badges'

const COLUMNS = 7

/** 社区成员（feedback/docs/07 第 10.3 节）。后台只能停用或恢复账号，不能重设用户密码。 */
export function UsersPage() {
  const can = useCan()
  const visible = can('fb_users.view')
  const search = useSearch({ from: '/_authenticated/feedback/users' })
  const navigate = useNavigate({ from: '/feedback/users' })
  const params: UserListParams = {
    q: search.q,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...userListQuery(params), enabled: visible })
  const [target, setTarget] = useState<{ user: FbUser; next: FbUserStatus } | null>(null)

  if (!visible) return <NoPermission title='社区成员' />
  const editable = can('fb_users.edit')
  const filtered = !!(search.q || search.status)
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='社区成员'
        description='账号只有用户名与密码，不收集邮箱和手机号。后台不能重设用户密码，用户靠注册时发放的恢复码自助找回。'
        readonly={!editable}
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜用户名或昵称' onSubmit={(q) => setFilter({ q })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={optionsOf(USER_STATUS_LABELS)}
          onChange={(v) => setFilter({ status: v as FbUserStatus | undefined })}
          className='w-36'
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
                <TableHead className='pl-4'>用户</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className='text-right'>贡献值</TableHead>
                <TableHead>公开等级</TableHead>
                <TableHead>简介</TableHead>
                <TableHead>注册时间</TableHead>
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
                    {filtered ? '没有符合条件的成员' : '还没有社区成员。在「邀请码」页生成邀请码后用户才能注册'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className='pl-4'>
                      <div className='flex flex-col'>
                        <span className='font-medium'>{u.nickname || u.handle}</span>
                        <span className='text-xs text-muted-foreground'>@{u.handle}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <UserStatusBadge status={u.status} />
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>{u.points}</TableCell>
                    <TableCell className='text-muted-foreground'>{u.show_level ? '已公开' : '已隐藏'}</TableCell>
                    <TableCell className='max-w-56 truncate text-muted-foreground'>{u.bio || '—'}</TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>{formatDateTime(u.created_at)}</TableCell>
                    <TableCell className='pr-4 text-right'>
                      <WithReason reason={readonlyReason}>
                        {u.status === 'active' ? (
                          <Button
                            variant='ghost'
                            size='sm'
                            disabled={!editable}
                            onClick={() => setTarget({ user: u, next: 'suspended' })}
                          >
                            封禁
                          </Button>
                        ) : (
                          <Button
                            variant='ghost'
                            size='sm'
                            disabled={!editable}
                            onClick={() => setTarget({ user: u, next: 'active' })}
                          >
                            恢复
                          </Button>
                        )}
                      </WithReason>
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
      {target && <StatusDialog user={target.user} next={target.next} onClose={() => setTarget(null)} />}
    </div>
  )
}

function StatusDialog({ user, next, onClose }: { user: FbUser; next: FbUserStatus; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.setUserStatus(user.id, next),
    onSuccess: () => {
      toast.success(next === 'active' ? `已恢复 @${user.handle}` : `已封禁 @${user.handle}`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.users })
      onClose()
    },
  })
  const banning = next === 'suspended'
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={banning ? `封禁 @${user.handle}` : `恢复 @${user.handle}`}
      desc={
        banning
          ? '封禁后该用户的登录状态立即失效，不能再提交反馈或投票。已提交的内容与贡献记录保留。'
          : '恢复后该用户可以重新登录并提交反馈。需要用原密码或恢复码重新登录。'
      }
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : banning ? '封禁' : '恢复'}
      destructive={banning}
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
