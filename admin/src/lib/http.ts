/**
 * 后台唯一的请求封装（docs/06 第 0.3 节）。
 * - 会话放在后端下发的 HttpOnly Cookie 中，前端不接触令牌，统一 credentials: 'include'
 * - 写请求自动携带内存中的 CSRF 令牌
 * - 统一解包 { code, message, data }，失败时抛出 HttpError
 */

/** 后台接口前缀（docs/07 第 0.5 节）。调用方只传前缀之后的路径，如 `http.get('/me')`。 */
export const API_BASE = '/api/admin'

const DEFAULT_TIMEOUT_MS = 15_000

/** 后端统一响应包。 */
export type Envelope<T> = {
  code: string
  message: string
  data?: T
  request_id?: string
}

/** 请求失败。status=0 表示网络错误或超时。 */
export class HttpError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string
  readonly retryAfter?: number

  constructor(status: number, code: string, message: string, requestId?: string, retryAfter?: number) {
    super(message)
    this.name = 'HttpError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.retryAfter = retryAfter
  }

  /** 会话失效或未登录。 */
  get isUnauthorized(): boolean {
    return this.status === 401
  }

  /** 后端要求重新验证二次因子（高敏操作）。 */
  get needsReauth(): boolean {
    return this.code === 'REAUTH_REQUIRED'
  }
}

let csrfToken: string | null = null

/** 登录成功或读取 /me 后设置 CSRF 令牌（只存在内存中，刷新页面后由 /me 重新获取）。 */
export function setCsrfToken(token: string | null) {
  csrfToken = token
}

/**
 * 高敏操作重新验证（S2-6）：写请求收到 412 REAUTH_REQUIRED 时，调用此处理器弹窗让用户重新验证二次因子，
 * 成功（返回 true）则自动重试一次原请求，对调用方透明。由 ReauthProvider 在挂载时注册。
 */
let reauthHandler: (() => Promise<boolean>) | null = null
let pendingReauth: Promise<boolean> | null = null

export function setReauthHandler(fn: (() => Promise<boolean>) | null) {
  reauthHandler = fn
}

/** 多个并发请求同时 412 时只弹一次窗，共用同一次重新验证结果。 */
function obtainReauth(): Promise<boolean> {
  if (!reauthHandler) return Promise.resolve(false)
  if (!pendingReauth) {
    pendingReauth = reauthHandler().finally(() => {
      pendingReauth = null
    })
  }
  return pendingReauth
}

type Query = Record<string, string | number | boolean | undefined | null>

export type RequestOptions = {
  query?: Query
  body?: unknown
  headers?: HeadersInit
  signal?: AbortSignal
  timeoutMs?: number
}

const WRITE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

