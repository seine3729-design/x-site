import { useId, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useBlocker, useSearch } from '@tanstack/react-router'
import { ChevronRight, ChevronsDownUp, ChevronsUpDown, Save, Search, Undo2 } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useAuthStore, useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { TriStateCheckbox } from '@/components/tri-state-checkbox'
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, Checkbox, Input, Skeleton } from '@/components/ui'
import { accessApi, accessKeys, permissionsQuery, rolesQuery, type PermissionNode, type RoleRow } from './api'
import { countChanges, grantLockReason, sameSet, toggleGrant, viewCodeOf, type GrantActor } from './grants'
import { checkState, codesOf, countGranted, filterTree, groupCodes, toggleNode, viewLockReason } from './permission-tree'
import { LoadError, PageHeading } from './shared'

/** 角色权限（docs/06 第 33b 节）：左侧选角色，右侧按菜单结构（一级 → 二级 → 权限点）勾选授权。 */
export function RolePermissionsPage() {
  const can = useCan()
  const me = useAuthStore((s) => s.user)
  const { role: roleParam } = useSearch({ from: '/_authenticated/role-permissions' })
  const visible = can('role_permissions.view')
  const tree = useQuery({ ...permissionsQuery, enabled: visible })
  const roles = useQuery({ ...rolesQuery, enabled: visible })
  // 展开状态在切换角色时保留
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  if (!visible) return <NoPermission title='角色权限' />

  const editable = can('role_permissions.edit')
  const list = roles.data ?? []
  const selected = list.find((r) => r.id === roleParam) ?? list.find((r) => !r.is_super) ?? list[0]
  const myRole = list.find((r) => r.code === me?.role)
  const actor: GrantActor = { roleId: myRole?.id ?? null, isSuper: !!me?.is_super, can }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='角色权限'
        readonly={!editable}
      />
      {tree.isError || roles.isError ? (
        <LoadError
          onRetry={() => {
            void tree.refetch()
            void roles.refetch()
          }}
        />
      ) : tree.isPending || roles.isPending || !selected ? (
        <Skeleton className='h-96 w-full' />
      ) : (
        <div className='grid items-start gap-4 lg:grid-cols-[15rem_minmax(0,1fr)]'>
          <RoleList roles={list} selectedId={selected.id} myRoleId={actor.isSuper ? null : actor.roleId} />
          <RoleTreeEditor
            key={selected.id}
            role={selected}
            tree={tree.data}
            actor={actor}
            expanded={expanded}
            onExpandedChange={setExpanded}
            onlySuper={list.every((r) => r.is_super)}
          />
        </div>
      )}
    </div>
  )
}

function RoleList({ roles, selectedId, myRoleId }: { roles: RoleRow[]; selectedId: number; myRoleId: number | null }) {
  return (
    <Card className='gap-0 py-0'>
      <CardHeader className='border-b px-4 py-3 [.border-b]:pb-3'>
        <CardTitle className='text-sm'>角色</CardTitle>
      </CardHeader>
      <CardContent className='p-2'>
        <nav aria-label='选择角色'>
          <ul className='flex flex-col gap-1'>
            {roles.map((r) => {
              const active = r.id === selectedId
              return (
                <li key={r.id}>
                  <Link
                    to='/role-permissions'
                    search={{ role: r.id }}
                    aria-current={active ? 'page' : undefined}
                    className={cn(
                      'flex flex-col gap-0.5 rounded-md px-3 py-2 text-sm transition-colors hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                      active && 'bg-muted font-medium'
                    )}
                  >
                    <span className='flex items-center gap-2'>
                      <span className='truncate'>{r.name}</span>
                      {r.is_super && <Badge className='text-[11px]'>全部权限</Badge>}
                      {r.status === 'inactive' && (
                        <Badge variant='secondary' className='text-[11px]'>
                          已停用
                        </Badge>
                      )}
                    </span>
                    <span className='text-xs font-normal text-muted-foreground'>
                      <code>{r.code}</code> · {r.member_count} 人{r.id === myRoleId && ' · 我的角色'}
                    </span>
                  </Link>
                </li>
              )
            })}
          </ul>
        </nav>
      </CardContent>
    </Card>
  )
}

