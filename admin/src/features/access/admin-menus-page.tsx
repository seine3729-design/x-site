import { Fragment, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  ChevronRight,
  ChevronsDownUp,
  ChevronsUpDown,
  ExternalLink,
  FolderPlus,
  Link2,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { MENU_ICON_NAMES, resolveIcon } from '@/config/icons'
import { cn } from '@/lib/utils'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { accessApi, accessKeys, MENU_KIND_LABELS, menusQuery, type MenuInput, type MenuRow } from './api'
import { buildMenuTree, reorder, type MenuTreeNode } from './menu-order'
import { LoadError, PageHeading, WithReason } from './shared'

type DialogState =
  | { type: 'create'; kind: 'group' | 'link'; parentId: number | null }
  | { type: 'edit' | 'delete'; menu: MenuRow }

/** 把菜单行转成修改接口的完整输入（PATCH 需要提交全部可改字段）。 */
const inputOf = (m: MenuRow): MenuInput => ({
  parent_id: m.parent_id,
  title: m.title,
  path: m.path,
  icon: m.icon,
  sort: m.sort,
  visible: m.visible,
})

const COLUMNS = 8

/** 后台菜单（docs/06 第 33c 节）：两级菜单的可展开列表。 */
export function AdminMenusPage() {
  const can = useCan()
  const qc = useQueryClient()
  const visible = can('admin_menus.view')
  const menus = useQuery({ ...menusQuery, enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [expanded, setExpanded] = useState<Set<number>>(new Set())

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: accessKeys.menus })
    void qc.invalidateQueries({ queryKey: accessKeys.permissions }) // 权限树按菜单结构组织
    void qc.invalidateQueries({ queryKey: ['auth', 'menus'] }) // 自己的侧栏
  }
  const patch = useMutation({
    mutationFn: async (updates: { menu: MenuRow; changes: Partial<MenuInput> }[]) => {
      for (const u of updates) await accessApi.updateMenu(u.menu.id, { ...inputOf(u.menu), ...u.changes })
    },
    onSettled: invalidate,
  })

  if (!visible) return <NoPermission title='后台菜单' />
  const editable = can('admin_menus.edit')
  const tree = buildMenuTree(menus.data ?? [])
  const directories = tree.filter((n) => n.kind === 'group')
  const busy = !editable || patch.isPending

  const toggleOpen = (id: number) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const move = (list: MenuRow[], index: number, dir: -1 | 1) =>
    patch.mutate(reorder(list, index, dir).map((x) => ({ menu: x.menu, changes: { sort: x.sort } })))

  const deleteReason = (m: MenuRow, children: number): string | null => {
    if (!editable) return '当前角色只能查看'
    if (m.builtin) return '内置菜单不能删除，可以隐藏'
    if (m.kind === 'group' && children > 0) return '目录下还有菜单，先移走或删除'
    return null
  }

  const row = (m: MenuRow, level: 1 | 2, siblings: MenuRow[], index: number, node?: MenuTreeNode) => {
    const Icon = m.icon ? resolveIcon(m.icon) : null
    const isDir = m.kind === 'group'
    const open = expanded.has(m.id)
    const childCount = node?.children.length ?? 0
    const reason = deleteReason(m, childCount)
    return (
      <TableRow key={m.id} className={cn(level === 2 && 'bg-muted/20')}>
        <TableCell>
          <div className={cn('flex items-center gap-2', level === 2 && 'ps-9', !m.visible && 'opacity-60')}>
            {isDir ? (
              <Button
                variant='ghost'
                size='icon'
                className='size-7'
                aria-expanded={open}
                aria-label={`${open ? '折叠' : '展开'}「${m.title}」`}
                onClick={() => toggleOpen(m.id)}
              >
                <ChevronRight className={cn('transition-transform duration-150 rtl:rotate-180', open && 'rotate-90 rtl:rotate-90')} />
              </Button>
            ) : (
              level === 1 && <span className='size-7 shrink-0' aria-hidden />
            )}
            {Icon ? <Icon className='size-4 shrink-0 text-muted-foreground' aria-hidden /> : <span className='size-4 shrink-0' aria-hidden />}
            <span className={cn(isDir || level === 1 ? 'font-medium' : '')}>{m.title}</span>
            {isDir && <span className='text-xs text-muted-foreground tabular-nums'>（{childCount}）</span>}
            {m.builtin && (
              <Badge variant='secondary' className='text-[11px] font-normal'>
                内置
              </Badge>
            )}
            {!m.visible && (
              <Badge variant='outline' className='text-[11px] font-normal'>
                已隐藏
              </Badge>
            )}
          </div>
        </TableCell>
        <TableCell>
          <Badge variant='outline' className='font-normal'>
            {MENU_KIND_LABELS[m.kind]}
          </Badge>
        </TableCell>
        <TableCell className='text-muted-foreground'>{level === 1 ? '一级' : '二级'}</TableCell>
        <TableCell className='max-w-72'>
          {m.kind === 'group' ? (
            <span className='text-muted-foreground'>—</span>
          ) : (
            <span className='flex items-center gap-1 truncate text-muted-foreground' title={m.path}>
              {m.kind === 'link' && <ExternalLink className='size-3 shrink-0' aria-label='外部链接' />}
              <span className='truncate'>{m.path}</span>
            </span>
          )}
        </TableCell>
        <TableCell>{m.kind === 'page' ? <code className='text-xs'>{m.code}</code> : <span className='text-muted-foreground'>—</span>}</TableCell>
        <TableCell>
          <div className='flex items-center justify-center gap-1'>
            <span className='w-10 text-end text-muted-foreground tabular-nums'>{m.sort}</span>
            <Button
              variant='ghost'
              size='icon'
              className='size-7'
              disabled={busy || index === 0}
              aria-label={`上移「${m.title}」`}
              onClick={() => move(siblings, index, -1)}
            >
              <ArrowUp className='size-4' />
            </Button>
            <Button
              variant='ghost'
              size='icon'
              className='size-7'
              disabled={busy || index === siblings.length - 1}
              aria-label={`下移「${m.title}」`}
              onClick={() => move(siblings, index, 1)}
            >
              <ArrowDown className='size-4' />
            </Button>
          </div>
        </TableCell>
        <TableCell className='text-center'>
          <Switch
            checked={m.visible}
            disabled={busy}
            onCheckedChange={(v) => patch.mutate([{ menu: m, changes: { visible: v } }])}
            aria-label={`在侧栏显示「${m.title}」`}
          />
        </TableCell>
        <TableCell>
          <div className='flex items-center justify-end gap-1'>
            {isDir && editable && (
              <Button
                variant='ghost'
                size='sm'
                disabled={patch.isPending}
                onClick={() => setDialog({ type: 'create', kind: 'link', parentId: m.id })}
              >
                <Plus /> 外链
              </Button>
            )}
            <Button
              variant='ghost'
              size='icon'
              className='size-8'
              disabled={busy}
              aria-label={`编辑「${m.title}」`}
              onClick={() => setDialog({ type: 'edit', menu: m })}
            >
              <Pencil className='size-4' />
            </Button>
            <WithReason reason={reason}>
              <Button
                variant='ghost'
                size='icon'
                className='size-8'
                disabled={!!reason || patch.isPending}
                aria-label={`删除「${m.title}」`}
                onClick={() => setDialog({ type: 'delete', menu: m })}
              >
                <Trash2 className='size-4' />
              </Button>
            </WithReason>
          </div>
        </TableCell>
      </TableRow>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='后台菜单'
        readonly={!editable}
        actions={
          <>
            <Button variant='outline' onClick={() => setExpanded(new Set(directories.map((d) => d.id)))}>
              <ChevronsUpDown /> 展开全部
            </Button>
            <Button variant='outline' onClick={() => setExpanded(new Set())}>
              <ChevronsDownUp /> 折叠全部
            </Button>
            {editable && (
              <>
                <Button variant='outline' onClick={() => setDialog({ type: 'create', kind: 'group', parentId: null })}>
                  <FolderPlus /> 新建目录
                </Button>
                <Button onClick={() => setDialog({ type: 'create', kind: 'link', parentId: null })}>
                  <Link2 /> 新建外链
                </Button>
              </>
            )}
          </>
        }
      />
      {menus.isError ? (
        <LoadError onRetry={() => void menus.refetch()} />
      ) : menus.isPending ? (
        <Skeleton className='h-96 w-full' />
      ) : (
        <div className='rounded-md border'>
          <Table>
            <caption className='sr-only'>后台菜单，目录可展开查看二级菜单</caption>
            <TableHeader className='bg-muted/50'>
              <TableRow>
                <TableHead className='min-w-64 ps-4'>菜单名称</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>层级</TableHead>
                <TableHead>地址</TableHead>
                <TableHead>页面编码</TableHead>
                <TableHead className='text-center'>排序</TableHead>
                <TableHead className='text-center'>显示</TableHead>
                <TableHead className='text-end'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tree.map((node, i) => (
                <Fragment key={node.id}>
                  {row(node, 1, tree, i, node)}
                  {node.kind === 'group' &&
                    expanded.has(node.id) &&
                    (node.children.length === 0 ? (
                      <TableRow className='bg-muted/20'>
                        <TableCell colSpan={COLUMNS} className='ps-20 text-sm text-muted-foreground'>
                          这个目录还没有菜单
                        </TableCell>
                      </TableRow>
                    ) : (
                      node.children.map((child, j) => row(child, 2, node.children, j))
                    ))}
                </Fragment>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <MenuDialog
          kind={dialog.type === 'create' ? dialog.kind : dialog.menu.kind}
          menu={dialog.type === 'edit' ? dialog.menu : undefined}
          defaultParentId={dialog.type === 'create' ? dialog.parentId : null}
          directories={directories}
          onSaved={(parentId) => {
            if (parentId !== null) setExpanded((prev) => new Set(prev).add(parentId))
            invalidate()
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'delete' && <DeleteMenuDialog menu={dialog.menu} onDeleted={invalidate} onClose={() => setDialog(null)} />}
    </div>
  )
}

const NO_ICON = '__none__'
const TOP_LEVEL = '__top__'

const menuSchema = z.object({
  title: z.string().trim().min(1, '请填写名称').max(32, '名称最多 32 个字'),
  icon: z.string(),
  parent_id: z.string(),
  path: z.string().trim(),
  sort: z.number({ error: '请填写 0–9999 的整数' }).int('请填写整数').min(0, '最小为 0').max(9999, '最大为 9999'),
  visible: z.boolean(),
})

type MenuForm = z.infer<typeof menuSchema>

const menuSchemaFor = (kind: MenuRow['kind']) =>
  menuSchema.superRefine((v, ctx) => {
    if (kind === 'link' && !/^https?:\/\/[^\s/]+/i.test(v.path)) {
      ctx.addIssue({ code: 'custom', path: ['path'], message: '请填写 http:// 或 https:// 开头的完整地址' })
    }
  })

function MenuDialog({
  kind,
  menu,
  defaultParentId,
  directories,
  onSaved,
  onClose,
}: {
  kind: MenuRow['kind']
  menu?: MenuRow
  defaultParentId: number | null
  directories: MenuRow[]
  onSaved: (parentId: number | null) => void
  onClose: () => void
}) {
  const creating = !menu
  const parent = menu ? menu.parent_id : defaultParentId
  const form = useForm<MenuForm>({
    resolver: zodResolver(menuSchemaFor(kind)),
    defaultValues: {
      title: menu?.title ?? '',
      icon: menu?.icon || NO_ICON,
      parent_id: parent === null ? TOP_LEVEL : String(parent),
      path: menu?.path ?? '',
      sort: menu?.sort ?? 100,
      visible: menu?.visible ?? true,
    },
  })
  const mutation = useMutation({
    mutationFn: async (v: MenuForm) => {
      const parentId = kind === 'group' || v.parent_id === TOP_LEVEL ? null : Number(v.parent_id)
      const body: MenuInput = {
        title: v.title,
        icon: v.icon === NO_ICON ? '' : v.icon,
        parent_id: parentId,
        path: kind === 'link' ? v.path : (menu?.path ?? ''),
        sort: v.sort,
        visible: v.visible,
      }
      if (creating) await accessApi.createMenu({ ...body, kind: kind === 'group' ? 'group' : 'link' })
      else await accessApi.updateMenu(menu.id, body)
      return parentId
    },
    onSuccess: (parentId) => {
      toast.success('已保存')
      onSaved(parentId)
      onClose()
    },
  })

  const title = creating ? (kind === 'group' ? '新建目录' : '新建外链') : `编辑${MENU_KIND_LABELS[kind]}：${menu.title}`
  const description =
    kind === 'group'
      ? '目录只能是一级菜单，用来收纳二级菜单；目录下没有可见菜单时侧栏不显示该目录。'
      : kind === 'link'
        ? '外链在新窗口打开，对所有管理员显示；不选上级目录时作为一级菜单。'
        : '页面地址由系统提供，不能修改；不选上级目录时作为一级菜单。'

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <FormField
              control={form.control}
              name='title'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>名称</FormLabel>
                  <FormControl>
                    <Input placeholder='请输入菜单名称，最多 32 个字' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {kind !== 'group' && (
              <FormField
                control={form.control}
                name='parent_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>上级目录</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue placeholder='请选择上级目录' />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value={TOP_LEVEL}>无（作为一级菜单）</SelectItem>
                        {directories.map((d) => (
                          <SelectItem key={d.id} value={String(d.id)}>
                            {d.title}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {kind === 'link' && (
              <FormField
                control={form.control}
                name='path'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>链接地址</FormLabel>
                    <FormControl>
                      <Input type='url' inputMode='url' placeholder='请输入完整地址，如 https://grafana.example.com' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {kind === 'page' && (
              <div className='grid gap-1'>
                <span className='text-sm font-medium'>页面地址</span>
                <code className='rounded bg-muted px-2 py-1 text-sm'>{menu?.path}</code>
              </div>
            )}
            <FormField
              control={form.control}
              name='icon'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>图标</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger className='w-full'>
                        <SelectValue placeholder='请选择图标' />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent className='max-h-72' searchable searchPlaceholder='搜索图标名称'>
                      <SelectItem value={NO_ICON}>无图标</SelectItem>
                      {MENU_ICON_NAMES.map((name) => {
                        const Icon = resolveIcon(name)
                        return (
                          <SelectItem key={name} value={name}>
                            <Icon className='size-4' aria-hidden /> {name}
                          </SelectItem>
                        )
                      })}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className='grid grid-cols-2 items-start gap-4'>
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
                    <FormDescription>同级菜单中越小越靠前</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='visible'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>在侧栏显示</FormLabel>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} />
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

function DeleteMenuDialog({ menu, onDeleted, onClose }: { menu: MenuRow; onDeleted: () => void; onClose: () => void }) {
  const mutation = useMutation({
    mutationFn: () => accessApi.deleteMenu(menu.id),
    onSuccess: () => {
      toast.success(`已删除「${menu.title}」`)
      onDeleted()
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${menu.title}」`}
      desc='删除后不可恢复。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
