<script setup lang="ts">
/**
 * Ordered phase timeline for a provisioning session. While the session is
 * `active` it opens a live SSE subscription (via the sessions store) so new
 * phases appear without a reload (SC-004); the subscription is torn down on
 * unmount and as soon as the session reaches a terminal state.
 *
 * A read-only evidence viewer is shown for `failed` / `stale` sessions.
 */
import { computed, onBeforeUnmount, watch } from 'vue'

import type { EventOutcome, ProvisioningEvent, ProvisioningSession } from '../api/types'
import { useSessionsStore } from '../stores/sessions'

const props = defineProps<{
  session: ProvisioningSession | null
  timeline: readonly ProvisioningEvent[]
  evidence: Record<string, unknown>
}>()

const store = useSessionsStore()

const OUTCOME_COLORS: Readonly<Record<EventOutcome, string>> = {
  ok: 'success',
  error: 'error',
  denied: 'warning',
}

const OUTCOME_ICONS: Readonly<Record<EventOutcome, string>> = {
  ok: 'mdi-check',
  error: 'mdi-alert-circle',
  denied: 'mdi-cancel',
}

/** Timeline sorted oldest-first by event time (stable for equal timestamps). */
const orderedEvents = computed<readonly ProvisioningEvent[]>(() =>
  [...props.timeline].sort((a, b) => a.time.localeCompare(b.time)),
)

const isActive = computed(() => props.session?.state === 'active')

const showEvidence = computed(
  () =>
    (props.session?.state === 'failed' || props.session?.state === 'stale') &&
    Object.keys(props.evidence).length > 0,
)

const evidenceJson = computed(() => JSON.stringify(props.evidence, null, 2))

function formatTime(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function phaseLabel(phase: string): string {
  return phase.replace(/_/g, ' ')
}

function detailText(detail: Record<string, unknown>): string {
  const keys = Object.keys(detail)
  if (keys.length === 0) return ''
  return JSON.stringify(detail)
}

/** Keep the live subscription in sync with the session id / active state. */
watch(
  () => (isActive.value ? props.session?.id : null),
  (sessionId) => {
    if (sessionId) {
      store.subscribeLive(sessionId)
    } else {
      store.unsubscribeLive()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => store.unsubscribeLive())
</script>

<template>
  <div>
    <v-timeline v-if="orderedEvents.length > 0" density="compact" side="end" truncate-line="both">
      <v-timeline-item
        v-for="(event, index) in orderedEvents"
        :key="`${event.time}-${event.phase}-${index}`"
        :dot-color="OUTCOME_COLORS[event.outcome]"
        :icon="OUTCOME_ICONS[event.outcome]"
        size="small"
      >
        <template #opposite>
          <span class="text-caption text-medium-emphasis">{{ formatTime(event.time) }}</span>
        </template>
        <div class="d-flex align-center">
          <strong class="text-capitalize">{{ phaseLabel(event.phase) }}</strong>
          <v-chip
            class="ml-2"
            :color="OUTCOME_COLORS[event.outcome]"
            label
            size="x-small"
            variant="tonal"
          >
            {{ event.outcome }}
          </v-chip>
        </div>
        <div v-if="detailText(event.detail)" class="text-caption text-medium-emphasis text-mono">
          {{ detailText(event.detail) }}
        </div>
      </v-timeline-item>
    </v-timeline>

    <div v-else class="py-6 text-medium-emphasis">No phase events recorded yet.</div>

    <div v-if="isActive" class="d-flex align-center text-caption text-medium-emphasis mt-2">
      <v-progress-circular class="mr-2" indeterminate size="14" width="2" />
      Live — waiting for new phases
    </div>

    <v-alert
      v-if="showEvidence"
      class="mt-4"
      density="comfortable"
      type="error"
      variant="tonal"
      title="Failure evidence"
    >
      <pre class="evidence-block text-mono">{{ evidenceJson }}</pre>
    </v-alert>
  </div>
</template>

<style scoped>
.text-mono {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
.evidence-block {
  margin: 0;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
