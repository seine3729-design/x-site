import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type ColumnDef } from '@tanstack/react-table'
import { KeyRound, LogOut, MoreHorizontal, Pencil, Plus, ShieldOff } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { useAuthStore, useCan } from '@/stores/auth-store'
import { DataTable } from '@/components/common/data-table'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableColumnHeader } from '@/components/data-table'
import { NoPermission } from '@/components/module-placeholder'
import { PasswordInput } from '@/components/password-input'
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Input,
} from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import {
  accessApi,
  accessKeys,
  ADMIN_STATUS_LABELS,
  adminsQuery,
  rolesQuery,
  type AdminRow,
  type RoleRow,
} from './api'
import { LoadError, PageHeading, WithReason } from './shared'

type DialogState =
  | { type: 'create' }
  | { type: 'edit' | 'password' | 'revoke' | 'reset-mfa'; admin: AdminRow }

const usernameSchema = z
  .string()
  .trim()
  .min(3, '用户名至少 3 个字符')
  .max(32, '用户名最多 32 个字符')
  .regex(/^[A-Za-z0-9_.-]+$/, '只能包含字母、数字、下划线、点和连字符')
const passwordSchema = z.string().min(8, '密码至少 8 位').max(128, '密码最多 128 位')

export function AdminsPage() {
  const can = useCan()
  const me = useAuthStore((s) => s.user)
  const visible = can('admins.view')
  const admins = useQuery({ ...adminsQuery, enabled: visible })
  const roles = useQuery({ ...rolesQuery, enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='管理员' />
  const editable = can('admins.edit')

  /** 不能操作某个管理员的原因（与后端规则一致）；可以操作时返回 null。 */
  const lockReason = (a: AdminRow): string | null => {
    if (!editable) return '当前角色只能查看'
    if (a.id === me?.id) return '不能修改自己的账号，请让其他管理员操作'
    if (a.role.is_super && !me?.is_super) return '只有超级管理员可以操作超级管理员'
    return null
  }

  const columns: ColumnDef<AdminRow, unknown>[] = [
    {
      accessorKey: 'username',
      header: ({ column }) => <DataTableColumnHeader column={column} title='用户名' />,
      cell: ({ row }) => (
        <span className='font-medium'>
          {row.original.username}
          {row.original.id === me?.id && <span className='ms-2 text-xs text-muted-foreground'>（我）</span>}
        </span>
      ),
    },
    {
      id: 'role',
      accessorFn: (a) => String(a.role.id),
      header: '角色',
      filterFn: (row, id, value: string[]) => value.includes(row.getValue(id)),
      cell: ({ row }) => (
        <Badge variant={row.original.role.is_super ? 'default' : 'outline'}>{row.original.role.name}</Badge>
      ),
    },
    {
      accessorKey: 'status',
      header: '状态',
      filterFn: (row, id, value: string[]) => value.includes(row.getValue(id)),
      cell: ({ row }) => (
        <Badge variant={row.original.status === 'inactive' ? 'destructive' : 'secondary'}>
          {ADMIN_STATUS_LABELS[row.original.status]}
        </Badge>
      ),
    },
    {
      accessorKey: 'mfa_enabled',
      header: '二次验证',
      cell: ({ row }) => (row.original.mfa_enabled ? '已开启' : <span className='text-muted-foreground'>未开启</span>),
    },
    {
      accessorKey: 'last_login_at',
      header: ({ column }) => <DataTableColumnHeader column={column} title='最近登录' />,
      cell: ({ row }) => (
        <span className='tabular-nums'>{row.original.last_login_at ? formatDateTime(row.original.last_login_at) : '—'}</span>
      ),
    },
    {
      accessorKey: 'created_at',
      header: ({ column }) => <DataTableColumnHeader column={column} title='创建时间' />,
      cell: ({ row }) => <span className='tabular-nums'>{formatDateTime(row.original.created_at)}</span>,
    },
    {
      id: 'actions',
      header: () => <span className='sr-only'>操作</span>,
      cell: ({ row }) => {
        const a = row.original
        const reason = lockReason(a)
        return (
          <WithReason reason={reason}>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant='ghost' size='icon' disabled={!!reason} aria-label={`操作 ${a.username}`}>
                  <MoreHorizontal className='size-4' />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align='end'>
                <DropdownMenuItem onClick={() => setDialog({ type: 'edit', admin: a })}>
                  <Pencil /> 修改角色与状态
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setDialog({ type: 'password', admin: a })}>
                  <KeyRound /> 重置密码
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setDialog({ type: 'reset-mfa', admin: a })}>
                  <ShieldOff /> 重置二次验证
                </DropdownMenuItem>
                <DropdownMenuItem variant='destructive' onClick={() => setDialog({ type: 'revoke', admin: a })}>
                  <LogOut /> 强制下线
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </WithReason>
        )
      },
    },
  ]

  const roleOptions = (roles.data ?? []).map((r) => ({ label: r.name, value: String(r.id) }))

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='管理员'
        description='后台账号与所属角色。停用、改角色、重置密码与强制下线立即生效；后台不能重设前台用户的密码。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建管理员
            </Button>
          )
        }
      />
      {admins.isError ? (
        <LoadError onRetry={() => void admins.refetch()} />
      ) : (
        <DataTable
          data={admins.data ?? []}
          columns={columns}
          loading={admins.isPending}
          enableRowSelection={false}
          searchKey='username'
          searchPlaceholder='搜索用户名'
          filters={[
            { columnId: 'role', title: '角色', options: roleOptions },
            {
              columnId: 'status',
              title: '状态',
              options: Object.entries(ADMIN_STATUS_LABELS).map(([value, label]) => ({ value, label })),
            },
          ]}
          // 管理员通常只有几十人，一页全部显示
          defaultPageSize={1000}
          hidePagination
        />
      )}

      {dialog?.type === 'create' && (
        <CreateAdminDialog roles={roles.data ?? []} canGrantSuper={!!me?.is_super} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'edit' && (
        <EditAdminDialog
          admin={dialog.admin}
          roles={roles.data ?? []}
          canGrantSuper={!!me?.is_super}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'password' && <ResetPasswordDialog admin={dialog.admin} onClose={() => setDialog(null)} />}
      {dialog?.type === 'revoke' && <RevokeDialog admin={dialog.admin} onClose={() => setDialog(null)} />}
      {dialog?.type === 'reset-mfa' && <ResetMfaDialog admin={dialog.admin} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 可分配的角色：已停用的不能选；超级管理员角色只有超管能授予。 */
function assignableRoles(roles: RoleRow[], canGrantSuper: boolean, keepId?: number) {
  return roles
    .filter((r) => (r.status === 'active' && (canGrantSuper || !r.is_super)) || r.id === keepId)
    .map((r) => ({ label: r.status === 'active' ? r.name : `${r.name}（已停用）`, value: String(r.id) }))
}

function useInvalidateAdmins() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: accessKeys.admins })
    void qc.invalidateQueries({ queryKey: accessKeys.roles }) // 成员数
  }
}

