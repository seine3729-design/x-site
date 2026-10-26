import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Pencil, Pin, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
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
import { SelectField, SwitchField, TextField } from '@/features/content/components/fields'
import { FilterSelect, ResetButton } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  POST_KIND_LABELS,
  POST_STATUS_LABELS,
  allProjectsQuery,
  feedbackApi,
  feedbackKeys,
  optionsOf,
  postListQuery,
  type FbPost,
  type FbPostStatus,
  type PostListParams,
} from './api'
import { PostStatusBadge } from './components/badges'

const COLUMNS = 7

/** 项目动态（feedback/docs/07 第 9.1 节）：公告、测试任务、上线小结。只有项目团队能发。 */
export function PostsPage() {
  const can = useCan()
  const visible = can('fb_posts.view')
  const search = useSearch({ from: '/_authenticated/feedback/posts' })
  const navigate = useNavigate({ from: '/feedback/posts' })
  const params: PostListParams = {
    project_id: search.project_id,
    status: search.status,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...postListQuery(params), enabled: visible })
  const projects = useQuery({ ...allProjectsQuery, enabled: visible })
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<FbPost | null>(null)
  const [deleting, setDeleting] = useState<FbPost | null>(null)

  if (!visible) return <NoPermission title='项目动态' />
  const editable = can('fb_posts.edit')
  const filtered = !!(search.project_id || search.status)
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='项目动态'
        description='测试任务告诉用户这轮测什么，上线小结告诉用户改了什么。发布后才会出现在前台动态流。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 新建动态
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
        <FilterSelect
          label='状态'
          value={search.status}
          options={optionsOf(POST_STATUS_LABELS)}
          onChange={(v) => setFilter({ status: v as FbPostStatus | undefined })}
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
                <TableHead className='pl-4'>标题</TableHead>
                <TableHead>项目</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>可见性</TableHead>
                <TableHead>发布时间</TableHead>
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
                    {filtered ? '没有符合条件的动态' : '还没有动态，点右上角「新建动态」发布第一条测试任务'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      <div className='flex max-w-sm items-center gap-2'>
                        {p.pinned && <Pin className='size-3.5 shrink-0 text-muted-foreground' aria-label='已置顶' />}
                        <span className='truncate font-medium'>{p.title}</span>
                      </div>
                    </TableCell>
                    <TableCell className='text-muted-foreground'>{p.project_name}</TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>{POST_KIND_LABELS[p.kind]}</TableCell>
                    <TableCell>
                      <PostStatusBadge status={p.status} />
                    </TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>
                      {p.visibility === 'public' ? '公开' : '仅成员'}
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>
                      {p.published_at ? formatDateTime(p.published_at) : '—'}
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <Button variant='ghost' size='icon' aria-label={`编辑 ${p.title}`} onClick={() => setEditing(p)}>
                          <Pencil className='size-4' />
                        </Button>
                        <WithReason reason={readonlyReason}>
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable}
                            aria-label={`删除 ${p.title}`}
                            onClick={() => setDeleting(p)}
                          >
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
        <PostDialog
          post={editing}
          editable={editable}
          projects={projects.data?.list ?? []}
          defaultProjectID={search.project_id}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
        />
      )}
      {deleting && <DeletePostDialog post={deleting} onClose={() => setDeleting(null)} />}
    </div>
  )
}

const schema = z.object({
  project_id: z.coerce.number().int().positive('请选择所属项目'),
  kind: z.enum(['announcement', 'test_task', 'changelog']),
  title: z.string().trim().min(1, '请填写标题').max(120, '最多 120 个字'),
  body: z.string().trim().max(50000, '最多 50000 个字'),
  status: z.enum(['draft', 'published', 'hidden']),
  visibility: z.enum(['public', 'members']),
  pinned: z.boolean(),
})

type FormValues = z.infer<typeof schema>

function PostDialog({
  post,
  editable,
  projects,
  defaultProjectID,
  onClose,
}: {
  post: FbPost | null
  editable: boolean
  projects: { id: number; name: string }[]
  defaultProjectID?: number
  onClose: () => void
}) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof schema>, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      project_id: post?.project_id ?? defaultProjectID ?? projects[0]?.id ?? 0,
      kind: post?.kind ?? 'test_task',
      title: post?.title ?? '',
      body: post?.body ?? '',
      status: post?.status ?? 'draft',
      visibility: post?.visibility ?? 'public',
      pinned: post?.pinned ?? false,
    },
  })
  const mutation = useMutation({
    mutationFn: (v: FormValues) => (post ? feedbackApi.updatePost(post.id, v) : feedbackApi.createPost(v)),
    onSuccess: (p) => {
      toast.success(post ? `已保存「${p.title}」` : `已新建${POST_STATUS_LABELS[p.status]}动态「${p.title}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.posts })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{post ? `编辑「${post.title}」` : '新建项目动态'}</DialogTitle>
          <DialogDescription>正文支持换行与简单 Markdown。状态设为「已发布」后立刻出现在前台动态流。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='project_id'
                label='所属项目'
                required
                placeholder='请选择所属项目'
                items={projects.map((p) => ({ label: p.name, value: String(p.id) }))}
                disabled={!editable}
              />
              <SelectField
                control={form.control}
                name='kind'
                label='动态类型'
                required
                placeholder='请选择动态类型'
                items={optionsOf(POST_KIND_LABELS)}
                disabled={!editable}
              />
            </div>
            <TextField control={form.control} name='title' label='标题' required placeholder='请输入标题，如 本轮请重点测试提交流程' disabled={!editable} />
            <TextField
              control={form.control}
              name='body'
              label='正文'
              multiline
              rows={8}
              placeholder='请输入正文：这次改了什么、请大家重点试哪几件事、怎么反馈'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={optionsOf(POST_STATUS_LABELS)}
                disabled={!editable}
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
            </div>
            <SwitchField
              control={form.control}
              name='pinned'
              label='置顶'
              description='置顶的动态排在前台动态流最前面'
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

function DeletePostDialog({ post, onClose }: { post: FbPost; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.deletePost(post.id),
    onSuccess: () => {
      toast.success(`已删除「${post.title}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.posts })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除动态「${post.title}」`}
      desc='删除后前台立即看不到这条动态。如果只是想暂时下架，可以把状态改为「已隐藏」。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
