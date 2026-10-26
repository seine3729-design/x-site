import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Eye, EyeOff, MessageCircle, Pencil, Pin, Plus, Star, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
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
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  TOPIC_STATUS_LABELS,
  allProjectsQuery,
  answerListQuery,
  feedbackApi,
  feedbackKeys,
  optionsOf,
  topicListQuery,
  type FbAnswer,
  type FbTopic,
  type FbTopicStatus,
  type TopicListParams,
} from './api'
import { TopicStatusBadge } from './components/badges'

const COLUMNS = 8

/**
 * 议题区（feedback/docs/08 第 8.5 节）：运营抛一个问题，用户自由回答，其他人可以点「有道理」。
 * 首期只做自由回答，没有单选/多选题型。
 */
export function TopicsPage() {
  const can = useCan()
  const visible = can('fb_topics.view')
  const search = useSearch({ from: '/_authenticated/feedback/topics' })
  const navigate = useNavigate({ from: '/feedback/topics' })
  const params: TopicListParams = {
    project_id: search.project_id,
    status: search.status,
    q: search.q,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...topicListQuery(params), enabled: visible })
  const projects = useQuery({ ...allProjectsQuery, enabled: visible })
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<FbTopic | null>(null)
  const [deleting, setDeleting] = useState<FbTopic | null>(null)
  const [viewing, setViewing] = useState<FbTopic | null>(null)

  if (!visible) return <NoPermission title='议题区' />
  const editable = can('fb_topics.edit')
  const filtered = !!(search.project_id || search.status || search.q)
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='议题区'
        description='抛一个具体问题，让用户自由回答。状态设为「征集中」后出现在前台，设为「已结束」就只能看不能答。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setCreating(true)}>
              <Plus /> 新建议题
            </Button>
          )
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜议题标题' onSubmit={(q) => setFilter({ q })} />
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
          options={optionsOf(TOPIC_STATUS_LABELS)}
          onChange={(v) => setFilter({ status: v as FbTopicStatus | undefined })}
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
                <TableHead className='pl-4'>议题</TableHead>
                <TableHead>项目</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className='text-right'>回答</TableHead>
                <TableHead>可见性</TableHead>
                <TableHead>截止时间</TableHead>
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
                    {filtered ? '没有符合条件的议题' : '还没有议题，点右上角「新建议题」问用户第一个问题'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell>
                      <div className='flex max-w-sm items-center gap-2'>
                        {t.pinned && <Pin className='size-3.5 shrink-0 text-muted-foreground' aria-label='已置顶' />}
                        <span className='truncate font-medium'>{t.title}</span>
                      </div>
                    </TableCell>
                    <TableCell className='text-muted-foreground'>{t.project_name}</TableCell>
                    <TableCell>
                      <TopicStatusBadge status={t.status} />
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>{t.answer_count}</TableCell>
                    <TableCell className='whitespace-nowrap text-muted-foreground'>
                      {t.visibility === 'public' ? '公开' : '仅成员'}
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>
                      {t.ends_at ? formatDateTime(t.ends_at) : '不限'}
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>
                      {t.published_at ? formatDateTime(t.published_at) : '—'}
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          variant='ghost'
                          size='icon'
                          aria-label={`查看「${t.title}」的回答`}
                          onClick={() => setViewing(t)}
                        >
                          <MessageCircle className='size-4' />
                        </Button>
                        <Button variant='ghost' size='icon' aria-label={`编辑 ${t.title}`} onClick={() => setEditing(t)}>
                          <Pencil className='size-4' />
                        </Button>
                        <WithReason
                          reason={editable ? (t.answer_count > 0 ? '已有回答，只能改为「已归档」' : null) : '当前角色只能查看'}
                        >
                          <Button
                            variant='ghost'
                            size='icon'
                            disabled={!editable || t.answer_count > 0}
                            aria-label={`删除 ${t.title}`}
                            onClick={() => setDeleting(t)}
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
        <TopicDialog
          topic={editing}
          editable={editable}
          projects={projects.data?.list ?? []}
          defaultProjectID={search.project_id}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
        />
      )}
      {viewing && <AnswersDialog topic={viewing} editable={editable} onClose={() => setViewing(null)} />}
      {deleting && <DeleteTopicDialog topic={deleting} onClose={() => setDeleting(null)} />}
    </div>
  )
}

