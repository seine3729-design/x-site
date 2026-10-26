import { binaryUpload, http } from '@/lib/http'

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
}

export type CreateMediaUploadInput = {
  purpose: MediaUploadPurpose
  mime: string
  image_format?: ImageFormat
  size_bytes: number
  episode_id?: number
}

export const mediaUploadApi = {
  create: (body: CreateMediaUploadInput) => http.post<MediaUpload>('/uploads', body),
  putContent: (id: string, body: Blob, contentType: string, onProgress?: (loaded: number, total: number) => void) =>
    binaryUpload.put<{ id: string; status: 'uploaded' }>(`/uploads/${id}/content`, body, { contentType, onProgress }),
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
