import 'server-only'
import { headers } from 'next/headers'
import { env } from '@/env'

/**
 * 服务端调用 Go 后端的唯一出口（feedback/docs/adr/0008）。
 *
 * 浏览器永远只请求本服务的同源地址；这里带 X-Service-Token 转发到 Go，
 * 命中 service 侧的 isInternalSSR 豁免走明文，所以前台不需要实现 HPKE。
 * 服务令牌只存在于这个进程里，不能出现在任何发给浏览器的内容中。
 */

/** 后端统一响应包（docs/07 第 0.1 节）。 */
type Envelope<T> = {
  code: string
  message: string
  data?: T
  request_id?: string
}

/** 后端返回的业务错误。页面据此决定显示登录墙、404、限流还是重试。 */
export class ServiceError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly requestId?: string,
    options?: ErrorOptions,
  ) {
    super(message, options)
    this.name = 'ServiceError'
  }

  get isNotFound() {
    return this.status === 404
  }

  get needLogin() {
    return this.code === 'NEED_LOGIN' || this.code === 'TOKEN_EXPIRED'
  }
}

export type RequestOptions = {
  /** 社区会话令牌。传了就以该用户身份请求，否则按匿名。 */
  token?: string
  /** 查询参数；undefined / 空串的键会被丢掉，避免拼出 ?status= 这种无意义参数。 */
  query?: Record<string, string | number | boolean | undefined | null>
  body?: unknown
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  /**
   * 缓存策略。默认 no-store：这个站的列表都带访问者视角（可见性过滤在 SQL 里做），
   * 共享缓存会串号。确定是完全公开且无个人字段的响应才可以传 revalidate。
   */
  revalidate?: number
  tags?: string[]
  timeoutMs?: number
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  const url = new URL(env.serviceInternalUrl + path)
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value === undefined || value === null || value === '') continue
    url.searchParams.set(key, String(value))
  }
  return url.toString()
}

/** 每个出站请求都要带的那几个头。 */
function baseHeaders(): Headers {
  const out = new Headers()
  out.set('accept', 'application/json')
  out.set('x-service-token', env.serviceToken)
  return out
}

/** 把浏览器请求的来源信息透传给后端，便于后端限流与日志串联。 */
async function forwardHeaders(): Promise<Headers> {
  const incoming = await headers()
  const out = baseHeaders()
  const requestId = incoming.get('x-request-id')
  if (requestId) out.set('x-request-id', requestId)
  const forwardedFor = incoming.get('x-forwarded-for')
  if (forwardedFor) out.set('x-forwarded-for', forwardedFor)
  const ua = incoming.get('user-agent')
  if (ua) out.set('user-agent', ua)
  return out
}

/**
 * 发请求、解包 data、按需重试。两个入口共用这一段。
 *
 * - 非 2xx 或 code != OK 抛 ServiceError（带后端的 request_id，方便对日志）；
 * - 只有 GET 在网络错误时重试一次，写请求不重试（避免重复提交）。
 */
async function execute<T>(
  path: string,
  headersOut: Headers,
  options: RequestOptions,
): Promise<T> {
  const method = options.method ?? 'GET'
  if (options.body !== undefined) headersOut.set('content-type', 'application/json')

  const init: RequestInit & { next?: { revalidate?: number; tags?: string[] } } = {
    method,
    headers: headersOut,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: AbortSignal.timeout(options.timeoutMs ?? env.timeoutMs),
  }
  if (options.revalidate !== undefined) {
    init.next = { revalidate: options.revalidate, tags: options.tags }
  } else {
    init.cache = 'no-store'
  }

  const url = buildUrl(path, options.query)
  const attempts = method === 'GET' ? 2 : 1
  let lastError: unknown

  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      const response = await fetch(url, init)
      const payload = (await response.json().catch(() => null)) as Envelope<T> | null

      if (!response.ok || !payload || payload.code !== 'OK') {
        throw new ServiceError(
          response.status,
          payload?.code ?? 'INTERNAL',
          payload?.message ?? '',
          payload?.request_id ?? response.headers.get('x-request-id') ?? undefined,
        )
      }
      return payload.data as T
    } catch (error) {
      // 业务错误不重试，只有网络错误与超时才重试
      if (error instanceof ServiceError) throw error
      lastError = error
    }
  }

  throw new ServiceError(503, 'UNAVAILABLE', '', undefined, { cause: lastError })
}

/** 带访问者身份与来源信息调后端。绝大多数读写都走这个。 */
export async function callService<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headersOut = await forwardHeaders()
  if (options.token) headersOut.set('authorization', `Bearer ${options.token}`)
  return execute<T>(path, headersOut, options)
}

/**
 * 以**匿名身份**读公开数据，不带任何请求上下文。
 *
 * 专门给 `use cache` 里的调用用（见 lib/api/public-queries.ts）。两个限制都是必须的：
 *
 * - 不读 Cookie：`use cache` 内部禁止访问请求上下文，读了会直接报错；
 *   更要紧的是缓存按源共享，带上身份就等于把某个人的可见范围发给所有人。
 * - 不转发 user-agent / x-forwarded-for / x-request-id：这些头会进 fetch 的
 *   Data Cache key。带上 UA 的话，每种浏览器版本各占一份缓存，命中率基本为零。
 *
 * 换来的代价是看不到「仅成员可见」的内容 —— 这正是公开视角该有的样子。
 */
export async function callPublicService<T>(
  path: string,
  options: Omit<RequestOptions, 'token'> = {},
): Promise<T> {
  return execute<T>(path, baseHeaders(), options)
}

/**
 * 读接口的包装：把「查不到 / 没权限」变成 null，让页面能直接渲染空状态，
 * 而不是每个调用点都写一遍 try/catch。
 *
 * 注意不要用它包写接口——写失败必须让用户知道。
 */
export async function tryService<T>(
  path: string,
  options?: RequestOptions,
): Promise<T | null> {
  try {
    return await callService<T>(path, options)
  } catch (error) {
    if (error instanceof ServiceError && (error.isNotFound || error.status === 403)) return null
    throw error
  }
}
