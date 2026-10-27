import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2, TriangleAlert, X } from 'lucide-react'
import { useFieldArray, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { SelectField, SwitchField, TextField } from '@/features/content/components/fields'
import { SortableList } from '@/features/content/components/sortable-list'
import { moveItem, sameOrder, SLUG_HINT, SLUG_PATTERN } from '@/features/content/utils'
import {
  ACTIVE_LABELS,
  addressesQuery,
  INTERNAL_PAGE_LABELS,
  INTERNAL_PAGES,
  MAX_BACKUP_URLS,
  MAX_MENU_BADGE,
  MAX_MENU_GROUP,
  MAX_MENU_LABEL,
  MAX_NOTICE,
  MAX_SOCIALS,
  MAX_TABBAR_ITEMS,
  MENU_DEVICE_LABELS,
  MENU_LINK_TYPE_LABELS,
  MENU_LINK_TYPES,
  MENU_POSITION_HINTS,
  MENU_POSITION_LABELS,
  MENU_POSITIONS,
  MENU_VISIBLE_LABELS,
  menuListQuery,
  siteApi,
  siteKeys,
  type MenuPosition,
  type SiteAddresses,
  type SiteMenu,
} from './api'
import {
  activeCount,
  addressesToForm,
  EXTERNAL_URL_HINT,
  formToAddresses,
  formToMenuInput,
  groupByPosition,
  isExternalURLOrEmpty,
  isIconNameOrEmpty,
  linkValueHint,
  menuLinkSummary,
  menuToForm,
  type AddressForm,
  type MenuForm,
} from './utils'

type DialogState = { type: 'create'; position: MenuPosition } | { type: 'edit' | 'delete'; item: SiteMenu }

/**
 * 前台菜单与防丢地址（docs/06 第 12 节）：四个位置各自排序，随站点配置下发，改动即时生效。
 * 链接目标不可用（静态页改回草稿、频道取值停用等）时前台自动隐藏，这里标出原因。
 */
export function MenusPage() {
  const can = useCan()
  const visible = can('menus.view')
  const list = useQuery({ ...menuListQuery(), enabled: visible })
  const addresses = useQuery({ ...addressesQuery(), enabled: visible })
  const [dialog, setDialog] = useState<DialogState | null>(null)

  if (!visible) return <NoPermission title='前台菜单' />
  const editable = can('menus.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const groups = groupByPosition(list.data?.list ?? [])

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='前台菜单'
        description='顶部导航、手机底栏、手机侧边抽屉与页脚四个位置。改动后前台立即生效；链接目标被删除或不再对前台显示时，前台自动隐藏该项（这里会标出原因），目标恢复后自动出现。'
        readonly={!editable}
      />

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : list.isPending ? (
        <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
      ) : (
        MENU_POSITIONS.map((pos) => (
          <PositionCard
            // 服务端数据有任何变化（增删、改名、改状态、链接目标失效）都重新载入这一段的本地顺序
            key={`${pos}:${JSON.stringify(groups[pos])}`}
            position={pos}
            items={groups[pos]}
            disabledReason={readonlyReason}
            onCreate={() => setDialog({ type: 'create', position: pos })}
            onEdit={(item) => setDialog({ type: 'edit', item })}
            onDelete={(item) => setDialog({ type: 'delete', item })}
          />
        ))
      )}

      {addresses.isError ? (
        <LoadError onRetry={() => void addresses.refetch()} />
      ) : addresses.isPending ? (
        <p className='text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <AddressCard key={JSON.stringify(addresses.data)} addresses={addresses.data} disabledReason={readonlyReason} />
      )}

      {dialog?.type === 'create' && <MenuDialog position={dialog.position} onClose={() => setDialog(null)} />}
      {dialog?.type === 'edit' && <MenuDialog item={dialog.item} onClose={() => setDialog(null)} />}
      {dialog?.type === 'delete' && <DeleteMenuDialog item={dialog.item} onClose={() => setDialog(null)} />}
    </div>
  )
}

