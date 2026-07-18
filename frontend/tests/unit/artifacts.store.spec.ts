import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../src/api/http'
import { useArtifactsStore } from '../../src/stores/artifacts'

const artifact = {
  id: 'b1000000-0000-4000-8000-000000000001',
  kind: 'kernel',
  ubuntu_release: 'noble',
  filename: 'vmlinuz-noble',
  size_bytes: '10485760',
  sha256: 'a'.repeat(64),
  uploaded_by: 'op-1',
  created_at: '2026-07-18T10:00:00Z',
  updated_at: '2026-07-18T10:00:00Z',
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function listResponse(artifacts: unknown[], total = artifacts.length): Response {
  return jsonResponse({
    success: true,
    data: { artifacts, meta: { total: String(total), page: 1, page_size: 10 } },
    error: null,
  })
}

function newFile(): File {
  return new File([new Uint8Array([1, 2, 3])], 'vmlinuz-noble', {
    type: 'application/octet-stream',
  })
}

describe('stores/artifacts', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('fetchArtifacts stores the list and coerces int64 size and total', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([artifact], 7))

    const store = useArtifactsStore()
    await store.fetchArtifacts()

    expect(store.artifacts).toHaveLength(1)
    expect(store.artifacts[0].size_bytes).toBe(10485760)
    expect(store.artifacts[0].ubuntu_release).toBe('noble')
    expect(store.total).toBe(7)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/artifacts?')
    expect(url).toContain('page=1')
  })

  it('fetchArtifacts surfaces server errors and clears the list', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        { success: false, data: null, error: { reason: 'INTERNAL', message: 'boom' } },
        500,
      ),
    )

    const store = useArtifactsStore()
    await store.fetchArtifacts()

    expect(store.error).toBe('boom')
    expect(store.artifacts).toEqual([])
    expect(store.total).toBe(0)
  })

  it('uploadArtifact posts multipart form data and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: artifact, error: null }))
      .mockResolvedValueOnce(listResponse([artifact]))

    const store = useArtifactsStore()
    await store.uploadArtifact({ kind: 'kernel', ubuntu_release: 'noble', file: newFile() })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/artifacts')
    expect(init.method).toBe('POST')
    expect(init.body).toBeInstanceOf(FormData)
    const form = init.body as FormData
    expect(form.get('kind')).toBe('kernel')
    expect(form.get('ubuntu_release')).toBe('noble')
    expect(form.get('file')).toBeInstanceOf(File)
    // Browser must set the multipart boundary itself; we never send Content-Type.
    const headers = (init.headers ?? {}) as Record<string, string>
    expect(headers['Content-Type']).toBeUndefined()
    expect(store.artifacts).toHaveLength(1)
    expect(store.uploading).toBe(false)
  })

  it('uploadArtifact surfaces 422 field details to the caller', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: {
            reason: 'VALIDATION_FAILED',
            message: 'validation failed',
            details: { file: 'unsupported file type' },
          },
        },
        422,
      ),
    )

    const store = useArtifactsStore()
    const failure = await store
      .uploadArtifact({ kind: 'other', ubuntu_release: '', file: newFile() })
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).details).toEqual({ file: 'unsupported file type' })
    expect(store.error).toBe('validation failed')
    expect(store.uploading).toBe(false)
  })

  it('replaceArtifact issues PUT and updates the row immutably', async () => {
    fetchMock.mockResolvedValueOnce(listResponse([artifact]))
    const store = useArtifactsStore()
    await store.fetchArtifacts()
    const before = store.artifacts

    const replaced = { ...artifact, sha256: 'b'.repeat(64), size_bytes: '2048' }
    fetchMock.mockResolvedValueOnce(jsonResponse({ success: true, data: replaced, error: null }))
    await store.replaceArtifact(artifact.id, {
      kind: 'kernel',
      ubuntu_release: 'noble',
      file: newFile(),
    })

    expect(store.artifacts).not.toBe(before)
    expect(store.artifacts[0].sha256).toBe('b'.repeat(64))
    expect(store.artifacts[0].size_bytes).toBe(2048)
    const [url, init] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(url).toBe(`/api/v1/artifacts/${artifact.id}`)
    expect(init.method).toBe('PUT')
  })

  it('deleteArtifact issues DELETE and refreshes the list', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ success: true, data: null, error: null }))
      .mockResolvedValueOnce(listResponse([]))

    const store = useArtifactsStore()
    await store.deleteArtifact(artifact.id)

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe(`/api/v1/artifacts/${artifact.id}`)
    expect(init.method).toBe('DELETE')
    expect(store.artifacts).toEqual([])
  })

  it('deleteArtifact rethrows a 409 conflict when referenced by a profile', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          success: false,
          data: null,
          error: { reason: 'CONFLICT', message: 'referenced by a profile' },
        },
        409,
      ),
    )

    const store = useArtifactsStore()
    const failure = await store
      .deleteArtifact(artifact.id)
      .then(() => null)
      .catch((e: unknown) => e)

    expect(failure).toBeInstanceOf(ApiError)
    expect((failure as ApiError).status).toBe(409)
    expect(store.error).toBe('referenced by a profile')
  })

  it('fetchTransfers stores rows, coerces bytes_sent, and applies the filename filter', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        success: true,
        data: {
          transfers: [
            {
              time: '2026-07-18T09:00:00Z',
              client_ip: '10.0.0.5',
              filename: 'vmlinuz-noble',
              bytes_sent: '10485760',
              success: true,
              error: '',
              protocol: 'tftp',
            },
          ],
          meta: { total: '1', page: 1, page_size: 50 },
        },
        error: null,
      }),
    )

    const store = useArtifactsStore()
    store.transfersFilename = 'vmlinuz'
    await store.fetchTransfers()

    expect(store.transfers).toHaveLength(1)
    expect(store.transfers[0].bytes_sent).toBe(10485760)
    expect(store.transfers[0].protocol).toBe('tftp')
    expect(store.transfers[0].success).toBe(true)
    const [url] = fetchMock.mock.calls[0] as [string]
    expect(url).toContain('/api/v1/artifacts/transfers?')
    expect(url).toContain('filename=vmlinuz')
  })
})
