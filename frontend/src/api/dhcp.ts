/**
 * DhcpService client: config read/write, enable/disable, live leases, and
 * foreign-server conflict observation. All calls go through the shared http
 * helper. protojson may serialise int64 fields (version, lease_ttl_seconds,
 * offers_seen, meta.total) as strings, so numeric wire fields are coerced.
 */
import { request } from './http'
import type { DhcpConfig, DhcpSubnet } from './types'

const BASE = '/api/v1/dhcp'

export interface PageMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

export interface LeaseFilters {
  readonly page?: number
  readonly page_size?: number
}

/** Subnet payload for PUT /dhcp/config (no server-assigned id). */
export interface DhcpSubnetInput {
  readonly network: string
  readonly range_start: string
  readonly range_end: string
  readonly gateway: string
  readonly dns: readonly string[]
}

export interface DhcpConfigInput {
  readonly lease_ttl_seconds: number
  readonly subnets: readonly DhcpSubnetInput[]
}

/** Live lease row; machine_name is present when the MAC maps to a machine. */
export interface DhcpLeaseEntry {
  readonly ip: string
  readonly mac: string
  readonly machine_id: string | null
  readonly machine_name: string | null
  readonly expires_at: string
}

export interface DhcpLeasePage {
  readonly leases: readonly DhcpLeaseEntry[]
  readonly meta: PageMeta
}

/** A competing DHCP server observed emitting OFFERs on the segment. */
export interface ForeignDhcpServer {
  readonly server_id: string
  readonly last_seen: string
  readonly offers_seen: number
}

export interface ForeignServerPage {
  readonly servers: readonly ForeignDhcpServer[]
  readonly meta: PageMeta
}

interface WireMeta {
  readonly total?: number | string
  readonly page?: number
  readonly page_size?: number
}

interface WireSubnet {
  readonly id?: string
  readonly network?: string
  readonly range_start?: string
  readonly range_end?: string
  readonly gateway?: string
  readonly dns?: readonly string[]
  readonly next_server?: string
}

interface WireConfig {
  readonly enabled?: boolean
  readonly version?: number | string
  readonly interface?: string
  readonly lease_ttl_seconds?: number | string
  readonly subnets?: readonly WireSubnet[]
  readonly updated_by?: string | null
  readonly updated_at?: string
}

interface WireLease {
  readonly ip?: string
  readonly mac?: string
  readonly machine_id?: string | null
  readonly machine_name?: string | null
  readonly expires_at?: string
}

interface WireLeaseList {
  readonly leases?: readonly WireLease[]
  readonly meta?: WireMeta
}

interface WireForeignServer {
  readonly server_id?: string
  readonly last_seen?: string
  readonly offers_seen?: number | string
}

interface WireForeignList {
  readonly servers?: readonly WireForeignServer[]
  readonly meta?: WireMeta
}

function normalizeMeta(meta: WireMeta | undefined, fallbackCount: number): PageMeta {
  return {
    total: Number(meta?.total ?? fallbackCount),
    page: Number(meta?.page ?? 1),
    page_size: Number(meta?.page_size ?? fallbackCount),
  }
}

function normalizeSubnet(wire: WireSubnet): DhcpSubnet {
  return {
    id: wire.id ?? '',
    network: wire.network ?? '',
    range_start: wire.range_start ?? '',
    range_end: wire.range_end ?? '',
    gateway: wire.gateway ?? '',
    dns: wire.dns ?? [],
    next_server: wire.next_server ?? '',
  }
}

function normalizeConfig(wire: WireConfig): DhcpConfig {
  return {
    enabled: wire.enabled ?? false,
    version: Number(wire.version ?? 1),
    interface: wire.interface ?? '',
    lease_ttl_seconds: Number(wire.lease_ttl_seconds ?? 0),
    subnets: (wire.subnets ?? []).map(normalizeSubnet),
    updated_by: wire.updated_by ?? null,
    updated_at: wire.updated_at ?? '',
  }
}

export async function getConfig(): Promise<DhcpConfig> {
  const data = await request<WireConfig>(`${BASE}/config`)
  return normalizeConfig(data)
}

export async function updateConfig(input: DhcpConfigInput): Promise<DhcpConfig> {
  const data = await request<WireConfig>(`${BASE}/config`, { method: 'PUT', body: input })
  return normalizeConfig(data)
}

export async function enableDhcp(): Promise<DhcpConfig> {
  const data = await request<WireConfig>(`${BASE}/enable`, { method: 'POST', body: {} })
  return normalizeConfig(data)
}

export async function disableDhcp(): Promise<DhcpConfig> {
  const data = await request<WireConfig>(`${BASE}/disable`, { method: 'POST', body: {} })
  return normalizeConfig(data)
}

export async function listLeases(filters: LeaseFilters = {}): Promise<DhcpLeasePage> {
  const data = await request<WireLeaseList>(`${BASE}/leases`, { query: { ...filters } })
  const leases = (data.leases ?? []).map(
    (lease): DhcpLeaseEntry => ({
      ip: lease.ip ?? '',
      mac: lease.mac ?? '',
      machine_id: lease.machine_id ?? null,
      machine_name: lease.machine_name ?? null,
      expires_at: lease.expires_at ?? '',
    }),
  )
  return { leases, meta: normalizeMeta(data.meta, leases.length) }
}

export async function listConflicts(filters: LeaseFilters = {}): Promise<ForeignServerPage> {
  const data = await request<WireForeignList>(`${BASE}/conflicts`, { query: { ...filters } })
  const servers = (data.servers ?? []).map(
    (server): ForeignDhcpServer => ({
      server_id: server.server_id ?? '',
      last_seen: server.last_seen ?? '',
      offers_seen: Number(server.offers_seen ?? 0),
    }),
  )
  return { servers, meta: normalizeMeta(data.meta, servers.length) }
}
