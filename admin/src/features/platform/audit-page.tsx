import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { CircleCheck, Link2, Link2Off, ShieldAlert, ShieldCheck, Unlink } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
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
  Input,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { LoadError, PageHeading } from '@/features/access/shared'
import { FilterSelect, ResetButton, SearchBox } from '@/features/content/components/list-toolbar'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  actionLabel,
  auditApi,
  auditFacetsQuery,
  auditListQuery,
  dayBound,
  targetLabel,
  type AuditDetail,
  type AuditItem,
  type AuditVerifyResult,
} from './audit-api'

/**
 * 审计日志（docs/06 第 34 节，A4-3 / S4-7）：所有后台写操作的记录，含改动前后对比。
 * 只读页面（迁移 00003 没有给 audit 生成 .edit 权限点）。哈希链校验能发现记录被改动或删除。
 */
export function AuditPage() {
  const can = useCan()
  const visible = can('audit.view')
  const search = useSearch({ from: '/_authenticated/audit' })
  const navigate = useNavigate({ from: '/audit' })
  const params = {
    q: search.q,
    action: search.action,
    target_type: search.target_type,
    admin_id: search.admin_id,
    from: dayBound(search.from ?? ''),
    to: dayBound(search.to ?? '', true),
    page: search.page ?? 1,
    page_size: search.page_size ?? DEFAULT_PAGE_SIZE,
  }
  const list = useQuery({ ...auditListQuery(params), enabled: visible })
  const facets = useQuery({ ...auditFacetsQuery, enabled: visible })
  const [detailID, setDetailID] = useState<number | null>(null)
  const [verified, setVerified] = useState<AuditVerifyResult | null>(null)

  const verify = useMutation({
    mutationFn: () => auditApi.verify(),
    onSuccess: (res) => setVerified(res),
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='审计日志' />
  const filtered = !!(search.q || search.action || search.target_type || search.admin_id || search.from || search.to)

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='审计日志'
        description='所有后台写操作的记录，含改动前后对比。改动按系统原字段名记录（英文，给技术排查用）；密码与密钥原文不记。本页只读，记录不可修改或删除。'
        actions={
          <Button variant='outline' disabled={verify.isPending} onClick={() => verify.mutate()}>
            <ShieldCheck /> {verify.isPending ? '校验中…' : '校验完整性'}
          </Button>
        }
      />

      <div className='flex flex-wrap items-center gap-2'>
        <SearchBox
          value={search.q}
          placeholder='搜操作人、动作或对象'
          onSubmit={(q) => void navigate({ search: (prev) => withFilter(prev, { q }) })}
        />
        <FilterSelect
          label='动作'
          className='w-44'
          value={search.action}
          options={(facets.data?.actions ?? []).map((a) => ({
            value: a.value,
            label: `${actionLabel(a.value)}（${a.count}）`,
          }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { action: v }) })}
        />
        <FilterSelect
          label='对象'
          className='w-40'
          value={search.target_type}
          options={(facets.data?.target_types ?? []).map((t) => ({
            value: t.value,
            label: `${targetLabel(t.value)}（${t.count}）`,
          }))}
          onChange={(v) => void navigate({ search: (prev) => withFilter(prev, { target_type: v }) })}
        />
        <FilterSelect
          label='操作人'
          className='w-40'
          value={search.admin_id ? String(search.admin_id) : undefined}
          options={(facets.data?.admins ?? []).map((a) => ({
            value: String(a.id),
            label: `${a.username}（${a.count}）`,
          }))}
          onChange={(v) =>
            void navigate({ search: (prev) => withFilter(prev, { admin_id: v ? Number(v) : undefined }) })
          }
        />
        <div className='flex items-center gap-1'>
          <label htmlFor='audit-from' className='text-sm text-muted-foreground'>
            从
          </label>
          <Input
            id='audit-from'
            type='date'
            className='h-9 w-36'
            value={search.from ?? ''}
            onChange={(e) => void navigate({ search: (prev) => withFilter(prev, { from: e.target.value || undefined }) })}
          />
          <label htmlFor='audit-to' className='text-sm text-muted-foreground'>
            到
          </label>
          <Input
            id='audit-to'
            type='date'
            className='h-9 w-36'
            value={search.to ?? ''}
            onChange={(e) => void navigate({ search: (prev) => withFilter(prev, { to: e.target.value || undefined }) })}
          />
        </div>
        <ResetButton disabled={!filtered} onClick={() => void navigate({ search: {} })} />
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>时间</TableHead>
                <TableHead>操作人</TableHead>
                <TableHead>动作</TableHead>
                <TableHead>对象</TableHead>
                <TableHead>来源</TableHead>
                <TableHead className='w-16'>完整性</TableHead>
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
                    {filtered ? '没有符合条件的记录' : '还没有审计记录'}
                  </TableCell>
                </TableRow>
              ) : (
                list.data.list.map((a) => <AuditRow key={a.id} item={a} onOpen={() => setDetailID(a.id)} />)
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

      {detailID !== null && <DetailDialog id={detailID} onClose={() => setDetailID(null)} />}
      {verified && <VerifyDialog result={verified} onClose={() => setVerified(null)} />}
    </div>
  )
}

