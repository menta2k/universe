import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useSessionsStore } from '../../src/stores/sessions'

const session = {
  id: 's-0000-0000-0000-000000000001',
  machine_id: 'm-1',
  machine_name: 'node-01',
  machine_mac: 'aa:bb:cc:dd:ee:ff',
  profile_id: 'p-1',
  profile_version: 3,
  state: 'active',
  started_at: '2026-07-18T10:00:00Z',
  ended_at: null,
  failure_phase: null,
}

/** Minimal EventSource stand-in that records instances and dispatched events. */
class MockEventSource {
  static instances: MockEventSource[] = []
  readonly url: string
  readonly withCredentials: boolean
  closed = false
  private readonly listeners: Record<string, Array<(event: unknown) => void>> = {}

  constructor(url: string, init?: { withCredentials?: boolean }) {
    this.url = url
    this.withCredentials = init?.withCredentials ?? false
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, cb: (event: unknown) => void): void {
    ;(this.listeners[type] ??= []).push(cb)
  }

  removeEventListener(type: string, cb: (event: unknown) => void): void {
    this.listeners[type] = (this.listeners[type] ?? []).filter((l) => l !== cb)
  }

  close(): void {
    this.closed = true
  }

  emitMessage(data: string): void {
    for (const cb of this.listeners['message'] ?? []) cb({ data })
  }
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function listResponse(sessions: unknown[], total = sessions.length): Response {
  return jsonResponse({
    success: true,
    data: { sessions, meta: { total: String(total), page: 1, page_size: 10 } },
    error: null,
  })
}

describe('stores/sessions', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('EventSource', MockEventSource)
    MockEventSource.instances = []
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('fetchSessions stores the list, coerces int64 total, and sends filters', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([session], 12))

    const store = useSessionsStore()
    store.stateFilter = 'active'
    store.machineIdFilter = 'm-1'
    await store.fetchSessions()

    expect(store.sessions).toHaveLength(1)
    expect(store.sessions[0].machine_name).toBe('node-01')
    expect(store.total).toBe(12)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/sessions?')
    expect(url).toContain('state=active')
    expect(url).toContain('machine_id=m-1')
  })

  it('fetchSessions surfaces server errors and clears the list', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: { message: 'boom' } }, 500),
    )

    const store = useSessionsStore()
    await store.fetchSessions()

    expect(store.error).toBe('boom')
    expect(store.sessions).toEqual([])
    expect(store.total).toBe(0)
  })

  it('fetchSession parses timeline detail and evidence JSON strings', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: {
          session,
          timeline: [
            {
              time: '2026-07-18T10:00:01Z',
              session_id: session.id,
              machine_mac: session.machine_mac,
              phase: 'dhcp_discover',
              outcome: 'ok',
              detail: '{"iface":"eth0"}',
            },
          ],
          evidence: '{"last_error":"disk full"}',
        },
        error: null,
      }),
    )

    const store = useSessionsStore()
    await store.fetchSession(session.id)

    expect(store.current?.id).toBe(session.id)
    expect(store.timeline).toHaveLength(1)
    expect(store.timeline[0].detail).toEqual({ iface: 'eth0' })
    expect(store.evidence).toEqual({ last_error: 'disk full' })
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe(`/api/v1/sessions/${session.id}`)
  })

  it('subscribeLive opens a scoped EventSource and appends events immutably', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: { session, timeline: [], evidence: '{}' },
        error: null,
      }),
    )
    const store = useSessionsStore()
    await store.fetchSession(session.id)
    const before = store.timeline

    store.subscribeLive(session.id)
    const source = MockEventSource.instances[0]
    expect(source.url).toContain('/api/v1/events/stream?session_id=')
    expect(source.withCredentials).toBe(true)

    source.emitMessage(
      JSON.stringify({
        time: '2026-07-18T10:00:05Z',
        session_id: session.id,
        machine_mac: session.machine_mac,
        phase: 'tftp_transfer',
        outcome: 'ok',
        detail: { file: 'vmlinuz' },
      }),
    )

    expect(store.timeline).not.toBe(before)
    expect(store.timeline).toHaveLength(1)
    expect(store.timeline[0].phase).toBe('tftp_transfer')
    expect(store.timeline[0].detail).toEqual({ file: 'vmlinuz' })
  })

  it('ignores malformed SSE payloads without appending or throwing', () => {
    const store = useSessionsStore()
    store.subscribeLive(session.id)
    const source = MockEventSource.instances[0]

    source.emitMessage('not-json')
    source.emitMessage(JSON.stringify({ phase: 'x', outcome: 'bogus' }))

    expect(store.timeline).toEqual([])
  })

  it('a terminal event refreshes the session list', async () => {
    fetchMock.mockResolvedValue(listResponse([session]))
    const store = useSessionsStore()

    store.subscribeLive(session.id)
    const source = MockEventSource.instances[0]
    source.emitMessage(
      JSON.stringify({
        time: '2026-07-18T10:05:00Z',
        session_id: session.id,
        machine_mac: session.machine_mac,
        phase: 'session_completed',
        outcome: 'ok',
        detail: {},
      }),
    )
    await Promise.resolve()

    expect(fetchMock).toHaveBeenCalled()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/sessions?')
  })

  it('unsubscribeLive closes the underlying EventSource', () => {
    const store = useSessionsStore()
    store.subscribeLive(session.id)
    const source = MockEventSource.instances[0]

    store.unsubscribeLive()

    expect(source.closed).toBe(true)
  })
})
