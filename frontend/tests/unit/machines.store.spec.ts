import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../src/api/http'
import { useMachinesStore } from '../../src/stores/machines'

const machine = {
  id: 'a4b2e6a0-0000-4000-8000-000000000001',
  mac: 'aa:bb:cc:dd:ee:ff',
  name: 'node-01',
  firmware: 'uefi_x64',
  profile_id: null,
  reservation_ip: null,
  provision_state: 'ready',
  notes: '',
  created_at: '2026-07-18T10:00:00Z',
  updated_at: '2026-07-18T10:00:00Z',
  active_session_id: null,
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function listResponse(machines: unknown[], total = machines.length): Response {
  return jsonResponse({
    success: true,
    data: { machines, meta: { total: String(total), page: 1, page_size: 10 } },
    error: null,
  })
}

describe('stores/machines', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('fetchMachines stores the list and coerces int64 total', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([machine], 42))

    const store = useMachinesStore()
    await store.fetchMachines()

    expect(store.machines).toEqual([machine])
    expect(store.total).toBe(42)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/machines?')
    expect(url).toContain('page=1')
    expect(url).toContain('page_size=10')
  })

  it('fetchMachines passes state and search filters as query params', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([]))

    const store = useMachinesStore()
    store.stateFilter = 'failed'
    store.search = 'node'
    await store.fetchMachines()

    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('state=failed')
    expect(url).toContain('q=node')
  })

  it('fetchMachines surfaces server errors and clears the list', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        { success: false, data: null, error: { reason: 'INTERNAL', message: 'boom' } },
        500,
      ),
    )

    const store = useMachinesStore()
    await store.fetchMachines()

    expect(store.error).toBe('boom')
    expect(store.machines).toEqual([])
    expect(store.total).toBe(0)
    expect(store.loading).toBe(false)
  })

  it('provision replaces the machine immutably with the server reply', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([machine]))
    const store = useMachinesStore()
    await store.fetchMachines()
    const before = store.machines

    const installing = { ...machine, provision_state: 'installing' }
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: installing, error: null }))
    await store.provision(machine.id)

    expect(store.machines).not.toBe(before)
    expect(store.machines[0].provision_state).toBe('installing')
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(url).toBe(`/api/v1/machines/${machine.id}/provision`)
    expect(init.method).toBe('POST')
  })

  it('provision failure sets the store error and rethrows an ApiError', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        { success: false, data: null, error: { reason: 'CONFLICT', message: 'session active' } },
        409,
      ),
    )

    const store = useMachinesStore()
    await expect(store.provision(machine.id)).rejects.toBeInstanceOf(ApiError)
    expect(store.error).toBe('session active')
  })

  it('createMachine posts the payload and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: machine, error: null }))
      .mockResolvedValueOnce(listResponse([machine]))

    const store = useMachinesStore()
    await store.createMachine({ mac: machine.mac, name: machine.name })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/machines')
    expect(init.method).toBe('POST')
    expect(store.machines).toEqual([machine])
  })

  it('createMachine surfaces 422 field details to the caller', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: {
            reason: 'VALIDATION_FAILED',
            message: 'validation failed',
            details: { mac: 'already registered' },
          },
        },
        422,
      ),
    )

    const store = useMachinesStore()
    const failure = await store
      .createMachine({ mac: machine.mac, name: machine.name })
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).details).toEqual({ mac: 'already registered' })
    expect(store.error).toBe('validation failed')
  })

  it('deleteMachine issues DELETE and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: null, error: null }))
      .mockResolvedValueOnce(listResponse([]))

    const store = useMachinesStore()
    await store.deleteMachine(machine.id)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/v1/machines/${machine.id}`)
    expect(init.method).toBe('DELETE')
    expect(store.machines).toEqual([])
  })

  it('fetchUnknownBoots stores boots with numeric attempts', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: {
          boots: [{ mac: '11:22:33:44:55:66', last_seen: '2026-07-18T09:00:00Z', attempts: '7' }],
          meta: { total: '1', page: 1, page_size: 10 },
        },
        error: null,
      }),
    )

    const store = useMachinesStore()
    await store.fetchUnknownBoots()

    expect(store.unknownBoots).toEqual([
      { mac: '11:22:33:44:55:66', last_seen: '2026-07-18T09:00:00Z', attempts: 7 },
    ])
    expect(store.unknownTotal).toBe(1)
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/machines/unknown?')
  })

  it('registerFromUnknown removes the boot and refreshes machines', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: {
          boots: [{ mac: '11:22:33:44:55:66', last_seen: '2026-07-18T09:00:00Z', attempts: 1 }],
          meta: { total: '1', page: 1, page_size: 10 },
        },
        error: null,
      }),
    )
    const store = useMachinesStore()
    await store.fetchUnknownBoots()

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: machine, error: null }))
      .mockResolvedValueOnce(listResponse([machine]))
    await store.registerFromUnknown({
      mac: '11:22:33:44:55:66',
      name: 'node-02',
      profile_id: 'p-1',
    })

    expect(store.unknownBoots).toEqual([])
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(url).toBe('/api/v1/machines/register-unknown')
    expect(init.method).toBe('POST')
    expect(store.machines).toEqual([machine])
  })
})
