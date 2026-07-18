import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '../../src/stores/auth'

const operator = {
  id: '3f6f0a1e-0000-4000-8000-000000000001',
  username: 'admin',
  display_name: 'Administrator',
  active: true,
  last_login_at: '2026-07-18T10:00:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('stores/auth', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('starts unauthenticated', () => {
    const store = useAuthStore()
    expect(store.operator).toBeNull()
    expect(store.isAuthenticated).toBe(false)
  })

  it('login success stores the operator and marks authenticated', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: operator, error: null }))

    const store = useAuthStore()
    await store.login('admin', 'correct-horse')

    expect(store.operator).toEqual(operator)
    expect(store.isAuthenticated).toBe(true)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/auth/login')
    expect(init.method).toBe('POST')
  })

  it('login failure throws with the server error and leaves state unauthenticated', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: 'invalid credentials' }, 401),
    )

    const store = useAuthStore()
    await expect(store.login('admin', 'wrong')).rejects.toThrow('invalid credentials')

    expect(store.operator).toBeNull()
    expect(store.isAuthenticated).toBe(false)
  })

  it('login rejects empty credentials without calling the API', async () => {
    const store = useAuthStore()
    await expect(store.login('', '')).rejects.toThrow()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('fetchMe populates the operator and sets checked', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: operator, error: null }))

    const store = useAuthStore()
    expect(store.checked).toBe(false)
    await store.fetchMe()

    expect(store.operator).toEqual(operator)
    expect(store.checked).toBe(true)
  })

  it('fetchMe on 401 leaves state unauthenticated but marks checked', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: 'UNAUTHENTICATED' }, 401),
    )

    const store = useAuthStore()
    await store.fetchMe()

    expect(store.operator).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    expect(store.checked).toBe(true)
  })

  it('logout clears operator state', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: operator, error: null }))
      .mockResolvedValueOnce(jsonResponse({ success: true, data: null, error: null }))

    const store = useAuthStore()
    await store.login('admin', 'correct-horse')
    await store.logout()

    expect(store.operator).toBeNull()
    expect(store.isAuthenticated).toBe(false)
  })

  it('logout clears state even when the API call fails', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: operator, error: null }))
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))

    const store = useAuthStore()
    await store.login('admin', 'correct-horse')
    await store.logout()

    expect(store.operator).toBeNull()
  })
})
