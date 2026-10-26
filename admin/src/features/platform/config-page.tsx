import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Download, FileUp, History, RotateCcw, ShieldCheck, ShieldOff } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Textarea,
} from '@/components/ui'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import {
  BACKUP_REASON_LABELS,
  backupsQuery,
  configApi,
  configKeys,
  countsSummary,
  downloadBackup,
  type BackupFile,
  type BackupSummary,
  type ImportResult,
} from './config-api'

/**
 * 配置导出导入（docs/06 第 29 节，S4-4）。
 *
 * 导出安全、导入危险：导入会覆盖现有配置，所以要二次确认、校验格式版本与签名，并在导入前自动存一份档。
 * 维度是「合并更新」而不是整表替换 —— 取值被剧集引用，删掉会连带丢失剧集的维度归属。
 */
export function ConfigPage() {
  const can = useCan()
  const visible = can('config.view')
  const qc = useQueryClient()
  const backups = useQuery({ ...backupsQuery, enabled: visible })
  const [pasted, setPasted] = useState('')
  const [pending, setPending] = useState<{ file: BackupFile; from: string } | null>(null)
  const [restoring, setRestoring] = useState<BackupSummary | null>(null)
  const [result, setResult] = useState<ImportResult | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  const refresh = () => void qc.invalidateQueries({ queryKey: configKeys.backups })

  const doExport = useMutation({
    mutationFn: () => configApi.exportConfig(),
    onSuccess: (file) => {
      const stamp = file.exported_at.replace(/[-:]/g, '').replace('T', '-').slice(0, 15)
      downloadBackup(file, `xsite-config-${stamp}.json`)
      refresh()
      toast.success('备份已下载，库里也留了一份存档')
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const doImport = useMutation({
    mutationFn: (file: BackupFile) => configApi.importConfig(file),
    onSuccess: (res) => {
      setPending(null)
      setPasted('')
      refresh()
      setResult(res)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const doRestore = useMutation({
    mutationFn: (id: number) => configApi.restore(id),
    onSuccess: (res) => {
      setRestoring(null)
      refresh()
      setResult(res)
    },
    onError: (err) => toast.error(errorMessage(err)),
  })

  const doDownload = useMutation({
    mutationFn: (id: number) => configApi.getBackup(id),
    onSuccess: (file, id) => downloadBackup(file, `xsite-config-backup-${id}.json`),
    onError: (err) => toast.error(errorMessage(err)),
  })

  if (!visible) return <NoPermission title='配置导出导入' />
  const editable = can('config.edit')
  const readonlyReason = editable ? null : '当前角色只能查看与导出'

  /** 解析用户给的 JSON 文本，顺手挡掉明显不是备份的内容。 */
  const parse = (text: string, from: string) => {
    let file: BackupFile
    try {
      file = JSON.parse(text) as BackupFile
    } catch {
      toast.error('这不是合法的 JSON 文件')
      return
    }
    // 导出接口返回的是 { code, message, data }，用户可能把整个响应存下来了
    const unwrapped = (file as unknown as { data?: BackupFile }).data
    if (unwrapped && typeof unwrapped === 'object' && 'format' in unwrapped) {
      file = unwrapped
    }
    if (typeof file?.format !== 'number' || typeof file?.data !== 'object') {
      toast.error('这个文件不像是配置备份（缺少 format 或 data）')
      return
    }
    setPending({ file, from })
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='配置导出导入'
        description='把筛选维度与广告素材导出成一份带签名的 JSON 备份，或从备份导回来。导入会覆盖现有配置，操作前会自动存一份档。'
        readonly={!editable}
        actions={
          <Button onClick={() => doExport.mutate()} disabled={doExport.isPending}>
            <Download /> {doExport.isPending ? '导出中…' : '导出备份'}
          </Button>
        }
      />

      <section className='flex flex-col gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-4 text-sm'>
        <div className='flex items-center gap-2 font-medium'>
          <AlertTriangle className='size-4 text-amber-600' aria-hidden /> 导入前请先了解
        </div>
        <ul className='ms-5 flex list-disc flex-col gap-1 text-muted-foreground'>
          <li>
            <span className='font-medium text-foreground'>筛选维度是合并更新</span>
            ：按标识匹配，备份里有的新增或覆盖，备份里没有的<span className='font-medium'>保留不删</span>。
            因为取值被剧集引用，删掉会连带丢失所有剧集的维度归属。多出来的项会在导入结果里列出，需要时去「筛选维度」页手动删。
          </li>
          <li>
            <span className='font-medium text-foreground'>广告素材是整表替换</span>
            ：原有广告会被删除重建，<span className='font-medium'>曝光与点击计数归零</span>（备份本就不含这两个统计）。
          </li>
          <li>备份文件带签名，改动过的文件会被拒绝。不要手改备份。</li>
          <li>菜单、静态页、区块暂不在备份范围内（这几个模块还没做）。</li>
        </ul>
      </section>

      {editable && (
        <section className='flex flex-col gap-3 rounded-md border p-4'>
          <div className='flex flex-col gap-0.5'>
            <h2 className='font-medium'>导入备份</h2>
            <p className='text-xs text-muted-foreground'>选择备份文件，或把备份内容粘贴到下面。导入前会再确认一次。</p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <input
              ref={fileInput}
              type='file'
              accept='application/json,.json'
              className='sr-only'
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (!f) return
                void f.text().then((text) => parse(text, f.name))
                e.target.value = ''
              }}
            />
            <Button variant='outline' onClick={() => fileInput.current?.click()}>
              <FileUp /> 选择备份文件
            </Button>
          </div>
          <div className='flex flex-col gap-2'>
            <label htmlFor='config-paste' className='text-sm text-muted-foreground'>
              或粘贴备份内容
            </label>
            <Textarea
              id='config-paste'
              rows={5}
              className='font-mono text-xs'
              placeholder='把备份文件的全部内容粘贴到这里，例如 {"format":1,"exported_at":"…","signature":"…","data":{…}}'
              value={pasted}
              onChange={(e) => setPasted(e.target.value)}
            />
            <Button
              variant='outline'
              className='self-start'
              disabled={!pasted.trim()}
              onClick={() => parse(pasted, '粘贴的内容')}
            >
              <FileUp /> 导入粘贴的内容
            </Button>
          </div>
        </section>
      )}

      <section className='flex flex-col gap-2'>
        <div className='flex items-center gap-2'>
          <History className='size-4 text-muted-foreground' aria-hidden />
          <h2 className='font-medium'>备份存档</h2>
        </div>
        <p className='text-xs text-muted-foreground'>
          每次导出与每次导入前都会在库里留一份。回滚直接用「回滚到这份」（库内存档不需要验签），下载下来再导入也可以。
        </p>
        {backups.isError ? (
          <LoadError onRetry={() => void backups.refetch()} />
        ) : (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>编号</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>内容</TableHead>
                  <TableHead>签名</TableHead>
                  <TableHead>时间</TableHead>
                  <TableHead className='pr-4'>
                    <span className='sr-only'>操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {backups.isPending ? (
                  <TableRow>
                    <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                      加载中…
                    </TableCell>
                  </TableRow>
                ) : backups.data.list.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className='h-24 text-center text-muted-foreground'>
                      还没有备份存档，点右上角「导出备份」生成第一份
                    </TableCell>
                  </TableRow>
                ) : (
                  backups.data.list.map((b) => (
                    <TableRow key={b.id}>
                      <TableCell className='pl-4 tabular-nums'>#{b.id}</TableCell>
                      <TableCell>
                        <Badge variant={b.reason === 'before_import' ? 'outline' : 'secondary'}>
                          {BACKUP_REASON_LABELS[b.reason]}
                        </Badge>
                        {b.note && <div className='mt-0.5 text-xs text-muted-foreground'>{b.note}</div>}
                      </TableCell>
                      <TableCell className='text-sm'>{countsSummary(b.counts)}</TableCell>
                      <TableCell>
                        {b.signed ? (
                          <span className='flex items-center gap-1 text-xs text-muted-foreground'>
                            <ShieldCheck className='size-3.5 text-emerald-600' aria-hidden /> 有
                          </span>
                        ) : (
                          <span className='flex items-center gap-1 text-xs text-muted-foreground'>
                            <ShieldOff className='size-3.5' aria-hidden /> 无
                          </span>
                        )}
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap tabular-nums'>
                        {new Date(b.created_at).toLocaleString('zh-CN', { hour12: false })}
                      </TableCell>
                      <TableCell className='pr-4'>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon'
                            aria-label={`下载存档 #${b.id}`}
                            disabled={doDownload.isPending}
                            onClick={() => doDownload.mutate(b.id)}
                          >
                            <Download className='size-4' />
                          </Button>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`回滚到存档 #${b.id}`}
                              onClick={() => setRestoring(b)}
                            >
                              <RotateCcw className='size-4' />
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
      </section>

      {pending && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setPending(null)}
          destructive
          title='确认导入并覆盖现有配置？'
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                来源：<span className='font-medium'>{pending.from}</span>，格式版本 {pending.file.format}
                {pending.file.site && `，导出时站点版本 ${pending.file.site}`}。
              </p>
              <p className='text-muted-foreground'>
                广告素材会被整表替换（曝光与点击计数归零）；筛选维度按标识合并更新，备份里没有的维度与取值会保留。
                导入前会自动存一份当前配置的存档，出问题可以回滚。
              </p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={doImport.isPending ? '导入中…' : '确认导入'}
          isLoading={doImport.isPending}
          handleConfirm={() => doImport.mutate(pending.file)}
        />
      )}

      {restoring && (
        <ConfirmDialog
          open
          onOpenChange={(o) => !o && setRestoring(null)}
          destructive
          title={`确认回滚到存档 #${restoring.id}？`}
          desc={
            <div className='flex flex-col gap-2 text-sm'>
              <p>
                这份存档来自{BACKUP_REASON_LABELS[restoring.reason]}，内容为 {countsSummary(restoring.counts)}，
                时间 {new Date(restoring.created_at).toLocaleString('zh-CN', { hour12: false })}。
              </p>
              <p className='text-muted-foreground'>
                回滚和导入一样会覆盖现有配置，并在动手前再存一份当前状态的档。
              </p>
            </div>
          }
          cancelBtnText='取消'
          confirmText={doRestore.isPending ? '回滚中…' : '确认回滚'}
          isLoading={doRestore.isPending}
          handleConfirm={() => doRestore.mutate(restoring.id)}
        />
      )}

      {result && <ResultDialog result={result} onClose={() => setResult(null)} />}
    </div>
  )
}

/** 导入 / 回滚结果：各类写入数量、保留未删的项、自动存档编号。 */
function ResultDialog({ result, onClose }: { result: ImportResult; onClose: () => void }) {
  const kept = result.taxonomies_kept + result.terms_kept
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title='导入完成'
      desc={
        <div className='flex flex-col gap-2 text-sm'>
          <p>
            已写入 {countsSummary(result.applied)}
            {result.signed ? '（备份签名已校验）' : '（这份备份没有签名）'}。
          </p>
          {result.ads_replaced && <p className='text-muted-foreground'>广告已整表替换，曝光与点击计数从零开始。</p>}
          {kept > 0 && (
            <div className='flex flex-col gap-1'>
              <p>
                库里另有 {result.taxonomies_kept} 个维度、{result.terms_kept} 个取值不在这份备份里，
                <span className='font-medium'>已保留未删</span>（删掉会连带丢失剧集的维度归属）：
              </p>
              <ul className='ms-5 list-disc text-muted-foreground'>
                {result.kept_names.slice(0, 10).map((n) => (
                  <li key={n}>{n}</li>
                ))}
                {result.kept_names.length > 10 && <li>还有 {result.kept_names.length - 10} 项…</li>}
              </ul>
              <p className='text-muted-foreground'>确认不再需要的话，去「筛选维度」页删除。</p>
            </div>
          )}
          <p className='text-muted-foreground'>
            导入前的配置已存为存档 <span className='font-medium'>#{result.backup_id}</span>，需要时可以回滚。
          </p>
        </div>
      }
      confirmText='知道了'
      handleConfirm={onClose}
    />
  )
}
