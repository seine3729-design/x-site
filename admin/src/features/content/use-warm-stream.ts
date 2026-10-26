import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { warmKeys, warmProgressQuery, warmStreamURL, type WarmProgress } from './resource-warm-api'

/**
 * 订阅预热进度。
 *
 * 任务在跑时用 SSE（省掉轮询的请求数），不在跑时只取一次。SSE 连不上就降级成 2 秒轮询 ——
 * 有些代理不透传 text/event-stream，降级比一直转圈好。
 */
export function useWarmProgress(enabled: boolean) {
  // 先用普通请求拿一次：知道有没有在跑的任务，才决定要不要开 SSE
  const initial = useQuery({ ...warmProgressQuery, enabled })
  const [streamed, setStreamed] = useState<WarmProgress | null>(null)
  const [streamBroken, setStreamBroken] = useState(false)
  const qc = useQueryClient()

  const data = streamed ?? initial.data
  const running = data?.running ?? false

  // SSE 正常工作时不轮询；连不上才兜底
  const poll = useQuery({
    ...warmProgressQuery,
    enabled: enabled && streamBroken && running,
    refetchInterval: 2000,
  })

  useEffect(() => {
    if (!enabled || streamBroken || !running) return
    const es = new EventSource(warmStreamURL, { withCredentials: true })
    let done = false

    es.addEventListener('progress', (e) => {
      try {
        const p = JSON.parse((e as MessageEvent).data) as WarmProgress
        setStreamed(p)
        if (!p.running) {
          // 跑完了：服务端会关掉这条流，先标记一下，免得 error 事件把正常结束当成故障。
          done = true
          es.close()
          // 丢掉流里的数据、重新取一次：最终状态要来自一次真实请求，
          // 不能靠「关流前最后那一帧刚好是最终值」这种时序上的巧合（它常常不是）。
          setStreamed(null)
          void qc.invalidateQueries({ queryKey: warmKeys.all })
        }
      } catch {
        /* 坏帧丢掉，下一帧还会来 */
      }
    })

    es.addEventListener('error', () => {
      es.close()
      // 任务跑完时服务端会主动关流，那是正常结束，不该降级成轮询
      if (!done) setStreamBroken(true)
    })
    return () => {
      done = true
      es.close()
    }
    // running 变化时重建连接：从「没有在跑」变成「在跑」时要接上
  }, [enabled, streamBroken, running, qc])

  // 新开一轮之后重新尝试 SSE（上一次可能是因为没有在跑的任务而没连）
  const retry = () => {
    setStreamBroken(false)
    setStreamed(null)
    void qc.invalidateQueries({ queryKey: warmKeys.progress })
  }

  return {
    data: poll.data ?? data,
    isPending: initial.isPending,
    isError: initial.isError,
    refetch: initial.refetch,
    /** 进度是通过 SSE 实时推来的（而不是轮询或单次请求）。 */
    streaming: !streamBroken && running && streamed !== null,
    retry,
  }
}
