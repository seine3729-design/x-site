import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowUpRight, Inbox, ThumbsUp } from 'lucide-react'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
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
  Textarea,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { withFilter } from '@/features/content/search'
import {
  DEFAULT_PAGE_SIZE,
  KIND_LABELS,
  PRIORITY_LABELS,
  SOURCE_LABELS,
  STATUS_LABELS,
  STATUS_TRANSITIONS,
  VISIBILITY_LABELS,
  allProjectsQuery,
  feedbackApi,
  feedbackKeys,
  issueDetailQuery,
  issueListQuery,
  optionsOf,
  overviewQuery,
  type FbFeedback,
  type FbKind,
  type FbPriority,
  type FbSource,
  type FbStatus,
  type FbVisibility,
  type IssueListParams,
} from './api'
import { KindBadge, PriorityBadge, SourceBadge, StatusBadge, VisibilityBadge } from './components/badges'

const COLUMNS = 10

/**
 * 提交者的显示名。外部网站送进来的反馈没有社区账号，不能退回「已注销用户」——
 * 那会让运营以为是个注销掉的社区成员。
 */
function authorOf(f: Pick<FbFeedback, 'source' | 'author_nickname' | 'author_handle'>): string {
  if (f.source === 'open_api') return SOURCE_LABELS.open_api
  return f.author_nickname || f.author_handle || '已注销用户'
}

const SORT_LABELS: Record<NonNullable<IssueListParams['sort']>, string> = {
  oldest: '最久未处理优先',
  latest: '最近活动',
  newest: '最新提交',
  votes: '票数最多',
}

