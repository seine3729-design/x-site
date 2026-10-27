import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
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
import { LoadError, PageHeading } from '@/features/access/shared'
import { Pager } from '@/features/content/components/pager'
import { DEFAULT_PAGE_SIZE, withFilter } from '@/features/content/search'
import {
  DAYS_OPTIONS,
  type DramaListParams,
  type DramaRow,
  type DramaSort,
  DRAMA_SORT_LABELS,
  dramaAnalyticsListQuery,
  formatInt,
  formatWatch,
  statusLabel,
} from './api'

const ROUTE = '/_authenticated/analytics/dramas/'

/** 剧集分析列表（docs/06 第 2 节，A4-1）。窗口口径同看板，点剧名进单剧分析。 */
export function DramaAnalyticsPage() {
  const can = useCan()
  const visible = can('analytics_dramas.view')
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: '/analytics/dramas/' })
  const days = search.days ?? 7
  const sort: DramaSort = search.sort ?? 'viewers'
  const params: DramaListParams = { days, sort, page: search.page ?? 1, page_size: search.page_size ?? DEFAULT_PAGE_SIZE }
  const list = useQuery({ ...dramaAnalyticsListQuery(params), enabled: visible })

  if (!visible) return <NoPermission title='剧集分析' />
  const setFilter = (patch: Partial<typeof search>) => void navigate({ search: (prev) => withFilter(prev, patch) })

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading title='剧集分析' description={`最近 ${days} 天（北京时间，到昨天）按窗口聚合观看规模，点剧名查看单剧留存`} />
      <div className='flex flex-wrap items-center gap-2'>
        <Select value={String(days)} onValueChange={(v) => setFilter({ days: Number(v) === 7 ? undefined : Number(v) })}>
          <SelectTrigger className='h-9 w-32' aria-label='时间范围'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {DAYS_OPTIONS.map((d) => (
              <SelectItem key={d} value={String(d)}>
                最近 {d} 天
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={sort} onValueChange={(v) => setFilter({ sort: v === 'viewers' ? undefined : (v as DramaSort) })}>
          <SelectTrigger className='h-9 w-40' aria-label='排序'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(Object.entries(DRAMA_SORT_LABELS) as [DramaSort, string][]).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {list.isError ? <LoadError onRetry={() => void list.refetch()} /> : <DramaAnalyticsTable rows={list.data?.list} loading={list.isPending} />}
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
    </div>
  )
}

function DramaAnalyticsTable({ rows, loading }: { rows: DramaRow[] | undefined; loading: boolean }) {
  const list = rows ?? []
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted'>
            <TableHead className='pl-4'>编号</TableHead>
            <TableHead>剧名</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className='text-right'>最新集</TableHead>
            <TableHead className='text-right'>观看人次</TableHead>
            <TableHead className='text-right'>起播次数</TableHead>
            <TableHead className='text-right'>有效观看</TableHead>
            <TableHead className='pr-4'>上线</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                加载中…
              </TableCell>
            </TableRow>
          ) : list.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className='h-24 text-center text-muted-foreground'>
                所选窗口内暂无观看数据
              </TableCell>
            </TableRow>
          ) : (
            list.map((d) => (
              <TableRow key={d.id}>
                <TableCell className='pl-4 tabular-nums'>#{d.id}</TableCell>
                <TableCell>
                  <Link to='/analytics/dramas/$id' params={{ id: String(d.id) }} className='font-medium hover:underline'>
                    {d.title}
                  </Link>
                </TableCell>
                <TableCell>{statusLabel(d.status)}</TableCell>
                <TableCell className='text-right tabular-nums'>{d.latest_episode}</TableCell>
                <TableCell className='text-right tabular-nums'>{formatInt(d.viewers)}</TableCell>
                <TableCell className='text-right tabular-nums'>{formatInt(d.play_starts)}</TableCell>
                <TableCell className='text-right tabular-nums'>{formatWatch(d.watch_seconds)}</TableCell>
                <TableCell className='pr-4'>{d.is_new && <Badge variant='secondary'>新</Badge>}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}
