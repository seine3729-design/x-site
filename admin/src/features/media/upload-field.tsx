import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { AlertCircle, Check, FileVideo, ImagePlus, LoaderCircle, Trash2, Upload } from 'lucide-react'
import { Button } from '@/components/ui'
import { FormDescription, FormField, FormItem, FormLabel, FormMessage, useFormField } from '@/components/ui/form'
import { errorMessage } from '@/lib/http'
import { cn } from '@/lib/utils'
import { mediaUploadApi, transcodeJobApi, type MediaUpload, type MediaUploadPurpose } from './api'
import { imagePreviewUrl } from './image-preview'
import { compressionSummary, percentOf, transcodeProgressText, uploadProgressText, type CompressionMode } from './progress'

type UploadState = 'idle' | 'preparing' | 'uploading' | 'processing' | 'ready' | 'failed'

/** 压缩结果的最长边。与服务端的 IMAGE_MAX_SIDE 默认值一致，兜底压缩时在浏览器里用同一个值。 */
const IMAGE_MAX_SIDE = 2400

function bytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}

/** 转码任务的轮询间隔。转码是分钟级的，2 秒一次足够及时又不压后台。 */
const JOB_POLL_MS = 2000

/**
 * 视频完成上传的超时。正常情况下服务端只核对一次暂存对象的元数据（毫秒级），
 * 但历史会话要在服务端复制一遍源文件，耗时与文件大小成正比，默认的 15 秒会在这里假报网络错误。
 */
const VIDEO_COMPLETE_TIMEOUT_MS = 300_000

/**
 * 浏览器兜底压缩的质量阶梯。从视觉无损往下退，取第一个确实比原图小的结果。
 *
 * 固定一个高质量值是上一版「压缩后比原图还大」的直接原因：canvas 先把 JPEG 解码再按 0.92 编码 WebP，
 * 对已经优化过的照片几乎必然涨体积 —— 既多一次有损，又没有降体积。
 */
const LOCAL_QUALITY_STEPS = [0.9, 0.82, 0.72] as const

/** 准备好的上传内容。 */
type Prepared = {
  blob: Blob
  originalBytes: number
  mode: CompressionMode
  /** 服务端优化没用上的原因；为空表示用上了。 */
  fallbackReason: string
}

/**
 * 压缩图片。优先交给服务端的图片优化服务，它按图片内容选量化参数，压缩比和画质都比
 * 浏览器 canvas 稳定得多；服务未配置、额度用尽或这张图它不认时退回浏览器压缩，不阻塞运营上传。
 */
async function prepareImage(file: File, purpose: MediaUploadPurpose): Promise<Prepared> {
  try {
    const optimized = await mediaUploadApi.optimizeImage(purpose, file)
    if (optimized.size > 0) {
      return { blob: optimized, originalBytes: file.size, mode: 'server', fallbackReason: '' }
    }
  } catch (error) {
    const local = await compressLocally(file)
    return { ...local, fallbackReason: errorMessage(error) }
  }
  const local = await compressLocally(file)
  return { ...local, fallbackReason: '服务端优化没有返回结果' }
}

/**
 * 浏览器里把图片压缩成 WebP。只用 WebP：所有现代浏览器都支持 canvas WebP 编码；
 * AVIF 的 canvas 编码主流浏览器（Chrome / Firefox 桌面）都不支持，前台要 AVIF 由 CDN 实时转（docs/08）。
 *
 * 两条保证不会「压完更大」：沿质量阶梯找第一个小于原图的结果；原图已经是不超尺寸的 WebP
 * 且仍比所有结果都小时直接上传原文件（服务端只认 WebP 文件头，所以这条捷径仅对 WebP 原图成立）。
 * 原图是 JPEG/PNG 时必须重编码，哪怕最省的一档仍然更大也只能用它 —— 摘要会如实写出来。
 */
