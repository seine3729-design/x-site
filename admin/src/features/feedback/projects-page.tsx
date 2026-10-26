import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ExternalLink, Pencil, Plus, Trash2, Users } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
  Input,
  Label,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
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
import { ImageUploadField } from '@/features/media/upload-field'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  JOIN_MODE_LABELS,
  MEMBER_ROLE_LABELS,
  PROJECT_STATUS_LABELS,
  feedbackApi,
  feedbackKeys,
  memberListQuery,
  optionsOf,
  userListQuery,
  projectListQuery,
  type FbMemberRole,
  type FbProject,
  type FbProjectStatus,
  type ProjectListParams,
} from './api'
import { ProjectStatusBadge, UserStatusBadge } from './components/badges'

const COLUMNS = 8

/** 内测项目（feedback/docs/07 第 8 节）。只有平台管理员能新建项目。 */
export function ProjectsPage() {
  const can = useCan()
  const visible = can('fb_projects.view')
  const search = useSearch({ from: '/_authenticated/feedback/projects' })
  const navigate = useNavigate({ from: '/feedback/projects' })
  const params: ProjectListParams = {
    q: search.q,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...projectListQuery(params), enabled: visible })
  const [editing, setEditing] = useState<FbProject | null>(null)
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<FbProject | null>(null)
  const [members, setMembers] = useState<FbProject | null>(null)

  if (!visible) return <NoPermission title='内测项目' />
  const editable = can('fb_projects.edit')
  const filtered = !!(search.q || search.status)
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='内测项目'
        description='每个项目有独立的成员、动态与反馈编号。只有平台管理员能新建项目。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 新建项目
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜项目名称或标识' onSubmit={(q) => setFilter({ q })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={optionsOf(PROJECT_STATUS_LABELS)}
          onChange={(v) => setFilter({ status: v as FbProjectStatus | undefined })}
          className='w-40'
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
                <TableHead className='pl-4'>编号前缀</TableHead>
                <TableHead>项目</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>可见性</TableHead>
                <TableHead>加入方式</TableHead>
                <TableHead className='text-right'>排序值</TableHead>
                <TableHead>修改时间</TableHead>
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
                    {filtered ? '没有符合条件的项目' : '还没有内测项目，点右上角「新建项目」开始'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className='pl-4 font-mono text-xs tabular-nums'>{p.key_prefix}</TableCell>
                    <TableCell>
                      <div className='flex max-w-sm flex-col'>
                        <span className='truncate font-medium'>{p.name}</span>
                        <span className='truncate text-xs text-muted-foreground'>
                          /{p.slug}
                          {p.summary ? ` · ${p.summary}` : ''}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <ProjectStatusBadge status={p.status} />
                    </TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>{p.visibility === 'public' ? '公开' : '仅成员'}</TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>{JOIN_MODE_LABELS[p.join_mode]}</TableCell>
                    <TableCell className='text-right tabular-nums'>{p.sort}</TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>{formatDateTime(p.updated_at)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        {p.try_url && (
                          <Button asChild variant='ghost' size='icon' aria-label={`打开 ${p.name} 的试用地址`}>
                            <a href={p.try_url} target='_blank' rel='noopener noreferrer'>
                              <ExternalLink className='size-4' />
                            </a>
                          </Button>
                        )}
                        <Button variant='ghost' size='icon' aria-label={`查看 ${p.name} 的成员`} onClick={() => setMembers(p)}>
                          <Users className='size-4' />
                        </Button>
                        <Button variant='ghost' size='icon' aria-label={`编辑 ${p.name}`} onClick={() => setEditing(p)}>
                          <Pencil className='size-4' />
                        </Button>
                        <WithReason reason={readonlyReason}>
                          <Button variant='ghost' size='icon' disabled={!editable} aria-label={`删除 ${p.name}`} onClick={() => setDeleting(p)}>
                            <Trash2 className='size-4' />
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
        <ProjectDialog
          project={editing}
          editable={editable}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
        />
      )}
      {deleting && <DeleteProjectDialog project={deleting} onClose={() => setDeleting(null)} />}
      {members && <MembersDialog project={members} editable={editable} onClose={() => setMembers(null)} />}
    </div>
  )
}

const schema = z.object({
  name: z.string().trim().min(1, '请填写项目名称').max(60, '最多 60 个字'),
  slug: z
    .string()
    .trim()
    .regex(/^[a-z0-9][a-z0-9-]{1,47}$/, '只能用 2–48 位小写字母、数字或连字符，且不以连字符开头'),
  key_prefix: z
    .string()
    .trim()
    .regex(/^[A-Z][A-Z0-9]{1,7}$/, '只能用 2–8 位大写字母或数字，且以字母开头'),
  summary: z.string().trim().max(200, '最多 200 个字'),
  description: z.string().trim().max(10000, '最多 10000 个字'),
  cover: z.string().trim().max(500, '地址过长'),
  try_url: z
    .string()
    .trim()
    .refine((v) => v === '' || /^https:\/\/.+/.test(v), '必须是完整的 https 链接'),
  status: z.enum(['draft', 'recruiting', 'active', 'paused', 'archived']),
  visibility: z.enum(['public', 'members']),
  join_mode: z.enum(['open', 'application', 'invite_only', 'closed']),
  sort: z.coerce.number().int().min(-9999).max(9999),
})