const createSchema = z
  .object({ username: usernameSchema, password: passwordSchema, role_id: z.string().min(1, '请选择角色') })
  .refine((v) => v.password.toLowerCase() !== v.username.toLowerCase(), {
    path: ['password'],
    message: '密码不能与用户名相同',
  })

function CreateAdminDialog({
  roles,
  canGrantSuper,
  onClose,
}: {
  roles: RoleRow[]
  canGrantSuper: boolean
  onClose: () => void
}) {
  const invalidate = useInvalidateAdmins()
  const form = useForm<z.infer<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: { username: '', password: '', role_id: '' },
  })
  const mutation = useMutation({
    mutationFn: (v: z.infer<typeof createSchema>) =>
      accessApi.createAdmin({ username: v.username, password: v.password, role_id: Number(v.role_id) }),
    onSuccess: () => {
      toast.success('已新建管理员，首次登录后状态变为「可登录」')
      invalidate()
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>新建管理员</DialogTitle>
          <DialogDescription>把初始密码通过可靠渠道交给对方。用户名不区分大小写。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='username'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>用户名</FormLabel>
                  <FormControl>
                    <Input autoComplete='off' placeholder='3–32 位字母、数字、下划线、点或连字符' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='password'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>初始密码</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete='new-password' placeholder='请输入初始密码' {...field} />
                  </FormControl>
                  <FormDescription>8–128 位，不能与用户名相同。</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='role_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>角色</FormLabel>
                  <SelectDropdown
                    isControlled
                    defaultValue={field.value}
                    onValueChange={field.onChange}
                    placeholder='请选择角色'
                    items={assignableRoles(roles, canGrantSuper)}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '新建'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

const editSchema = z.object({ role_id: z.string().min(1, '请选择角色'), status: z.enum(['active', 'inactive']) })

function EditAdminDialog({
  admin,
  roles,
  canGrantSuper,
  onClose,
}: {
  admin: AdminRow
  roles: RoleRow[]
  canGrantSuper: boolean
  onClose: () => void
}) {
  const invalidate = useInvalidateAdmins()
  const form = useForm<z.infer<typeof editSchema>>({
    resolver: zodResolver(editSchema),
    defaultValues: { role_id: String(admin.role.id), status: admin.status === 'inactive' ? 'inactive' : 'active' },
  })
  const mutation = useMutation({
    mutationFn: (v: z.infer<typeof editSchema>) =>
      accessApi.updateAdmin(admin.id, { role_id: Number(v.role_id), status: v.status }),
    onSuccess: () => {
      toast.success('已保存，立即生效')
      invalidate()
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>修改 {admin.username}</DialogTitle>
          <DialogDescription>停用后该管理员立即被踢下线且不能登录；改角色后按新角色的权限判定。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='role_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>角色</FormLabel>
                  <SelectDropdown
                    isControlled
                    defaultValue={field.value}
                    onValueChange={field.onChange}
                    placeholder='请选择角色'
                    items={assignableRoles(roles, canGrantSuper, admin.role.id)}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
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
                      { label: admin.status === 'unused' ? '可登录（尚未登录过）' : '可登录', value: 'active' },
                      { label: '停用', value: 'inactive' },
                    ]}
                  />
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

const passwordFormSchema = z
  .object({ password: passwordSchema, confirm: z.string() })
  .refine((v) => v.password === v.confirm, { path: ['confirm'], message: '两次输入的密码不一致' })

function ResetPasswordDialog({ admin, onClose }: { admin: AdminRow; onClose: () => void }) {
  const invalidate = useInvalidateAdmins()
  const form = useForm<z.infer<typeof passwordFormSchema>>({
    resolver: zodResolver(passwordFormSchema),
    defaultValues: { password: '', confirm: '' },
  })
  const mutation = useMutation({
    mutationFn: (v: z.infer<typeof passwordFormSchema>) => accessApi.resetPassword(admin.id, v.password),
    onSuccess: () => {
      toast.success(`已重置 ${admin.username} 的密码，其全部会话已失效`)
      invalidate()
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>重置 {admin.username} 的密码</DialogTitle>
          <DialogDescription>重置后该管理员的全部会话立即失效，需要用新密码重新登录。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='password'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>新密码</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete='new-password' placeholder='8–128 位，不能与用户名相同' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='confirm'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>确认新密码</FormLabel>
                  <FormControl>
                    <PasswordInput autoComplete='new-password' placeholder='请再次输入新密码' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' variant='destructive' disabled={mutation.isPending}>
                {mutation.isPending ? '重置中…' : '重置密码'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function RevokeDialog({ admin, onClose }: { admin: AdminRow; onClose: () => void }) {
  const invalidate = useInvalidateAdmins()
  const mutation = useMutation({
    mutationFn: () => accessApi.revokeSessions(admin.id),
    onSuccess: () => {
      toast.success(`${admin.username} 已被强制下线`)
      invalidate()
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`强制下线 ${admin.username}`}
      desc='该管理员在所有设备上的会话立即失效，需要重新登录。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '强制下线'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function ResetMfaDialog({ admin, onClose }: { admin: AdminRow; onClose: () => void }) {
  const invalidate = useInvalidateAdmins()
  const mutation = useMutation({
    mutationFn: () => accessApi.resetMfa(admin.id),
    onSuccess: () => {
      toast.success(`已重置 ${admin.username} 的二次验证`)
      invalidate()
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`重置 ${admin.username} 的二次验证`}
      desc='清除该管理员已绑定的认证器与全部 Passkey，并强制其下线。用于其丢失认证设备时恢复登录——恢复后对方需重新开启二次验证。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '重置'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
