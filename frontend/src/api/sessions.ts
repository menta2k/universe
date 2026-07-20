/**
 * SessionService client: provisioning observability. `listSessions` returns a
 * paginated list of session rows; `getSession` returns a single session with
 * its ordered phase timeline and evidence.
 *
 * Wire quirks handled here (mirroring the other API clients):
 *   - protojson serialises int64 fields (total, profile_version) as strings.
 *   - timeline `detail` and the session `evidence` arrive as JSON *strings*
 *     and are parsed into objects on read.
 */
import { nestPageQuery, request } from './http'
import type {
  EventOutcome,
  EventPhase,
  ProvisioningEvent,
  ProvisioningSession,
  SessionState,
} from './types'

const BASE = '/api/v1/sessions'

export interface SessionListFilters {
  readonly page?: number
  readonly page_size?: number
  readonly machine_id?: string
  readonly state?: SessionState
}

export interface PageMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

export interface SessionListPage {
  readonly sessions: readonly ProvisioningSession[]
  readonly meta: PageMeta
}

export interface SessionDetail {
  readonly session: ProvisioningSession
  readonly timeline: readonly ProvisioningEvent[]
  readonly evidence: Record<string, unknown>
}

interface WireMeta {
  readonly total?: number | string
  readonly page?: number
  readonly page_size?: number
}

interface WireSession {
  readonly id: string
  readonly machine_id?: string
  readonly machine_name?: string
  readonly machine_mac?: string
  readonly profile_id?: string
  readonly profile_version?: number | string
  readonly state?: SessionState
  readonly started_at?: string
  readonly ended_at?: string | null
  readonly failure_phase?: string | null
}

interface WireTimelineEntry {
  readonly time?: string
  readonly session_id?: string | null
  readonly machine_mac?: string
  readonly phase?: string
  readonly outcome?: string
  /** JSON-encoded string of structured detail. */
  readonly detail?: string
}

interface WireSessionList {
  readonly sessions?: readonly WireSession[]
  readonly meta?: WireMeta
}

interface WireSessionDetail {
  readonly session?: WireSession
  readonly timeline?: readonly WireTimelineEntry[]
  /** JSON-encoded string of collected evidence. */
  readonly evidence?: string
}

const VALID_OUTCOMES: readonly EventOutcome[] = ['ok', 'error', 'denied']

function parseJsonObject(value: string | undefined | null): Record<string, unknown> {
  if (!value || !value.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return typeof parsed === 'object' && parsed !== null
      ? (parsed as Record<string, unknown>)
      : {}
  } catch {
    return {}
  }
}

function normalizeMeta(meta: WireMeta | undefined, fallbackCount: number): PageMeta {
  return {
    total: Number(meta?.total ?? fallbackCount),
    page: Number(meta?.page ?? 1),
    page_size: Number(meta?.page_size ?? fallbackCount),
  }
}

function normalizeSession(wire: WireSession): ProvisioningSession {
  return {
    id: wire.id,
    machine_id: wire.machine_id ?? '',
    machine_name: wire.machine_name ?? '',
    machine_mac: wire.machine_mac ?? '',
    profile_id: wire.profile_id ?? '',
    profile_version: Number(wire.profile_version ?? 0),
    state: wire.state ?? 'active',
    started_at: wire.started_at ?? '',
    ended_at: wire.ended_at ?? null,
    failure_phase: wire.failure_phase ?? null,
  }
}

function normalizeTimelineEntry(wire: WireTimelineEntry): ProvisioningEvent {
  const outcome =
    typeof wire.outcome === 'string' && VALID_OUTCOMES.includes(wire.outcome as EventOutcome)
      ? (wire.outcome as EventOutcome)
      : 'ok'
  return {
    time: wire.time ?? '',
    session_id: wire.session_id ?? null,
    machine_mac: wire.machine_mac ?? '',
    phase: (wire.phase ?? 'unknown_machine') as EventPhase,
    outcome,
    detail: parseJsonObject(wire.detail),
  }
}

export async function listSessions(filters: SessionListFilters = {}): Promise<SessionListPage> {
  const data = await request<WireSessionList>(BASE, { query: nestPageQuery(filters) })
  const sessions = (data.sessions ?? []).map(normalizeSession)
  return { sessions, meta: normalizeMeta(data.meta, sessions.length) }
}

export async function getSession(id: string): Promise<SessionDetail> {
  const data = await request<WireSessionDetail>(`${BASE}/${encodeURIComponent(id)}`)
  const session = normalizeSession(data.session ?? { id })
  const timeline = (data.timeline ?? []).map(normalizeTimelineEntry)
  return { session, timeline, evidence: parseJsonObject(data.evidence) }
}