function AuditRow({ item, onOpen }: { item: AuditItem; onOpen: () => void }) {
  return (
    <TableRow>
      <TableCell className='pl-4 text-sm whitespace-nowrap tabular-nums'>
        {new Date(item.created_at).toLocaleString('zh-CN', { hour12: false })}
      </TableCell>
      <TableCell className='text-sm'>{item.username}</TableCell>
      <TableCell>
        <div className='flex flex-col'>
          <span className='text-sm'>{actionLabel(item.action)}</span>
          <code className='text-xs text-muted-foreground'>{item.action}</code>
        </div>
      </TableCell>
      <TableCell className='text-sm'>
        {item.target_type ? (
          <>
            {targetLabel(item.target_type)}
            {item.target_id && <code className='ms-1 text-xs text-muted-foreground'>#{item.target_id}</code>}
          </>
        ) : (
          <span className='text-muted-foreground'>—</span>
        )}
      </TableCell>
      <TableCell className='text-xs text-muted-foreground tabular-nums'>{item.ip_prefix || '—'}</TableCell>
      <TableCell>
        {item.chained ? (
          <span title='已接入哈希链'>
            <Link2 className='size-4 text-emerald-600' aria-label='已接入哈希链' />
          </span>
        ) : (
          <span title='启用哈希链之前的记录'>
            <Link2Off className='size-4 text-muted-foreground' aria-label='启用哈希链之前的记录' />
          </span>
        )}
      </TableCell>
      <TableCell className='pr-4'>
        <Button variant='ghost' size='sm' onClick={onOpen}>
          详情
        </Button>
      </TableCell>
    </TableRow>
  )
}

/** 详情：改动前后对比 + 哈希链信息。 */
function DetailDialog({ id, onClose }: { id: number; onClose: () => void }) {
  const detail = useQuery({
    queryKey: ['platform', 'audit', 'detail', id],
    queryFn: () => auditApi.detail(id),
  })
  const d: AuditDetail | undefined = detail.data

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle className='flex flex-wrap items-center gap-2'>
            {d ? actionLabel(d.action) : '审计详情'}
            {d && <code className='text-xs font-normal text-muted-foreground'>{d.action}</code>}
          </DialogTitle>
          <DialogDescription>
            改动内容按系统原字段名显示（英文），密码与密钥类字段的值已替换为 ***。
          </DialogDescription>
        </DialogHeader>

        {detail.isError ? (
          <LoadError onRetry={() => void detail.refetch()} />
        ) : !d ? (
          <p className='text-sm text-muted-foreground'>加载中…</p>
        ) : (
          <div className='flex flex-col gap-3'>
            <dl className='grid grid-cols-2 gap-2 text-sm sm:grid-cols-4'>
              <Field label='时间' value={new Date(d.created_at).toLocaleString('zh-CN', { hour12: false })} />
              <Field label='操作人' value={d.username} />
              <Field label='对象' value={d.target_type ? `${targetLabel(d.target_type)} #${d.target_id}` : '—'} />
              <Field label='来源 IP 段' value={d.ip_prefix || '—'} />
            </dl>
            <Field label='请求编号' value={d.request_id || '—'} mono />

            <div className='grid gap-3 sm:grid-cols-2'>
              <DiffPane title='改动前' value={d.before} emptyText='新建操作，没有改动前的内容' />
              <DiffPane title='改动后' value={d.after} emptyText='删除操作，没有改动后的内容' />
            </div>

            <div className='flex flex-col gap-1 rounded-md border p-3 text-xs'>
              <div className='flex items-center gap-2'>
                {d.chained ? (
                  <>
                    <Link2 className='size-3.5 text-emerald-600' aria-hidden />
                    <span className='font-medium'>已接入哈希链</span>
                  </>
                ) : (
                  <>
                    <Link2Off className='size-3.5 text-muted-foreground' aria-hidden />
                    <span className='font-medium'>启用哈希链之前的记录</span>
                  </>
                )}
              </div>
              {d.chained && (
                <>
                  <span className='text-muted-foreground'>
                    前驱哈希：<code className='break-all'>{d.prev_hash || '（链首，没有前驱）'}</code>
                  </span>
                  <span className='text-muted-foreground'>
                    本条哈希：<code className='break-all'>{d.hash}</code>
                  </span>
                </>
              )}
            </div>
          </div>
        )}

        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <dt className='text-xs text-muted-foreground'>{label}</dt>
      <dd className={mono ? 'font-mono text-xs break-all' : 'text-sm'}>{value}</dd>
    </div>
  )
}

