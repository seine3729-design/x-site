import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { KeyRound, ShieldAlert } from 'lucide-react'
import { useCan } from '@/stores/auth-store'
import { DatePicker } from '@/components/date-picker'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui'
import { LoadError, PageHeading } from '@/features/access/shared'
import { Pager } from '@/features/content/components/pager'
import {
  RESULT_VARIANTS,
  keyRotationsQuery,
  loginLogsQuery,
  riskQuery,
  type LoginLogParams,
  type LoginResult,
} from './api'

/**
 * 安全中心（docs/06 第 39 节，A4-4）：管理员登录记录、密钥轮换记录、风控概览。
 * 纯只读页（只有 security.view，没有写接口）。
 */
export function SecurityPage() {
  const can = useCan()
  if (!can('security.view')) return <NoPermission title='安全中心' />

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading title='安全中心' description='管理员登录记录与异常告警、密钥轮换记录、风控概览。只读。' />
      <Tabs defaultValue='logins'>
        <TabsList>
          <TabsTrigger value='logins'>登录记录</TabsTrigger>
          <TabsTrigger value='keys'>密钥轮换</TabsTrigger>
          <TabsTrigger value='risk'>风控概览</TabsTrigger>
        </TabsList>
        <TabsContent value='logins' className='mt-4'>
          <LoginLogsTab />
        </TabsContent>
        <TabsContent value='keys' className='mt-4'>
          <KeyRotationsTab />
        </TabsContent>
        <TabsContent value='risk' className='mt-4'>
          <RiskTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// ---------- 登录记录 ----------

const RESULTS: LoginResult[] = ['success', 'bad_password', 'mfa_failed', 'locked', 'inactive']

function LoginLogsTab() {
  const [result, setResult] = useState<LoginResult | 'all'>('all')
  const [q, setQ] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  const params: LoginLogParams = {
    result: result === 'all' ? undefined : result,
    q: q.trim() || undefined,
    from: from || undefined,
    to: to || undefined,
    page,
    page_size: pageSize,
  }
  const list = useQuery(loginLogsQuery(params))
  const data = list.data
  const resultTitle = (r: LoginResult) => data?.results.find((x) => x.result === r)?.title ?? r

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex flex-col gap-1'>
          <span className='text-xs text-muted-foreground'>结果</span>
          <Select
            value={result}
            onValueChange={(v) => {
              setResult(v as LoginResult | 'all')
              setPage(1)
            }}
          >
            <SelectTrigger className='h-9 w-36'>
              <SelectValue placeholder='结果' />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>全部结果</SelectItem>
              {RESULTS.map((r) => (
                <SelectItem key={r} value={r}>
                  {resultTitle(r)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='flex flex-col gap-1'>
          <span className='text-xs text-muted-foreground'>用户名</span>
          <Input
            className='h-9 w-44'
            placeholder='搜尝试的用户名'
            value={q}
            onChange={(e) => {
              setQ(e.target.value)
              setPage(1)
            }}
          />
        </div>
        <div className='flex flex-col gap-1'>
          <label htmlFor='sec-from' className='text-xs text-muted-foreground'>
            起
          </label>
          <DatePicker
            id='sec-from'
            className='h-9 w-36'
            placeholder='开始日期'
            max={to || undefined}
            value={from}
            onChange={(v) => {
              setFrom(v)
              setPage(1)
            }}
          />
        </div>
        <div className='flex flex-col gap-1'>
          <label htmlFor='sec-to' className='text-xs text-muted-foreground'>
            止
          </label>
          <DatePicker
            id='sec-to'
            className='h-9 w-36'
            placeholder='结束日期'
            min={from || undefined}
            value={to}
            onChange={(v) => {
              setTo(v)
              setPage(1)
            }}
          />
        </div>
      </div>

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow className='bg-muted'>
                <TableHead className='pl-4'>时间</TableHead>
                <TableHead>用户名</TableHead>
                <TableHead>结果</TableHead>
                <TableHead>IP 段</TableHead>
                <TableHead className='pr-4'>设备摘要</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.isPending ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    加载中…
                  </TableCell>
                </TableRow>
              ) : (data?.list.length ?? 0) === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className='h-24 text-center text-muted-foreground'>
                    没有符合条件的登录记录
                  </TableCell>
                </TableRow>
              ) : (
                data!.list.map((row) => (
                  <TableRow key={row.id} className={row.suspicious ? 'bg-destructive/5' : undefined}>
                    <TableCell className='pl-4 text-xs whitespace-nowrap tabular-nums'>
                      {new Date(row.created_at).toLocaleString('zh-CN', { hour12: false })}
                    </TableCell>
                    <TableCell className='text-sm'>
                      <span className='font-medium'>{row.username || '—'}</span>
                      {row.current_username && row.current_username !== row.username && (
                        <span className='text-xs text-muted-foreground'>（当前 {row.current_username}）</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={RESULT_VARIANTS[row.result]}>{row.result_title}</Badge>
                    </TableCell>
                    <TableCell className='text-xs tabular-nums'>{row.ip_prefix || '—'}</TableCell>
                    <TableCell className='max-w-56 truncate pr-4 font-mono text-xs text-muted-foreground' title={row.ua_digest}>
                      {row.ua_digest || '—'}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {data && data.total > 0 && (
        <Pager
          page={page}
          pageSize={pageSize}
          total={data.total}
          onChange={(p, s) => {
            setPage(p)
            setPageSize(s)
          }}
        />
      )}
    </div>
  )
}

// ---------- 密钥轮换 ----------

function KeyRotationsTab() {
  const q = useQuery(keyRotationsQuery())
  if (q.isError) return <LoadError onRetry={() => void q.refetch()} />
  if (q.isPending) return <p className='text-sm text-muted-foreground'>加载中…</p>
  const list = q.data.list
  return (
    <div className='flex flex-col gap-3'>
      <p className='flex items-center gap-2 text-sm text-muted-foreground'>
        <KeyRound className='size-4' aria-hidden />
        只展示各密钥当前启用的版本号（kid）与用途，不展示密钥内容。轮换通过更新密钥环境变量完成。
      </p>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow className='bg-muted'>
              <TableHead className='pl-4'>用途</TableHead>
              <TableHead>版本（kid）</TableHead>
              <TableHead className='pr-4'>当前使用</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.length === 0 ? (
              <TableRow>
                <TableCell colSpan={3} className='h-24 text-center text-muted-foreground'>
                  未配置任何密钥
                </TableCell>
              </TableRow>
            ) : (
              list.map((k) => (
                <TableRow key={`${k.purpose}:${k.version}`}>
                  <TableCell className='pl-4'>
                    <span className='text-sm'>{k.purpose_title}</span>
                    <span className='ms-2 font-mono text-xs text-muted-foreground'>{k.purpose}</span>
                  </TableCell>
                  <TableCell className='font-mono text-sm'>{k.version}</TableCell>
                  <TableCell className='pr-4'>
                    {k.primary ? <Badge variant='outline'>当前签发</Badge> : <Badge variant='secondary'>保留校验</Badge>}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

// ---------- 风控概览 ----------

function RiskTab() {
  const q = useQuery(riskQuery())
  if (q.isError) return <LoadError onRetry={() => void q.refetch()} />
  if (q.isPending) return <p className='text-sm text-muted-foreground'>加载中…</p>
  const { items, note } = q.data
  return (
    <div className='flex flex-col gap-4'>
      <div className='grid gap-3 sm:grid-cols-3'>
        {items.map((it) => (
          <Card key={it.kind}>
            <CardHeader className='pb-2'>
              <CardTitle className='text-sm font-medium text-muted-foreground'>{it.title}</CardTitle>
              <CardDescription className='text-2xl font-semibold text-foreground tabular-nums'>
                {it.value === null ? '暂无' : it.value}
              </CardDescription>
            </CardHeader>
            <CardContent className='text-xs text-muted-foreground'>{it.desc}</CardContent>
          </Card>
        ))}
      </div>
      {note && (
        <p className='flex items-start gap-2 rounded-md border p-3 text-sm text-muted-foreground'>
          <ShieldAlert className='mt-0.5 size-4 shrink-0' aria-hidden />
          <span>{note}</span>
        </p>
      )}
    </div>
  )
}
