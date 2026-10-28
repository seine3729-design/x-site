import { binaryUpload, http, postBinaryForBlob } from '@/lib/http'

export const MEDIA_UPLOAD_PURPOSES = [
  'drama_cover',
  'episode_cover',
  'announcement_image',
  'ad_image',
  'ad_poster',
  'feedback_project_cover',
  'static_page_image',
  'episode_video',
  'ad_video',
] as const

export type MediaUploadPurpose = (typeof MEDIA_UPLOAD_PURPOSES)[number]
export type ImageFormat = 'webp' | 'avif'
export type MediaUploadStatus = 'created' | 'uploading' | 'uploaded' | 'queued' | 'running' | 'ready' | 'failed'

export type MediaUpload = {
  id: string
  provider_id: number
  purpose: MediaUploadPurpose
  kind: 'image' | 'video'
  mime: string
  image_format: ImageFormat | ''
  size_bytes: number
  source_key: string
  output_key: string
  episode_id: number
  transcode_job_id: string | null
  status: MediaUploadStatus
  progress: number
  error: string
  expires_at: string
  created_at: string
  updated_at: string
  /** 分片大小与片数；都为 0 表示整段单次上传（图片，或建会话时没有声明 multipart）。 */
  part_size: number
  part_count: number
}

export type CreateMediaUploadInput = {
  purpose: MediaUploadPurpose
  mime: string
  image_format?: ImageFormat
  size_bytes: number
  episode_id?: number
  /** 视频按分片并行上传（分片大小由服务端决定）；图片不支持。 */
  multipart?: boolean
}

/** 服务端图片优化的超时：后端要等外部优化服务两次往返（它自己的上限是 90 秒）。 */
const IMAGE_OPTIMIZE_TIMEOUT_MS = 120_000

export const mediaUploadApi = {
  create: (body: CreateMediaUploadInput) => http.post<MediaUpload>('/uploads', body),
  /**
   * 把原图交给服务端压缩并转成 WebP。
   *
   * 压缩必须在服务端做：优化服务的 API Key 不能下发浏览器。优化服务未配置、本月额度用尽（都是 503）
   * 或图片无法识别（400）时抛 HttpError，调用方退回浏览器本地压缩，不要因此阻塞运营上传。
   */
  optimizeImage: (purpose: MediaUploadPurpose, file: Blob) =>
    postBinaryForBlob('/images/optimize', file, {
      contentType: file.type || 'application/octet-stream',
      query: { purpose },
      timeoutMs: IMAGE_OPTIMIZE_TIMEOUT_MS,
    }),
  putContent: (id: string, body: Blob, contentType: string, onProgress?: (loaded: number, total: number) => void) =>
    binaryUpload.put<{ id: string; status: 'uploaded' }>(`/uploads/${id}/content`, body, { contentType, onProgress }),
  /** 提交视频的第 number 片（从 1 开始）。同一片可以重复提交，以最后一次为准。 */
  putPart: (id: string, number: number, body: Blob, onProgress?: (loaded: number, total: number) => void, signal?: AbortSignal) =>
    binaryUpload.put<{ id: string; part_number: number; size_bytes: number }>(`/uploads/${id}/parts/${number}`, body, {
      contentType: 'application/octet-stream',
      onProgress,
      signal,
    }),
  /**
   * 完成上传：服务端核对对象元数据后进入最终状态或转码队列。
   *
   * 视频要确认转码暂存对象已就位，历史会话还会在服务端复制一次源文件，耗时与文件大小成正比，
   * 所以调用方必须显式放宽超时 —— 用默认的 15 秒会让几十 MB 的分集在传完之后才报「网络异常或请求超时」。
   */
  complete: (id: string, timeoutMs?: number) => http.post<MediaUpload>(`/uploads/${id}/complete`, undefined, { timeoutMs }),
  get: (id: string) => http.get<MediaUpload>(`/uploads/${id}`),
}

// ---------- 分集转封装任务（S1-5，docs/07）----------

/** queued 排队或等待重试；running 转码中；succeeded 已切换到新产物；skipped 源文件未变；failed 不可恢复。 */
export type TranscodeJobStatus = 'queued' | 'running' | 'succeeded' | 'skipped' | 'failed'

export type TranscodeJob = {
  id: string
  episode_id: number
  drama_id: number
  episode_number: number
  status: TranscodeJobStatus
  progress: number
  attempts: number
  error: string
  duration_sec: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  updated_at: string
}

// ---------- 后台试播（docs/06 第 37 节）----------

export type EpisodePreview = {
  episode_id: number
  drama_id: number
  line_id: number
  line_name: string
  /** 后台专用的主清单地址（同源、走后台会话、不加密），可直接交给 hls.js。 */
  url: string
  expires_at: string
}

export const episodePreviewApi = {
  /** 签发一次试播地址（需要 dramas.view）。地址有效期十分钟，过期重新点预览即可。 */
  grant: (episodeID: number) => http.post<EpisodePreview>(`/episodes/${episodeID}/preview`),
}

export const transcodeJobApi = {
  /** 轮询单个任务的进度（需要 dramas.view）。 */
  get: (id: string) => http.get<TranscodeJob>(`/transcode-jobs/${id}`),
  /** 某部剧的最近若干个任务，用于在分集列表上显示「转码 x%」。 */
  listByDrama: (dramaID: number, pageSize = 100) =>
    http.get<{ list: TranscodeJob[]; total: number; page: number; page_size: number }>(`/transcode-jobs?drama_id=${dramaID}&page_size=${pageSize}`),
}
