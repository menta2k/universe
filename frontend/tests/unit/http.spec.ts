import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, request, requestWithMeta, setUnauthorizedHandler } from '../../src/api/http'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api/http request', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
    setUnauthorizedHandler(null)
  })

  it('unwraps the response envelope and returns data', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: true, data: { id: 'm1', name: 'node-01' }, error: null }),
    )

    const data = await request<{ id: string; name: string }>('/api/v1/machines/m1')

    expect(data).toEqual({ id: 'm1', name: 'node-01' })
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/machines/m1')
    expect(init.credentials).toBe('include')
  })

  it('returns data plus meta via requestWithMeta', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: [{ id: 'm1' }],
        error: null,
        meta: { total: 42, page: 2, page_size: 20 },
      }),
    )

    const result = await requestWithMeta<{ id: string }[]>('/api/v1/machines', {
      query: { page: 2, page_size: 20 },
    })

    expect(result.data).toEqual([{ id: 'm1' }])
    expect(result.meta).toEqual({ total: 42, page: 2, page_size: 20 })
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/machines?page=2&page_size=20')
  })

  it('serializes body as JSON and sets content type', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: null, error: null }))

    await request('/api/v1/auth/login', {
      method: 'POST',
      body: { username: 'admin', password: 'pw' },
    })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ username: 'admin', password: 'pw' }))
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
  })

  it('throws ApiError with the server error string on failed envelope', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: 'profile name already exists' }, 422),
    )

    const err = await request('/api/v1/profiles', { method: 'POST', body: {} }).catch(
      (e: unknown) => e,
    )

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).message).toBe('profile name already exists')
    expect((err as ApiError).status).toBe(422)
  })

  it('throws ApiError on non-envelope / invalid JSON responses', async () => {
    fetchMock.mockResolvedValueOnce(new Response('<html>gateway error</html>', { status: 502 }))

    const err = await request('/api/v1/machines').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(502)
  })

  it('wraps network failures in ApiError', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))

    const err = await request('/api/v1/machines').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(0)
  })

  it('invokes the unauthorized handler on 401 and throws', async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: 'UNAUTHENTICATED' }, 401),
    )

    const err = await request('/api/v1/machines').catch((e: unknown) => e)

    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(401)
  })

  it('skips the unauthorized handler when skipAuthRedirect is set', async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: 'invalid credentials' }, 401),
    )

    const err = await request('/api/v1/auth/login', {
      method: 'POST',
      body: { username: 'a', password: 'b' },
      skipAuthRedirect: true,
    }).catch((e: unknown) => e)

    expect(onUnauthorized).not.toHaveBeenCalled()
    expect((err as ApiError).message).toBe('invalid credentials')
  })
})
