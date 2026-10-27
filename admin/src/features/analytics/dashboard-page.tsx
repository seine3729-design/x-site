import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import {
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
} from '@/components/ui'
import { LoadError, PageHeading } from '@/features/access/shared'
import { type DailyPoint, DAYS_OPTIONS, deltaPct, exportAnalytics, formatInt, formatWatch, overviewQuery, ratePct } from './api'

/** 运营数据看板（docs/06 第 1 节，A4-1）。第一版只呈现服务端可观测指标，窗口为北京时间日、到昨天。 */
export function DashboardPage() {
  const can = useCan()
  const visible = can('dashboard.view')
  const [days, setDays] = useState(7)
  const [copying, setCopying] = useState(false)
  const q = useQuery({ ...overviewQuery(days), enabled: visible })

  if (!visible) return <NoPermission title='运营数据看板' />
  const data = q.data

  async function copyForAgent() {
    setCopying(true)
    try {
      const pkg = await exportAnalytics(days)
      await navigator.clipboard.writeText(pkg.content)
      toast.success('已复制数据到剪贴板，可粘贴给 AI 生成简报')
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setCopying(false)
    }
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='运营数据看板'
        description={data ? `全站概览 · ${data.from} ~ ${data.to}（北京时间，到昨天）` : '全站概览（北京时间，到昨天）'}
        actions={
          <div className='flex items-center gap-2'>
            <Button variant='outline' size='sm' onClick={() => void copyForAgent()} disabled={copying || !data}>
              {copying ? '生成中…' : '复制数据给 Agent'}
            </Button>
            <DaysSelect days={days} onChange={setDays} />
          </div>
        }
      />
      {q.isError ? (
        <LoadError onRetry={() => void q.refetch()} />
      ) : q.isPending || !data ? (
        <Skeleton className='h-96 w-full' />
      ) : (
        <>
          <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
            <MetricCard label='独立访客（UV）' value={formatInt(data.totals.uv)} delta={deltaPct(data.totals.uv, data.prev.uv)} />
            <MetricCard label='起播次数' value={formatInt(data.totals.play_starts)} delta={deltaPct(data.totals.play_starts, data.prev.play_starts)} />
            <MetricCard label='有效观看时长' value={formatWatch(data.totals.watch_seconds)} delta={deltaPct(data.totals.watch_seconds, data.prev.watch_seconds)} />
            <MetricCard label='注册数' value={formatInt(data.totals.registrations)} delta={deltaPct(data.totals.registrations, data.prev.registrations)} />
            <RateCard label='起播成功率' value={ratePct(data.totals.play_starts, data.totals.play_attempts)} hint='起播 / 播放尝试' />
            <RateCard label='注册墙转化' value={ratePct(data.totals.registrations, data.totals.reg_wall_views)} hint='注册 / 注册墙曝光' />
            <RateCard label='广告点击率' value={ratePct(data.totals.ad_clicks, data.totals.ad_impressions)} hint='点击 / 曝光' />
            <RateCard label='播放失败数' value={formatInt(data.totals.play_errors)} hint='窗口内累计' />
          </div>

          <TrendCard
            title='访问与播放趋势'
            data={data.daily}
            lines={[
              { key: 'uv', name: 'UV', color: 'var(--chart-1)' },
              { key: 'play_starts', name: '起播', color: 'var(--chart-2)' },
              { key: 'play_errors', name: '播放失败', color: 'var(--chart-5)' },
            ]}
          />
          <TrendCard
            title='注册与广告趋势'
            data={data.daily}
            lines={[
              { key: 'registrations', name: '注册', color: 'var(--chart-3)' },
              { key: 'reg_wall_views', name: '注册墙曝光', color: 'var(--chart-4)' },
              { key: 'ad_clicks', name: '广告点击', color: 'var(--chart-2)' },
            ]}
          />

          <p className='text-xs text-muted-foreground'>
            口径：窗口为北京时间日、到昨天为止（今天为未定稿不计入），环比对比上一等长窗口。当前仅呈现服务端可观测指标；
            留存、QoS（首帧 / 卡顿）、来源渠道、会员收益与福利等板块待前台埋点 SDK（W4-1）与对应模块接入后补齐。
          </p>
        </>
      )}
    </div>
  )
}

function DaysSelect({ days, onChange }: { days: number; onChange: (d: number) => void }) {
  return (
    <Select value={String(days)} onValueChange={(v) => onChange(Number(v))}>
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
  )
}

function MetricCard({ label, value, delta }: { label: string; value: string; delta: number | null }) {
  return (
    <Card>
      <CardHeader className='gap-1 pb-2'>
        <CardDescription>{label}</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <DeltaText delta={delta} />
      </CardContent>
    </Card>
  )
}

function RateCard({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <Card>
      <CardHeader className='gap-1 pb-2'>
        <CardDescription>{label}</CardDescription>
        <CardTitle className='text-2xl tabular-nums'>{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <span className='text-xs text-muted-foreground'>{hint}</span>
      </CardContent>
    </Card>
  )
}

function DeltaText({ delta }: { delta: number | null }) {
  if (delta === null) return <span className='text-xs text-muted-foreground'>环比 —</span>
  const up = delta >= 0
  return (
    <span className='text-xs text-muted-foreground tabular-nums'>
      环比 {up ? '↑' : '↓'} {Math.abs(delta).toFixed(1)}%
    </span>
  )
}

type LineDef = { key: string; name: string; color: string }

function TrendCard({ title, data, lines }: { title: string; data: DailyPoint[]; lines: LineDef[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width='100%' height={280}>
          <LineChart data={data} margin={{ left: 4, right: 12, top: 4, bottom: 4 }}>
            <CartesianGrid strokeDasharray='3 3' stroke='var(--border)' />
            <XAxis dataKey='day' tickFormatter={(d: string) => d.slice(5)} tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }} minTickGap={16} />
            <YAxis tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }} width={44} allowDecimals={false} />
            <Tooltip
              contentStyle={{ background: 'var(--popover)', border: '1px solid var(--border)', borderRadius: 8, fontSize: 12 }}
              labelStyle={{ color: 'var(--foreground)' }}
            />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            {lines.map((l) => (
              <Line key={l.key} type='monotone' dataKey={l.key} name={l.name} stroke={l.color} dot={false} strokeWidth={2} />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
