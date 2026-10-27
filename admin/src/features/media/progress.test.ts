import { describe, expect, it } from 'vitest'
import { compressionSummary, formatBytes, formatEta, percentOf, transcodeProgressText, uploadProgressText } from './progress'

describe('formatBytes', () => {
  it('按 1024 进制给出中文可读的大小', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(900)).toBe('900 B')
    expect(formatBytes(1536)).toBe('1.5 KiB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MiB')
    expect(formatBytes(3 * 1024 * 1024 * 1024)).toBe('3.00 GiB')
  })

  it('非法值按 0 处理，不渲染出 NaN', () => {
    expect(formatBytes(Number.NaN)).toBe('0 B')
    expect(formatBytes(-1)).toBe('0 B')
  })
})

describe('percentOf', () => {
  it('夹在 0–100，总量未知时为 0', () => {
    expect(percentOf(50, 200)).toBe(25)
    expect(percentOf(300, 200)).toBe(100)
    expect(percentOf(10, 0)).toBe(0)
    expect(percentOf(-5, 100)).toBe(0)
  })
})

describe('formatEta', () => {
  it('分段显示剩余时间', () => {
    expect(formatEta(30)).toBe('约剩 30 秒')
    expect(formatEta(95)).toBe('约剩 1 分 35 秒')
    expect(formatEta(120)).toBe('约剩 2 分')
    expect(formatEta(3720)).toBe('约剩 1 小时 2 分')
  })

  it('算不出来时留空，由调用方省略这一段', () => {
    expect(formatEta(0)).toBe('')
    expect(formatEta(Number.POSITIVE_INFINITY)).toBe('')
  })
})

describe('uploadProgressText', () => {
  it('开头不足 1 秒时只报字节数（速度样本太少会抖）', () => {
    expect(uploadProgressText({ loaded: 1024, total: 10 * 1024, elapsedMs: 300 })).toBe('已传 1.0 KiB / 10.0 KiB')
  })

  it('有足够样本时附上速度与预计剩余', () => {
    // 4 秒传了 4 MiB → 1 MiB/s，剩 6 MiB 约 6 秒
    const text = uploadProgressText({ loaded: 4 * 1024 * 1024, total: 10 * 1024 * 1024, elapsedMs: 4000 })
    expect(text).toBe('已传 4.0 MiB / 10.0 MiB · 1.0 MiB/s · 约剩 6 秒')
  })

  it('传完后不再显示剩余时间', () => {
    const text = uploadProgressText({ loaded: 1024 * 1024, total: 1024 * 1024, elapsedMs: 2000 })
    expect(text).toBe('已传 1.0 MiB / 1.0 MiB · 512.0 KiB/s')
  })
})

describe('transcodeProgressText', () => {
  it('每种任务状态都有明确说法', () => {
    expect(transcodeProgressText('queued', 0)).toContain('已排队')
    expect(transcodeProgressText('running', 42)).toBe('正在转码为 HLS… 42%')
    expect(transcodeProgressText('succeeded', 100)).toContain('完成')
    expect(transcodeProgressText('skipped', 0)).toContain('沿用')
  })

  it('失败时带上后端给的原因', () => {
    expect(transcodeProgressText('failed', 30, '源文件不是有效的视频')).toBe('转码失败：源文件不是有效的视频')
    expect(transcodeProgressText('failed', 30)).toBe('转码失败，请重新上传。')
  })
})

describe('compressionSummary', () => {
  it('给出压缩前后大小与节省比例', () => {
    expect(compressionSummary(2 * 1024 * 1024, 512 * 1024, 'server')).toBe('原图 2.0 MiB → WebP 512.0 KiB（省 75%，服务端压缩）')
  })

  it('压完反而变大时如实说明（不假装省了）', () => {
    expect(compressionSummary(100 * 1024, 120 * 1024, 'server')).toBe('原图 100.0 KiB → WebP 120.0 KiB（大 20%，服务端压缩）')
  })

  it('浏览器兜底压缩要标出来：画质明显更差，运营看到才知道去查优化服务', () => {
    expect(compressionSummary(1024 * 1024, 512 * 1024, 'local')).toBe('原图 1.0 MiB → WebP 512.0 KiB（省 50%，浏览器兜底压缩）')
  })

  it('原图更优时说明按原文件上传、没有重新编码', () => {
    const text = compressionSummary(300 * 1024, 300 * 1024, 'reused')
    expect(text).toContain('无损')
    expect(text).toContain('原文件')
    expect(text).not.toContain('→')
  })

  it('大小缺失时留空，不渲染出 NaN', () => {
    expect(compressionSummary(0, 100, 'server')).toBe('')
    expect(compressionSummary(100, Number.NaN, 'server')).toBe('')
  })
})
