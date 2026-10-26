import { afterEach, describe, expect, it, vi } from 'vitest'
import { buildUrl, errorMessage, http, HttpError, setCsrfToken } from './http'

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  setCsrfToken(null)
})

describe('buildUrl', () => {
  it('拼接前缀并忽略空值', () => {
    expect(buildUrl('/dramas', { page: 1, q: '', status: undefined, tag: 'hot' })).toBe(
      '/api/admin/dramas?page=1&tag=hot'
    )
  })
})

describe('http', () => {
  it('成功时解包 data，并携带 Cookie', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(200, { code: 'OK', message: 'ok', data: { id: 1 } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(http.get('/me')).resolves.toEqual({ id: 1 })
    expect(fetchMock.mock.calls[0][1]?.credentials).toBe('include')
  })

  it('写请求携带 CSRF 令牌，读请求不携带', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockImplementation(() => Promise.resolve(jsonResponse(200, { code: 'OK', message: 'ok' })))
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-1')

    await http.post('/dramas', { title: 'x' })
    await http.get('/dramas')

    const postHeaders = fetchMock.mock.calls[0][1]?.headers as Headers
    const getHeaders = fetchMock.mock.calls[1][1]?.headers as Headers
    expect(postHeaders.get('x-csrf-token')).toBe('csrf-1')
    expect(getHeaders.get('x-csrf-token')).toBeNull()
  })

  it('业务错误抛出 HttpError 并保留业务码与请求编号', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse(412, { code: 'REAUTH_REQUIRED', message: '请重新验证身份', request_id: 'r-1' })
      )
    )

    const error = await http.patch('/storage/providers/1', {}).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(HttpError)
    expect((error as HttpError).needsReauth).toBe(true)
    expect(errorMessage(error)).toBe('请重新验证身份（请求编号 r-1）')
  })

  it('限流时提示重试秒数', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(
        jsonResponse(429, { code: 'RATE_LIMITED', message: '操作太频繁' }, { 'retry-after': '30' })
      )
    )

    const error = await http.get('/dramas').catch((e: unknown) => e)
    expect(errorMessage(error)).toBe('操作太频繁（30 秒后可重试）')
  })

  it('网络错误转换为 status=0', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch')))

    const error = await http.get('/me').catch((e: unknown) => e)
    expect((error as HttpError).status).toBe(0)
  })
})
