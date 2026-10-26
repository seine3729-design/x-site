import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronUp, File, Folder, Link2, RefreshCw } from 'lucide-react'
import { errorMessage } from '@/lib/http'
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
import {
  ENTRY_TYPE_LABELS,
  formatSize,
  ftpApi,
  joinPath,
  parentPath,
  PROBE_STEP_LABELS,
  type FTPEntry,
  type FTPServer,
  type ProbeResult,
} from './api'

/**
 * 打开目录（docs/06 第 31 节流程 ②）：由 worker 连上资源机列目录，可点进子目录。
 * 每次都是一条新的 FTP 连接，所以进子目录用服务器返回的实际路径拼绝对路径。
 * 目录内容按请求过的路径缓存（staleTime 无限），回上级是瞬时的；要看最新内容点「刷新」。
 */
export function BrowseDialog({ server, onClose }: { server: FTPServer; onClose: () => void }) {
  // path 是「要请求的路径」，空串表示登录目录；实际路径由后端返回。
  const [path, setPath] = useState('')
  const [draft, setDraft] = useState('')

  const dir = useQuery({
    queryKey: ['ftp', 'dir', server.id, path],
    queryFn: () => ftpApi.listDir(server.id, path),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    // 一次探测最多 25 秒，重试会把等待拖到一分钟以上
    retry: false,
  })

  const result: ProbeResult | undefined = dir.data
  const current = result?.path ?? ''
  const up = parentPath(current)
  const failedStep = result?.steps.find((s) => !s.ok)

  const goto = (next: string) => {
    setPath(next)
    setDraft('')
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle className='flex flex-wrap items-center gap-2'>
            打开目录：{server.name}
            {result && !result.ok && <Badge variant='destructive'>读取失败</Badge>}
            {result?.truncated && <Badge variant='outline'>已截断</Badge>}
          </DialogTitle>
          <DialogDescription>
            由 worker 连上资源机读取，最多等 25 秒。目录在前、按名称排序，最多显示 1000 条。查新剧与入库随 S4-6 开放。
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-wrap items-center gap-2'>
          <Button
            variant='outline'
            size='icon'
            aria-label='上一级目录'
            disabled={!up || dir.isFetching}
            onClick={() => up && goto(up)}
          >
            <ChevronUp className='size-4' />
          </Button>
          <form
            className='flex flex-1 items-center gap-2'
            onSubmit={(e) => {
              e.preventDefault()
              goto(draft.trim())
            }}
          >
            <label htmlFor='ftp-path' className='sr-only'>
              目录路径
            </label>
            <Input
              id='ftp-path'
              className='h-9 min-w-48 flex-1 font-mono text-xs'
              placeholder={current || '请输入目录路径，留空为登录目录，如 /drama/2026'}
              maxLength={1024}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <Button type='submit' variant='secondary' className='h-9' disabled={dir.isFetching}>
              打开
            </Button>
          </form>
          <Button
            variant='ghost'
            size='icon'
            aria-label='刷新当前目录'
            disabled={dir.isFetching}
            onClick={() => void dir.refetch()}
          >
            <RefreshCw className={`size-4 ${dir.isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        {current && (
          <p className='truncate font-mono text-xs text-muted-foreground' title={current}>
            当前目录：{current}
          </p>
        )}

        {dir.isError ? (
          <div role='alert' className='rounded-md border border-destructive/40 p-4 text-sm'>
            {errorMessage(dir.error)}
            <button
              type='button'
              className='ms-2 font-medium text-primary underline-offset-4 hover:underline'
              onClick={() => void dir.refetch()}
            >
              重试
            </button>
          </div>
        ) : failedStep ? (
          <div role='alert' className='rounded-md border border-destructive/40 p-4 text-sm'>
            <span className='font-medium'>{PROBE_STEP_LABELS[failedStep.name]}失败：</span>
            {failedStep.message}
          </div>
        ) : (
          <div className='max-h-96 overflow-auto rounded-md border'>
            <Table>
              <TableHeader className='sticky top-0'>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>名称</TableHead>
                  <TableHead className='w-20'>类型</TableHead>
                  <TableHead className='w-24 text-right'>大小</TableHead>
                  <TableHead className='w-40 pr-4'>修改时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {dir.isPending ? (
                  <TableRow>
                    <TableCell colSpan={4} className='h-24 text-center text-muted-foreground'>
                      正在连接资源机…最多等 25 秒
                    </TableCell>
                  </TableRow>
                ) : result && result.entries.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className='h-24 text-center text-muted-foreground'>
                      这个目录是空的
                    </TableCell>
                  </TableRow>
                ) : (
                  result?.entries.map((e) => (
                    <EntryRow key={e.name} entry={e} onEnter={() => goto(joinPath(current, e.name))} />
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}

        {result?.truncated && (
          <p className='text-xs text-muted-foreground'>
            这个目录超过 1000 条，只显示了前 1000 条。请在上面填写更具体的子目录路径。
          </p>
        )}
        {result?.ok && (
          <p className='text-xs text-muted-foreground tabular-nums'>
            共 {result.entries.length} 条，耗时 {result.elapsed_ms} 毫秒。
          </p>
        )}

        <DialogFooter>
          <Button onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function EntryRow({ entry, onEnter }: { entry: FTPEntry; onEnter: () => void }) {
  const isDir = entry.type === 'dir'
  const Icon = isDir ? Folder : entry.type === 'link' ? Link2 : File
  return (
    <TableRow>
      <TableCell className='pl-4'>
        <div className='flex items-center gap-2'>
          <Icon className={`size-4 shrink-0 ${isDir ? 'text-primary' : 'text-muted-foreground'}`} aria-hidden />
          {isDir ? (
            <button
              type='button'
              className='truncate text-start font-medium text-primary underline-offset-4 hover:underline'
              onClick={onEnter}
            >
              {entry.name}
            </button>
          ) : (
            <span className='truncate'>{entry.name}</span>
          )}
          {entry.target && <span className='truncate text-xs text-muted-foreground'>→ {entry.target}</span>}
        </div>
      </TableCell>
      <TableCell className='text-sm text-muted-foreground'>{ENTRY_TYPE_LABELS[entry.type]}</TableCell>
      <TableCell className='text-right text-sm tabular-nums'>{formatSize(entry.size, entry.type)}</TableCell>
      <TableCell className='pr-4 text-sm whitespace-nowrap tabular-nums'>
        {entry.modified_at ? new Date(entry.modified_at).toLocaleString('zh-CN', { hour12: false }) : '—'}
      </TableCell>
    </TableRow>
  )
}
