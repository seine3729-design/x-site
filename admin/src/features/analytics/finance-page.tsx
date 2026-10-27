import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { formatDateTime } from '@/lib/format'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { DatePicker } from '@/components/date-picker'
import { NoPermission } from '@/components/module-placeholder'
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui'
import { LoadError, PageHeading } from '@/features/access/shared'
import {
  channelLabel,
  type FinanceEntry,
  type FinanceReport,
  type FinanceSaveInput,
  financeKeys,
  financeQuery,
  money,
  planLabel,
  saveFinance,
  yuan,
} from './api'

type FormValues = {
  ad_revenue: string
  referral_revenue: string
  traffic_cost: string
  server_cost: string
  payment_fee: string
  ad_note: string
  referral_note: string
  traffic_note: string
  server_note: string
  payment_note: string
}

const EMPTY: FormValues = {
  ad_revenue: '',
  referral_revenue: '',
  traffic_cost: '',
  server_cost: '',
  payment_fee: '',
  ad_note: '',
  referral_note: '',
  traffic_note: '',
  server_note: '',
  payment_note: '',
}

const ITEMS: { amount: keyof FormValues; note: keyof FormValues; label: string; kind: '收入' | '成本' }[] = [
  { amount: 'ad_revenue', note: 'ad_note', label: '广告收入', kind: '收入' },
  { amount: 'referral_revenue', note: 'referral_note', label: '导流 / 互推收入', kind: '收入' },
  { amount: 'traffic_cost', note: 'traffic_note', label: '视频流量费', kind: '成本' },
  { amount: 'server_cost', note: 'server_note', label: '服务器 / 数据库费', kind: '成本' },
  { amount: 'payment_fee', note: 'payment_note', label: '支付手续费', kind: '成本' },
]

function currentMonth(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

/** 分 → 元输入字符串（0 显示为空，便于录入时看到占位提示）。 */
function fenStr(fen: number): string {
  return fen === 0 ? '' : (fen / 100).toString()
}

/** 元输入字符串 → 分（非正数或非法记 0）。 */
function toFen(s: string): number {
  const n = Number.parseFloat(s)
  return Number.isFinite(n) && n > 0 ? Math.round(n * 100) : 0
}

function entryToForm(e: FinanceEntry): FormValues {
  return {
    ad_revenue: fenStr(e.ad_revenue),
    referral_revenue: fenStr(e.referral_revenue),
    traffic_cost: fenStr(e.traffic_cost),
    server_cost: fenStr(e.server_cost),
    payment_fee: fenStr(e.payment_fee),
    ad_note: e.ad_note,
    referral_note: e.referral_note,
    traffic_note: e.traffic_note,
    server_note: e.server_note,
    payment_note: e.payment_note,
  }
}

function formToInput(v: FormValues): FinanceSaveInput {
  return {
    ad_revenue: toFen(v.ad_revenue),
    referral_revenue: toFen(v.referral_revenue),
    traffic_cost: toFen(v.traffic_cost),
    server_cost: toFen(v.server_cost),
    payment_fee: toFen(v.payment_fee),
    ad_note: v.ad_note.trim(),
    referral_note: v.referral_note.trim(),
    traffic_note: v.traffic_note.trim(),
    server_note: v.server_note.trim(),
    payment_note: v.payment_note.trim(),
  }
}

/** 收入与成本（docs/06 第 23 节，A4-1 / S4-3）。按月手填收支 + 会员收入自动汇总 + 人民币口径毛利。 */
export function FinancePage() {
  const can = useCan()
  const visible = can('finance.view')
  const editable = can('finance.edit')
  const qc = useQueryClient()
  const [month, setMonth] = useState(currentMonth)
  const report = useQuery({ ...financeQuery(month), enabled: visible })
  const form = useForm<FormValues>({ defaultValues: EMPTY })
  useEffect(() => {
    if (report.data) form.reset(entryToForm(report.data.entry))
  }, [report.data, form])
  const mutation = useMutation({
    mutationFn: (v: FormValues) => saveFinance(month, formToInput(v)),
    onSuccess: (rep) => {
      qc.setQueryData(financeKeys.month(month), rep)
      form.reset(entryToForm(rep.entry))
      toast.success(`已保存 ${rep.month} 的收支`)
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  if (!visible) return <NoPermission title='收入与成本' />

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='收入与成本'
        readonly={!editable}
        description='按月手填收支，会员收入按已支付订单自动汇总，供数据看板核算毛利'
        actions={
          <DatePicker
            mode='month'
            value={month}
            onChange={(v) => {
              if (v) setMonth(v)
            }}
            clearable={false}
            className='h-9 w-40'
            placeholder='选择月份'
            aria-label='选择月份'
          />
        }
      />
      {report.isError ? (
        <LoadError onRetry={() => void report.refetch()} />
      ) : report.isPending || !report.data ? (
        <Skeleton className='h-96 w-full' />
      ) : (
        <>
          <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))} className='flex flex-col gap-4' noValidate>
            <Card>
              <CardHeader>
                <CardTitle className='text-base'>手填收支（人民币元）</CardTitle>
                <CardDescription>
                  每月填一次，每项可写备注。{report.data.entry.updated_at ? `最近更新 ${formatDateTime(report.data.entry.updated_at)}` : '本月尚未保存'}
                </CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-4'>
                {ITEMS.map((it) => (
                  <div key={it.amount} className='grid gap-2 sm:grid-cols-[13rem_10rem_1fr] sm:items-center'>
                    <Label htmlFor={it.amount}>
                      {it.label}
                      <span className={`ml-2 text-xs ${it.kind === '收入' ? 'text-emerald-600' : 'text-muted-foreground'}`}>{it.kind}</span>
                    </Label>
                    <Input
                      id={it.amount}
                      type='number'
                      step='0.01'
                      min='0'
                      inputMode='decimal'
                      placeholder='0.00'
                      disabled={!editable}
                      className='tabular-nums'
                      {...form.register(it.amount)}
                    />
                    <Input placeholder='备注（选填）' disabled={!editable} {...form.register(it.note)} />
                  </div>
                ))}
              </CardContent>
            </Card>
            {editable && (
              <div className='flex justify-end'>
                <Button type='submit' disabled={mutation.isPending}>
                  {mutation.isPending ? '保存中…' : '保存本月'}
                </Button>
              </div>
            )}
          </form>

          <ProfitCard report={report.data} />
          <MembershipCard report={report.data} />
        </>
      )}
    </div>
  )
}