type FormValues = z.infer<typeof schema>

function ProjectDialog({ project, editable, onClose }: { project: FbProject | null; editable: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof schema>, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: project?.name ?? '',
      slug: project?.slug ?? '',
      key_prefix: project?.key_prefix ?? '',
      summary: project?.summary ?? '',
      description: project?.description ?? '',
      cover: project?.cover ?? '',
      try_url: project?.try_url ?? '',
      status: project?.status ?? 'draft',
      visibility: project?.visibility ?? 'public',
      join_mode: project?.join_mode ?? 'invite_only',
      sort: project?.sort ?? 0,
    },
  })
  const mutation = useMutation({
    mutationFn: (v: FormValues) => (project ? feedbackApi.updateProject(project.id, v) : feedbackApi.createProject(v)),
    onSuccess: (p) => {
      toast.success(project ? `已保存「${p.name}」` : `已新建项目「${p.name}」，编号前缀 ${p.key_prefix}`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.projects })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{project ? `编辑「${project.name}」` : '新建内测项目'}</DialogTitle>
          <DialogDescription>编号前缀决定反馈的展示编号（如 APP-1024）。项目标识用于前台地址，上线后改动会影响已分享的链接。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='项目名称' required placeholder='请输入项目名称，如 猫咪短剧' disabled={!editable} />
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField
                control={form.control}
                name='slug'
                label='项目标识'
                required
                placeholder='请输入小写英文标识，如 maomi-app'
                disabled={!editable}
              />
              <TextField
                control={form.control}
                name='key_prefix'
                label='反馈编号前缀'
                required
                placeholder='请输入大写前缀，如 MM'
                disabled={!editable}
              />
            </div>
            <TextField
              control={form.control}
              name='summary'
              label='一句话介绍'
              placeholder='请输入一句话介绍，展示在项目卡片上'
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='description'
              label='项目说明'
              multiline
              rows={4}
              placeholder='请输入项目说明：这轮在测什么、希望用户重点反馈哪些方面'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='try_url' label='去试用地址' placeholder='请输入 https:// 开头的地址' disabled={!editable} />
              <ImageUploadField
                control={form.control}
                name='cover'
                label='封面图'
                purpose='feedback_project_cover'
                description='项目卡片使用；上传前在浏览器压缩为 WebP。'
                disabled={!editable}
              />
            </div>
            <div className='grid gap-4 sm:grid-cols-3'>
              <SelectField
                control={form.control}
                name='status'
                label='项目状态'
                placeholder='请选择项目状态'
                items={optionsOf(PROJECT_STATUS_LABELS)}
                disabled={!editable}
                description='招募中与进行中才接受新反馈'
              />
              <SelectField
                control={form.control}
                name='visibility'
                label='可见性'
                placeholder='请选择可见性'
                items={[
                  { label: '公开（匿名可见）', value: 'public' },
                  { label: '仅成员', value: 'members' },
                ]}
                disabled={!editable}
              />
              <SelectField
                control={form.control}
                name='join_mode'
                label='加入方式'
                placeholder='请选择加入方式'
                items={optionsOf(JOIN_MODE_LABELS)}
                disabled={!editable}
              />
            </div>
            <TextField
              control={form.control}
              name='sort'
              label='排序值'
              inputMode='numeric'
              placeholder='-9999 到 9999，越大越靠前'
              disabled={!editable}
            />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <WithReason reason={editable ? null : '当前角色只能查看'}>
                <Button type='submit' disabled={!editable || mutation.isPending}>
                  {mutation.isPending ? '保存中…' : '保存'}
                </Button>
              </WithReason>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteProjectDialog({ project, onClose }: { project: FbProject; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.deleteProject(project.id),
    onSuccess: () => {
      toast.success(`已删除项目「${project.name}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.projects })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除项目「${project.name}」`}
      desc='只有还没有任何反馈的项目能删除。已经收到反馈的项目请改为「已归档」，保留历史内容。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function MembersDialog({ project, editable, onClose }: { project: FbProject; editable: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const [page, setPage] = useState(1)
  const list = useQuery(memberListQuery(project.id, page, DEFAULT_PAGE_SIZE))
  const mutation = useMutation({
    mutationFn: (v: { userID: number; role: FbMemberRole; remove?: boolean }) =>
      feedbackApi.setMemberRole(project.id, v.userID, { role: v.role, remove: v.remove }),
    onSuccess: (_, v) => {
      toast.success(v.remove ? '已移除成员' : '已调整成员角色')
      void qc.invalidateQueries({ queryKey: feedbackKeys.members(project.id) })
    },
  })
  const rows = list.data?.list ?? []

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>「{project.name}」的成员</DialogTitle>
          <DialogDescription>测试成员之外的角色属于项目团队，可以看到仅成员内容与私密反馈。</DialogDescription>
        </DialogHeader>
        {editable && <AddMemberBox projectID={project.id} />}
        {list.isError ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>用户</TableHead>
                  <TableHead>账号状态</TableHead>
                  <TableHead>项目角色</TableHead>
                  <TableHead>加入时间</TableHead>
                  <TableHead className='pr-4'>
                    <span className='sr-only'>操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.isPending ? (
                  <TableRow>
                    <TableCell colSpan={5} className='h-20 text-center text-muted-foreground'>
                      加载中…
                    </TableCell>
                  </TableRow>
                ) : rows.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className='h-20 text-center text-muted-foreground'>
                      还没有成员。在「邀请码」页生成带本项目的邀请码，用户注册后会自动加入。
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((m) => (
                    <TableRow key={m.user_id}>
                      <TableCell className='pl-4'>
                        <div className='flex flex-col'>
                          <span className='font-medium'>{m.nickname || m.handle}</span>
                          <span className='text-xs text-muted-foreground'>@{m.handle}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <UserStatusBadge status={m.user_status} />
                      </TableCell>
                      <TableCell className='text-muted-foreground'>{MEMBER_ROLE_LABELS[m.role]}</TableCell>
                      <TableCell className='whitespace-nowrap tabular-nums'>{formatDateTime(m.joined_at)}</TableCell>
                      <TableCell className='pr-4 text-right'>
                        <WithReason reason={editable ? (m.role === 'owner' ? '不能移除项目所有者' : null) : '当前角色只能查看'}>
                          <Button
                            variant='ghost'
                            size='sm'
                            disabled={!editable || m.role === 'owner' || mutation.isPending}
                            onClick={() => mutation.mutate({ userID: m.user_id, role: m.role, remove: true })}
                          >
                            移除
                          </Button>
                        </WithReason>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        {list.data && <Pager page={list.data.page} pageSize={list.data.page_size} total={list.data.total} onChange={(p) => setPage(p)} />}
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * 添加成员。
 *
 * 邀请码只在注册时核销，所以已经注册的账号没有别的路进「仅邀请」项目，
 * 这里是运营把人拉进来的唯一入口（后端语义见 openapi 的
 * PATCH /api/admin/feedback/projects/{id}/members/{user_id}）。
 *
 * 搜索要手动触发，不随输入实时请求：社区账号表会很大，每敲一个字查一次
 * 既浪费也容易把人搜出来当通讯录用。
 */
function AddMemberBox({ projectID }: { projectID: number }) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const search = useQuery({
    ...userListQuery({ q: keyword, page: 1, page_size: 8 }),
    enabled: keyword.length > 0,
  })
  const add = useMutation({
    mutationFn: (userID: number) => feedbackApi.setMemberRole(projectID, userID, { role: 'member' }),
    onSuccess: (m) => {
      toast.success(`已把 ${m.nickname || m.handle} 加入项目`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.members(projectID) })
      void qc.invalidateQueries({ queryKey: feedbackKeys.projects })
      setKeyword('')
      setDraft('')
    },
  })

  const found = search.data?.list ?? []

  return (
    <div className='rounded-md border p-3'>
      <form
        className='flex items-end gap-2'
        onSubmit={(e) => {
          e.preventDefault()
          setKeyword(draft.trim())
        }}
      >
        <div className='flex-1'>
          <Label htmlFor='add-member-q'>添加成员</Label>
          <Input
            id='add-member-q'
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder='请输入要添加的用户名或昵称，如 xiaoyu'
            className='mt-1.5'
          />
        </div>
        <Button type='submit' variant='outline' disabled={!draft.trim()}>
          搜索
        </Button>
      </form>
      <p className='mt-1.5 text-xs text-muted-foreground'>加进来的人默认是「测试成员」，加完可以在下面的列表里改角色。</p>

      {keyword && (
        <div className='mt-3'>
          {search.isPending ? (
            <p className='text-sm text-muted-foreground'>搜索中…</p>
          ) : found.length === 0 ? (
            <p className='text-sm text-muted-foreground'>没有匹配「{keyword}」的社区账号</p>
          ) : (
            <ul className='flex flex-col gap-1'>
              {found.map((u) => (
                <li key={u.id} className='flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted'>
                  <span className='min-w-0 flex-1 truncate text-sm'>
                    {u.nickname || u.handle}
                    <span className='ml-2 text-xs text-muted-foreground'>{u.handle}</span>
                  </span>
                  <UserStatusBadge status={u.status} />
                  <Button size='sm' variant='outline' disabled={add.isPending} onClick={() => add.mutate(u.id)}>
                    加入
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
