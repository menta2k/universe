/**
 * ArtifactService client: boot-file CRUD (kernel, initrd, iPXE binaries) plus
 * TFTP/HTTP transfer activity. Uploads and replacements are raw multipart
 * requests; everything else flows through the shared JSON http helper.
 * int64 wire fields (sizes, byte counts, meta.total) arrive as strings under
 * protojson and are coerced with Number().
 */
import { request, requestMultipart } from './http'
import type { ArtifactKind, BootArtifact, UbuntuRelease } from './types'

const BASE = '/api/v1/artifacts'

export interface PageMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

export interface ArtifactListPage {
  readonly artifacts: readonly BootArtifact[]
  readonly meta: PageMeta
}

export type TransferProtocol = 'tftp' | 'http'

export interface Transfer {
  readonly time: string
  readonly client_ip: string
  readonly filename: string
  readonly bytes_sent: number
  readonly success: boolean
  readonly error: string
  readonly protocol: TransferProtocol
}

export interface TransferListPage {
  readonly transfers: readonly Transfer[]
  readonly meta: PageMeta
}

export interface ArtifactUploadInput {
  readonly kind: ArtifactKind
  readonly ubuntu_release: UbuntuRelease | ''
  readonly file: File
}

/** Wire shapes: protojson serialises int64 fields as strings. */
interface WireMeta {
  readonly total?: number | string
  readonly page?: number
  readonly page_size?: number
}

interface WireArtifact {
  readonly id: string
  readonly kind: ArtifactKind
  readonly ubuntu_release?: string
  readonly filename: string
  readonly size_bytes?: number | string
  readonly sha256: string
  readonly uploaded_by: string
  readonly created_at: string
  readonly updated_at: string
}

interface WireArtifactList {
  readonly artifacts?: readonly WireArtifact[]
  readonly meta?: WireMeta
}

interface WireTransfer {
  readonly time: string
  readonly client_ip?: string
  readonly filename?: string
  readonly bytes_sent?: number | string
  readonly success?: boolean
  readonly error?: string
  readonly protocol?: string
}

interface WireTransferList {
  readonly transfers?: readonly WireTransfer[]
  readonly meta?: WireMeta
}

function normalizeMeta(meta: WireMeta | undefined, fallbackCount: number): PageMeta {
  return {
    total: Number(meta?.total ?? fallbackCount),
    page: Number(meta?.page ?? 1),
    page_size: Number(meta?.page_size ?? fallbackCount),
  }
}

function normalizeRelease(value: string | undefined): UbuntuRelease | null {
  return value === 'jammy' || value === 'noble' ? value : null
}

function normalizeArtifact(wire: WireArtifact): BootArtifact {
  return {
    id: wire.id,
    kind: wire.kind,
    ubuntu_release: normalizeRelease(wire.ubuntu_release),
    filename: wire.filename,
    size_bytes: Number(wire.size_bytes ?? 0),
    sha256: wire.sha256,
    uploaded_by: wire.uploaded_by,
    created_at: wire.created_at,
    updated_at: wire.updated_at,
  }
}

function normalizeTransfer(wire: WireTransfer): Transfer {
  return {
    time: wire.time,
    client_ip: wire.client_ip ?? '',
    filename: wire.filename ?? '',
    bytes_sent: Number(wire.bytes_sent ?? 0),
    success: wire.success ?? false,
    error: wire.error ?? '',
    protocol: wire.protocol === 'http' ? 'http' : 'tftp',
  }
}

export interface ArtifactListFilters {
  readonly page?: number
  readonly page_size?: number
}

export async function listArtifacts(filters: ArtifactListFilters = {}): Promise<ArtifactListPage> {
  const data = await request<WireArtifactList>(BASE, { query: { ...filters } })
  const artifacts = (data.artifacts ?? []).map(normalizeArtifact)
  return { artifacts, meta: normalizeMeta(data.meta, artifacts.length) }
}

export async function getArtifact(id: string): Promise<BootArtifact> {
  const data = await request<WireArtifact>(`${BASE}/${encodeURIComponent(id)}`)
  return normalizeArtifact(data)
}

function buildUploadForm(input: ArtifactUploadInput): FormData {
  const form = new FormData()
  form.append('kind', input.kind)
  form.append('ubuntu_release', input.ubuntu_release)
  form.append('file', input.file)
  return form
}

export async function uploadArtifact(input: ArtifactUploadInput): Promise<BootArtifact> {
  const data = await requestMultipart<WireArtifact>(BASE, buildUploadForm(input), { method: 'POST' })
  return normalizeArtifact(data)
}

export async function replaceArtifact(
  id: string,
  input: ArtifactUploadInput,
): Promise<BootArtifact> {
  const data = await requestMultipart<WireArtifact>(
    `${BASE}/${encodeURIComponent(id)}`,
    buildUploadForm(input),
    { method: 'PUT' },
  )
  return normalizeArtifact(data)
}

export function deleteArtifact(id: string): Promise<void> {
  return request<void>(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export interface TransferListFilters {
  readonly page?: number
  readonly page_size?: number
  readonly filename?: string
}

export async function listTransfers(filters: TransferListFilters = {}): Promise<TransferListPage> {
  const data = await request<WireTransferList>(`${BASE}/transfers`, { query: { ...filters } })
  const transfers = (data.transfers ?? []).map(normalizeTransfer)
  return { transfers, meta: normalizeMeta(data.meta, transfers.length) }
}
