import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { type ColumnDef } from '@tanstack/react-table'
import { ListTree, Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
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
  Textarea,
} from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { accessApi, accessKeys, rolesQuery, type RoleRow } from './api'
import { LoadError, PageHeading, WithReason } from './shared'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete'; role: RoleRow }

export function RolesPage() {
  const can = useCan()
  const visible = can('roles.view')
  const roles = useQuery({ ...rolesQuery, enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='角色' />
  const editable = can('roles.edit')
  const canGrant = can('role_permissions.view')

  const deleteReason = (r: RoleRow): string | null => {
    if (!editable) return '当前角色只能查看'
    if (r.builtin) return '内置角色不能删除'
    if (r.member_count > 0) return '还有成员，先把成员换到其他角色'
    return null
  }

  const columns: ColumnDef<RoleRow, unknown>[] = [
    {
      accessorKey: 'name',
      header: '角色',
      cell: ({ row }) => (
        <div className='flex flex-col'>
          <span className='font-medium'>{row.original.name}</span>
          <code className='text-xs text-muted-foreground'>{row.original.code}</code>
        </div>
      ),
    },
    {
      accessorKey: 'description',
      header: '说明',
      cell: ({ row }) => <span className='line-clamp-2 max-w-md text-muted-foreground'>{row.original.description || '—'}</span>,
    },
    {
      accessorKey: 'member_count',
      header: '成员',
      cell: ({ row }) => <span className='tabular-nums'>{row.original.member_count}</span>,
    },
    {
      id: 'permissions',
      header: '权限点',
      cell: ({ row }) =>
        row.original.is_super ? <Badge>全部</Badge> : <span className='tabular-nums'>{row.original.permissions.length}</span>,
    },
    {
      accessorKey: 'status',
      header: '状态',
      cell: ({ row }) =>
        row.original.status === 'active' ? (
          <Badge variant='secondary'>启用</Badge>
        ) : (
          <Badge variant='destructive'>停用</Badge>
        ),
    },
    {
      accessorKey: 'builtin',
      header: '类型',
      cell: ({ row }) => (row.original.builtin ? '内置' : '自定义'),
    },
    {
      id: 'actions',
      header: () => <span className='sr-only'>操作</span>,
      cell: ({ row }) => {
        const r = row.original
        return (
          <div className='flex items-center justify-end gap-1'>
            {canGrant && !r.is_super && (
              <Button asChild variant='ghost' size='sm'>
                <Link to='/role-permissions' search={{ role: r.id }}>
                  <ListTree /> 配置权限
                </Link>
              </Button>
            )}
            <WithReason reason={editable ? null : '当前角色只能查看'}>
              <Button variant='ghost' size='icon' disabled={!editable} aria-label={`编辑 ${r.name}`} onClick={() => setDialog({ type: 'edit', role: r })}>
                <Pencil className='size-4' />
              </Button>
            </WithReason>
            <WithReason reason={deleteReason(r)}>
              <Button
                variant='ghost'
                size='icon'
                disabled={!!deleteReason(r)}
                aria-label={`删除 ${r.name}`}
                onClick={() => setDialog({ type: 'delete', role: r })}
              >
                <Trash2 className='size-4' />
              </Button>
            </WithReason>
          </div>
        )
      },
    },
  ]

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='角色'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'create' })}>
              <Plus /> 新建角色
            </Button>
          )
        }
      />
      {roles.isError ? (
        <LoadError onRetry={() => void roles.refetch()} />
      ) : (
        <DataTable data={roles.data ?? []} columns={columns} loading={roles.isPending} enableRowSelection={false} hidePagination />
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <RoleDialog role={dialog.type === 'edit' ? dialog.role : undefined} onClose={() => setDialog(null)} />
      )}
      {dialog?.type === 'delete' && <DeleteRoleDialog role={dialog.role} onClose={() => setDialog(null)} />}
    </div>
  )
}

const roleSchema = z.object({
  code: z.string().trim(),
  name: z.string().trim().min(1, '请填写名称').max(32, '名称最多 32 个字'),
  description: z.string().trim().max(200, '说明最多 200 个字'),
  status: z.enum(['active', 'inactive']),
  sort: z.number({ error: '请填写 0–9999 的整数' }).int('请填写整数').min(0, '最小为 0').max(9999, '最大为 9999'),
})

type RoleForm = z.infer<typeof roleSchema>

/** 编码只在新建时填写与校验，之后不能修改。 */
const roleSchemaFor = (creating: boolean) =>
  roleSchema.superRefine((v, ctx) => {
    if (creating && !/^[a-z][a-z0-9_]{1,31}$/.test(v.code)) {
      ctx.addIssue({ code: 'custom', path: ['code'], message: '2–32 位，小写字母开头，只能包含小写字母、数字和下划线' })
    }
  })

function RoleDialog({ role, onClose }: { role?: RoleRow; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !role
  const form = useForm<RoleForm>({
    resolver: zodResolver(roleSchemaFor(creating)),
    defaultValues: {
      code: role?.code ?? '',
      name: role?.name ?? '',
      description: role?.description ?? '',
      status: role?.status ?? 'active',
      sort: role?.sort ?? 100,
    },
  })
  const mutation = useMutation({
    mutationFn: (v: RoleForm) =>
      creating
        ? accessApi.createRole(v).then(() => undefined)
        : accessApi.updateRole(role.id, { name: v.name, description: v.description, status: v.status, sort: v.sort }),
    onSuccess: () => {
      toast.success(creating ? '已新建角色，请在「角色权限」中授予权限' : '已保存')
      void qc.invalidateQueries({ queryKey: accessKeys.roles })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建角色' : `编辑角色：${role.name}`}</DialogTitle>
          <DialogDescription>{creating ? '新角色没有任何权限。' : '角色编码创建后不能修改。'}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='code'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>编码</FormLabel>
                  <FormControl>
                    <Input autoComplete='off' disabled={!creating} placeholder='小写字母开头，如 content_editor' {...field} />
                  </FormControl>
                  <FormDescription>用于命令行建管理员与日志，创建后不能修改。</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='name'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>名称</FormLabel>
                  <FormControl>
                    <Input placeholder='请输入角色名称，如 内容编辑' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='description'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>说明</FormLabel>
                  <FormControl>
                    <Textarea rows={2} placeholder='选填，说明这个角色负责什么，最多 200 个字' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className='grid grid-cols-2 gap-4'>
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
                      disabled={role?.is_super}
                      placeholder='请选择状态'
                      items={[
                        { label: '启用', value: 'active' },
                        { label: '停用', value: 'inactive' },
                      ]}
                    />
                    {role?.is_super && <FormDescription>超级管理员角色不能停用。</FormDescription>}
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='sort'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>排序</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        inputMode='numeric'
                        placeholder='0–9999'
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

function DeleteRoleDialog({ role, onClose }: { role: RoleRow; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => accessApi.deleteRole(role.id),
    onSuccess: () => {
      toast.success(`已删除角色「${role.name}」`)
      void qc.invalidateQueries({ queryKey: accessKeys.roles })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除角色「${role.name}」`}
      desc='删除后不可恢复，该角色的授权一并删除。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
