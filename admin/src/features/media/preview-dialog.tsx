import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AlertCircle, LoaderCircle } from 'lucide-react'
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui'
import { errorMessage } from '@/lib/http'
import { episodePreviewApi } from './api'

/**
 * 分集试播（docs/06 第 37 节）：后台内直接播一集，确认转码产物与线路真的通。
 *
 * 地址来自 POST /api/admin/episodes/:id/preview：同源、走后台会话、不加密，所以浏览器
 * 取清单与内容密钥时自动带上后台 Cookie，不需要前台那套报文加密客户端。
 */

/** 超过这个时间还没有画面，基本是产物路径不对或线路连不上（docs/06 第 37 节）。 */
const STALL_HINT_MS = 15_000

export type PreviewEpisode = {
  id: number
  number: number
  title: string
  duration_sec: number
  accessLabel: string
  trialLabel: string
}

export function EpisodePreviewDialog({ episode, onClose }: { episode: PreviewEpisode; onClose: () => void }) {
  const video = useRef<HTMLVideoElement>(null)
  const [playError, setPlayError] = useState('')
  const [stalled, setStalled] = useState(false)
  const preview = useQuery({
    queryKey: ['media', 'preview', episode.id],
    queryFn: () => episodePreviewApi.grant(episode.id),
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  const url = preview.data?.url

  useEffect(() => {
    const element = video.current
    if (!url || !element) return
    let cancelled = false
    let hls: { destroy: () => void } | null = null
    setPlayError('')
    setStalled(false)
    void (async () => {
      // 动态导入：hls.js 有几百 KB，只有真正试播时才下载。
      const { default: Hls } = await import('hls.js')
      if (cancelled) return
      if (Hls.isSupported()) {
        // 清单、密钥都在后台同源接口上，带上 Cookie 才能过会话校验。
        const instance = new Hls({
          xhrSetup: (xhr) => {
            xhr.withCredentials = true
          },
        })
        hls = instance
        instance.on(Hls.Events.ERROR, (_event, data) => {
          if (data.fatal) setPlayError(`播放失败（${data.details}）。检查该集产物是否完整、线路能否访问。`)
        })
        instance.loadSource(url)
        instance.attachMedia(element)
      } else if (element.canPlayType('application/vnd.apple.mpegurl')) {
        element.src = url // Safari 原生支持 HLS
      } else {
        setPlayError('当前浏览器不支持 HLS 播放，请换 Chrome 或 Safari。')
      }
    })()
    const timer = window.setTimeout(() => {
      if (!cancelled && (video.current?.readyState ?? 0) < 1) setStalled(true)
    }, STALL_HINT_MS)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
      hls?.destroy()
    }
  }, [url])

  const notice = playError || (preview.isError ? errorMessage(preview.error) : '')
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            试播第 {episode.number} 集{episode.title ? `「${episode.title}」` : ''}
          </DialogTitle>
          <DialogDescription>
            {episode.accessLabel} · {episode.trialLabel} · 完整时长播放，不受试看秒数限制；地址十分钟后失效。
          </DialogDescription>
        </DialogHeader>

        <video ref={video} controls playsInline className='aspect-video w-full rounded bg-black' aria-label={`第 ${episode.number} 集试播`}>
          {/* 转封装产物目前不含字幕轨，空 track 明确声明「没有字幕」而不是漏了 */}
          <track kind='captions' />
        </video>

        <div className='flex min-h-5 flex-col gap-1 text-xs' aria-live='polite'>
          {preview.isPending && (
            <p className='flex items-center gap-2 text-muted-foreground'>
              <LoaderCircle className='size-4 animate-spin' aria-hidden /> 正在获取播放地址…
            </p>
          )}
          {notice && (
            <p className='flex items-center gap-2 text-destructive'>
              <AlertCircle className='size-4' aria-hidden /> {notice}
            </p>
          )}
          {!notice && preview.data && <p className='text-muted-foreground'>线路：{preview.data.line_name}</p>}
          {!notice && stalled && <p className='text-destructive'>十几秒没有画面：可能是产物路径不对，或这条线路连不上。</p>}
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={() => void preview.refetch()} disabled={preview.isFetching}>
            重新获取地址
          </Button>
          <Button type='button' onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