const schema = z.object({
  project_id: z.coerce.number().int().positive('请选择所属项目'),
  title: z.string().trim().min(1, '请填写议题标题').max(160, '最多 160 个字'),
  body: z.string().trim().max(20000, '最多 20000 个字'),
  answer_hint: z.string().trim().max(200, '最多 200 个字'),
  status: z.enum(['draft', 'open', 'closed', 'archived']),
  visibility: z.enum(['public', 'members']),
  pinned: z.boolean(),
  ends_at: z.string().trim(),
})

type FormValues = z.infer<typeof schema>

const pad2 = (n: number) => String(n).padStart(2, '0')

/** 把接口返回的 ISO 时间转成 datetime-local 能用的本地时间字符串。 */
function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

function TopicDialog({
  topic,
  editable,
  projects,
  defaultProjectID,
  onClose,
}: {
  topic: FbTopic | null
  editable: boolean
  projects: { id: number; name: string }[]
  defaultProjectID?: number
  onClose: () => void
}) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof schema>, unknown, FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      project_id: topic?.project_id ?? defaultProjectID ?? projects[0]?.id ?? 0,
      title: topic?.title ?? '',
      body: topic?.body ?? '',
      answer_hint: topic?.answer_hint ?? '',
      status: topic?.status ?? 'draft',
      visibility: topic?.visibility ?? 'public',
      pinned: topic?.pinned ?? false,
      ends_at: toLocalInput(topic?.ends_at ?? null),
    },
  })
  const mutation = useMutation({
    mutationFn: (v: FormValues) => {
      const body = { ...v, ends_at: v.ends_at ? new Date(v.ends_at).toISOString() : null }
      return topic ? feedbackApi.updateTopic(topic.id, body) : feedbackApi.createTopic(body)
    },
    onSuccess: (t) => {
      toast.success(topic ? `已保存「${t.title}」` : `已新建${TOPIC_STATUS_LABELS[t.status]}议题「${t.title}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.topics })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{topic ? `编辑「${topic.title}」` : '新建议题'}</DialogTitle>
          <DialogDescription>
            问题越具体，回答越有用。状态设为「征集中」后前台就能看到并回答；已有回答的议题不能删除，改为「已归档」保留结论。
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <SelectField
              control={form.control}
              name='project_id'
              label='所属项目'
              required
              placeholder='请选择所属项目'
              items={projects.map((p) => ({ label: p.name, value: String(p.id) }))}
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='title'
              label='议题标题'
              required
              placeholder='请输入一个具体问题，如 你希望下一版先做哪个功能？'
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='body'
              label='议题说明'
              multiline
              rows={6}
              placeholder='请输入背景与约束：为什么问这个、有哪些选项、希望大家从什么角度回答'
              disabled={!editable}
            />
            <TextField
              control={form.control}
              name='answer_hint'
              label='回答引导语'
              placeholder='请输入输入框里的提示语，如 说说你最常用的场景'
              description='显示在前台回答输入框里，留空就用默认提示。'
              disabled={!editable}
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='status'
                label='状态'
                placeholder='请选择状态'
                items={optionsOf(TOPIC_STATUS_LABELS)}
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
            <TextField
              control={form.control}
              name='ends_at'
              label='截止时间'
              type='datetime-local'
              placeholder='留空表示不限时间'
              description='到点后前台自动停止收回答，状态仍显示「征集中」。'
              disabled={!editable}
            />
            <SwitchField
              control={form.control}
              name='pinned'
              label='置顶'
              description='置顶的议题排在前台议题区最前面'
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

const ANSWER_PAGE_SIZE = 20

/** 回答管理：精选会排到前台最前面，隐藏后前台立刻看不到且不能再被点「有道理」。 */
function AnswersDialog({ topic, editable, onClose }: { topic: FbTopic; editable: boolean; onClose: () => void }) {
  const [page, setPage] = useState(1)
  const list = useQuery(answerListQuery(topic.id, page, ANSWER_PAGE_SIZE))
  const rows = list.data?.list ?? []

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>「{topic.title}」的回答</DialogTitle>
          <DialogDescription>
            共 {list.data?.total ?? topic.answer_count} 条（含已隐藏）。精选的回答排在前台最前面；隐藏后前台立刻看不到。
          </DialogDescription>
        </DialogHeader>
        {list.isError ? (
          <LoadError onRetry={() => void list.refetch()} />
        ) : list.isPending ? (
          <p className='py-8 text-center text-muted-foreground'>加载中…</p>
        ) : rows.length === 0 ? (
          <p className='py-8 text-center text-muted-foreground'>还没有人回答这个议题</p>
        ) : (
          <ul className='flex flex-col gap-3'>
            {rows.map((a) => (
              <AnswerRow key={a.id} answer={a} topicID={topic.id} editable={editable} />
            ))}
          </ul>
        )}
        {list.data && list.data.total > ANSWER_PAGE_SIZE && (
          <Pager
            page={list.data.page}
            pageSize={list.data.page_size}
            total={list.data.total}
            onChange={(p) => setPage(p)}
          />
        )}
        <DialogFooter>
          <Button type='button' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AnswerRow({ answer, topicID, editable }: { answer: FbAnswer; topicID: number; editable: boolean }) {
  const qc = useQueryClient()
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: feedbackKeys.answers(topicID) })
    void qc.invalidateQueries({ queryKey: feedbackKeys.topics })
  }
  const featured = useMutation({
    mutationFn: () => feedbackApi.setAnswerFeatured(answer.id, !answer.featured),
    onSuccess: (a) => {
      toast.success(a.featured ? '已设为精选' : '已取消精选')
      refresh()
    },
  })
  const status = useMutation({
    mutationFn: () => feedbackApi.setAnswerStatus(answer.id, answer.status === 'hidden' ? 'visible' : 'hidden'),
    onSuccess: (a) => {
      toast.success(a.status === 'hidden' ? '已隐藏这条回答' : '已恢复显示')
      refresh()
    },
  })
  const hidden = answer.status === 'hidden'
  const busy = featured.isPending || status.isPending
  const author = answer.nickname || answer.handle || '匿名'

  return (
    <li className={`rounded-md border p-3 ${hidden ? 'bg-muted/50' : ''}`}>
      <div className='flex flex-wrap items-center gap-2 text-sm'>
        {answer.tester_no ? <Badge variant='outline'>内测{answer.tester_no}</Badge> : null}
        <span className='font-medium'>{author}</span>
        <span className='text-muted-foreground tabular-nums'>{formatDateTime(answer.created_at)}</span>
        {answer.parent_id ? <Badge variant='outline'>回复</Badge> : null}
        {answer.featured && <Badge>精选</Badge>}
        {hidden && <Badge variant='secondary'>已隐藏</Badge>}
        <span className='ms-auto text-muted-foreground tabular-nums'>有道理 {answer.helpful_count}</span>
      </div>
      <p className='mt-2 text-sm whitespace-pre-wrap'>{answer.body}</p>
      <div className='mt-2 flex justify-end gap-1'>
        <WithReason reason={editable ? (hidden ? '已隐藏的回答不能设为精选' : null) : '当前角色只能查看'}>
          <Button
            variant='ghost'
            size='sm'
            disabled={!editable || hidden || busy}
            onClick={() => featured.mutate()}
            aria-label={answer.featured ? `取消精选 ${author} 的回答` : `把 ${author} 的回答设为精选`}
          >
            <Star className={`size-4 ${answer.featured ? 'fill-current' : ''}`} />
            {answer.featured ? '取消精选' : '精选'}
          </Button>
        </WithReason>
        <WithReason reason={editable ? null : '当前角色只能查看'}>
          <Button
            variant='ghost'
            size='sm'
            disabled={!editable || busy}
            onClick={() => status.mutate()}
            aria-label={hidden ? `恢复显示 ${author} 的回答` : `隐藏 ${author} 的回答`}
          >
            {hidden ? <Eye className='size-4' /> : <EyeOff className='size-4' />}
            {hidden ? '恢复显示' : '隐藏'}
          </Button>
        </WithReason>
      </div>
    </li>
  )
}

function DeleteTopicDialog({ topic, onClose }: { topic: FbTopic; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => feedbackApi.deleteTopic(topic.id),
    onSuccess: () => {
      toast.success(`已删除「${topic.title}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.topics })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除议题「${topic.title}」`}
      desc='删除后前台立即看不到。如果只是想停止收集，把状态改为「已结束」或「已归档」。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '处理中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
