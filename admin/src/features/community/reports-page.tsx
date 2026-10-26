import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { useCan } from '@/stores/auth-store'
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
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  type Report,
  REPORT_REASON_LABELS,
  REPORT_REASONS,
  REPORT_STATUS_LABELS,
  REPORT_STATUSES,
  REPORT_TARGET_LABELS,
  REPORT_TARGETS,
  type ReportStatus,
  type ReportTarget,
  type ReportReason,
  reportApi,
  reportKeys,
  reportListQuery,
} from './api'

const STATUS_VARIANT: Record<ReportStatus, 'secondary' | 'default' | 'outline'> = {
  pending: 'secondary',
  resolved: 'default',
  rejected: 'outline',
}

/** 举报队列（docs/06 第 25 节）：按状态 / 对象 / 原因筛选，保存处理结果（已处理 / 已驳回 + 说明）。 */
export function ReportsPage() {
  const can = useCan()
  const visible = can('reports.view')
  const search = useSearch({ from: '/_authenticated/reports' })
  const navigate = useNavigate({ from: '/reports' })
  const params = {
    q: search.q,
    status: search.status,
    target_type: search.target_type,
    reason: search.reason,
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...reportListQuery(params), enabled: visible })
  const [handling, setHandling] = useState<Report | null>(null)

  if (!visible) return <NoPermission title='举报队列' />
  const editable = can('reports.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const filtered = !!(search.q || search.status || search.target_type || search.reason)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='举报队列'
        description='用户对评论 / 弹幕 / 剧 / 评论图片的举报。核对后保存处理结果（已处理或已驳回，可留处理说明）。处罚举报人或被举报人（改账号状态）在「用户」页进行。'
        readonly={!editable}
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox value={search.q} placeholder='搜说明或举报人' onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })} />
        <FilterSelect
          label='状态'
          value={search.status}
          options={REPORT_STATUSES.map((s) => ({ value: s, label: REPORT_STATUS_LABELS[s] }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { status: v as ReportStatus | undefined }) })}
        />
        <FilterSelect
          label='对象'
          value={search.target_type}
          options={REPORT_TARGETS.map((t) => ({ value: t, label: REPORT_TARGET_LABELS[t] }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { target_type: v as ReportTarget | undefined }) })}
        />
        <FilterSelect
          label='原因'
          value={search.reason}
          options={REPORT_REASONS.map((r) => ({ value: r, label: REPORT_REASON_LABELS[r] }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { reason: v as ReportReason | undefined }) })}
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
                <TableHead className='pl-4'>举报人</TableHead>
                <TableHead>对象</TableHead>
                <TableHead>原因</TableHead>
                <TableHead>说明 / 处理</TableHead>
                <TableHead>状态</TableHead>
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
              ) : list.data.list.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className='h-24 text-center text-muted-foreground'>
                    {filtered ? '没有符合条件的举报' : '还没有举报'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((r) => (
                  <TableRow key={r.id}>
                    <TableCell className='pl-4'>{r.reporter.is_guest ? '游客' : r.reporter.nickname || '用户'}</TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {REPORT_TARGET_LABELS[r.target_type]} <span className='text-muted-foreground tabular-nums'>#{r.target_id}</span>
                    </TableCell>
                    <TableCell>
                      <Badge variant='outline'>{REPORT_REASON_LABELS[r.reason]}</Badge>
                    </TableCell>
                    <TableCell className='max-w-xs'>
                      {r.detail ? <div className='truncate' title={r.detail}>{r.detail}</div> : <span className='text-muted-foreground'>—</span>}
                      {r.handle_note && <div className='truncate text-xs text-muted-foreground' title={r.handle_note}>处理：{r.handle_note}</div>}
                    </TableCell>
                    <TableCell>
                      <Badge variant={STATUS_VARIANT[r.status]}>{REPORT_STATUS_LABELS[r.status]}</Badge>
                    </TableCell>
                    <TableCell className='text-sm whitespace-nowrap tabular-nums'>{formatDateTime(r.created_at).slice(0, 16)}</TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end'>
                        <WithReason reason={readonlyReason}>
                          <Button variant='outline' size='sm' disabled={!editable} onClick={() => setHandling(r)}>
                            {r.status === 'pending' ? '处理' : '重新处理'}
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
              search: (prev) => ({ ...prev, page: page > 1 ? page : undefined, page_size: page_size === DEFAULT_PAGE_SIZE ? undefined : page_size }),
            })
          }
        />
      )}

      {handling && <HandleReportDialog report={handling} onClose={() => setHandling(null)} />}
    </div>
  )
}

const handleSchema = z.object({
  status: z.enum(['resolved', 'rejected']),
  note: z.string().trim().max(500, '处理说明最多 500 个字'),
})

function HandleReportDialog({ report, onClose }: { report: Report; onClose: () => void }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof handleSchema>, unknown, z.output<typeof handleSchema>>({
    resolver: zodResolver(handleSchema),
    defaultValues: { status: report.status === 'rejected' ? 'rejected' : 'resolved', note: report.handle_note },
  })
  const mutation = useMutation({
    mutationFn: (v: z.output<typeof handleSchema>) => reportApi.handle(report.id, v),
    onSuccess: (saved) => {
      toast.success(saved.status === 'resolved' ? '已标记为已处理' : '已驳回')
      void qc.invalidateQueries({ queryKey: reportKeys.all })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>处理举报</DialogTitle>
          <DialogDescription>
            {REPORT_TARGET_LABELS[report.target_type]} #{report.target_id} · {REPORT_REASON_LABELS[report.reason]}
            {report.detail && `｜${report.detail}`}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <SelectField
              control={form.control}
              name='status'
              label='处理结果'
              required
              placeholder='请选择处理结果'
              items={[
                { value: 'resolved', label: '已处理' },
                { value: 'rejected', label: '已驳回（举报不成立）' },
              ]}
            />
            <TextField
              control={form.control}
              name='note'
              label='处理说明'
              placeholder='请输入处理说明（可留空）'
              multiline
              description='仅内部可见，用于记录处理依据。'
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
