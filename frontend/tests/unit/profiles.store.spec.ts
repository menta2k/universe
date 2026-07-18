import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../src/api/http'
import type { ProfileInput } from '../../src/api/profiles'
import { useProfilesStore } from '../../src/stores/profiles'

const wireProfile = {
  id: 'p-0000-4000-8000-000000000001',
  name: 'ubuntu-server',
  version: '3',
  ubuntu_release: 'noble',
  storage_layout: '{"mode":"lvm"}',
  network_config: '{"version":2}',
  packages: ['vim'],
  ssh_authorized_keys: ['ssh-ed25519 AAAA'],
  user_data_template: null,
  late_commands: [],
  kernel_cmdline_extra: 'console=ttyS0',
  created_at: '2026-07-18T10:00:00Z',
  updated_at: '2026-07-18T10:00:00Z',
  assigned_machines: '2',
}

const input: ProfileInput = {
  name: 'ubuntu-server',
  ubuntu_release: 'noble',
  storage_layout: '{"mode":"lvm"}',
  network_config: '',
  packages: ['vim'],
  ssh_authorized_keys: ['ssh-ed25519 AAAA'],
  user_data_template: null,
  late_commands: [],
  kernel_cmdline_extra: 'console=ttyS0',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function listResponse(profiles: unknown[], total = profiles.length): Response {
  return jsonResponse({
    success: true,
    data: { profiles, meta: { total: String(total), page: 1, page_size: 10 } },
    error: null,
  })
}

describe('stores/profiles', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('fetchProfiles parses JSON-string fields and coerces int64 values', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([wireProfile], 42))

    const store = useProfilesStore()
    await store.fetchProfiles()

    expect(store.total).toBe(42)
    expect(store.profiles).toHaveLength(1)
    const profile = store.profiles[0]
    expect(profile.version).toBe(3)
    expect(profile.assigned_machines).toBe(2)
    expect(profile.storage_layout).toEqual({ mode: 'lvm', custom: undefined })
    expect(profile.network_config).toEqual({ version: 2 })
    expect(store.error).toBeNull()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/profiles?')
    expect(url).toContain('page=1')
  })

  it('fetchProfiles surfaces server errors and clears the list', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ success: false, data: null, error: { reason: 'INTERNAL', message: 'boom' } }, 500),
    )

    const store = useProfilesStore()
    await store.fetchProfiles()

    expect(store.error).toBe('boom')
    expect(store.profiles).toEqual([])
    expect(store.total).toBe(0)
  })

  it('createProfile POSTs the input and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: wireProfile, error: null }))
      .mockResolvedValueOnce(listResponse([wireProfile]))

    const store = useProfilesStore()
    await store.createProfile(input)

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/profiles')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual(input)
    expect(store.profiles).toHaveLength(1)
  })

  it('createProfile surfaces 422 field details to the caller', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: {
            reason: 'VALIDATION_FAILED',
            message: 'validation failed',
            details: { name: 'already taken' },
          },
        },
        422,
      ),
    )

    const store = useProfilesStore()
    const failure = await store
      .createProfile(input)
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).details).toEqual({ name: 'already taken' })
    expect(store.error).toBe('validation failed')
  })

  it('updateProfile PUTs {profile} and replaces the row immutably', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([wireProfile]))
    const store = useProfilesStore()
    await store.fetchProfiles()
    const before = store.profiles

    const bumped = { ...wireProfile, version: '4' }
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: bumped, error: null }))
    await store.updateProfile(wireProfile.id, input)

    expect(store.profiles).not.toBe(before)
    expect(store.profiles[0].version).toBe(4)
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(url).toBe(`/api/v1/profiles/${wireProfile.id}`)
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body as string)).toEqual({ profile: input })
  })

  it('cloneProfile POSTs new_name and refreshes the list', async () => {
    const clone = { ...wireProfile, id: 'p-clone', name: 'ubuntu-server-copy' }
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: clone, error: null }))
      .mockResolvedValueOnce(listResponse([wireProfile, clone]))

    const store = useProfilesStore()
    const created = await store.cloneProfile(wireProfile.id, 'ubuntu-server-copy')

    expect(created.name).toBe('ubuntu-server-copy')
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/v1/profiles/${wireProfile.id}/clone`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ new_name: 'ubuntu-server-copy' })
    expect(store.profiles).toHaveLength(2)
  })

  it('removeProfile DELETEs and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: null, error: null }))
      .mockResolvedValueOnce(listResponse([]))

    const store = useProfilesStore()
    await store.removeProfile(wireProfile.id)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/v1/profiles/${wireProfile.id}`)
    expect(init.method).toBe('DELETE')
    expect(store.profiles).toEqual([])
  })

  it('removeProfile surfaces a 409 conflict when machines are assigned', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: { reason: 'CONFLICT', message: 'profile is assigned to 2 machines' },
        },
        409,
      ),
    )

    const store = useProfilesStore()
    const failure = await store
      .removeProfile(wireProfile.id)
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).status).toBe(409)
    expect(store.error).toBe('profile is assigned to 2 machines')
  })

  it('previewProfile POSTs to the preview endpoint and returns the render', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: { user_data: '#cloud-config\n', cmdline: 'console=ttyS0' },
        error: null,
      }),
    )

    const store = useProfilesStore()
    const preview = await store.previewProfile(wireProfile.id, 'm-1')

    expect(preview.user_data).toBe('#cloud-config\n')
    expect(preview.cmdline).toBe('console=ttyS0')
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/v1/profiles/${wireProfile.id}/preview`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ machine_id: 'm-1' })
  })
})
