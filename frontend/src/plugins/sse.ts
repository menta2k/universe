/**
 * Server-Sent Events helper for the provisioning event stream.
 *
 * Opens an `EventSource` to `/api/v1/events/stream` with optional filters,
 * parses each `data:` payload into a `ProvisioningEvent`, and invokes the
 * supplied callback. Parse failures are reported via `onError` (or ignored)
 * rather than tearing down the stream, so a single malformed message never
 * stops live updates. Returns a close function that detaches all listeners.
 *
 * EventSource is same-origin here, so the browser sends the session cookie
 * automatically (cookie auth); no extra headers are required.
 */
import type { EventOutcome, EventPhase, ProvisioningEvent } from '../api/types'

const STREAM_PATH = '/api/v1/events/stream'

export interface StreamFilters {
  readonly session_id?: string
  readonly machine_mac?: string
  readonly machine_id?: string
}

export interface StreamHandlers {
  /** Called once per successfully parsed event. */
  readonly onEvent: (event: ProvisioningEvent) => void
  /** Called on parse failures and transport errors. Optional. */
  readonly onError?: (error: unknown) => void
}

/** Function that tears down an open stream. Safe to call more than once. */
export type CloseStream = () => void

const VALID_OUTCOMES: readonly EventOutcome[] = ['ok', 'error', 'denied']

function buildStreamUrl(filters: StreamFilters): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== '') params.set(key, value)
  }
  const qs = params.toString()
  return qs.length > 0 ? `${STREAM_PATH}?${qs}` : STREAM_PATH
}

/** Narrow an untrusted parsed payload into a ProvisioningEvent, or throw. */
function toEvent(raw: unknown): ProvisioningEvent {
  if (typeof raw !== 'object' || raw === null) {
    throw new Error('Event payload is not an object')
  }
  const record = raw as Record<string, unknown>
  const phase = record.phase
  const outcome = record.outcome
  if (typeof phase !== 'string') throw new Error('Event missing phase')
  if (typeof outcome !== 'string' || !VALID_OUTCOMES.includes(outcome as EventOutcome)) {
    throw new Error('Event has invalid outcome')
  }
  const detail = record.detail
  return {
    time: typeof record.time === 'string' ? record.time : '',
    session_id: typeof record.session_id === 'string' ? record.session_id : null,
    machine_mac: typeof record.machine_mac === 'string' ? record.machine_mac : '',
    phase: phase as EventPhase,
    outcome: outcome as EventOutcome,
    detail: typeof detail === 'object' && detail !== null ? (detail as Record<string, unknown>) : {},
  }
}

/**
 * Open a live event stream. Returns a `close` function; the caller is
 * responsible for invoking it on unmount / when no longer needed.
 */
export function openEventStream(filters: StreamFilters, handlers: StreamHandlers): CloseStream {
  const source = new EventSource(buildStreamUrl(filters), { withCredentials: true })

  const onMessage = (message: MessageEvent<string>): void => {
    try {
      const parsed: unknown = JSON.parse(message.data)
      handlers.onEvent(toEvent(parsed))
    } catch (error: unknown) {
      handlers.onError?.(error)
    }
  }

  const onErrorEvent = (error: Event): void => {
    handlers.onError?.(error)
  }

  source.addEventListener('message', onMessage)
  source.addEventListener('error', onErrorEvent)

  let closed = false
  return () => {
    if (closed) return
    closed = true
    source.removeEventListener('message', onMessage)
    source.removeEventListener('error', onErrorEvent)
    source.close()
  }
}
