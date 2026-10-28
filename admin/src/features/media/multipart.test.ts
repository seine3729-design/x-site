import { describe, expect, it } from 'vitest'
import { HttpError } from '@/lib/http'
import { isRetryable, planParts, retryDelay, uploadParts, type PartRange } from './multipart'

const MiB = 1024 * 1024
const noWait = async () => {}
const gatewayError = () => new HttpError(502, 'INTERNAL', '网关错误')

describe('planParts', () => {
  it('按分片大小切，最后一片放余数，片号从 1 开始', () => {
    expect(planParts(12, 5)).toEqual([
      { number: 1, start: 0, end: 5 },
      { number: 2, start: 5, end: 10 },
      { number: 3, start: 10, end: 12 },
    ])
  })

  it('不足一片时只有一片，整除时没有空的最后一片', () => {
    expect(planParts(3, 5)).toEqual([{ number: 1, start: 0, end: 3 }])
    expect(planParts(10, 5).map((p) => p.number)).toEqual([1, 2])
  })

  it('30 MiB 按 5 MiB 切成 6 片，各片之和等于文件大小', () => {
    const parts = planParts(30 * MiB, 5 * MiB)
    expect(parts).toHaveLength(6)
    expect(parts.reduce((sum, p) => sum + p.end - p.start, 0)).toBe(30 * MiB)
  })

  it('非法输入给空数组，不会死循环', () => {
    expect(planParts(0, 5)).toEqual([])
    expect(planParts(10, 0)).toEqual([])
    expect(planParts(Number.NaN, 5)).toEqual([])
  })
})

describe('isRetryable', () => {
  it('网络错误、超时、限流与 5xx 重试', () => {
    for (const status of [0, 408, 425, 429, 500, 502, 503, 524]) {
      expect(isRetryable(new HttpError(status, 'X', 'x'))).toBe(true)
    }
  })

  it('其余 4xx、用户取消与非 HttpError 不重试', () => {
    for (const status of [400, 401, 403, 404, 409, 413]) {
      expect(isRetryable(new HttpError(status, 'X', 'x'))).toBe(false)
    }
    expect(isRetryable(new HttpError(0, 'ABORTED', '上传已取消'))).toBe(false)
    expect(isRetryable(new Error('boom'))).toBe(false)
  })
})

describe('retryDelay', () => {
  it('指数退避封顶 15 秒，抖动最多加 30%', () => {
    expect(retryDelay(1, () => 0)).toBe(1000)
    expect(retryDelay(3, () => 0)).toBe(4000)
    expect(retryDelay(10, () => 0)).toBe(15_000)
    expect(retryDelay(1, () => 1)).toBe(1300)
  })
})

/** 记录调用的假 send：可按片号注入若干次失败，并统计同时在传的最大路数。 */
function fakeSender(failures: Record<number, HttpError[]> = {}) {
  const calls: number[] = []
  const sent = new Map<number, number>()
  let active = 0
  let peak = 0
  const send = async (part: PartRange, body: Blob, onProgress: (loaded: number) => void, signal: AbortSignal) => {
    calls.push(part.number)
    active++
    peak = Math.max(peak, active)
    try {
      await new Promise((resolve) => setTimeout(resolve, 1))
      if (signal.aborted) throw new HttpError(0, 'ABORTED', '上传已取消')
      onProgress(body.size / 2)
      const queue = failures[part.number]
      if (queue?.length) throw queue.shift()
      onProgress(body.size)
      sent.set(part.number, body.size)
    } finally {
      active--
    }
  }
  return { send, calls, sent, peak: () => peak }
}

describe('uploadParts', () => {
  const file = new Blob([new Uint8Array(23)])

  it('全部分片送达，同时在传的路数不超过上限，进度最终到 100%', async () => {
    const sender = fakeSender()
    const progress: number[] = []
    await uploadParts({ file, partSize: 5, concurrency: 3, send: sender.send, onProgress: (loaded) => progress.push(loaded), wait: noWait })
    expect([...sender.sent.keys()].toSorted((a, b) => a - b)).toEqual([1, 2, 3, 4, 5])
    expect(sender.sent.get(5)).toBe(3)
    expect(sender.peak()).toBeLessThanOrEqual(3)
    expect(sender.peak()).toBeGreaterThan(1)
    expect(progress.at(-1)).toBe(23)
    expect(Math.max(...progress)).toBeLessThanOrEqual(23)
  })

  it('可重试的失败只重传那一片', async () => {
    const sender = fakeSender({ 2: [new HttpError(503, 'UNAVAILABLE', '忙'), new HttpError(0, 'NETWORK', '断网')] })
    await uploadParts({ file, partSize: 5, send: sender.send, wait: noWait })
    expect(sender.calls.filter((n) => n === 2)).toHaveLength(3)
    expect(sender.calls.filter((n) => n === 1)).toHaveLength(1)
    expect(sender.sent.size).toBe(5)
  })

  it('不可重试的错误立刻停下并抛出，不再提交后面的分片', async () => {
    const fatal = new HttpError(400, 'BAD_REQUEST', '上传会话已过期，请重新选择文件')
    const sender = fakeSender({ 1: [fatal] })
    await expect(uploadParts({ file, partSize: 5, concurrency: 1, send: sender.send, wait: noWait })).rejects.toBe(fatal)
    expect(sender.calls).toEqual([1])
  })

  it('重试用尽后抛出最后一次的错误', async () => {
    const sender = fakeSender({ 1: [gatewayError(), gatewayError(), gatewayError()] })
    await expect(uploadParts({ file, partSize: 5, concurrency: 1, maxAttempts: 3, send: sender.send, wait: noWait })).rejects.toMatchObject({
      status: 502,
    })
    expect(sender.calls).toEqual([1, 1, 1])
  })

  it('调用方取消时抛出「上传已取消」', async () => {
    const controller = new AbortController()
    const sender = fakeSender()
    const running = uploadParts({ file, partSize: 5, concurrency: 1, send: sender.send, signal: controller.signal, wait: noWait })
    controller.abort()
    await expect(running).rejects.toMatchObject({ code: 'ABORTED' })
    expect(sender.sent.size).toBeLessThan(5)
  })
})