function DiffPane({ title, value, emptyText }: { title: string; value: unknown; emptyText: string }) {
  const empty = value === null || value === undefined
  return (
    <div className='flex flex-col gap-1'>
      <span className='text-xs text-muted-foreground'>{title}</span>
      {empty ? (
        <p className='rounded-md border border-dashed p-3 text-xs text-muted-foreground'>{emptyText}</p>
      ) : (
        <pre className='max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs'>
          {JSON.stringify(value, null, 2)}
        </pre>
      )}
    </div>
  )
}

/** 哈希链校验结果。 */
function VerifyDialog({ result, onClose }: { result: AuditVerifyResult; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            完整性校验
            {result.ok ? <Badge variant='secondary'>通过</Badge> : <Badge variant='destructive'>发现异常</Badge>}
          </DialogTitle>
          <DialogDescription>
            每条记录都带上一条的哈希，所以改动或删除任何一条都会让链对不上。这能发现「改完库再抹痕迹」。
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-2 text-sm'>
          {result.ok ? (
            <div className='flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3'>
              <CircleCheck className='mt-0.5 size-4 shrink-0 text-emerald-600' aria-hidden />
              <span>
                校验了 {result.checked} 条记录，哈希链完整，没有被改动或删除的痕迹。
              </span>
            </div>
          ) : (
            <div className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3'>
              <ShieldAlert className='mt-0.5 size-4 shrink-0 text-destructive' aria-hidden />
              <div className='flex flex-col gap-1'>
                <span className='font-medium'>
                  在第 {result.checked + 1} 条处发现异常（记录 #{result.broken?.id}）
                </span>
                <span>{result.broken?.reason}</span>
                <span className='text-muted-foreground'>
                  这条记录是 {result.broken?.at && new Date(result.broken.at).toLocaleString('zh-CN', { hour12: false })}
                  的「{result.broken && actionLabel(result.broken.action)}」。请联系技术排查数据库是否被直接改动。
                </span>
              </div>
            </div>
          )}

          <dl className='grid grid-cols-2 gap-2'>
            <Field label='已接入哈希链' value={`${result.chained} 条`} />
            <Field label='本次校验' value={`${result.checked} 条`} />
            <Field
              label='最早'
              value={result.chained > 0 ? new Date(result.first_at).toLocaleString('zh-CN', { hour12: false }) : '—'}
            />
            <Field
              label='最晚'
              value={result.chained > 0 ? new Date(result.last_at).toLocaleString('zh-CN', { hour12: false }) : '—'}
            />
          </dl>

          {result.unchained > 0 && (
            <div className='flex items-start gap-2 rounded-md border p-3 text-xs'>
              <Unlink className='mt-0.5 size-3.5 shrink-0 text-muted-foreground' aria-hidden />
              <span className='text-muted-foreground'>
                另有 {result.unchained} 条是启用哈希链之前的历史记录。它们没有哈希可以比对，
                <span className='font-medium'>不参与校验</span>，列表里标为未接入链。
              </span>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
