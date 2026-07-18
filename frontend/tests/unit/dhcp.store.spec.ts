import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { DhcpConfigInput } from '../../src/api/dhcp'
import { ApiError } from '../../src/api/http'
import { useDhcpStore } from '../../src/stores/dhcp'

const wireConfig = {
  enabled: false,
  version: '2',
  interface: 'eth0',
  lease_ttl_seconds: '3600',
  subnets: [
    {
      id: 's-1',
      network: '10.0.0.0/24',
      range_start: '10.0.0.100',
      range_end: '10.0.0.200',
      gateway: '10.0.0.1',
      dns: ['10.0.0.1'],
      next_server: '10.0.0.2',
    },
  ],
  updated_by: 'op-1',
  updated_at: '2026-07-18T10:00:00Z',
}

const configInput: DhcpConfigInput = {
  lease_ttl_seconds: 7200,
  subnets: [
    {
      network: '10.0.0.0/24',
      range_start: '10.0.0.100',
      range_end: '10.0.0.200',
      gateway: '10.0.0.1',
      dns: ['10.0.0.1'],
    },
  ],
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function ok(data: unknown): Response {
  return jsonResponse({ success: true, data, error: null })
}

describe('stores/dhcp', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('fetchConfig coerces int64 wire fields and normalizes subnets', async () => {
    fetchMock.mockResolvedValueOnce(ok(wireConfig))

    const store = useDhcpStore()
    await store.fetchConfig()

    expect(store.config?.version).toBe(2)
    expect(store.config?.lease_ttl_seconds).toBe(3600)
    expect(store.config?.enabled).toBe(false)
    expect(store.config?.subnets).toHaveLength(1)
    expect(store.config?.subnets[0].network).toBe('10.0.0.0/24')
    expect(store.error).toBeNull()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toBe('/api/v1/dhcp/config')
  })

  it('fetchConfig surfaces server errors', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: { message: 'boom' } }, 500),
    )

    const store = useDhcpStore()
    await store.fetchConfig()

    expect(store.error).toBe('boom')
    expect(store.config).toBeNull()
  })

  it('updateConfig PUTs the input and replaces config immutably', async () => {
    fetchMock.mockResolvedValueOnce(ok(wireConfig))
    const store = useDhcpStore()
    await store.fetchConfig()
    const before = store.config

    fetchMock.mockResolvedValueOnce(ok({ ...wireConfig, version: '3' }))
    await store.updateConfig(configInput)

    expect(store.config).not.toBe(before)
    expect(store.config?.version).toBe(3)
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(url).toBe('/api/v1/dhcp/config')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body as string)).toEqual(configInput)
  })

  it('updateConfig surfaces 422 field details to the caller', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: {
            reason: 'VALIDATION_FAILED',
            message: 'validation failed',
            details: { 'subnets[0].range': 'range end precedes start' },
          },
        },
        422,
      ),
    )

    const store = useDhcpStore()
    const failure = await store
      .updateConfig(configInput)
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).status).toBe(422)
    expect((failure as ApiError).details).toEqual({
      'subnets[0].range': 'range end precedes start',
    })
    expect(store.error).toBe('validation failed')
  })

  it('enable POSTs and updates config to enabled', async () => {
    fetchMock.mockResolvedValueOnce(ok({ ...wireConfig, enabled: true, version: '3' }))

    const store = useDhcpStore()
    await store.enable()

    expect(store.config?.enabled).toBe(true)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/dhcp/enable')
    expect(init.method).toBe('POST')
  })

  it('disable POSTs and updates config to disabled', async () => {
    fetchMock.mockResolvedValueOnce(ok({ ...wireConfig, enabled: false }))

    const store = useDhcpStore()
    await store.disable()

    expect(store.config?.enabled).toBe(false)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/dhcp/disable')
    expect(init.method).toBe('POST')
  })

  it('fetchLeases loads rows and total, coercing meta', async () => {
    fetchMock.mockResolvedValueOnce(
      ok({
        leases: [
          {
            ip: '10.0.0.100',
            mac: 'aa:bb:cc:dd:ee:ff',
            machine_id: 'm-1',
            machine_name: 'node-01',
            expires_at: '2026-07-18T12:00:00Z',
          },
        ],
        meta: { total: '5', page: 1, page_size: 50 },
      }),
    )

    const store = useDhcpStore()
    await store.fetchLeases()

    expect(store.leases).toHaveLength(1)
    expect(store.leases[0].machine_name).toBe('node-01')
    expect(store.leasesTotal).toBe(5)
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/dhcp/leases?')
    expect(url).toContain('page=1')
  })

  it('fetchConflicts loads foreign servers, coercing offers_seen', async () => {
    fetchMock.mockResolvedValueOnce(
      ok({
        servers: [{ server_id: '10.9.9.9', last_seen: '2026-07-18T11:00:00Z', offers_seen: '12' }],
        meta: { total: '1', page: 1, page_size: 50 },
      }),
    )

    const store = useDhcpStore()
    await store.fetchConflicts()

    expect(store.conflicts).toHaveLength(1)
    expect(store.conflicts[0].server_id).toBe('10.9.9.9')
    expect(store.conflicts[0].offers_seen).toBe(12)
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/dhcp/conflicts?')
  })

  it('fetchConflicts surfaces server errors without clearing prior config', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: { message: 'segment scan failed' } }, 500),
    )

    const store = useDhcpStore()
    await store.fetchConflicts()

    expect(store.error).toBe('segment scan failed')
    expect(store.conflicts).toEqual([])
  })
})
