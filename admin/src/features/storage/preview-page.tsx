import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { type ColumnDef } from '@tanstack/react-table'
import { Activity, Radar } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { DataTable } from '@/components/common/data-table'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui'
import {
  EVAL_STATUS_LABELS,
  healthQuery,
  LINE_KIND_LABELS,
  LINE_STATUS_LABELS,
  previewApi,
  type EvalStatus,
  type HealthLine,
  type LineEval,
  type LineKind,
  type PreviewResult,
} from './api'

const statusVariant: Record<EvalStatus, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  candidate: 'default',
  overflow: 'outline',
  excluded: 'destructive',
  unmatched: 'secondary',
  unhealthy: 'destructive',
  disabled: 'secondary',
}

/** 健康分着色：≥70 正常，30–69 观察，<30 偏低（低于 30 会被选线过滤）。 */
function scoreBadge(score: number) {
  const variant = score >= 70 ? 'default' : score >= 30 ? 'secondary' : 'destructive'
  return (
    <Badge variant={variant} className='tabular-nums'>
      {score}
    </Badge>
  )
}

const healthColumns: ColumnDef<HealthLine, unknown>[] = [
  {
    accessorKey: 'name',
    header: '线路',
    cell: ({ row }) => (
      <div className='flex items-center gap-2'>
        <span className='font-medium'>{row.original.name || `线路 ${row.original.line_id}`}</span>
        <Badge variant='outline'>{LINE_KIND_LABELS[row.original.kind]}</Badge>
      </div>
    ),
  },
  { accessorKey: 'status', header: '状态', cell: ({ row }) => LINE_STATUS_LABELS[row.original.status] },
  {
    id: 'score',
    header: '健康分',
    cell: ({ row }) => {
      const l = row.original
      if (!l.has_data) return <span className='text-muted-foreground'>无数据</span>
      return scoreBadge(l.score)
    },
  },
  {
    id: 'probe',
    header: '服务端探测',
    cell: ({ row }) => {
      const l = row.original
      if (!l.has_probe) return <span className='text-muted-foreground'>未探测</span>
      return (
        <div className='flex flex-col gap-0.5 text-xs'>
          <div className='flex items-center gap-1.5'>
            {scoreBadge(l.probe_score)}
            <Badge variant={l.probe_ok ? 'outline' : 'destructive'}>{l.probe_ok ? '可达' : '不可达'}</Badge>
            {l.http_status > 0 && <span className='tabular-nums text-muted-foreground'>HTTP {l.http_status}</span>}
            <span className='tabular-nums text-muted-foreground'>{l.elapsed_ms}ms</span>
            {l.cors && <Badge variant='outline'>CORS</Badge>}
          </div>
          {l.message && <span className='text-muted-foreground'>{l.message}</span>}
          {l.checked_at && (
            <span className='text-muted-foreground'>{new Date(l.checked_at).toLocaleString('zh-CN')}</span>
          )}
        </div>
      )
    },
  },
  {
    id: 'client',
    header: '客户端上报',
    cell: ({ row }) => {
      const c = row.original.client
      if (c.samples === 0) return <span className='text-muted-foreground'>暂无上报</span>
      return (
        <div className='flex flex-col gap-0.5 text-xs'>
          <div className='flex items-center gap-1.5'>
            {c.counted ? scoreBadge(c.score) : <Badge variant='outline'>样本不足</Badge>}
            <span className='tabular-nums text-muted-foreground'>{c.samples} 条</span>
          </div>
          <div className='flex items-center gap-2 text-muted-foreground'>
            {c.first_frame_ms > 0 && <span className='tabular-nums'>首帧 {c.first_frame_ms}ms</span>}
            <span className='tabular-nums'>失败 {c.fail_rate_pct}%</span>
            {c.stalls > 0 && <span className='tabular-nums'>卡顿 {c.stalls}</span>}
          </div>
        </div>
      )
    },
  },
]