/** 一个位置的菜单：拖动手柄、方向键或上移 / 下移调整顺序，点「保存顺序」后生效。 */
function PositionCard({
  position,
  items,
  disabledReason,
  onCreate,
  onEdit,
  onDelete,
}: {
  position: MenuPosition
  items: SiteMenu[]
  disabledReason: string | null
  onCreate: () => void
  onEdit: (item: SiteMenu) => void
  onDelete: (item: SiteMenu) => void
}) {
  const qc = useQueryClient()
  const [order, setOrder] = useState(items)
  const changed = !sameOrder(
    order.map((m) => m.id),
    items.map((m) => m.id)
  )
  const save = useMutation({
    mutationFn: () =>
      siteApi.reorderMenus(
        position,
        order.map((m) => m.id)
      ),
    onSuccess: () => {
      toast.success(`已保存「${MENU_POSITION_LABELS[position]}」的顺序，前台立即生效`)
      void qc.invalidateQueries({ queryKey: siteKeys.menus })
    },
  })
  const full = position === 'mobile_tabbar' && activeCount(items) >= MAX_TABBAR_ITEMS
  const createReason = disabledReason ?? (full ? `手机底栏最多 ${MAX_TABBAR_ITEMS} 项，请先停用或删除其他项` : null)

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {MENU_POSITION_LABELS[position]}（{items.length} 项
          {position === 'mobile_tabbar' ? ` · 启用 ${activeCount(items)} / ${MAX_TABBAR_ITEMS}` : ''}）
        </CardTitle>
        <CardDescription>{MENU_POSITION_HINTS[position]}</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        {order.length === 0 ? (
          <p className='rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground'>这个位置还没有菜单。</p>
        ) : (
          <SortableList
            items={order}
            getKey={(m) => m.id}
            getLabel={(m) => m.label}
            onMove={(from, to) => setOrder((o) => moveItem(o, from, to))}
            disabled={!!disabledReason || save.isPending}
            renderItem={(m, i) => (
              <div className='flex flex-1 flex-wrap items-center gap-2 text-sm'>
                <span className='w-6 text-right text-xs text-muted-foreground tabular-nums'>{i + 1}</span>
                <span className='font-medium break-all'>{m.label}</span>
                <span className='text-xs break-all text-muted-foreground'>{menuLinkSummary(m)}</span>
                {m.badge && <Badge variant='secondary'>{m.badge}</Badge>}
                {m.visible_to !== 'all' && <Badge variant='outline'>{MENU_VISIBLE_LABELS[m.visible_to]}</Badge>}
                {m.device !== 'all' && <Badge variant='outline'>{MENU_DEVICE_LABELS[m.device]}</Badge>}
                {m.status === 'inactive' && <Badge variant='outline'>已停用</Badge>}
                {m.target_issue && (
                  <Badge variant='destructive' title={m.target_issue}>
                    <TriangleAlert aria-hidden /> 目标不可用
                  </Badge>
                )}
                <div className='ms-auto flex items-center gap-1'>
                  <WithReason reason={disabledReason}>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-8'
                      disabled={!!disabledReason}
                      aria-label={`编辑「${m.label}」`}
                      onClick={() => onEdit(m)}
                    >
                      <Pencil className='size-4' />
                    </Button>
                  </WithReason>
                  <WithReason reason={disabledReason}>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='size-8'
                      disabled={!!disabledReason}
                      aria-label={`删除「${m.label}」`}
                      onClick={() => onDelete(m)}
                    >
                      <Trash2 className='size-4' />
                    </Button>
                  </WithReason>
                </div>
              </div>
            )}
          />
        )}
        {order.some((m) => m.target_issue) && (
          <ul className='flex flex-col gap-1 text-xs text-destructive'>
            {order
              .filter((m) => m.target_issue)
              .map((m) => (
                <li key={m.id}>
                  「{m.label}」：{m.target_issue}（前台暂不显示）
                </li>
              ))}
          </ul>
        )}
        <div className='flex flex-wrap justify-end gap-2'>
          {changed && (
            <Button variant='outline' disabled={save.isPending} onClick={() => setOrder(items)}>
              还原顺序
            </Button>
          )}
          {order.length > 1 && (
            <WithReason reason={disabledReason}>
              <Button variant='secondary' disabled={!!disabledReason || !changed || save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? '保存中…' : '保存顺序'}
              </Button>
            </WithReason>
          )}
          <WithReason reason={createReason}>
            <Button disabled={!!createReason} onClick={onCreate}>
              <Plus /> 新建
            </Button>
          </WithReason>
        </div>
      </CardContent>
    </Card>
  )
}

