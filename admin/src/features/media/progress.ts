/**
 * 上传与转码进度的文案。抽成纯函数，既让上传组件保持轻薄，也能直接单测
 * （组件里的 XHR 进度事件与轮询很难在测试里复现）。
 */

import type { TranscodeJobStatus } from './api'

/** 二进制大小的中文显示，统一用 1024 进制（与后端日志、对象存储一致）。 */
export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  if (value < 1024) return `${Math.round(value)} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MiB`
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GiB`
}

/** 百分比，夹在 0–100；总量未知（0）时按 0 处理。 */
export function percentOf(loaded: number, total: number): number {
  if (!Number.isFinite(loaded) || !Number.isFinite(total) || total <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((loaded / total) * 100)))
}

/** 剩余时间。小于 1 秒或算不出来时返回空串，由调用方省略这一段。 */
export function formatEta(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return ''
  if (seconds < 60) return `约剩 ${Math.ceil(seconds)} 秒`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    const rest = Math.ceil(seconds % 60)
    return rest > 0 ? `约剩 ${minutes} 分 ${rest} 秒` : `约剩 ${minutes} 分`
  }
  const hours = Math.floor(minutes / 60)
  return `约剩 ${hours} 小时 ${minutes % 60} 分`
}

export type UploadStat = {
  loaded: number
  total: number
  /** 从开始上传到现在的毫秒数，用来算速度与剩余时间。 */
  elapsedMs: number
}

/**
 * 上传阶段的文案：已传多少 / 共多少、速度、预计剩余。
 * 大视频上传要好几分钟，只给一个百分比看不出来是在传还是卡住了。
 * 开头 1 秒内样本太少，算出来的速度会抖得厉害，这时只显示字节数。
 */
export function uploadProgressText({ loaded, total, elapsedMs }: UploadStat): string {
  const parts = [`已传 ${formatBytes(loaded)} / ${formatBytes(total)}`]
  const seconds = elapsedMs / 1000
  if (seconds >= 1 && loaded > 0) {
    const speed = loaded / seconds
    parts.push(`${formatBytes(speed)}/s`)
    const eta = formatEta((total - loaded) / speed)
    if (eta) parts.push(eta)
  }
  return parts.join(' · ')
}

/** 转码阶段的文案。失败时把后端给的原因接在后面。 */
export function transcodeProgressText(status: TranscodeJobStatus, progress: number, error = ''): string {
  switch (status) {
    case 'queued':
      return '已排队，等待转码…'
    case 'running':
      return `正在转码为 HLS… ${percentOf(progress, 100)}%`
    case 'succeeded':
      return 'HLS 转码完成，分集可以播放了。'
    case 'skipped':
      return '源文件与现有产物相同，沿用上次的 HLS 产物。'
    case 'failed':
      return error ? `转码失败：${error}` : '转码失败，请重新上传。'
  }
}

/**
 * 图片压缩的来源。
 * - `server`：服务端的图片优化服务（按内容选量化参数，压缩比与画质都更稳）
 * - `local`：浏览器 canvas 兜底（优化服务没配置或额度用尽时）
 * - `reused`：原图本身已是更优的 WebP，直接上传原文件、没有重新编码
 */
export type CompressionMode = 'server' | 'local' | 'reused'

/**
 * 图片压缩前后的对比文案。运营要能判断「压得狠不狠」：封面压过头会在渐变和文字边缘出噪点，
 * 看到省了多少才好决定要不要换一张图。顺带标出是服务端压的还是浏览器兜底压的 ——
 * 浏览器兜底的画质明显更差，运营看到这行就知道该去检查优化服务的配置或额度。
 */
export function compressionSummary(originalBytes: number, finalBytes: number, mode: CompressionMode): string {
  if (!Number.isFinite(originalBytes) || originalBytes <= 0 || !Number.isFinite(finalBytes) || finalBytes <= 0) return ''
  if (mode === 'reused') return `原图 ${formatBytes(originalBytes)}，已是更优的 WebP，按原文件上传（无损）`
  const saved = Math.round((1 - finalBytes / originalBytes) * 100)
  const change = saved > 0 ? `省 ${saved}%` : saved === 0 ? '体积不变' : `大 ${-saved}%`
  const by = mode === 'server' ? '服务端压缩' : '浏览器兜底压缩'
  return `原图 ${formatBytes(originalBytes)} → WebP ${formatBytes(finalBytes)}（${change}，${by}）`
}
