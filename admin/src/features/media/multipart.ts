/**
 * 视频分片并行上传的调度：按服务端给的分片大小切片，几路并发提交，单片失败按退避重试。
 *
 * 为什么不再整段 PUT：一个请求传几十上百 MB 会撞上 Cloudflare 的请求体上限（Free / Pro 100 MB）与
 * Traefik 的 60 秒读超时，而且只能用到一条「边缘 → 源站」连接 —— 跨区回源时单条连接很慢，几路并发才能叠出带宽。
 * 断一次也只重传那一片，不用从头再来。
 *
 * 只管调度，不碰 React 与 XHR：真正发请求的 send 由调用方注入，方便单测。
 */
import { HttpError } from '@/lib/http'

/** 一片在文件里的字节范围（左闭右开），片号从 1 开始。 */
export type PartRange = { number: number; start: number; end: number }

/** 同时在传的分片数。浏览器到边缘是一条 HTTP/2 连接，这几路在边缘之后会各用一条回源连接。 */
export const PART_CONCURRENCY = 6
/** 每片最多尝试几次（含第一次）。 */
export const PART_MAX_ATTEMPTS = 5

/** 按服务端给的分片大小切片。 */
export function planParts(size: number, partSize: number): PartRange[] {
  if (!Number.isFinite(size) || !Number.isFinite(partSize) || size <= 0 || partSize <= 0) return []
  const parts: PartRange[] = []
  for (let start = 0, number = 1; start < size; start += partSize, number++) {
    parts.push({ number, start, end: Math.min(start + partSize, size) })
  }
  return parts
}

/**
 * 这次失败值不值得重试：网络错误（status 0）、超时、限流与 5xx 重试；
 * 其余 4xx 是请求本身的问题（会话过期、片长不符、分片上传已失效），重试也一样，直接报给运营。
 */
export function isRetryable(error: unknown): boolean {
  if (!(error instanceof HttpError) || error.code === 'ABORTED') return false
  return error.status === 0 || error.status === 408 || error.status === 425 || error.status === 429 || error.status >= 500
}

/** 第 attempt 次失败后的等待：1、2、4、8 秒…封顶 15 秒，再加 0–30% 抖动，免得几片同时失败后又同时重试。 */
export function retryDelay(attempt: number, random: () => number = Math.random): number {
  const base = Math.min(15_000, 1000 * 2 ** Math.max(0, attempt - 1))
  return Math.round(base * (1 + 0.3 * random()))
}

export type SendPart = (part: PartRange, body: Blob, onProgress: (loaded: number) => void, signal: AbortSignal) => Promise<unknown>

export type UploadPartsOptions = {
  file: Blob
  partSize: number
  send: SendPart
  concurrency?: number
  maxAttempts?: number
  /** 已确认送达的字节 + 在传分片的进度，夹在 0…total。 */
  onProgress?: (loaded: number, total: number) => void
  signal?: AbortSignal
  /** 重试前的等待；测试里替换成立即返回。等待中被取消时应提前结束。 */
  wait?: (ms: number, signal: AbortSignal) => Promise<void>
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer)
        resolve()
      },
      { once: true }
    )
  })
}

/**
 * 并发上传全部分片。任一片重试用尽或遇到不可重试的错误时，取消其余在传的分片并抛出那个错误；
 * 调用方的 signal 被取消时抛出「上传已取消」。全部成功后正常返回，由调用方再提交完成。
 */
export async function uploadParts(options: UploadPartsOptions): Promise<void> {
  const { file, partSize, send, onProgress, signal } = options
  const parts = planParts(file.size, partSize)
  const total = file.size
  const concurrency = Math.max(1, Math.min(options.concurrency ?? PART_CONCURRENCY, parts.length))
  const maxAttempts = Math.max(1, options.maxAttempts ?? PART_MAX_ATTEMPTS)
  const wait = options.wait ?? sleep

  const controller = new AbortController()
  const cancel = () => controller.abort()
  if (signal?.aborted) controller.abort()
  signal?.addEventListener('abort', cancel, { once: true })

  const inflight = new Map<number, number>()
  let confirmed = 0
  let failure: unknown = null
  let next = 0
  const report = () => {
    let loaded = confirmed
    for (const value of inflight.values()) loaded += value
    onProgress?.(Math.min(loaded, total), total)
  }

  const worker = async () => {
    while (!controller.signal.aborted) {
      const part = parts[next++]
      if (!part) return
      const length = part.end - part.start
      const body = file.slice(part.start, part.end)
      for (let attempt = 1; ; attempt++) {
        inflight.set(part.number, 0)
        try {
          await send(
            part,
            body,
            (loaded) => {
              inflight.set(part.number, Math.min(Math.max(loaded, 0), length))
              report()
            },
            controller.signal
          )
          inflight.delete(part.number)
          confirmed += length
          report()
          break
        } catch (error) {
          // 失败的这一片从头再传：进度如实回退，不假装已经传过
          inflight.delete(part.number)
          report()
          if (controller.signal.aborted) return
          if (attempt >= maxAttempts || !isRetryable(error)) {
            failure ??= error
            controller.abort()
            return
          }
          await wait(retryDelay(attempt), controller.signal)
          if (controller.signal.aborted) return
        }
      }
    }
  }

  try {
    await Promise.all(Array.from({ length: concurrency }, worker))
  } finally {
    signal?.removeEventListener('abort', cancel)
  }
  if (failure) throw failure
  if (signal?.aborted) throw new HttpError(0, 'ABORTED', '上传已取消')
}
