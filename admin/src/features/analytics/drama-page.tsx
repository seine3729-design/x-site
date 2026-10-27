import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { formatDateTime } from '@/lib/format'
import { HttpError } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { LoadError, PageHeading } from '@/features/access/shared'
import { DAYS_OPTIONS, deltaPct, dramaAnalyticsQuery, formatInt, formatWatch, statusLabel } from './api'

const ROUTE = '/_authenticated/analytics/dramas/$id'

/** 单剧分析（docs/06 第 3 节，A4-1）。集间留存曲线 + 窗口汇总 + 每日趋势 + 集明细。 */
export function DramaAnalyticsDetailPage() {
  const can = useCan()
  const visible = can('analytics_dramas.view')
  const { id } = useParams({ from: ROUTE })
  const [days, setDays] = useState(7)
  const dramaId = Number(id)
  const valid = Number.isInteger(dramaId) && dramaId > 0
  const q = useQuery({ ...dramaAnalyticsQuery(dramaId, days), enabled: visible && valid })

  if (!visible) return <NoPermission title='剧集分析' />
  const d = q.data
  const notFound = !valid || (q.error instanceof HttpError && q.error.status === 404)
  const back = (
    <Button asChild variant='outline' size='sm'>
      <Link to='/analytics/dramas'>
        <ArrowLeft className='size-4' /> 返回列表
      </Link>
    </Button>
  )

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title={d ? `剧集分析 · ${d.title}` : '剧集分析'}
        description={d ? `${d.from} ~ ${d.to}（北京时间，到昨天）` : undefined}
        actions={
          <div className='flex items-center gap-2'>
            <Select value={String(days)} onValueChange={(v) => setDays(Number(v))}>
              <SelectTrigger className='h-9 w-32' aria-label='时间范围'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DAYS_OPTIONS.map((x) => (
                  <SelectItem key={x} value={String(x)}>
                    最近 {x} 天
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {back}
          </div>
        }
      />
      {notFound ? (
        <div className='flex flex-col items-center gap-3 rounded-md border border-dashed py-16 text-sm text-muted-foreground'>
          <span>这部剧不存在或已被彻底删除</span>
          {back}
        </div>
      ) : q.isError ? (
        <LoadError onRetry={() => void q.refetch()} />
      ) : q.isPending || !d ? (
        <Skeleton className='h-96 w-full' />
      ) : (
        <>
          <div className='flex flex-wrap items-center gap-x-6 gap-y-1 text-sm text-muted-foreground'>
            <span className='flex items-center gap-1'>
              状态：<Badge variant='secondary'>{statusLabel(d.status)}</Badge>
            </span>
            <span>
              最新集：<span className='text-foreground tabular-nums'>{d.latest_episode}</span>
            </span>
            <span>
              上架天数：<span className='text-foreground tabular-nums'>{d.online_days}</span> 天
            </span>
            <span>
              创建：<span className='text-foreground tabular-nums'>{formatDateTime(d.created_at, 'date')}</span>
            </span>
          </div>

          <div className='grid gap-3 sm:grid-cols-3'>
            <StatCard label='观看人次' value={formatInt(d.totals.viewers)} delta={deltaPct(d.totals.viewers, d.prev.viewers)} />
            <StatCard label='起播次数' value={formatInt(d.totals.play_starts)} delta={deltaPct(d.totals.play_starts, d.prev.play_starts)} />
            <StatCard label='有效观看' value={formatWatch(d.totals.watch_seconds)} delta={deltaPct(d.totals.watch_seconds, d.prev.watch_seconds)} />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>集间留存曲线</CardTitle>
              <CardDescription>每 100 个第 1 集的观众，看到第 n 集还剩多少（窗口内按集去重设备累加的规模近似）。</CardDescription>
            </CardHeader>
            <CardContent>
              {d.episodes.length === 0 ? (
                <p className='py-8 text-center text-sm text-muted-foreground'>窗口内暂无分集观看数据</p>
              ) : (
                <ResponsiveContainer width='100%' height={280}>
                  <LineChart data={d.episodes} margin={{ left: 4, right: 12, top: 4, bottom: 4 }}>
                    <CartesianGrid strokeDasharray='3 3' stroke='var(--border)' />
                    <XAxis
                      dataKey='number'
                      tickFormatter={(n: number) => `第${n}集`}
                      tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                      minTickGap={16}
                    />
                    <YAxis tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }} width={44} unit='%' domain={[0, 'auto']} />
                    <Tooltip
                      contentStyle={{ background: 'var(--popover)', border: '1px solid var(--border)', borderRadius: 8, fontSize: 12 }}
                      labelFormatter={(n) => `第 ${n} 集`}
                    />
                    <Line type='monotone' dataKey='retention_pct' name='留存%' stroke='var(--chart-1)' dot={false} strokeWidth={2} />
                  </LineChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>每日观看趋势</CardTitle>
            </CardHeader>
            <CardContent>
              <ResponsiveContainer width='100%' height={240}>
                <LineChart data={d.daily} margin={{ left: 4, right: 12, top: 4, bottom: 4 }}>
                  <CartesianGrid strokeDasharray='3 3' stroke='var(--border)' />
                  <XAxis dataKey='day' tickFormatter={(x: string) => x.slice(5)} tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }} minTickGap={16} />
                  <YAxis tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }} width={44} allowDecimals={false} />
                  <Tooltip contentStyle={{ background: 'var(--popover)', border: '1px solid var(--border)', borderRadius: 8, fontSize: 12 }} />
                  <Line type='monotone' dataKey='viewers' name='观看人次' stroke='var(--chart-1)' dot={false} strokeWidth={2} />
                  <Line type='monotone' dataKey='play_starts' name='起播' stroke='var(--chart-2)' dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>集号</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead className='text-right'>观看人次</TableHead>
                  <TableHead className='text-right'>起播</TableHead>
                  <TableHead className='text-right'>有效观看</TableHead>
                  <TableHead className='pr-4 text-right'>留存</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {d.episodes.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                      窗口内暂无分集数据
                    </TableCell>
                  </TableRow>
                ) : (
                  d.episodes.map((e) => (
                    <TableRow key={e.episode_id}>
                      <TableCell className='pl-4 tabular-nums'>第 {e.number} 集</TableCell>
                      <TableCell className='max-w-xs truncate'>{e.title || '—'}</TableCell>
                      <TableCell className='text-right tabular-nums'>{formatInt(e.viewers)}</TableCell>
                      <TableCell className='text-right tabular-nums'>{formatInt(e.play_starts)}</TableCell>
                      <TableCell className='text-right tabular-nums'>{formatWatch(e.watch_seconds)}</TableCell>
                      <TableCell className='pr-4 text-right tabular-nums'>{e.retention_pct}%</TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          <p className='text-xs text-muted-foreground'>
            口径：窗口为北京时间日、到昨天，环比对比上一等长窗口；留存基于按天去重观众的规模近似，非严格去重。追剧 / 评论 / 举报、来源与刷剧入口、端占比、与全站均值对比、流失拐点等待前台埋点
            SDK（W4-1）或相应模块接入后补齐。
          </p>
        </>
      )}
    </div>
  )
}

function StatCard({ label, value, delta }: { label: string; value: string; delta: number | null }) {
  return (
    <Card>
      <CardHeader className='gap-1 pb-2'>
        <CardDescription>{label}</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{value}</CardTitle>
      </CardHeader>
      <CardContent>
        {delta === null ? (
          <span className='text-xs text-muted-foreground'>环比 —</span>
        ) : (
          <span className='text-xs text-muted-foreground tabular-nums'>
            环比 {delta >= 0 ? '↑' : '↓'} {Math.abs(delta).toFixed(1)}%
          </span>
        )}
      </CardContent>
    </Card>
  )
}
