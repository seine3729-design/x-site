import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Play, Plus, Trash2, Upload } from 'lucide-react'
import { useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
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
import { Form } from '@/components/ui/form'
import { LoadError, WithReason } from '@/features/access/shared'
import { transcodeJobApi } from '@/features/media/api'
import { EpisodePreviewDialog } from '@/features/media/preview-dialog'
import { ImageThumb } from '@/features/media/image-preview'
import { ImageUploadField, VideoUploadControl } from '@/features/media/upload-field'
import {
  ACCESS_LABELS,
  contentApi,
  contentKeys,
  episodesQuery,
  MEDIA_STATUS_LABELS,
  type Access,
  type DramaDetail,
  type Episode,
  type MediaStatus,
} from './api'
import { SelectField, TextField } from './components/fields'
import { formatDuration } from './utils'

type DialogState = { type: 'create' } | { type: 'edit' | 'delete' | 'video' | 'preview'; episode: Episode }

/** 视频状态的文字颜色：可播放为正文色，失败标红，其余为次要色。 */
const MEDIA_STATUS_CLASS: Record<MediaStatus, string | undefined> = {
  ready: undefined,
  pending: 'text-muted-foreground',
  transcoding: 'text-muted-foreground',
  failed: 'text-destructive',
}

/** 转封装中的分集带上任务进度，运营才知道「传上去之后到哪一步了」。 */
export function mediaStatusText(label: string, status: MediaStatus, progress?: number) {
  if (status !== 'transcoding' || progress === undefined) return label
  return `${label} ${Math.max(0, Math.min(100, Math.round(progress)))}%`
}

/** 分集（docs/06 第 5 节「分集」）：搜集号或标题；新增、编辑、删除；每集设谁能看与可试看秒数。 */
export function EpisodesSection({ drama, editable }: { drama: DramaDetail; editable: boolean }) {
  // 有分集在转封装时自动轮询，运营关掉上传弹窗后仍能看到进度推进。
  const episodes = useQuery({
    ...episodesQuery(drama.id),
    refetchInterval: (q) => (q.state.data?.some((e) => e.media_status === 'transcoding') ? 5000 : false),
  })
  const [query, setQuery] = useState('')
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const list = episodes.data ?? []
  const transcoding = list.some((e) => e.media_status === 'transcoding')
  const jobs = useQuery({
    queryKey: ['media', 'transcode-jobs', 'drama', drama.id],
    queryFn: () => transcodeJobApi.listByDrama(drama.id),
    enabled: transcoding,
    refetchInterval: transcoding ? 3000 : false,
  })
  /** 某集正在跑的转封装任务（列表按创建时间倒序，第一条就是最近一次）。 */
  const jobOf = (episodeID: number) => jobs.data?.list.find((j) => j.episode_id === episodeID && (j.status === 'running' || j.status === 'queued'))
  const q = query.trim()
  const shown = q ? list.filter((e) => String(e.number) === q.replace(/^第|集$/g, '') || e.title.includes(q)) : list
  const nextNumber = list.reduce((max, e) => Math.max(max, e.number), 0) + 1
  const readonlyReason = editable ? null : '当前角色只能查看'

  return (
    <Card>
      <CardHeader className='flex flex-row flex-wrap items-start justify-between gap-2'>
        <div className='flex flex-col gap-1.5'>
          <CardTitle className='text-base'>分集（{list.length} 集）</CardTitle>
          <CardDescription>
            集号在本剧内唯一；前台的「更新至 N 集」只算可播放的分集，增删改后自动刷新。视频由转封装生成（提交与重试随 A1-5 提供），试播随 S1-8 提供。
          </CardDescription>
        </div>
        <div className='flex items-center gap-2'>
          <Input className='h-9 w-40' placeholder='搜集号或标题' aria-label='搜集号或标题' value={query} onChange={(e) => setQuery(e.target.value)} />
          {editable && (
            <Button size='sm' onClick={() => setDialog({ type: 'create' })}>
              <Plus className='size-4' /> 新增分集
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent>
        {episodes.isError ? (
          <LoadError onRetry={() => void episodes.refetch()} />
        ) : (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow className='bg-muted'>
                  <TableHead className='pl-4'>集号</TableHead>
                  <TableHead>标题</TableHead>
                  <TableHead>时长</TableHead>
                  <TableHead>谁能看</TableHead>
                  <TableHead>可试看</TableHead>
                  <TableHead>视频</TableHead>
                  <TableHead className='pr-4'>
                    <span className='sr-only'>操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {episodes.isPending ? (
                  <TableRow>
                    <TableCell colSpan={7} className='h-20 text-center text-muted-foreground'>
                      加载中…
                    </TableCell>
                  </TableRow>
                ) : shown.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className='h-20 text-center text-muted-foreground'>
                      {q ? '没有符合条件的分集' : '还没有分集'}
                    </TableCell>
                  </TableRow>
                ) : (
                  shown.map((e) => (
                    <TableRow key={e.id}>
                      <TableCell className='pl-4'>
                        <div className='flex items-center gap-2'>
                          <ImageThumb value={e.cover} alt={`第 ${e.number} 集封面`} className='h-10 w-7' />
                          <span className='whitespace-nowrap tabular-nums'>第 {e.number} 集</span>
                        </div>
                      </TableCell>
                      <TableCell>{e.title || <span className='text-muted-foreground'>—</span>}</TableCell>
                      <TableCell className='tabular-nums'>{formatDuration(e.duration_sec)}</TableCell>
                      <TableCell>
                        <Badge variant={e.access === 'free' ? 'secondary' : 'outline'}>{ACCESS_LABELS[e.access]}</Badge>
                      </TableCell>
                      <TableCell className='tabular-nums'>{e.access === 'free' ? '—' : e.trial_sec > 0 ? `${e.trial_sec} 秒` : '不试看'}</TableCell>
                      <TableCell className={MEDIA_STATUS_CLASS[e.media_status]}>
                        <span className='tabular-nums'>
                          {mediaStatusText(MEDIA_STATUS_LABELS[e.media_status], e.media_status, jobOf(e.id)?.progress)}
                        </span>
                      </TableCell>
                      <TableCell className='pr-4'>
                        <div className='flex justify-end gap-1'>
                          <WithReason reason={e.media_status === 'ready' ? null : '这一集还没有可播放的视频'}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={e.media_status !== 'ready'}
                              aria-label={`试播第 ${e.number} 集`}
                              onClick={() => setDialog({ type: 'preview', episode: e })}
                            >
                              <Play className='size-4' />
                            </Button>
                          </WithReason>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`上传第 ${e.number} 集视频`}
                              onClick={() => setDialog({ type: 'video', episode: e })}
                            >
                              <Upload className='size-4' />
                            </Button>
                          </WithReason>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`编辑第 ${e.number} 集`}
                              onClick={() => setDialog({ type: 'edit', episode: e })}
                            >
                              <Pencil className='size-4' />
                            </Button>
                          </WithReason>
                          <WithReason reason={readonlyReason}>
                            <Button
                              variant='ghost'
                              size='icon'
                              disabled={!editable}
                              aria-label={`删除第 ${e.number} 集`}
                              onClick={() => setDialog({ type: 'delete', episode: e })}
                            >
                              <Trash2 className='size-4' />
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
      </CardContent>
      {(dialog?.type === 'create' || dialog?.type === 'edit') && (
        <EpisodeDialog
          dramaId={drama.id}
          episode={dialog.type === 'edit' ? dialog.episode : undefined}
          nextNumber={nextNumber}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'video' && <EpisodeVideoDialog dramaId={drama.id} episode={dialog.episode} onClose={() => setDialog(null)} />}
      {dialog?.type === 'preview' && (
        <EpisodePreviewDialog
          episode={{
            id: dialog.episode.id,
            number: dialog.episode.number,
            title: dialog.episode.title,
            duration_sec: dialog.episode.duration_sec,
            accessLabel: ACCESS_LABELS[dialog.episode.access],
            trialLabel: dialog.episode.trial_sec > 0 ? `可试看 ${dialog.episode.trial_sec} 秒` : '不试看',
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog?.type === 'delete' && <DeleteEpisodeDialog dramaId={drama.id} episode={dialog.episode} onClose={() => setDialog(null)} />}
    </Card>
  )
}

const seconds = (label: string) =>
  z
    .string()
    .trim()
    .regex(/^\d{0,5}$/, `${label}请填写整数秒`)
    .refine((v) => v === '' || Number(v) <= 86400, `${label}最长 86400 秒`)

const episodeSchema = z
  .object({
    number: z
      .string()
      .trim()
      .regex(/^\d+$/, '请填写集号')
      .refine((v) => Number(v) >= 1 && Number(v) <= 100000, '集号为 1–100000'),
    title: z.string().trim().max(200, '标题最多 200 个字'),
    cover: z.string().trim().max(500, '最多 500 个字符'),
    duration_sec: seconds('时长'),
    access: z.enum(['free', 'login', 'vip']),
    trial_sec: seconds('可试看'),
  })
  .refine((v) => !(Number(v.duration_sec || 0) > 0 && Number(v.trial_sec || 0) > Number(v.duration_sec || 0)), {
    path: ['trial_sec'],
    message: '可试看秒数不能超过时长',
  })

type EpisodeForm = z.infer<typeof episodeSchema>

const ACCESS_HINTS: Record<Access, string> = {
  free: '所有人可看。',
  login: '登录后可看；未登录可试看下面的秒数（0 为不试看）。',
  vip: 'VIP 会员可看；非会员可试看下面的秒数（0 为不试看）。',
}

function EpisodeDialog({ dramaId, episode, nextNumber, onClose }: { dramaId: number; episode?: Episode; nextNumber: number; onClose: () => void }) {
  const qc = useQueryClient()
  const creating = !episode
  const form = useForm<z.input<typeof episodeSchema>, unknown, EpisodeForm>({
    resolver: zodResolver(episodeSchema),
    defaultValues: {
      number: String(episode?.number ?? nextNumber),
      title: episode?.title ?? '',
      cover: episode?.cover ?? '',
      duration_sec: episode ? String(episode.duration_sec) : '',
      access: episode?.access ?? 'free',
      trial_sec: episode ? String(episode.trial_sec) : '',
    },
  })
  const access = useWatch({ control: form.control, name: 'access' })
  const mutation = useMutation({
    mutationFn: (v: EpisodeForm) => {
      const body = {
        number: Number(v.number),
        title: v.title,
        cover: v.cover,
        duration_sec: Number(v.duration_sec || 0),
        access: v.access,
        trial_sec: Number(v.trial_sec || 0),
      }
      return creating ? contentApi.createEpisode(dramaId, body) : contentApi.updateEpisode(episode.id, body)
    },
    onSuccess: (saved) => {
      toast.success(creating ? `已新增第 ${saved.number} 集` : `已保存第 ${saved.number} 集`)
      void qc.invalidateQueries({ queryKey: contentKeys.episodes(dramaId) })
      void qc.invalidateQueries({ queryKey: contentKeys.drama(dramaId) }) // 更新至 N 集
      void qc.invalidateQueries({ queryKey: [...contentKeys.dramas, 'list'] })
      onClose()
    },
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{creating ? '新增分集' : `编辑第 ${episode.number} 集`}</DialogTitle>
          <DialogDescription>封面通过图片上传组件生成 img/ 资源；视频上传后会自动转为 HLS m3u8。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2'>
              <TextField control={form.control} name='number' label='集号' required inputMode='numeric' placeholder='如 1' />
              <TextField control={form.control} name='duration_sec' label='时长（秒）' inputMode='numeric' placeholder='选填，0 或留空为未知' />
            </div>
            <TextField control={form.control} name='title' label='标题' placeholder='选填，如 初遇' />
            <ImageUploadField
              control={form.control}
              name='cover'
              label='封面'
              purpose='episode_cover'
              description='选填；上传前在浏览器压缩为 WebP。'
            />
            <div className='grid gap-4 sm:grid-cols-2'>
              <SelectField
                control={form.control}
                name='access'
                label='谁能看'
                placeholder='请选择'
                description={ACCESS_HINTS[access]}
                items={(Object.entries(ACCESS_LABELS) as [Access, string][]).map(([value, label]) => ({ value, label }))}
              />
              <TextField
                control={form.control}
                name='trial_sec'
                label='可试看秒数'
                inputMode='numeric'
                placeholder='选填，0 为不试看'
                disabled={access === 'free'}
                description={access === 'free' ? '免费分集不需要试看。' : '不能超过时长（时长未知时不限）。'}
              />
            </div>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : creating ? '新增' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteEpisodeDialog({ dramaId, episode, onClose }: { dramaId: number; episode: Episode; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => contentApi.deleteEpisode(episode.id),
    onSuccess: () => {
      toast.success(`已删除第 ${episode.number} 集`)
      void qc.invalidateQueries({ queryKey: contentKeys.episodes(dramaId) })
      void qc.invalidateQueries({ queryKey: contentKeys.drama(dramaId) })
      void qc.invalidateQueries({ queryKey: [...contentKeys.dramas, 'list'] })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除第 ${episode.number} 集${episode.title ? `「${episode.title}」` : ''}`}
      desc='分集直接删除，不进回收站，不可恢复。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function EpisodeVideoDialog({ dramaId, episode, onClose }: { dramaId: number; episode: Episode; onClose: () => void }) {
  const qc = useQueryClient()
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>上传第 {episode.number} 集视频</DialogTitle>
          <DialogDescription>视频源文件会进入私有 video/source/，后台 worker 会生成受保护的 HLS m3u8 分集产物。</DialogDescription>
        </DialogHeader>
        <VideoUploadControl
          purpose='episode_video'
          episodeID={episode.id}
          label='分集视频'
          onCompleted={() => {
            toast.success(`第 ${episode.number} 集已提交 HLS 转码`)
            void qc.invalidateQueries({ queryKey: contentKeys.episodes(dramaId) })
            void qc.invalidateQueries({ queryKey: contentKeys.drama(dramaId) })
          }}
          onTranscoded={() => {
            toast.success(`第 ${episode.number} 集转码完成，可以播放了`)
            void qc.invalidateQueries({ queryKey: contentKeys.episodes(dramaId) })
            void qc.invalidateQueries({ queryKey: contentKeys.drama(dramaId) })
          }}
        />
        <DialogFooter>
          <Button type='button' variant='outline' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