function RoleTreeEditor({
  role,
  tree,
  actor,
  expanded,
  onExpandedChange,
  onlySuper,
}: {
  role: RoleRow
  tree: PermissionNode[]
  actor: GrantActor
  expanded: Set<string>
  onExpandedChange: (next: Set<string>) => void
  onlySuper: boolean
}) {
  const qc = useQueryClient()
  const allCodes = tree.flatMap(codesOf)
  const original = new Set(role.is_super ? allCodes : role.permissions)
  const [draft, setDraft] = useState<Set<string>>(original)
  const [query, setQuery] = useState('')
  const changes = role.is_super ? 0 : countChanges(original, draft)

  useBlocker({
    shouldBlockFn: () => changes > 0 && !window.confirm(`「${role.name}」的权限修改尚未保存，确定离开吗？`),
    enableBeforeUnload: () => changes > 0,
  })

  const save = useMutation({
    mutationFn: () => accessApi.setRolePermissions(role.id, [...draft].toSorted()),
    onSuccess: (res) => {
      setDraft(new Set(res.permissions))
      toast.success(`已保存「${role.name}」的权限，该角色成员的下一次请求即按新权限判定`)
      void qc.invalidateQueries({ queryKey: accessKeys.roles })
      void qc.invalidateQueries({ queryKey: ['auth'] })
    },
  })

  const lockOf = (code: string) => grantLockReason(actor, role, code)
  const locked = (code: string) => lockOf(code) !== null
  const readonly = allCodes.every(locked)
  const busy = save.isPending

  const update = (next: Set<string>) => setDraft(sameSet(next, original) ? original : next)
  const visibleTree = filterTree(tree, query)
  const isOpen = (code: string) => query.trim() !== '' || expanded.has(code)
  const toggleOpen = (code: string) => {
    const next = new Set(expanded)
    if (next.has(code)) next.delete(code)
    else next.add(code)
    onExpandedChange(next)
  }

  const lockNotice = role.is_super
    ? '超级管理员拥有全部权限点（含以后新增的），不能修改。'
    : !actor.can('role_permissions.edit')
      ? '当前角色只能查看角色权限。'
      : !actor.isSuper && actor.roleId === role.id
        ? '不能修改自己所属角色的权限，请让超级管理员操作。'
        : !actor.isSuper
          ? '你只能授予或收回自己拥有的权限点，其余勾选框不可用。'
          : null

  return (
    <Card className='gap-0 py-0'>
      <CardHeader className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3 [.border-b]:pb-3'>
        <div className='flex flex-col gap-0.5'>
          <CardTitle className='flex items-center gap-2 text-base'>
            {role.name}
            {role.status === 'inactive' && <Badge variant='secondary'>已停用</Badge>}
          </CardTitle>
          <p className='text-sm text-muted-foreground' aria-live='polite'>
            已授予 <span className='tabular-nums'>{role.is_super ? allCodes.length : draft.size}</span> /{' '}
            <span className='tabular-nums'>{allCodes.length}</span> 个权限点
            {changes > 0 && <span className='text-primary'> · 已修改 {changes} 处，尚未保存</span>}
          </p>
        </div>
        {!readonly && (
          <div className='flex flex-wrap items-center gap-2'>
            <Button variant='outline' disabled={changes === 0 || busy} onClick={() => setDraft(original)}>
              <Undo2 /> 放弃修改
            </Button>
            <Button disabled={changes === 0 || busy} onClick={() => save.mutate()}>
              <Save /> {busy ? '保存中…' : '保存'}
            </Button>
          </div>
        )}
      </CardHeader>
      <CardContent className='flex flex-col gap-3 p-4'>
        {onlySuper && (
          <p className='rounded-md border border-dashed p-3 text-sm text-muted-foreground'>
            还没有自定义角色。先到
            <Link to='/roles' className='mx-1 font-medium text-primary underline-offset-4 hover:underline'>
              角色
            </Link>
            页新建角色，再回来授予权限。
          </p>
        )}
        {lockNotice && <p className='text-sm text-muted-foreground'>{lockNotice}</p>}
        <div className='flex flex-wrap items-center gap-2'>
          <div className='relative w-full sm:w-72'>
            <Search className='pointer-events-none absolute inset-s-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground' aria-hidden />
            <Input
              type='search'
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder='搜索菜单、权限点名称或编码'
              aria-label='搜索菜单或权限点'
              className='ps-9'
            />
          </div>
          <Button variant='outline' onClick={() => onExpandedChange(new Set(groupCodes(tree)))}>
            <ChevronsUpDown /> 展开全部
          </Button>
          <Button variant='outline' onClick={() => onExpandedChange(new Set())}>
            <ChevronsDownUp /> 折叠全部
          </Button>
          {!readonly && (
            <>
              <Button variant='ghost' size='sm' disabled={busy} onClick={() => update(applyAll(tree, draft, true, locked))}>
                全选
              </Button>
              <Button variant='ghost' size='sm' disabled={busy} onClick={() => update(applyAll(tree, draft, false, locked))}>
                全不选
              </Button>
            </>
          )}
        </div>

        {visibleTree.length === 0 ? (
          <p className='rounded-md border p-6 text-center text-sm text-muted-foreground'>没有匹配「{query.trim()}」的菜单或权限点</p>
        ) : (
          <ul className='divide-y rounded-md border' aria-label={`「${role.name}」的权限树`}>
            {visibleTree.map((node) =>
              node.kind === 'group' ? (
                <GroupNode
                  key={node.code}
                  node={node}
                  open={isOpen(node.code)}
                  onToggleOpen={() => toggleOpen(node.code)}
                  draft={draft}
                  original={original}
                  lockOf={lockOf}
                  disabled={busy}
                  onChange={update}
                />
              ) : (
                <PageNode
                  key={node.code}
                  page={node}
                  level={1}
                  draft={draft}
                  original={original}
                  lockOf={lockOf}
                  disabled={busy}
                  onChange={update}
                />
              )
            )}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

/** 对整棵树执行全选或全不选（跳过锁定的权限点）。 */
function applyAll(tree: PermissionNode[], granted: Set<string>, checked: boolean, locked: (code: string) => boolean) {
  return tree.reduce<Set<string>>((acc, node) => toggleNode(acc, node, checked, locked), granted)
}

type NodeProps = {
  draft: Set<string>
  original: Set<string>
  lockOf: (code: string) => string | null
  disabled: boolean
  onChange: (next: Set<string>) => void
}

function GroupNode({ node, open, onToggleOpen, draft, original, lockOf, disabled, onChange }: NodeProps & {
  node: PermissionNode
  open: boolean
  onToggleOpen: () => void
}) {
  const id = useId()
  const codes = codesOf(node)
  const locked = (code: string) => lockOf(code) !== null
  const allLocked = codes.every(locked)
  return (
    <li>
      <div className='flex items-center gap-2 px-3 py-2 hover:bg-muted/40'>
        <Button
          variant='ghost'
          size='icon'
          className='size-7'
          aria-expanded={open}
          aria-controls={`${id}-children`}
          aria-label={`${open ? '折叠' : '展开'}「${node.title}」`}
          onClick={onToggleOpen}
        >
          <ChevronRight className={cn('transition-transform duration-150 rtl:rotate-180', open && 'rotate-90 rtl:rotate-90')} />
        </Button>
        <TriStateCheckbox
          id={id}
          checked={checkState(node, draft)}
          disabled={allLocked || disabled}
          onCheckedChange={(v) => onChange(toggleNode(draft, node, v === true, locked))}
        />
        <label htmlFor={id} className={cn('font-medium', allLocked ? 'cursor-default' : 'cursor-pointer')}>
          {node.title}
        </label>
        <Badge variant='outline' className='text-[11px] font-normal'>
          目录
        </Badge>
        <span className='ms-auto text-xs text-muted-foreground tabular-nums'>
          {countGranted(node, draft)}/{codes.length}
        </span>
      </div>
      {open && (
        <ul id={`${id}-children`} className='divide-y border-t bg-muted/20'>
          {node.children.map((page) => (
            <PageNode
              key={page.code}
              page={page}
              level={2}
              draft={draft}
              original={original}
              lockOf={lockOf}
              disabled={disabled}
              onChange={onChange}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

function PageNode({ page, level, draft, original, lockOf, disabled, onChange }: NodeProps & {
  page: PermissionNode
  level: 1 | 2
}) {
  const id = useId()
  const locked = (code: string) => lockOf(code) !== null
  const allLocked = page.permissions.every((p) => locked(p.code))
  const view = viewCodeOf(page)
  return (
    // 缩进与目录行对齐：一级页面的勾选框对齐目录的勾选框，二级页面对齐目录名称
    <li className={cn('flex flex-wrap items-center gap-x-6 gap-y-2 py-2 pe-3', level === 1 ? 'ps-12' : 'ps-18')}>
      <div className='flex min-w-40 flex-1 items-center gap-2'>
        <TriStateCheckbox
          id={id}
          checked={checkState(page, draft)}
          disabled={allLocked || disabled}
          onCheckedChange={(v) => onChange(toggleNode(draft, page, v === true, locked))}
        />
        <label htmlFor={id} className={allLocked ? 'cursor-default' : 'cursor-pointer'}>
          {page.title}
        </label>
        {level === 1 && (
          <Badge variant='outline' className='text-[11px] font-normal'>
            一级页面
          </Badge>
        )}
      </div>
      <fieldset className='flex min-w-0 flex-wrap items-center gap-x-5 gap-y-1'>
        <legend className='sr-only'>「{page.title}」的权限点</legend>
        {page.permissions.map((p) => {
          const checked = draft.has(p.code)
          const changed = checked !== original.has(p.code)
          const reason = lockOf(p.code) ?? (p.code === view && checked ? viewLockReason(page, draft, locked) : null)
          return (
            <label
              key={p.code}
              title={reason ?? p.code}
              className={cn(
                'flex items-center gap-1.5 rounded px-1 py-0.5 text-sm',
                reason ? 'cursor-default text-muted-foreground' : 'cursor-pointer',
                changed && 'bg-primary/10'
              )}
            >
              <Checkbox
                checked={checked}
                disabled={!!reason || disabled}
                onCheckedChange={(v) => onChange(toggleGrant(draft, page, p.code, v === true))}
                aria-label={`${page.title} · ${p.name}${changed ? '（未保存）' : ''}${reason ? `（${reason}）` : ''}`}
              />
              {p.name}
              {p.sensitive && (
                <Badge variant='outline' className='text-[11px] font-normal'>
                  高敏
                </Badge>
              )}
            </label>
          )
        })}
      </fieldset>
    </li>
  )
}
