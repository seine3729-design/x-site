import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Checkbox,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsList,
  TabsTrigger,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  type ModerationAction,
  type ModerationItem,
  type ModerationKind,
  type ModerationStatus,
  MODERATION_STATUS_LABELS,
  MODERATION_STATUSES,
  commentPolicyQuery,
  moderationApi,
  moderationKeys,
  moderationListQuery,
} from './api'

const STATUS_VARIANT: Record<ModerationStatus, 'secondary' | 'default' | 'outline'> = {
  pending: 'secondary',
  visible: 'default',
  hidden: 'outline',
}
const ACTION_LABELS: Record<ModerationAction, string> = { approve: '通过', hide: '隐藏', delete: '删除' }

/** 评论与弹幕审核（docs/06 第 24 节）：两区（评论 / 弹幕），先审后显开关，按状态 / 被举报 / 含屏蔽词筛选，通过 / 隐藏 / 删除（支持批量）。 */
export function ModerationPage() {
  const can = useCan()
  const visible = can('moderation.view')
  const search = useSearch({ from: '/_authenticated/moderation' })
  const navigate = useNavigate({ from: '/moderation' })
  const kind: ModerationKind = search.kind ?? 'comment'
  const params = {
    kind,
    status: search.status,
    reported: search.reported,
    blockword: search.blockword,
    q: search.q,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...moderationListQuery(params), enabled: visible })
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [confirm, setConfirm] = useState<{ action: ModerationAction; ids: number[] } | null>(null)
  const qc = useQueryClient()
  const resetSelection = () => setSelected(new Set())
  const act = useMutation({
    mutationFn: (v: { kind: ModerationKind; action: ModerationAction; ids: number[] }) => moderationApi.act(v),
    onSuccess: (res, v) => {
      toast.success(`已${ACTION_LABELS[v.action]} ${res.affected} 条`)
      resetSelection()
      setConfirm(null)
      void qc.invalidateQueries({ queryKey: moderationKeys.all })
    },
  })

  if (!visible) return <NoPermission title='评论与弹幕审核' />
  const editable = can('moderation.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.status || search.reported || search.blockword || search.q)
  const rows = list.data?.list ?? []

  const go = (patch: Record<string, unknown>) => {
    resetSelection()
    void navigate({ search: (prev) => withFilter(prev, patch) })
  }
  const toggleOne = (id: number, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  const allOnPage = rows.length > 0 && rows.every((r) => selected.has(r.id))
  const toggleAll = (on: boolean) => setSelected(on ? new Set(rows.map((r) => r.id)) : new Set())

  const selectedIds = [...selected]
  const runAction = (action: ModerationAction, ids: number[]) => {
    if (action === 'delete') setConfirm({ action, ids })
    else act.mutate({ kind, action, ids })
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='评论与弹幕审核'
        description='命中「先送审核」屏蔽词或开启先审后显时，内容进入等待审核。通过后显示、隐藏后不再展示、删除后移除（评论可从回收站还原，弹幕永久删除）。'
        readonly={!editable}
        actions={<CommentPolicyToggle editable={editable} />}
      />

      <Tabs value={kind} onValueChange={(v) => go({ kind: v, status: undefined, reported: undefined, blockword: undefined, q: undefined })}>
        <TabsList>
          <TabsTrigger value='comment'>评论</TabsTrigger>
          <TabsTrigger value='danmaku'>弹幕</TabsTrigger>
        </TabsList>
      </Tabs>

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜内容或用户名' onSubmit={(q) => go({ q })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={MODERATION_STATUSES.map((s) => ({ value: s, label: MODERATION_STATUS_LABELS[s] }))}
          onChange={(v) => go({ status: v as ModerationStatus | undefined })}
        />
        <Button variant={search.reported ? 'default' : 'outline'} size='sm' onClick={() => go({ reported: search.reported ? undefined : true })}>
          只看被举报
        </Button>
        <Button variant={search.blockword ? 'default' : 'outline'} size='sm' onClick={() => go({ blockword: search.blockword ? undefined : true })}>
          只看含屏蔽词
        </Button>
        <ResetButton disabled={!filtered} onClick={() => go({ status: undefined, reported: undefined, blockword: undefined, q: undefined })} />
      </div>

      {editable && selectedIds.length > 0 && (
        <div className='flex flex-wrap items-center gap-2 rounded-md border bg-muted/50 px-3 py-2'>
          <span className='text-sm'>已选 {selectedIds.length} 条</span>
          <Button size='sm' variant='outline' onClick={() => runAction('approve', selectedIds)} disabled={act.isPending}>
            通过
          </Button>
          <Button size='sm' variant='outline' onClick={() => runAction('hide', selectedIds)} disabled={act.isPending}>
            隐藏
          </Button>
          <Button size='sm' variant='destructive' onClick={() => runAction('delete', selectedIds)} disabled={act.isPending}>
            删除
          </Button>
        </div>
      )}

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='w-10 pl-4'>
                  {editable && <Checkbox checked={allOnPage} onCheckedChange={(v) => toggleAll(!!v)} aria-label='全选本页' />}
                </TableHead>
                <TableHead>内容</TableHead>
                <TableHead>作者</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>举报</TableHead>
                <TableHead>时间</TableHead>
                <TableHead className='pr-4'>
                  <span className='sr-only'>操作</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={7} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的内容' : '没有待审核内容'}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((item) => (
                  <ModerationRow
                    key={item.id}
                    item={item}
                    kind={kind}
                    editable={editable}
                    readonlyReason={readonlyReason}
                    checked={selected.has(item.id)}
                    onCheck={(on) => toggleOne(item.id, on)}
                    onAction={(action) => runAction(action, [item.id])}
                  />
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
          onChange={(page, page_size) => {
            resetSelection()
            void navigate({
              search: (prev) => ({ ...prev, page: page > 1 ? page : undefined, page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size }),
            })
          }}
        />
      )}

      {confirm && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setConfirm(null)}
          title={`删除 ${confirm.ids.length} 条${kind === 'comment' ? '评论' : '弹幕'}`}
          desc={kind === 'comment' ? '评论删除后移入回收站，30 天内可还原。' : '弹幕永久删除，不可恢复。'}
          cancelBtnText='取消'
          confirmText={act.isPending ? '删除中…' : '删除'}
          destructive
          isLoading={act.isPending}
          handleConfirm={() => act.mutate({ kind, action: 'delete', ids: confirm.ids })}
        />
      )}
    </div>
  )
}