const menuSchema = z
  .object({
    position: z.enum(MENU_POSITIONS),
    label: z.string().trim().min(1, '请填写名称').max(MAX_MENU_LABEL, `名称最多 ${MAX_MENU_LABEL} 个字`),
    link_type: z.enum(MENU_LINK_TYPES),
    link_value: z.string().trim().min(1, '请填写链接'),
    group_title: z.string().trim().max(MAX_MENU_GROUP, `分组标题最多 ${MAX_MENU_GROUP} 个字`),
    icon: z.string().trim().refine(isIconNameOrEmpty, '图标名只能用字母、数字和连字符，如 house'),
    tabbar_icon: z.string().trim().refine(isIconNameOrEmpty, '图标名只能用字母、数字和连字符，如 house'),
    badge: z.string().trim().max(MAX_MENU_BADGE, `角标最多 ${MAX_MENU_BADGE} 个字`),
    visible_to: z.enum(['all', 'auth', 'guest']),
    device: z.enum(['all', 'mobile', 'desktop']),
    new_tab: z.boolean(),
    status: z.enum(['active', 'inactive']),
  })
  .superRefine((v, ctx) => {
    // 链接按类型校验（与后端一致）
    if (v.link_type === 'internal' && !INTERNAL_PAGES.includes(v.link_value as (typeof INTERNAL_PAGES)[number])) {
      ctx.addIssue({ code: 'custom', path: ['link_value'], message: '请选择站内页' })
    }
    if (v.link_type === 'external' && !(v.link_value !== '' && isExternalURLOrEmpty(v.link_value))) {
      ctx.addIssue({ code: 'custom', path: ['link_value'], message: `外部网址要填 ${EXTERNAL_URL_HINT}` })
    }
    if (['page', 'channel', 'topic', 'tag'].includes(v.link_type) && !SLUG_PATTERN.test(v.link_value.toLowerCase())) {
      ctx.addIssue({ code: 'custom', path: ['link_value'], message: `slug 格式不对：${SLUG_HINT}` })
    }
  })