/** 反馈分诊（feedback/docs/07 第 6 节）。默认按最久未处理优先，便于逐条清队列。 */
export function IssuesPage() {
  const can = useCan()
  const visible = can('fb_issues.view')
  const search = useSearch({ from: '/_authenticated/feedback/issues' })
  const navigate = useNavigate({ from: '/feedback/issues' })
  const params: IssueListParams = {
    project_id: search.project_id,
    status: search.status,
    kind: search.kind,
    priority: search.priority,
    visibility: search.visibility,
    source: search.source,
    q: search.q,
    sort: search.sort ?? 'oldest',
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...issueListQuery(params), enabled: visible })
  const projects = useQuery({ ...allProjectsQuery, enabled: visible })
  const overview = useQuery({ ...overviewQuery(search.project_id), enabled: visible })
  const [openID, setOpenID] = useState<number | null>(null)

  if (!visible) return <NoPermission title='反馈分诊' />
  const editable = can('fb_issues.edit')
  const filtered = !!(
    search.q ||
    search.status ||
    search.kind ||
    search.priority ||
    search.visibility ||
    search.source ||
    search.project_id ||
    search.sort
  )
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })
  const rows = list.data?.list ?? []
  const counts = overview.data?.list ?? []
  const pending = counts.find((c) => c.status === 'submitted')?.total ?? 0

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='反馈分诊'
        description='确认类型与优先级、回复提交者、流转状态。公开说明会展示给用户，内部备注只有后台可见。'
        readonly={!editable}
        actions={
          <Button
            variant={search.status === 'submitted' ? 'default' : 'secondary'}
            onClick={() => setFilter({ status: search.status === 'submitted' ? undefined : 'submitted' })}
          >
            <Inbox /> 待分诊 {pending}
          </Button>
        }
      />

      {counts.length > 0 && (
        <div className='flex flex-wrap gap-2'>
          {counts.map((c) => (
            <button
              key={c.status}
              type='button'
              className='rounded-md border px-3 py-1.5 text-sm hover:bg-accent'
              aria-pressed={search.status === c.status}
              onClick={() => setFilter({ status: search.status === c.status ? undefined : c.status })}
            >
              {STATUS_LABELS[c.status]}
              <span className='ms-2 text-muted-foreground tabular-nums'>{c.total}</span>
            </button>
          ))}
        </div>
      )}

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜反馈标题' onSubmit={(q) => setFilter({ q })} />
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
          options={optionsOf(STATUS_LABELS)}
          onChange={(v) => setFilter({ status: v as FbStatus | undefined })}
          className='w-40'
        />
        <FilterSelect
          label='类型'
          value={search.kind}
          options={optionsOf(KIND_LABELS)}
          onChange={(v) => setFilter({ kind: v as FbKind | undefined })}
          className='w-36'
        />
        <FilterSelect
          label='优先级'
          value={search.priority}
          options={optionsOf(PRIORITY_LABELS)}
          onChange={(v) => setFilter({ priority: v as FbPriority | undefined })}
          className='w-36'
        />
        <FilterSelect
          label='可见性'
          value={search.visibility}
          options={optionsOf(VISIBILITY_LABELS)}
          onChange={(v) => setFilter({ visibility: v as FbVisibility | undefined })}
          className='w-36'
        />
        <FilterSelect
          label='来源'
          value={search.source}
          options={optionsOf(SOURCE_LABELS)}
          onChange={(v) => setFilter({ source: v as FbSource | undefined })}
          className='w-40'
        />
        <Select value={search.sort ?? 'oldest'} onValueChange={(v) => setFilter({ sort: v as IssueListParams['sort'] })}>
          <SelectTrigger className='h-9 w-48' aria-label='排序'>
            <SelectValue placeholder='排序' />
          </SelectTrigger>
          <SelectContent>
            {(Object.entries(SORT_LABELS) as [NonNullable<IssueListParams['sort']>, string][]).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                排序：{label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>编号</TableHead>
                <TableHead>标题</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>优先级</TableHead>
                <TableHead>可见性</TableHead>
                <TableHead>来源</TableHead>
                <TableHead className='text-right'>票数</TableHead>
                <TableHead>最近活动</TableHead>
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
                    {filtered ? '没有符合条件的反馈' : '还没有收到反馈'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((f) => (
                  <TableRow key={f.id}>
                    <TableCell className='pl-4 font-mono text-xs tabular-nums'>{f.key}</TableCell>
                    <TableCell>
                      <div className='flex max-w-sm flex-col'>
                        <button
                          type='button'
                          className='truncate text-start font-medium hover:underline'
                          onClick={() => setOpenID(f.id)}
                        >
                          {f.title}
                        </button>
                        <span className='truncate text-xs text-muted-foreground'>
                          {f.project_name} · {authorOf(f)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <KindBadge kind={f.kind} />
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={f.status} />
                    </TableCell>
                    <TableCell>
                      <PriorityBadge priority={f.priority} />
                    </TableCell>
                    <TableCell>
                      <VisibilityBadge visibility={f.visibility} />
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      <SourceBadge source={f.source} />
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      <span className='inline-flex items-center gap-1'>
                        <ThumbsUp className='size-3 text-muted-foreground' aria-hidden />
                        {f.vote_count}
                      </span>
                    </TableCell>
                    <TableCell className='whitespace-nowrap tabular-nums'>{formatDateTime(f.last_activity_at)}</TableCell>
                    <TableCell className='pr-4 text-right'>
                      <Button variant='ghost' size='sm' onClick={() => setOpenID(f.id)}>
                        {editable ? '分诊' : '查看'} <ArrowUpRight className='size-4' />
                      </Button>
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
      {openID !== null && <TriageDialog id={openID} editable={editable} onClose={() => setOpenID(null)} />}
    </div>
  )
}

function TriageDialog({ id, editable, onClose }: { id: number; editable: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const detail = useQuery(issueDetailQuery(id))
  const fb = detail.data?.feedback
  const events = detail.data?.events ?? []

  const [status, setStatus] = useState<FbStatus | ''>('')
  const [priority, setPriority] = useState<FbPriority | ''>('')
  const [visibility, setVisibility] = useState<FbVisibility | ''>('')
  const [duplicateOf, setDuplicateOf] = useState('')
  const [note, setNote] = useState('')
  const [internalNote, setInternalNote] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      feedbackApi.triage(id, {
        status: status || undefined,
        priority: priority || undefined,
        visibility: visibility || undefined,
        duplicate_of: duplicateOf ? Number(duplicateOf) : undefined,
        note: note.trim() || undefined,
        internal_note: internalNote.trim() || undefined,
        version: fb?.version ?? 0,
      }),
    onSuccess: (f) => {
      toast.success(`已保存 ${f.key}，当前状态「${STATUS_LABELS[f.status]}」`)
      void qc.invalidateQueries({ queryKey: feedbackKeys.issues })
      onClose()
    },
  })

  const nextStates: FbStatus[] = fb ? [fb.status, ...STATUS_TRANSITIONS[fb.status]] : []
  const needNote = status === 'declined'
  const needDuplicate = status === 'duplicate'
  const blocked = !editable || mutation.isPending || (needNote && !note.trim()) || (needDuplicate && !duplicateOf)

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{fb ? `${fb.key} ${fb.title}` : '加载中…'}</DialogTitle>
          <DialogDescription>
            {fb
              ? `${fb.project_name} · ${KIND_LABELS[fb.kind]} · 由 ${authorOf(fb)} 于 ${formatDateTime(fb.created_at)} 提交`
              : '正在读取反馈详情'}
          </DialogDescription>
        </DialogHeader>

        {detail.isError ? (
          <LoadError onRetry={() => void detail.refetch()} />
        ) : !fb ? (
          <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
        ) : (
          <div className='flex flex-col gap-4'>
            <div className='flex flex-wrap items-center gap-2'>
              <StatusBadge status={fb.status} />
              <PriorityBadge priority={fb.priority} />
              <VisibilityBadge visibility={fb.visibility} />
              <SourceBadge source={fb.source} />
              <span className='text-sm text-muted-foreground tabular-nums'>{fb.vote_count} 人赞同</span>
            </div>

            <section className='rounded-md border p-3'>
              <h3 className='mb-2 text-sm font-medium'>用户描述</h3>
              <p className='whitespace-pre-wrap text-sm'>{fb.body}</p>
              {fb.env && <p className='mt-2 text-xs text-muted-foreground'>环境：{fb.env}</p>}
              {fb.page_url && <p className='mt-1 text-xs break-all text-muted-foreground'>页面：{fb.page_url}</p>}
              {fb.contact && (
                <p className='mt-1 text-xs text-muted-foreground'>
                  联系方式：{fb.contact}
                  <span className='ms-1'>（提交者自愿填写，不下发前台）</span>
                </p>
              )}
            </section>

            <section>
              <h3 className='mb-2 text-sm font-medium'>处理过程</h3>
              <ol className='flex flex-col gap-2'>
                {events.map((e) => (
                  <li key={e.id} className='rounded-md border px-3 py-2 text-sm'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='font-medium'>
                        {e.from_status ? `${STATUS_LABELS[e.from_status as FbStatus]} → ` : ''}
                        {STATUS_LABELS[e.to_status]}
                      </span>
                      <span className='text-xs text-muted-foreground tabular-nums'>{formatDateTime(e.created_at)}</span>
                      {e.actor_name && <span className='text-xs text-muted-foreground'>{e.actor_name}</span>}
                    </div>
                    {e.note && <p className='mt-1 whitespace-pre-wrap'>{e.note}</p>}
                    {e.internal_note && (
                      <p className='mt-1 whitespace-pre-wrap rounded bg-muted px-2 py-1 text-xs'>内部备注：{e.internal_note}</p>
                    )}
                  </li>
                ))}
              </ol>
            </section>

            <section className='flex flex-col gap-4 rounded-md border p-3'>
              <h3 className='text-sm font-medium'>分诊</h3>
              <div className='grid gap-4 sm:grid-cols-3'>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='fb-status'>下一步状态</Label>
                  <Select value={status || fb.status} onValueChange={(v) => setStatus(v as FbStatus)} disabled={!editable}>
                    <SelectTrigger id='fb-status' className='h-9 w-full'>
                      <SelectValue placeholder='请选择状态' />
                    </SelectTrigger>
                    <SelectContent>
                      {nextStates.map((s) => (
                        <SelectItem key={s} value={s}>
                          {STATUS_LABELS[s]}
                          {s === fb.status ? '（保持不变）' : ''}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='fb-priority'>优先级</Label>
                  <Select
                    value={priority || fb.priority}
                    onValueChange={(v) => setPriority(v as FbPriority)}
                    disabled={!editable}
                  >
                    <SelectTrigger id='fb-priority' className='h-9 w-full'>
                      <SelectValue placeholder='请选择优先级' />
                    </SelectTrigger>
                    <SelectContent>
                      {optionsOf(PRIORITY_LABELS).map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {o.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='fb-visibility'>可见性</Label>
                  <Select
                    value={visibility || fb.visibility}
                    onValueChange={(v) => setVisibility(v as FbVisibility)}
                    disabled={!editable}
                  >
                    <SelectTrigger id='fb-visibility' className='h-9 w-full'>
                      <SelectValue placeholder='请选择可见性' />
                    </SelectTrigger>
                    <SelectContent>
                      {optionsOf(VISIBILITY_LABELS).map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {o.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {needDuplicate && (
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='fb-duplicate'>主反馈 ID</Label>
                  <Input
                    id='fb-duplicate'
                    className='h-9'
                    inputMode='numeric'
                    placeholder='请输入同项目内主反馈的数字 ID'
                    value={duplicateOf}
                    onChange={(e) => setDuplicateOf(e.target.value.replace(/\D/g, ''))}
                    disabled={!editable}
                  />
                </div>
              )}

              <div className='flex flex-col gap-2'>
                <Label htmlFor='fb-note'>公开说明{needNote && <span className='text-destructive'> *</span>}</Label>
                <Textarea
                  id='fb-note'
                  rows={3}
                  placeholder={needNote ? '暂不采纳必须写明可公开的原因' : '请输入会展示给提交者的说明，留空则自动生成一句状态变更提示'}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  disabled={!editable}
                />
              </div>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='fb-internal'>内部备注</Label>
                <Textarea
                  id='fb-internal'
                  rows={2}
                  placeholder='请输入只有后台可见的备注，不会下发给用户'
                  value={internalNote}
                  onChange={(e) => setInternalNote(e.target.value)}
                  disabled={!editable}
                />
              </div>
            </section>
          </div>
        )}

        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            关闭
          </Button>
          <WithReason reason={editable ? null : '当前角色只能查看'}>
            <Button type='button' disabled={blocked} onClick={() => mutation.mutate()}>
              {mutation.isPending ? '保存中…' : '保存分诊'}
            </Button>
          </WithReason>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