export function StoragePreviewPage() {
  const can = useCan()
  const [kind, setKind] = useState<LineKind>('video')
  const [ip, setIp] = useState('')
  const [country, setCountry] = useState('')
  const [province, setProvince] = useState('')
  const [isp, setIsp] = useState('')
  const [asn, setAsn] = useState('')
  const [result, setResult] = useState<PreviewResult | null>(null)

  const canView = can('storage_preview.view')
  const health = useQuery({ ...healthQuery, enabled: canView })

  const run = useMutation({
    mutationFn: () => previewApi.run({ kind, ip, country, province, isp, asn }),
    onSuccess: setResult,
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!canView) return <NoPermission title='选线预览与健康' />

  const columns: ColumnDef<LineEval, unknown>[] = [
    {
      id: 'rank',
      header: '顺序',
      cell: ({ row }) => (row.original.rank > 0 ? <span className='tabular-nums'>{row.original.rank}</span> : <span className='text-muted-foreground'>—</span>),
    },
    {
      accessorKey: 'name',
      header: '线路',
      cell: ({ row }) => (
        <div className='flex items-center gap-2'>
          <span className='font-medium'>{row.original.name || `线路 ${row.original.line_id}`}</span>
          {row.original.recommended && <Badge variant='secondary'>推荐</Badge>}
        </div>
      ),
    },
    {
      accessorKey: 'status',
      header: '结果',
      cell: ({ row }) => <Badge variant={statusVariant[row.original.status]}>{EVAL_STATUS_LABELS[row.original.status]}</Badge>,
    },
    { accessorKey: 'region_tag', header: '地区标签', cell: ({ row }) => row.original.region_tag || '—' },
    { id: 'precision', header: '精度', cell: ({ row }) => <span className='tabular-nums'>{row.original.detail.precision}</span> },
    { id: 'boost', header: '加分', cell: ({ row }) => <span className='tabular-nums'>{row.original.detail.boost}</span> },
    { id: 'priority', header: '优先级', cell: ({ row }) => <span className='tabular-nums'>{row.original.detail.priority}</span> },
    {
      id: 'health',
      header: '健康',
      cell: ({ row }) => {
        const h = row.original.health
        return (
          <div className='flex flex-col gap-0.5'>
            <span className='tabular-nums'>{row.original.detail.health}</span>
            {h && (h.has_probe || h.has_client) && (
              <span className='text-xs text-muted-foreground'>
                {h.has_probe && `探测 ${h.probe_score}`}
                {h.has_probe && h.has_client && ' · '}
                {h.has_client && `上报 ${h.client_score}${h.by_region ? '（本地区）' : '（汇总）'}`}
              </span>
            )}
          </div>
        )
      },
    },
    { id: 'watch', header: '观察扣分', cell: ({ row }) => <span className='tabular-nums'>{row.original.detail.watch ? `-${row.original.detail.watch}` : '0'}</span> },
    { id: 'total', header: '总分', cell: ({ row }) => <span className='font-medium tabular-nums'>{row.original.detail.total}</span> },
  ]

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex items-center gap-2'>
        <Radar className='size-6 text-muted-foreground' aria-hidden />
        <h1 className='text-2xl font-semibold tracking-tight'>选线预览与健康</h1>
      </div>
      <p className='text-sm text-muted-foreground'>
        输入 IP 或「国家 + 省份 + 运营商」，查看当前线路在该地区的选线顺序与得分明细；下方健康看板展示各线路当前健康分。IP 解析需 GeoIP 数据库，未就位时请直接填地区。
      </p>

      <div className='flex flex-col gap-3 rounded-md border p-4'>
        <div className='flex flex-wrap items-end gap-3'>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-kind' className='text-muted-foreground'>
              类型
            </label>
            <Select value={kind} onValueChange={(v) => setKind(v as LineKind)}>
              <SelectTrigger id='preview-kind' className='w-32'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='video'>视频</SelectItem>
                <SelectItem value='image'>图片</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-ip' className='text-muted-foreground'>
              IP（可选，需 GeoIP）
            </label>
            <Input id='preview-ip' className='w-44' placeholder='如 1.2.3.4' value={ip} onChange={(e) => setIp(e.target.value)} />
          </div>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-country' className='text-muted-foreground'>
              国家
            </label>
            <Input id='preview-country' className='w-24' placeholder='CN' value={country} onChange={(e) => setCountry(e.target.value)} />
          </div>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-province' className='text-muted-foreground'>
              省份
            </label>
            <Input id='preview-province' className='w-28' placeholder='广东' value={province} onChange={(e) => setProvince(e.target.value)} />
          </div>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-isp' className='text-muted-foreground'>
              运营商
            </label>
            <Input id='preview-isp' className='w-28' placeholder='电信' value={isp} onChange={(e) => setIsp(e.target.value)} />
          </div>
          <div className='flex flex-col gap-1 text-sm'>
            <label htmlFor='preview-asn' className='text-muted-foreground'>
              ASN（可选）
            </label>
            <Input id='preview-asn' className='w-28' placeholder='4134' value={asn} onChange={(e) => setAsn(e.target.value)} />
          </div>
          <Button onClick={() => run.mutate()} disabled={run.isPending}>
            {run.isPending ? '查询中…' : '预览选线'}
          </Button>
        </div>
      </div>

      {result && (
        <div className='flex flex-col gap-2'>
          <div className='flex items-center gap-2 text-sm'>
            <span className='text-muted-foreground'>解析地区</span>
            <Badge variant='outline'>{result.region}</Badge>
            <span className='text-muted-foreground'>候选数量 {result.candidate_count}</span>
          </div>
          <DataTable data={result.lines} columns={columns} enableRowSelection={false} hidePagination />
        </div>
      )}

      <div className='flex flex-col gap-2'>
        <div className='flex items-center gap-2'>
          <Activity className='size-5 text-muted-foreground' aria-hidden />
          <h2 className='text-lg font-semibold tracking-tight'>线路健康看板</h2>
          {health.data && !health.data.probe_enabled && <Badge variant='secondary'>探测已关闭</Badge>}
          {health.isFetching && <span className='text-xs text-muted-foreground'>刷新中…</span>}
        </div>
        <p className='text-sm text-muted-foreground'>
          健康分 = 服务端探测 × {health.data?.probe_weight ?? 40}% + 客户端上报 × {health.data?.client_weight ?? 60}%，
          缺一边时只用另一边、都没有时按满分参与选线。探测由后台 worker 周期拉线路的 probe_path 得出（成功率与耗时，EMA 平滑）；
          客户端上报统计最近 {health.data?.window_min ?? 15} 分钟（此处为全部地区汇总，选线时按用户所在地区单独计算）。本表每 30 秒自动刷新。
        </p>
        {health.isError ? (
          <p className='text-sm text-destructive'>{errorMessage(health.error)}</p>
        ) : health.data && health.data.lines.length > 0 ? (
          <DataTable data={health.data.lines} columns={healthColumns} enableRowSelection={false} hidePagination />
        ) : (
          <p className='text-sm text-muted-foreground'>暂无线路。先在「线路」页新建线路，worker 探测后这里会显示健康分。</p>
        )}
      </div>
    </div>
  )
}
