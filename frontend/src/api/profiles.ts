/**
 * ProfileService client: CRUD plus clone and preview. Wire profiles carry
 * `storage_layout` and `network_config` as JSON strings; this module parses
 * them into objects on read and expects callers to serialise them back to
 * strings (via ProfileInput) on write. `listProfiles` keeps its list-only
 * signature so the machine dialog and machines page can populate selects.
 */
import { request } from './http'
import type { Profile, StorageLayout, UbuntuRelease } from './types'

const BASE = '/api/v1/profiles'

export interface PageMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

export interface ProfileListFilters {
  readonly page?: number
  readonly page_size?: number
}

export interface ProfileListPage {
  readonly profiles: readonly Profile[]
  readonly meta: PageMeta
}

/** Create/update payload. storage_layout and network_config are JSON strings. */
export interface ProfileInput {
  readonly name: string
  readonly ubuntu_release: UbuntuRelease
  readonly keyboard_layout: string
  readonly keyboard_variant: string
  readonly locale: string
  readonly timezone: string
  readonly storage_layout: string
  readonly network_config: string
  readonly packages: readonly string[]
  readonly ssh_authorized_keys: readonly string[]
  readonly user_data_template: string | null
  readonly late_commands: readonly string[]
  readonly kernel_cmdline_extra: string
  /** Login account for the installed OS; empty keeps the default ("ubuntu"). */
  readonly install_username: string
  /** Plaintext password, hashed server-side. Empty on update keeps the current one. */
  readonly password?: string
  /** Remove any stored password on update. */
  readonly clear_password?: boolean
}

export interface ProfilePreview {
  readonly user_data: string
  readonly cmdline: string
}

/** Wire shape: JSON-string layout/config, protojson may stringify int64 fields. */
interface WireProfile {
  readonly id: string
  readonly name: string
  readonly version?: number | string
  readonly ubuntu_release: UbuntuRelease
  readonly keyboard_layout?: string
  readonly keyboard_variant?: string
  readonly locale?: string
  readonly timezone?: string
  readonly storage_layout?: string
  readonly network_config?: string
  readonly packages?: readonly string[]
  readonly ssh_authorized_keys?: readonly string[]
  readonly user_data_template?: string | null
  readonly late_commands?: readonly string[]
  readonly kernel_cmdline_extra?: string
  readonly install_username?: string
  readonly has_password?: boolean
  readonly created_at?: string
  readonly updated_at?: string
  readonly assigned_machines?: number | string
}

interface WireMeta {
  readonly total?: number | string
  readonly page?: number
  readonly page_size?: number
}

interface WireProfileList {
  readonly profiles?: readonly WireProfile[]
  readonly meta?: WireMeta
}

function parseJsonObject(value: string | undefined): Record<string, unknown> {
  if (!value || !value.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

function parseStorageLayout(value: string | undefined): StorageLayout {
  const obj = parseJsonObject(value)
  const mode = obj.mode
  const layout: StorageLayout = {
    mode: mode === 'direct' || mode === 'custom' ? mode : 'lvm',
    custom: typeof obj.custom === 'string' ? obj.custom : undefined,
  }
  return layout
}

function normalizeProfile(wire: WireProfile): Profile {
  return {
    id: wire.id,
    name: wire.name,
    version: Number(wire.version ?? 1),
    ubuntu_release: wire.ubuntu_release,
    keyboard_layout: wire.keyboard_layout ?? 'us',
    keyboard_variant: wire.keyboard_variant ?? '',
    locale: wire.locale ?? 'en_US.UTF-8',
    timezone: wire.timezone ?? '',
    storage_layout: parseStorageLayout(wire.storage_layout),
    network_config: parseJsonObject(wire.network_config),
    packages: wire.packages ?? [],
    ssh_authorized_keys: wire.ssh_authorized_keys ?? [],
    user_data_template: wire.user_data_template ?? null,
    late_commands: wire.late_commands ?? [],
    kernel_cmdline_extra: wire.kernel_cmdline_extra ?? '',
    install_username: wire.install_username ?? '',
    has_password: wire.has_password ?? false,
    created_at: wire.created_at ?? '',
    updated_at: wire.updated_at ?? '',
    assigned_machines: Number(wire.assigned_machines ?? 0),
  }
}

function normalizeMeta(meta: WireMeta | undefined, fallbackCount: number): PageMeta {
  return {
    total: Number(meta?.total ?? fallbackCount),
    page: Number(meta?.page ?? 1),
    page_size: Number(meta?.page_size ?? fallbackCount),
  }
}

/** List-only helper retained for the machine dialog / machines page selects. */
export async function listProfiles(): Promise<readonly Profile[]> {
  const data = await request<WireProfileList>(BASE)
  return (data.profiles ?? []).map(normalizeProfile)
}

export async function listProfilesPage(filters: ProfileListFilters = {}): Promise<ProfileListPage> {
  const data = await request<WireProfileList>(BASE, { query: { ...filters } })
  const profiles = (data.profiles ?? []).map(normalizeProfile)
  return { profiles, meta: normalizeMeta(data.meta, profiles.length) }
}

export async function getProfile(id: string): Promise<Profile> {
  const data = await request<WireProfile>(`${BASE}/${encodeURIComponent(id)}`)
  return normalizeProfile(data)
}

export async function createProfile(input: ProfileInput): Promise<Profile> {
  const data = await request<WireProfile>(BASE, { method: 'POST', body: input })
  return normalizeProfile(data)
}

export async function updateProfile(id: string, input: ProfileInput): Promise<Profile> {
  const data = await request<WireProfile>(`${BASE}/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: { profile: input },
  })
  return normalizeProfile(data)
}

export async function cloneProfile(id: string, newName: string): Promise<Profile> {
  const data = await request<WireProfile>(`${BASE}/${encodeURIComponent(id)}/clone`, {
    method: 'POST',
    body: { new_name: newName },
  })
  return normalizeProfile(data)
}

export function removeProfile(id: string): Promise<void> {
  return request<void>(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function previewProfile(id: string, machineId?: string): Promise<ProfilePreview> {
  return request<ProfilePreview>(`${BASE}/${encodeURIComponent(id)}/preview`, {
    method: 'POST',
    body: machineId ? { machine_id: machineId } : {},
  })
}