async function compressLocally(file: File): Promise<Omit<Prepared, 'fallbackReason'>> {
  const source = await createImageBitmap(file)
  const oversized = Math.max(source.width, source.height) > IMAGE_MAX_SIDE
  try {
    const scale = Math.min(1, IMAGE_MAX_SIDE / Math.max(source.width, source.height))
    const width = Math.max(1, Math.round(source.width * scale))
    const height = Math.max(1, Math.round(source.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext('2d')
    if (!context) throw new Error('当前浏览器无法创建图片画布')
    context.drawImage(source, 0, 0, width, height)

    let smallest: Blob | null = null
    for (const quality of LOCAL_QUALITY_STEPS) {
      const encoded = await encodeWebP(canvas, quality)
      smallest = encoded
      if (encoded.size < file.size) break
    }
    if (!smallest) throw new Error('图片压缩失败，请重试')
    if (file.type === 'image/webp' && !oversized && file.size <= smallest.size) {
      return { blob: file, originalBytes: file.size, mode: 'reused' }
    }
    return { blob: smallest, originalBytes: file.size, mode: 'local' }
  } finally {
    source.close()
  }
}

function encodeWebP(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
  return new Promise<Blob>((resolve, reject) =>
    canvas.toBlob(
      (blob) => {
        if (!blob || blob.type !== 'image/webp') {
          reject(new Error('图片压缩失败，请重试'))
          return
        }
        resolve(blob)
      },
      'image/webp',
      quality
    )
  )
}

type ImageUploadControlProps = {
  purpose: Exclude<MediaUploadPurpose, 'episode_video' | 'ad_video'>
  value?: string
  onChange: (key: string) => void
  label: string
  description?: ReactNode
  disabled?: boolean
  inputId?: string
  inputDescribedBy?: string
  invalid?: boolean
  required?: boolean
}

/** 可复用的图片上传器：本地压缩为 WebP 后提交，服务端再验证文件头。 */
export function ImageUploadControl({
  purpose,
  value = '',
  onChange,
  label,
  description,
  disabled,
  inputId,
  inputDescribedBy,
  invalid,
  required,
}: ImageUploadControlProps) {
  const generatedID = useId()
  const id = inputId ?? `image-upload-${generatedID}`
  const fileInput = useRef<HTMLInputElement>(null)
  const [state, setState] = useState<UploadState>('idle')
  const [progress, setProgress] = useState(0)
  const [notice, setNotice] = useState('')
  const [summary, setSummary] = useState('')
  const [preview, setPreview] = useState<string | null>(null)
  const [brokenPreview, setBrokenPreview] = useState('')

  useEffect(
    () => () => {
      if (preview) URL.revokeObjectURL(preview)
    },
    [preview]
  )

  // 本次刚选的文件用本地 blob 预览；打开已有记录时 value 是对象 key，走同源预览代理取回图片。
  const remotePreview = imagePreviewUrl(value)
  const previewSrc = preview || remotePreview
  const previewBroken = !!previewSrc && previewSrc === brokenPreview

  async function choose(file?: File) {
    if (!file || disabled || state === 'preparing' || state === 'uploading') return
    if (!file.type.startsWith('image/')) {
      setState('failed')
      setNotice('请选择图片文件。')
      return
    }
    try {
      setState('preparing')
      setProgress(0)
      setSummary('')
      setBrokenPreview('')
      setNotice('正在压缩为 WebP…')
      const { blob, originalBytes, mode, fallbackReason } = await prepareImage(file, purpose)
      setSummary(compressionSummary(originalBytes, blob.size, mode))
      const nextPreview = URL.createObjectURL(blob)
      setPreview((previous) => {
        if (previous) URL.revokeObjectURL(previous)
        return nextPreview
      })
      const session = await mediaUploadApi.create({
        purpose,
        mime: blob.type,
        image_format: 'webp',
        size_bytes: blob.size,
      })
      setState('uploading')
      setNotice(`正在上传 ${bytes(blob.size)}…`)
      await mediaUploadApi.putContent(session.id, blob, blob.type, (loaded, total) => setProgress(total > 0 ? Math.round((loaded / total) * 100) : 0))
      const complete = await mediaUploadApi.complete(session.id)
      onChange(complete.output_key)
      setProgress(100)
      setState('ready')
      setNotice(
        mode === 'reused'
          ? '原图已是更优的 WebP，按原文件上传（未重新编码）。'
          : fallbackReason
            ? `已上传为 WebP（服务端优化未生效，改用浏览器压缩：${fallbackReason}）。`
            : '已上传为 WebP，路径已写入表单。'
      )
    } catch (error) {
      setState('failed')
      setNotice(errorMessage(error))
    }
  }

  const busy = state === 'preparing' || state === 'uploading'
  const statusText = state === 'ready' ? '已就绪' : state === 'failed' ? '上传失败' : busy ? notice : value ? '已选择资源' : '尚未上传'

  return (
    <div
      className='flex flex-col gap-2 rounded-md border border-dashed p-3'
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {
        event.preventDefault()
        void choose(event.dataTransfer.files[0])
      }}
    >
      <input
        ref={fileInput}
        id={id}
        type='file'
        accept='image/*'
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-describedby={inputDescribedBy}
        required={required}
        className='sr-only'
        disabled={disabled || busy}
        onChange={(event) => {
          void choose(event.target.files?.[0])
          event.currentTarget.value = ''
        }}
      />
      <div className='flex flex-wrap items-center gap-2'>
        <ImagePlus className='size-5 text-muted-foreground' aria-hidden />
        <div className='min-w-0 flex-1'>
          <p className='text-sm font-medium'>{label}</p>
          <p className='text-xs text-muted-foreground'>选择或拖入图片；上传前压缩为 WebP、最长边 {IMAGE_MAX_SIDE}px。</p>
        </div>
        <Button type='button' variant='outline' className='h-10' disabled={disabled || busy} onClick={() => fileInput.current?.click()}>
          {busy ? <LoaderCircle className='animate-spin' /> : <Upload />} 选择图片
        </Button>
      </div>
      {previewSrc && !previewBroken && (
        <img
          src={previewSrc}
          alt={`${label}预览`}
          loading='lazy'
          decoding='async'
          className='max-h-40 max-w-full self-start rounded border bg-muted object-contain'
          onError={() => setBrokenPreview(previewSrc)}
        />
      )}
      {previewBroken && <p className='text-xs text-muted-foreground'>图片读取失败，可能已从存储源删除或当前图片主存储已切换。</p>}
      {summary && <p className='text-xs tabular-nums text-muted-foreground'>{summary}</p>}
      {value && <code className='max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded bg-muted px-2 py-1 text-xs'>{value}</code>}
      <div className='flex items-center gap-2 text-xs text-muted-foreground' aria-live='polite'>
        {state === 'failed' ? (
          <AlertCircle className='size-4 text-destructive' aria-hidden />
        ) : state === 'ready' ? (
          <Check className='size-4 text-primary' aria-hidden />
        ) : null}
        <span>{statusText}</span>
        {state === 'uploading' && <span className='tabular-nums'>{progress}%</span>}
        {value && !busy && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='ms-auto h-7'
            disabled={disabled}
            onClick={() => {
              onChange('')
              setPreview((previous) => {
                if (previous) URL.revokeObjectURL(previous)
                return null
              })
              setState('idle')
              setNotice('')
              setSummary('')
              setBrokenPreview('')
            }}
          >
            <Trash2 /> 移除
          </Button>
        )}
      </div>
      {state === 'uploading' && (
        <div
          className='h-1.5 overflow-hidden rounded bg-muted'
          role='progressbar'
          aria-label={`${label}上传进度`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
        >
          <div className='h-full bg-primary transition-[width]' style={{ width: `${progress}%` }} />
        </div>
      )}
      {description && <p className='text-xs text-muted-foreground'>{description}</p>}
    </div>
  )
}

function ImageUploadFieldControl({ purpose, value, onChange, label, description, disabled, required }: ImageUploadControlProps) {
  const { formItemId, formDescriptionId, formMessageId, error } = useFormField()
  return (
    <ImageUploadControl
      inputId={formItemId}
      inputDescribedBy={`${formDescriptionId} ${formMessageId}`}
      invalid={!!error}
      purpose={purpose}
      value={value}
      onChange={onChange}
      label={label}
      description={description}
      disabled={disabled}
      required={required}
    />
  )
}

export function ImageUploadField<T extends FieldValues>({
  control,
  name,
  label,
  purpose,
  description,
  disabled,
  required,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  purpose: Exclude<MediaUploadPurpose, 'episode_video' | 'ad_video'>
  description?: ReactNode
  disabled?: boolean
  required?: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {label}
            {required && (
              <span className='text-destructive' aria-hidden>
                {' '}
                *
              </span>
            )}
          </FormLabel>
          <ImageUploadFieldControl
            purpose={purpose}
            value={String(field.value ?? '')}
            onChange={field.onChange}
            label={label}
            description={description}
            disabled={disabled}
            required={required}
          />
          <FormDescription>图片会先压缩为 WebP 再上传，不以原图上传。</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

type VideoUploadControlProps = {
  purpose: Extract<MediaUploadPurpose, 'episode_video' | 'ad_video'>
  episodeID?: number
  value?: string
  onChange?: (key: string) => void
  onCompleted?: (asset: MediaUpload) => void
  /** 分集视频转封装结束（成功或沿用旧产物）时回调，供列表刷新 media_status。 */
  onTranscoded?: () => void
  label?: string
  inputId?: string
  inputDescribedBy?: string
  invalid?: boolean
  required?: boolean
  disabled?: boolean
}

/**
 * 视频上传器：源文件进入 video/source/，之后由 worker 转封装成 HLS。
 * 进度分两段各 0–100%：先是上传（XHR 的字节进度，带速度与预计剩余），
 * 再是转码 —— 广告视频轮询上传会话，分集视频轮询转封装任务（上传完才拿到任务号）。
 */
export function VideoUploadControl({
  purpose,
  episodeID,
  value = '',
  onChange,
  onCompleted,
  onTranscoded,
  label = '视频文件',
  inputId,
  inputDescribedBy,
  invalid,
  required,
  disabled,
}: VideoUploadControlProps) {
  const generatedID = useId()
  const inputID = inputId ?? `video-upload-${generatedID}`
  const fileInput = useRef<HTMLInputElement>(null)
  const [state, setState] = useState<UploadState>('idle')
  const [progress, setProgress] = useState(0)
  const [notice, setNotice] = useState('')
  const [detail, setDetail] = useState('')
  const [uploadID, setUploadID] = useState<string | null>(null)
  const [jobID, setJobID] = useState<string | null>(null)
  const startedAt = useRef(0)

  // 分集视频：转封装由 River worker 执行，进度在转码任务上。
  useEffect(() => {
    if (!jobID) return
    let active = true
    const poll = async () => {
      try {
        const job = await transcodeJobApi.get(jobID)
        if (!active) return
        setNotice(transcodeProgressText(job.status, job.progress, job.error))
        if (job.status === 'succeeded' || job.status === 'skipped') {
          setProgress(100)
          setState('ready')
          setJobID(null)
          onTranscoded?.()
        } else if (job.status === 'failed') {
          setState('failed')
          setJobID(null)
        } else {
          setProgress(job.progress)
          setState('processing')
        }
      } catch (error) {
        if (!active) return
        // 文件已经传上去了，读进度失败不代表转码失败：停止轮询并提示去列表看，别误报成上传失败。
        setNotice(`已提交转码，但读取进度失败：${errorMessage(error)}`)
        setJobID(null)
      }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), JOB_POLL_MS)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [jobID, onTranscoded])

  useEffect(() => {
    if (!uploadID || purpose !== 'ad_video') return
    let active = true
    const poll = async () => {
      try {
        const asset = await mediaUploadApi.get(uploadID)
        if (!active) return
        setProgress(asset.progress)
        if (asset.status === 'ready') {
          onChange?.(asset.output_key)
          onCompleted?.(asset)
          setState('ready')
          setNotice('HLS 转码完成，已回填 master.m3u8 路径。')
          setUploadID(null)
        } else if (asset.status === 'failed') {
          setState('failed')
          setNotice(asset.error || '视频转码失败，请重新上传。')
          setUploadID(null)
        } else {
          setState('processing')
          setNotice(`正在转码为 HLS… ${asset.progress}%`)
        }
      } catch (error) {
        if (active) {
          setState('failed')
          setNotice(errorMessage(error))
          setUploadID(null)
        }
      }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), JOB_POLL_MS)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [onChange, onCompleted, purpose, uploadID])

  async function choose(file?: File) {
    if (!file || disabled || state === 'uploading' || state === 'processing') return
    if (!file.type.startsWith('video/')) {
      setState('failed')
      setNotice('请选择 MP4、MOV、WebM 或 MKV 视频文件。')
      return
    }
    try {
      setState('uploading')
      setProgress(0)
      setDetail('')
      startedAt.current = Date.now()
      setNotice(`正在上传 ${bytes(file.size)}…`)
      const session = await mediaUploadApi.create({ purpose, mime: file.type, size_bytes: file.size, episode_id: episodeID })
      await mediaUploadApi.putContent(session.id, file, file.type, (loaded, total) => {
        setProgress(percentOf(loaded, total))
        setDetail(uploadProgressText({ loaded, total, elapsedMs: Date.now() - startedAt.current }))
      })
      setNotice('上传完成，正在提交转码…')
      setDetail('')
      const complete = await mediaUploadApi.complete(session.id, VIDEO_COMPLETE_TIMEOUT_MS)
      onCompleted?.(complete)
      setProgress(0)
      setState('processing')
      if (purpose === 'episode_video') {
        if (complete.transcode_job_id) {
          setJobID(complete.transcode_job_id)
          setNotice('上传完成，已提交 HLS 转码…')
        } else {
          setState('ready')
          setProgress(100)
          setNotice('已提交分集 HLS 转码，列表会显示处理进度。')
        }
      } else {
        setUploadID(complete.id)
        setNotice('上传完成，正在转码为 HLS…')
      }
    } catch (error) {
      setState('failed')
      setNotice(errorMessage(error))
    }
  }

  const busy = state === 'uploading' || state === 'processing'
  const phase = state === 'uploading' ? '上传' : '转码'
  return (
    <div className='flex flex-col gap-2 rounded-md border border-dashed p-3'>
      <input
        ref={fileInput}
        id={inputID}
        type='file'
        accept='video/mp4,video/quicktime,video/webm,video/x-matroska,.mkv'
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-describedby={inputDescribedBy}
        required={required}
        className='sr-only'
        disabled={disabled || busy}
        onChange={(event) => {
          void choose(event.target.files?.[0])
          event.currentTarget.value = ''
        }}
      />
      <div className='flex flex-wrap items-center gap-2'>
        <FileVideo className='size-5 text-muted-foreground' aria-hidden />
        <div className='min-w-0 flex-1'>
          <p className='text-sm font-medium'>{label}</p>
          <p className='text-xs text-muted-foreground'>源文件进入私有 video/source/；上传后自动转为 HLS m3u8。</p>
        </div>
        <Button type='button' variant='outline' className='h-10' disabled={disabled || busy} onClick={() => fileInput.current?.click()}>
          {busy ? <LoaderCircle className='animate-spin' /> : <Upload />} 选择视频
        </Button>
      </div>
      {value && <code className='max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded bg-muted px-2 py-1 text-xs'>{value}</code>}
      <p className={cn('flex items-center gap-2 text-xs text-muted-foreground', state === 'failed' && 'text-destructive')} aria-live='polite'>
        {state === 'failed' ? (
          <AlertCircle className='size-4' aria-hidden />
        ) : state === 'ready' ? (
          <Check className='size-4 text-primary' aria-hidden />
        ) : null}
        <span className='min-w-0 flex-1'>{notice || (value ? 'HLS 已就绪' : '尚未上传')}</span>
        {busy && <span className='tabular-nums'>{progress}%</span>}
      </p>
      {detail && <p className='text-xs tabular-nums text-muted-foreground'>{detail}</p>}
      {busy && (
        <div
          className='h-1.5 overflow-hidden rounded bg-muted'
          role='progressbar'
          aria-label={`${label}${phase}进度`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progress}
        >
          <div className='h-full bg-primary transition-[width]' style={{ width: `${Math.max(progress, state === 'processing' ? 1 : 0)}%` }} />
        </div>
      )}
    </div>
  )
}

function VideoUploadFieldControl({
  label,
  purpose,
  value,
  onChange,
  disabled,
  required,
}: Pick<VideoUploadControlProps, 'label' | 'purpose' | 'value' | 'onChange' | 'disabled' | 'required'>) {
  const { formItemId, formDescriptionId, formMessageId, error } = useFormField()
  return (
    <VideoUploadControl
      inputId={formItemId}
      inputDescribedBy={`${formDescriptionId} ${formMessageId}`}
      invalid={!!error}
      label={label}
      purpose={purpose}
      value={value}
      onChange={onChange}
      disabled={disabled}
      required={required}
    />
  )
}

export function VideoUploadField<T extends FieldValues>({
  control,
  name,
  label,
  purpose = 'ad_video',
  disabled,
  required,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  purpose?: 'ad_video'
  disabled?: boolean
  required?: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {label}
            {required && (
              <span className='text-destructive' aria-hidden>
                {' '}
                *
              </span>
            )}
          </FormLabel>
          <VideoUploadFieldControl
            label={label}
            purpose={purpose}
            value={String(field.value ?? '')}
            onChange={field.onChange}
            disabled={disabled}
            required={required}
          />
          <FormDescription>视频完成 HLS 转码后才会回填可投放的 m3u8 地址。</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
