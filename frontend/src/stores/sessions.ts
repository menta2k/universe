/**
 * Sessions store: server-paginated session list with filters, a selected
 * session detail (timeline + evidence), and a live SSE subscription that
 * appends new phase events to the current timeline without a reload (SC-004).
 *
 * Components never call the sessions API directly. All state updates replace
 * arrays/objects immutably.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

import * as sessionsApi from '../api/sessions'
import type { ProvisioningEvent, ProvisioningSession, SessionState } from '../api/types'
import { openEventStream } from '../plugins/sse'
import type { CloseStream, StreamFilters } from '../plugins/sse'

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}

const TERMINAL_PHASES = new Set(['session_completed', 'session_failed'])

export const useSessionsStore = defineStore('sessions', () => {
  const sessions = ref<readonly ProvisioningSession[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const stateFilter = ref<SessionState | null>(null)
  const machineIdFilter = ref<string | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const current = ref<ProvisioningSession | null>(null)
  const timeline = ref<readonly ProvisioningEvent[]>([])
  const evidence = ref<Record<string, unknown>>({})
  const detailLoading = ref(false)

  let closeStream: CloseStream | null = null

  async function fetchSessions(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const result = await sessionsApi.listSessions({
        page: page.value,
        page_size: pageSize.value,
        state: stateFilter.value ?? undefined,
        machine_id: machineIdFilter.value ?? undefined,
      })
      sessions.value = result.sessions
      total.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      sessions.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
  }

  async function fetchSession(id: string): Promise<void> {
    detailLoading.value = true
    error.value = null
    try {
      const detail = await sessionsApi.getSession(id)
      current.value = detail.session
      timeline.value = detail.timeline
      evidence.value = detail.evidence
    } catch (e: unknown) {
      error.value = errorMessage(e)
      current.value = null
      timeline.value = []
      evidence.value = {}
    } finally {
      detailLoading.value = false
    }
  }

  /** Append a live event to the current timeline immutably (skips duplicates). */
  function appendEvent(event: ProvisioningEvent): void {
    const isDuplicate = timeline.value.some(
      (existing) => existing.time === event.time && existing.phase === event.phase,
    )
    if (!isDuplicate) {
      timeline.value = [...timeline.value, event]
    }
    if (TERMINAL_PHASES.has(event.phase)) {
      // The session reached a terminal phase: refresh its row in the list and
      // reload the authoritative detail (final state + evidence).
      void fetchSessions()
      if (current.value) void fetchSession(current.value.id)
    }
  }

  /**
   * Open a live SSE subscription. When `sessionId` is provided, events are
   * scoped to that session and appended to the current timeline. Any previous
   * subscription is torn down first. Returns a manual unsubscribe function.
   */
  function subscribeLive(sessionId?: string): CloseStream {
    unsubscribeLive()
    const filters: StreamFilters = sessionId ? { session_id: sessionId } : {}
    closeStream = openEventStream(filters, {
      onEvent: (event) => appendEvent(event),
      onError: () => {
        // Transport/parse errors are non-fatal for the UI; the stream helper
        // keeps the connection unless the browser closes it.
      },
    })
    return unsubscribeLive
  }

  function unsubscribeLive(): void {
    if (closeStream) {
      closeStream()
      closeStream = null
    }
  }

  function clearDetail(): void {
    unsubscribeLive()
    current.value = null
    timeline.value = []
    evidence.value = {}
  }

  return {
    sessions,
    total,
    page,
    pageSize,
    stateFilter,
    machineIdFilter,
    loading,
    error,
    current,
    timeline,
    evidence,
    detailLoading,
    fetchSessions,
    fetchSession,
    appendEvent,
    subscribeLive,
    unsubscribeLive,
    clearDetail,
  }
})
