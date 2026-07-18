<script setup lang="ts">
/**
 * Sessions page: server-paginated provisioning-session list with a state
 * filter. Clicking a row opens a detail dialog with the phase timeline and,
 * for active sessions, live SSE updates.
 */
import { storeToRefs } from 'pinia'
import { ref, watch } from 'vue'

import type { ProvisioningSession, SessionState } from '../api/types'
import SessionStateChip from '../components/SessionStateChip.vue'
import SessionTimeline from '../components/SessionTimeline.vue'
import { useSessionsStore } from '../stores/sessions'

const store = useSessionsStore()
const { sessions, total, stateFilter, loading, error, current, timeline, evidence, detailLoading } =
  storeToRefs(store)

const STATE_OPTIONS: readonly { title: string; value: SessionState }[] = [
  { title: 'Active', value: 'active' },
  { title: 'Completed', value: 'completed' },
  { title: 'Failed', value: 'failed' },
  { title: 'Stale', value: 'stale' },
]

const headers = [
  { title: 'Machine', key: 'machine_name', sortable: false },
  { title: 'State', key: 'state', sortable: false },
  { title: 'Started', key: 'started_at', sortable: false },
  { title: 'Ended', key: 'ended_at', sortable: false },
  { title: 'Failure phase', key: 'failure_phase', sortable: false },
]

function formatTime(value: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function onTableOptions(options: { page: number; itemsPerPage: number }): void {
  store.page = options.page
  store.pageSize = options.itemsPerPage
  void store.fetchSessions()
}

watch(stateFilter, () => {
  store.page = 1
  void store.fetchSessions()
})

// --- Detail dialog ---
const detailOpen = ref(false)

async function openDetail(session: ProvisioningSession): Promise<void> {
  detailOpen.value = true
  await store.fetchSession(session.id)
}

function onRowClick(_event: unknown, row: { item: ProvisioningSession }): void {
  void openDetail(row.item)
}

watch(detailOpen, (open) => {
  if (!open) store.clearDetail()
})

// --- Error snackbar ---
const snackbar = ref(false)
watch(error, (message) => {
  if (message !== null) snackbar.value = true
})
</script>

<template>
  <v-card border rounded="lg" variant="flat">
    <v-card-item>
      <v-card-title>Sessions</v-card-title>
      <v-card-subtitle>Provisioning session history and timelines</v-card-subtitle>
    </v-card-item>

    <v-toolbar color="transparent" density="comfortable">
      <v-select
        v-model="stateFilter"
        class="ml-4"
        clearable
        density="compact"
        hide-details
        :items="STATE_OPTIONS"
        label="State"
        max-width="200"
        variant="outlined"
      />
      <v-spacer />
    </v-toolbar>

    <v-data-table-server
      :headers="headers"
      item-value="id"
      :items="sessions"
      :items-length="total"
      :loading="loading"
      @click:row="onRowClick"
      @update:options="onTableOptions"
    >
      <template #[`item.machine_name`]="{ item }">
        <div>{{ item.machine_name || '—' }}</div>
        <div class="text-caption text-medium-emphasis text-mono">{{ item.machine_mac }}</div>
      </template>
      <template #[`item.state`]="{ item }">
        <SessionStateChip :state="item.state" />
      </template>
      <template #[`item.started_at`]="{ item }">
        {{ formatTime(item.started_at) }}
      </template>
      <template #[`item.ended_at`]="{ item }">
        {{ formatTime(item.ended_at) }}
      </template>
      <template #[`item.failure_phase`]="{ item }">
        <span v-if="item.failure_phase" class="text-error">{{ item.failure_phase }}</span>
        <span v-else>—</span>
      </template>
      <template #no-data>
        <div class="py-6 text-medium-emphasis">No provisioning sessions recorded yet.</div>
      </template>
    </v-data-table-server>
  </v-card>

  <v-dialog v-model="detailOpen" max-width="720" scrollable>
    <v-card>
      <v-card-item>
        <v-card-title>
          {{ current?.machine_name || 'Session' }}
        </v-card-title>
        <v-card-subtitle v-if="current">
          <SessionStateChip class="mr-2" :state="current.state" />
          <span class="text-mono">{{ current.machine_mac }}</span>
        </v-card-subtitle>
      </v-card-item>
      <v-divider />
      <v-card-text>
        <div v-if="detailLoading" class="d-flex justify-center py-8">
          <v-progress-circular indeterminate />
        </div>
        <SessionTimeline
          v-else
          :evidence="evidence"
          :session="current"
          :timeline="timeline"
        />
      </v-card-text>
      <v-divider />
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="detailOpen = false">Close</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-snackbar v-model="snackbar" color="error" timeout="5000">
    {{ error }}
  </v-snackbar>
</template>

<style scoped>
.text-mono {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