function ModerationRow({
  item,
  kind,
  editable,
  readonlyReason,
  checked,
  onCheck,
  onAction,
}: {
  item: ModerationItem
  kind: ModerationKind
  editable: boolean
  readonlyReason: string | null
  checked: boolean
  onCheck: (on: boolean) => void
  onAction: (action: ModerationAction) => void
}) {
  return (
    <TableRow>
      <TableCell className='pl-4'>
        {editable && <Checkbox checked={checked} onCheckedChange={(v) => onCheck(!!v)} aria-label={`选择 ${item.id}`} />}
      </TableCell>
      <TableCell className='max-w-md'>
        <div className='break-words whitespace-pre-wrap'>{item.content}</div>
        <div className='mt-1 flex flex-wrap items-center gap-1'>
          {kind === 'danmaku' && item.time_ms != null && <span className='text-xs text-muted-foreground tabular-nums'>{Math.floor(item.time_ms / 1000)}s</span>}
          {item.hit_words.map((w) => (
            <Badge key={w} variant='destructive' className='text-xs'>
              {w}
            </Badge>
          ))}
        </div>
      </TableCell>
      <TableCell className='whitespace-nowrap'>{item.author.is_guest ? '游客' : item.author.nickname || '用户'}</TableCell>
      <TableCell>
        <Badge variant={STATUS_VARIANT[item.status]}>{MODERATION_STATUS_LABELS[item.status]}</Badge>
      </TableCell>
      <TableCell className='tabular-nums'>{item.report_count > 0 ? <Badge variant='destructive'>{item.report_count}</Badge> : <span className='text-muted-foreground'>—</span>}</TableCell>
      <TableCell className='text-sm whitespace-nowrap tabular-nums'>{formatDateTime(item.created_at).slice(0, 16)}</TableCell>
      <TableCell className='pr-4'>
        <div className='flex justify-end gap-1'>
          <WithReason reason={readonlyReason}>
            <Button variant='ghost' size='sm' disabled={!editable || item.status === 'visible'} onClick={() => onAction('approve')}>
              通过
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button variant='ghost' size='sm' disabled={!editable || item.status === 'hidden'} onClick={() => onAction('hide')}>
              隐藏
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button variant='ghost' size='sm' disabled={!editable} onClick={() => onAction('delete')}>
              删除
            </Button>
          </WithReason>
        </div>
      </TableCell>
    </TableRow>
  )
}

function CommentPolicyToggle({ editable }: { editable: boolean }) {
  const qc = useQueryClient()
  const policy = useQuery(commentPolicyQuery())
  const mutation = useMutation({
    mutationFn: (pre: boolean) => moderationApi.setPolicy({ pre_moderation: pre }),
    onSuccess: (saved) => {
      toast.success(saved.pre_moderation ? '已开启：新评论先审后显' : '已关闭先审后显')
      void qc.invalidateQueries({ queryKey: moderationKeys.policy })
    },
  })
  return (
    <div className='flex items-center gap-2 rounded-md border px-3 py-2'>
      <Switch
        id='comment-pre-moderation'
        checked={!!policy.data?.pre_moderation}
        disabled={!editable || policy.isPending || mutation.isPending}
        onCheckedChange={(v) => mutation.mutate(v)}
      />
      <label htmlFor='comment-pre-moderation' className='text-sm'>
        评论先审后显
      </label>
    </div>
  )
}