function ProfitCard({ report }: { report: FinanceReport }) {
  const e = report.entry
  const revenue = e.ad_revenue + e.referral_revenue
  const cost = e.traffic_cost + e.server_cost + e.payment_fee
  const cnyNet = report.membership.totals.find((t) => t.currency === 'CNY')?.net ?? 0
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>毛利（人民币口径）</CardTitle>
        <CardDescription>广告 + 导流 + 会员 CNY 净收入 − 流量 − 服务器 − 支付手续费；外币会员收入单列、不并入</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-wrap gap-x-10 gap-y-3'>
        <Metric label='手填收入' value={`¥${yuan(revenue)}`} />
        <Metric label='会员净收入（CNY）' value={`¥${yuan(cnyNet)}`} />
        <Metric label='手填成本' value={`¥${yuan(cost)}`} />
        <Metric label='毛利' value={`¥${yuan(report.gross_profit_fen)}`} strong />
      </CardContent>
    </Card>
  )
}

function MembershipCard({ report }: { report: FinanceReport }) {
  const m = report.membership
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>会员收入（自动汇总）</CardTitle>
        <CardDescription>按已支付订单的支付月汇总，不可手填；退款按原支付月计入，未支付不计</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        {m.by_channel.length === 0 ? (
          <p className='py-6 text-center text-sm text-muted-foreground'>本月暂无已支付会员订单</p>
        ) : (
          <>
            <div className='flex flex-wrap gap-x-10 gap-y-3'>
              {m.totals.map((t) => (
                <Metric key={t.currency} label={`${t.currency} 净收入（${t.orders} 单）`} value={money(t.net, t.currency)} />
              ))}
            </div>
            <div className='grid gap-4 lg:grid-cols-2'>
              <RevenueTable
                title='按渠道'
                head='渠道'
                rows={m.by_channel.map((c) => ({ name: channelLabel(c.channel), currency: c.currency, orders: c.orders, gross: c.gross, refund: c.refund }))}
              />
              <RevenueTable
                title='按套餐'
                head='套餐'
                rows={m.by_plan.map((p) => ({ name: planLabel(p.plan_code), currency: p.currency, orders: p.orders, gross: p.gross, refund: p.refund }))}
              />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

type Row = { name: string; currency: string; orders: number; gross: number; refund: number }

function RevenueTable({ title, head, rows }: { title: string; head: string; rows: Row[] }) {
  return (
    <div>
      <p className='mb-2 text-sm font-medium'>{title}</p>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted'>
              <TableHead className='pl-4'>{head}</TableHead>
              <TableHead>币种</TableHead>
              <TableHead className='text-right'>订单</TableHead>
              <TableHead className='text-right'>毛额</TableHead>
              <TableHead className='pr-4 text-right'>退款</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r, i) => (
              <TableRow key={`${r.name}-${r.currency}-${i}`}>
                <TableCell className='pl-4'>{r.name}</TableCell>
                <TableCell>{r.currency}</TableCell>
                <TableCell className='text-right tabular-nums'>{r.orders}</TableCell>
                <TableCell className='text-right tabular-nums'>{money(r.gross, r.currency)}</TableCell>
                <TableCell className='pr-4 text-right tabular-nums'>{money(r.refund, r.currency)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function Metric({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <span className='text-xs text-muted-foreground'>{label}</span>
      <span className={`tabular-nums ${strong ? 'text-xl font-semibold' : 'text-base'}`}>{value}</span>
    </div>
  )
}
