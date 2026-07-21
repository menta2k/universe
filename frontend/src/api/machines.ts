/**
 * MachineService client: CRUD, provisioning actions, and unknown-boot
 * registration. All calls go through the shared http helper; pagination
 * `meta` arrives inside the reply payload (backend ListMachinesReply).
 */
import { nestPageQuery, request } from './http'
import type { Firmware, Machine, ProvisionState } from './types'

const BASE = '/api/v1/machines'

export interface MachineListFilters {
  readonly page?: number
  readonly page_size?: number
  readonly state?: ProvisionState
  readonly profile_id?: string
  readonly q?: string
}

export interface CreateMachineInput {
  readonly mac: string
  readonly name: string
  readonly firmware?: Firmware
  readonly profile_id?: string
  readonly reservation_ip?: string
  readonly notes?: string
  /** Per-machine netplan override as a JSON string; empty uses the profile's. */
  readonly network_config?: string
}

export interface UpdateMachineInput {
  readonly name?: string
  readonly profile_id?: string
  readonly reservation_ip?: string
  readonly notes?: string
  /** JSON netplan override; empty string clears it (falls back to the profile). */
  readonly network_config?: string
}

export interface UnknownBoot {
  readonly mac: string
  readonly last_seen: string
  readonly attempts: number
}

export interface RegisterFromUnknownInput {
  readonly mac: string
  readonly name: string
  readonly profile_id: string
}

export interface PageMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

export interface MachineListPage {
  readonly machines: readonly Machine[]
  readonly meta: PageMeta
}

export interface UnknownBootPage {
  readonly boots: readonly UnknownBoot[]
  readonly meta: PageMeta
}

/** Wire shapes: protojson serialises int64 fields (total, attempts) as strings. */
interface WireMeta {
  readonly total?: number | string
  readonly page?: number
  readonly page_size?: number
}

interface WireMachineList {
  readonly machines?: readonly Machine[]
  readonly meta?: WireMeta
}

interface WireUnknownBoot {
  readonly mac: string
  readonly last_seen: string
  readonly attempts?: number | string
}

interface WireUnknownBootList {
  readonly boots?: readonly WireUnknownBoot[]
  readonly meta?: WireMeta
}

function normalizeMeta(meta: WireMeta | undefined, fallbackCount: number): PageMeta {
  return {
    total: Number(meta?.total ?? fallbackCount),
    page: Number(meta?.page ?? 1),
    page_size: Number(meta?.page_size ?? fallbackCount),
  }
}

export async function listMachines(filters: MachineListFilters = {}): Promise<MachineListPage> {
  const data = await request<WireMachineList>(BASE, { query: nestPageQuery(filters) })
  const machines = data.machines ?? []
  return { machines, meta: normalizeMeta(data.meta, machines.length) }
}

export function createMachine(input: CreateMachineInput): Promise<Machine> {
  return request<Machine>(BASE, { method: 'POST', body: input })
}

export function updateMachine(id: string, input: UpdateMachineInput): Promise<Machine> {
  return request<Machine>(`${BASE}/${encodeURIComponent(id)}`, { method: 'PATCH', body: input })
}

export function deleteMachine(id: string): Promise<void> {
  return request<void>(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function provisionMachine(id: string): Promise<Machine> {
  return request<Machine>(`${BASE}/${encodeURIComponent(id)}/provision`, {
    method: 'POST',
    body: {},
  })
}

export function cancelProvision(id: string): Promise<Machine> {
  return request<Machine>(`${BASE}/${encodeURIComponent(id)}/cancel`, { method: 'POST', body: {} })
}

export async function listUnknownBoots(page = 1, pageSize = 10): Promise<UnknownBootPage> {
  const data = await request<WireUnknownBootList>(`${BASE}/unknown`, {
    query: { page, page_size: pageSize },
  })
  const boots = (data.boots ?? []).map((boot): UnknownBoot => ({
    mac: boot.mac,
    last_seen: boot.last_seen,
    attempts: Number(boot.attempts ?? 0),
  }))
  return { boots, meta: normalizeMeta(data.meta, boots.length) }
}

export function registerFromUnknown(input: RegisterFromUnknownInput): Promise<Machine> {
  return request<Machine>(`${BASE}/register-unknown`, { method: 'POST', body: input })
}