/** 拼接查询串，忽略空值。 */
export function buildUrl(path: string, query?: Query): string {
  const url = `${API_BASE}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== '') {
      params.set(key, String(value))
    }
  }
  const qs = params.toString()
  return qs ? `${url}?${qs}` : url
}

async function request<T>(method: string, path: string, options: RequestOptions = {}, retried = false): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('accept', 'application/json')
  if (options.body !== undefined) {
    headers.set('content-type', 'application/json')
  }
  if (WRITE_METHODS.has(method) && csrfToken) {
    headers.set('x-csrf-token', csrfToken)
  }

  const timeout = AbortSignal.timeout(options.timeoutMs ?? DEFAULT_TIMEOUT_MS)
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), {
      method,
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      credentials: 'include',
      signal,
    })
  } catch (error) {
    if (options.signal?.aborted) throw error
    throw new HttpError(0, 'NETWORK', '网络异常或请求超时，请稍后重试')
  }

  const requestId = response.headers.get('x-request-id') ?? undefined
  const body = (await response.json().catch(() => null)) as Envelope<T> | null

  if (!response.ok || !body || body.code !== 'OK') {
    const retryAfter = Number(response.headers.get('retry-after')) || undefined
    const error = new HttpError(
      response.status,
      body?.code ?? 'INTERNAL',
      body?.message ?? `请求失败（${response.status}）`,
      body?.request_id ?? requestId,
      retryAfter
    )
    // 高敏写操作要求重新验证：弹窗验证通过后自动重试一次（只重试一次，避免死循环）
    if (!retried && error.needsReauth && WRITE_METHODS.has(method) && (await obtainReauth())) {
      return request<T>(method, path, options, true)
    }
    throw error
  }

  return body.data as T
}

export const http = {
  get: <T>(path: string, options?: RequestOptions) => request<T>('GET', path, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('POST', path, { ...options, body }),
  put: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('PUT', path, { ...options, body }),
  patch: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('PATCH', path, { ...options, body }),
  delete: <T>(path: string, options?: RequestOptions) => request<T>('DELETE', path, options),
}

/** 给用户看的错误文案：附带 request_id 便于排查。 */
export function errorMessage(error: unknown): string {
  if (error instanceof HttpError) {
    if (error.code === 'RATE_LIMITED' && error.retryAfter) {
      return `${error.message}（${error.retryAfter} 秒后可重试）`
    }
    return error.requestId ? `${error.message}（请求编号 ${error.requestId}）` : error.message
  }
  return '操作失败，请稍后重试'
}
/**
 * multipart 表单提交（后台 CSV 批量导入）：复用 Cookie、CSRF、统一错误类型与 API 前缀。
 *
 * 刻意不设 content-type —— multipart 的 boundary 要由浏览器自己生成，手写一定对不上。
 * 导入可能要跑几秒（几千行解析 + 单事务写入），所以超时单独放宽，不用默认的 15 秒。
 */
export async function postForm<T>(
  path: string,
  form: FormData,
  options?: { timeoutMs?: number; signal?: AbortSignal }
): Promise<T> {
  const headers = new Headers({ accept: 'application/json' })
  if (csrfToken) headers.set('x-csrf-token', csrfToken)

  const timeout = AbortSignal.timeout(options?.timeoutMs ?? 60_000)
  const signal = options?.signal ? AbortSignal.any([options.signal, timeout]) : timeout

  let response: Response
  try {
    response = await fetch(buildUrl(path), { method: 'POST', body: form, credentials: 'include', headers, signal })
  } catch (error) {
    if (options?.signal?.aborted) throw error
    throw new HttpError(0, 'NETWORK', '网络异常或请求超时，请稍后重试')
  }

  const requestId = response.headers.get('x-request-id') ?? undefined
  const body = (await response.json().catch(() => null)) as Envelope<T> | null
  if (!response.ok || !body || body.code !== 'OK') {
    throw new HttpError(
      response.status,
      body?.code ?? 'INTERNAL',
      body?.message ?? `请求失败（${response.status}）`,
      body?.request_id ?? requestId
    )
  }
  return body.data as T
}

/**
 * 提交二进制、取回二进制（后台图片优化）：复用 Cookie、CSRF、统一错误类型与 API 前缀。
 *
 * 和 `uploadBinary` 的区别是这里要的是响应体本身而不是 JSON 包。失败时服务端仍返回标准响应包，
 * 所以按 JSON 解析出业务码；超时要由调用方放宽（服务端要等外部优化服务往返）。
 */
export async function postBinaryForBlob(
  path: string,
  body: Blob,
  options: { contentType: string; query?: Query; timeoutMs?: number; signal?: AbortSignal }
): Promise<Blob> {
  const headers = new Headers({ 'content-type': options.contentType })
  if (csrfToken) headers.set('x-csrf-token', csrfToken)

  const timeout = AbortSignal.timeout(options.timeoutMs ?? DEFAULT_TIMEOUT_MS)
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), { method: 'POST', body, headers, credentials: 'include', signal })
  } catch (error) {
    if (options.signal?.aborted) throw error
    throw new HttpError(0, 'NETWORK', '网络异常或请求超时，请稍后重试')
  }

  const requestId = response.headers.get('x-request-id') ?? undefined
  if (!response.ok) {
    const envelope = (await response.json().catch(() => null)) as Envelope<never> | null
    throw new HttpError(
      response.status,
      envelope?.code ?? 'INTERNAL',
      envelope?.message ?? `请求失败（${response.status}）`,
      envelope?.request_id ?? requestId
    )
  }
  return await response.blob()
}

/** 原始二进制上传（仅后台受会话保护的上传会话）：仍复用 Cookie、CSRF、统一错误类型与 API 前缀。 */
export type BinaryUploadOptions = {
  contentType: string
  onProgress?: (loaded: number, total: number) => void
  signal?: AbortSignal
}

function uploadBinary<T>(path: string, body: Blob, options: BinaryUploadOptions): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    xhr.open('PUT', buildUrl(path))
    xhr.withCredentials = true
    xhr.responseType = 'text'
    xhr.setRequestHeader('accept', 'application/json')
    xhr.setRequestHeader('content-type', options.contentType)
    if (csrfToken) xhr.setRequestHeader('x-csrf-token', csrfToken)
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) options.onProgress?.(event.loaded, event.total)
    }
    xhr.onload = () => {
      const requestId = xhr.getResponseHeader('x-request-id') ?? undefined
      let envelope: Envelope<T> | null = null
      try {
        envelope = JSON.parse(xhr.responseText) as Envelope<T>
      } catch {
        // 非 JSON 响应统一映射为内部错误，避免把网关页面展示给运营人员。
      }
      if (xhr.status >= 200 && xhr.status < 300 && envelope?.code === 'OK' && envelope.data !== undefined) {
        resolve(envelope.data)
        return
      }
      reject(
        new HttpError(
          xhr.status || 0,
          envelope?.code ?? 'INTERNAL',
          envelope?.message ?? (xhr.status ? `上传失败（${xhr.status}）` : '网络异常或上传已取消，请重试'),
          envelope?.request_id ?? requestId
        )
      )
    }
    xhr.onerror = () => reject(new HttpError(0, 'NETWORK', '网络异常，上传未完成，请重试'))
    xhr.onabort = () => reject(new HttpError(0, 'ABORTED', '上传已取消'))
    if (options.signal) {
      if (options.signal.aborted) {
        abort()
        return
      }
      options.signal.addEventListener('abort', abort, { once: true })
    }
    xhr.send(body)
  })
}

export const binaryUpload = {
  put: <T>(path: string, body: Blob, options: BinaryUploadOptions) => uploadBinary<T>(path, body, options),
}