function MenuDialog({ item, position, onClose }: { item?: SiteMenu; position?: MenuPosition; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !item
  const form = useForm<z.input<typeof menuSchema>, unknown, MenuForm>({
    resolver: zodResolver(menuSchema),
    defaultValues: menuToForm(item, position),
  })
  const [pos, kind] = useWatch({ control: form.control, name: ['position', 'link_type'] })
  const mutation = useMutation({
    mutationFn: (v: MenuForm) => {
      const body = formToMenuInput(v)
      return creating ? siteApi.createMenu(body) : siteApi.updateMenu(item.id, body)
    },
    onSuccess: () => {
      toast.success(creating ? '已新建菜单，前台立即生效' : '已保存，前台立即生效')
      void qc.invalidateQueries({ queryKey: siteKeys.menus })
      onClose()
    },
  })
  const slugLike = kind !== 'internal' && kind !== 'external'

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{creating ? '新建菜单' : `编辑菜单 #${item.id}`}</DialogTitle>
          <DialogDescription>新建或改位置时排在该位置的末尾，顺序随后在列表里拖动调整。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='position'
                label='位置'
                required
                placeholder='请选择位置'
                items={MENU_POSITIONS.map((value) => ({ value, label: MENU_POSITION_LABELS[value] }))}
                description={MENU_POSITION_HINTS[pos as MenuPosition]}
              />
              <TextField control={form.control} name='label' label='名称' required placeholder='请输入菜单名称，如 首页' />
            </div>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='link_type'
                label='链接类型'
                required
                placeholder='请选择链接类型'
                items={MENU_LINK_TYPES.map((value) => ({ value, label: MENU_LINK_TYPE_LABELS[value] }))}
                description={linkValueHint(kind as (typeof MENU_LINK_TYPES)[number])}
              />
              {kind === 'internal' ? (
                <SelectField
                  control={form.control}
                  name='link_value'
                  label='站内页'
                  required
                  placeholder='请选择站内页'
                  items={INTERNAL_PAGES.map((value) => ({ value, label: INTERNAL_PAGE_LABELS[value] }))}
                />
              ) : (
                <TextField
                  control={form.control}
                  name='link_value'
                  label={kind === 'external' ? '外部网址' : `${MENU_LINK_TYPE_LABELS[kind as (typeof MENU_LINK_TYPES)[number]]} slug`}
                  required
                  placeholder={kind === 'external' ? '请输入完整地址，如 https://example.com' : '请输入 slug，如 terms'}
                />
              )}
            </div>
            <div className='grid gap-4 sm:grid-cols-3'>
              <TextField control={form.control} name='icon' label='图标' placeholder='选填，lucide 图标名，如 house' />
              {pos === 'mobile_tabbar' && (
                <TextField control={form.control} name='tabbar_icon' label='底栏图标' placeholder='选填，lucide 图标名，如 house' />
              )}
              {pos === 'footer' && (
                <TextField
                  control={form.control}
                  name='group_title'
                  label='分组标题'
                  placeholder='选填，如 帮助与条款'
                  description='页脚按分组标题分栏。'
                />
              )}
              <TextField control={form.control} name='badge' label='角标' placeholder='选填，如 新' />
            </div>
            <div className='grid gap-4 sm:grid-cols-3'>
              <SelectField
                control={form.control}
                name='visible_to'
                label='谁能看'
                placeholder='请选择可见范围'
                items={Object.entries(MENU_VISIBLE_LABELS).map(([value, label]) => ({ value, label }))}
              />
              <SelectField
                control={form.control}
                name='device'
                label='设备'
                placeholder='请选择设备'
                items={Object.entries(MENU_DEVICE_LABELS).map(([value, label]) => ({ value, label }))}
              />
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={Object.entries(ACTIVE_LABELS).map(([value, label]) => ({ value, label }))}
              />
            </div>
            <SwitchField
              control={form.control}
              name='new_tab'
              label='新窗口打开'
              description={slugLike ? '站内链接通常不需要。' : '外部网址建议打开。'}
            />
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

function DeleteMenuDialog({ item, onClose }: { item: SiteMenu; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => siteApi.deleteMenu(item.id),
    onSuccess: () => {
      toast.success(`已删除「${item.label}」`)
      void qc.invalidateQueries({ queryKey: siteKeys.menus })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除菜单「${item.label}」`}
      desc={`删除后不可恢复，前台「${MENU_POSITION_LABELS[item.position]}」立即不再显示这一项。只是暂时不显示的话，请改为停用。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

// ---------- 防丢地址 ----------

const urlField = (label: string) => z.string().trim().refine(isExternalURLOrEmpty, `${label}要填 ${EXTERNAL_URL_HINT}`)

const addressSchema = z.object({
  notice: z.string().trim().max(MAX_NOTICE, `提醒文字最多 ${MAX_NOTICE} 个字`),
  latest_url: urlField('最新地址'),
  permanent_url: urlField('永久地址'),
  backup_urls: z.array(z.object({ url: urlField('备用地址') })).max(MAX_BACKUP_URLS, `备用地址最多 ${MAX_BACKUP_URLS} 个`),
  publish_page: urlField('发布页'),
  support_url: urlField('客服链接'),
  telegram: urlField('Telegram'),
  socials: z
    .array(
      z
        .object({ name: z.string().trim().max(20, '名称最多 20 个字'), url: urlField('链接') })
        .refine((s) => (s.name === '') === (s.url === ''), { message: '名称与链接都要填', path: ['url'] })
    )
    .max(MAX_SOCIALS, `社交账号最多 ${MAX_SOCIALS} 个`),
})

/** 防丢地址「回家的路」：与菜单同页。保存后站点配置版本 +1，前台秒级生效。 */
function AddressCard({ addresses, disabledReason }: { addresses: SiteAddresses; disabledReason: string | null }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof addressSchema>, unknown, AddressForm>({
    resolver: zodResolver(addressSchema),
    defaultValues: addressesToForm(addresses),
  })
  const backups = useFieldArray({ control: form.control, name: 'backup_urls' })
  const socials = useFieldArray({ control: form.control, name: 'socials' })
  const save = useMutation({
    mutationFn: (v: AddressForm) => siteApi.setAddresses(formToAddresses(v)),
    onSuccess: ({ site_version }) => {
      toast.success(`已保存防丢地址，站点配置版本 ${site_version}，前台立即生效`)
      void qc.invalidateQueries({ queryKey: siteKeys.addresses })
    },
  })
  const editable = !disabledReason
  const dirty = form.formState.isDirty

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>防丢地址「回家的路」</CardTitle>
        <CardDescription>
          前台「回家的路」页面显示的内容：最新 / 永久 / 备用地址、发布页、客服与社交账号。都填 http(s) 完整地址，留空的不显示。保存后前台立即生效。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
            <TextField
              control={form.control}
              name='notice'
              label='提醒文字'
              multiline
              rows={2}
              placeholder='选填，如 请收藏永久地址，主域打不开时从这里回来'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='latest_url' label='最新地址' placeholder='如 https://new.example.com' disabled={!editable} />
              <TextField
                control={form.control}
                name='permanent_url'
                label='永久地址'
                placeholder='如 https://perm.example.com'
                disabled={!editable}
              />
              <TextField control={form.control} name='publish_page' label='发布页' placeholder='如 https://pub.example.com' disabled={!editable} />
              <TextField control={form.control} name='support_url' label='客服链接' placeholder='如 https://t.me/support' disabled={!editable} />
              <TextField control={form.control} name='telegram' label='Telegram' placeholder='如 https://t.me/channel' disabled={!editable} />
            </div>

            <fieldset className='flex flex-col gap-2 rounded-md border p-3' disabled={!editable}>
              <legend className='px-1 text-sm font-medium'>
                备用地址（{backups.fields.length} / {MAX_BACKUP_URLS}）
              </legend>
              {backups.fields.length === 0 && <p className='text-xs text-muted-foreground'>选填。主域与永久地址都打不开时的备选。</p>}
              {backups.fields.map((f, i) => (
                <div key={f.id} className='flex items-start gap-2'>
                  <div className='flex-1'>
                    <TextField
                      control={form.control}
                      name={`backup_urls.${i}.url`}
                      label={`备用地址 ${i + 1}`}
                      placeholder='如 https://b1.example.com'
                      disabled={!editable}
                    />
                  </div>
                  {editable && (
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='mt-7'
                      aria-label={`删除备用地址 ${i + 1}`}
                      onClick={() => backups.remove(i)}
                    >
                      <X className='size-4' />
                    </Button>
                  )}
                </div>
              ))}
              {editable && (
                <Button
                  type='button'
                  variant='secondary'
                  size='sm'
                  className='self-start'
                  disabled={backups.fields.length >= MAX_BACKUP_URLS}
                  onClick={() => backups.append({ url: '' })}
                >
                  <Plus className='size-4' /> 添加备用地址
                </Button>
              )}
            </fieldset>

            <fieldset className='flex flex-col gap-2 rounded-md border p-3' disabled={!editable}>
              <legend className='px-1 text-sm font-medium'>
                社交账号（{socials.fields.length} / {MAX_SOCIALS}）
              </legend>
              {socials.fields.length === 0 && <p className='text-xs text-muted-foreground'>选填。名称与链接都要填。</p>}
              {socials.fields.map((f, i) => (
                <div key={f.id} className='flex items-start gap-2'>
                  <div className='grid flex-1 gap-2 sm:grid-cols-[12rem_1fr]'>
                    <TextField control={form.control} name={`socials.${i}.name`} label={`名称 ${i + 1}`} placeholder='如 微博' disabled={!editable} />
                    <TextField
                      control={form.control}
                      name={`socials.${i}.url`}
                      label={`链接 ${i + 1}`}
                      placeholder='如 https://weibo.com/xxx'
                      disabled={!editable}
                    />
                  </div>
                  {editable && (
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='mt-7'
                      aria-label={`删除社交账号 ${i + 1}`}
                      onClick={() => socials.remove(i)}
                    >
                      <X className='size-4' />
                    </Button>
                  )}
                </div>
              ))}
              {editable && (
                <Button
                  type='button'
                  variant='secondary'
                  size='sm'
                  className='self-start'
                  disabled={socials.fields.length >= MAX_SOCIALS}
                  onClick={() => socials.append({ name: '', url: '' })}
                >
                  <Plus className='size-4' /> 添加社交账号
                </Button>
              )}
            </fieldset>

            <div className='flex flex-wrap items-center justify-end gap-2'>
              {dirty && <span className='me-auto text-sm text-muted-foreground'>有未保存的修改</span>}
              <Button type='button' variant='outline' disabled={!dirty || save.isPending} onClick={() => form.reset()}>
                还原
              </Button>
              <WithReason reason={disabledReason}>
                <Button type='submit' disabled={!editable || !dirty || save.isPending}>
                  {save.isPending ? '保存中…' : '保存防丢地址'}
                </Button>
              </WithReason>
            </div>
          </form>
        </Form>
      </CardContent>
    </Card>
  )
}
