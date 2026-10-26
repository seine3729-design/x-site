import { describe, expect, it } from 'vitest'
import { DEFAULT_WARM_CONCURRENCY, DEFAULT_WARM_SEGMENTS_PER_QUALITY, warmStartPayload, type WarmStartForm } from './resource-warm-api'

const defaultForm: WarmStartForm = {
  scope: 'all',
  dramaID: '',
  includeUnpublished: false,
  mode: 'with_segments',
  segments: String(DEFAULT_WARM_SEGMENTS_PER_QUALITY),
  concurrency: String(DEFAULT_WARM_CONCURRENCY),
}

describe('资源预热开始请求', () => {
  it('默认提交 MPEG-TS Profile 的每清晰度 2 片', () => {
    expect(DEFAULT_WARM_SEGMENTS_PER_QUALITY).toBe(2)
    expect(warmStartPayload(defaultForm)).toEqual({
      include_unpublished: false,
      mode: 'with_segments',
      segments_per_quality: 2,
      concurrency: 4,
    })
  })

  it('指定剧与运营手动填写的片数原样进入请求体', () => {
    expect(
      warmStartPayload({
        ...defaultForm,
        scope: 'one',
        dramaID: '42',
        includeUnpublished: true,
        mode: 'playlist_only',
        segments: '20',
        concurrency: '1',
      })
    ).toEqual({
      drama_id: 42,
      mode: 'playlist_only',
      segments_per_quality: 20,
      concurrency: 1,
    })
  })
})
