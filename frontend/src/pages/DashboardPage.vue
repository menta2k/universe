<script setup lang="ts">
/**
 * Dashboard: at-a-glance provisioning overview. Summary cards count machines
 * by provision state and sessions by session state, derived from a snapshot
 * fetch of each list. A "recent sessions" mini list links into the sessions
 * page. Kept intentionally simple — a single snapshot on mount.
 */
import { computed, onMounted } from 'vue'

import type { ProvisionState, SessionState } from '../api/types'
import SessionStateChip from '../components/SessionStateChip.vue'
import { useMachinesStore } from '../stores/machines'
import { useSessionsStore } from '../stores/sessions'

const SNAPSHOT_SIZE = 200

const machinesStore = useMachinesStore()
const sessionsStore = useSessionsStore()

interface StateCount<T extends string> {
  readonly title: string
  readonly value: T
  readonly color: string
}

const MACHINE_STATES: readonly StateCount<ProvisionState>[] = [
  { title: 'New', value: 'new', color: 'grey' },
  { title: 'Ready', value: 'ready', color: 'blue' },
  { title: 'Installing', value: 'installing', color: 'amber' },
  { title: 'Installed', value: 'installed', color: 'green' },
  { title: 'Failed', value: 'failed', color: 'red' },
]

const SESSION_STATES: readonly StateCount<SessionState>[] = [
  { title: 'Active', value: 'active', color: 'amber' },
  { title: 'Completed', value: 'completed', color: 'green' },
  { title: 'Failed', value: 'failed', color: 'red' },
  { title: 'Stale', value: 'stale', color: 'grey' },
]

const machineCounts = computed<Readonly<Record<ProvisionState, number>>>(() => {
  const counts = { new: 0, ready: 0, installing: 0, installed: 0, failed: 0 }
  for (const machine of machinesStore.machines) counts[machine.provision_state] += 1
  return counts
})

const sessionCounts = computed<Readonly<Record<SessionState, number>>>(() => {
  const counts = { active: 0, completed: 0, failed: 0, stale: 0 }
  for (const session of sessionsStore.sessions) counts[session.state] += 1
  return counts
})

const recentSessions = computed(() =>
  [...sessionsStore.sessions]
    .sort((a, b) => b.started_at.localeCompare(a.started_at))
    .slice(0, 5),
)

function formatTime(value: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

onMounted(() => {
  machinesStore.pageSize = SNAPSHOT_SIZE
  machinesStore.page = 1
  sessionsStore.pageSize = SNAPSHOT_SIZE
  sessionsStore.page = 1
  void machinesStore.fetchMachines()
  void sessionsStore.fetchSessions()
})
</script>

<template>
  <div class="d-flex flex-column ga-6">
    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <v-card-title>Machines</v-card-title>
        <v-card-subtitle>{{ machinesStore.total }} registered</v-card-subtitle>
      </v-card-item>
      <v-card-text>
        <v-row dense>
          <v-col v-for="state in MACHINE_STATES" :key="state.value" cols="6" md="2" sm="4">
            <v-sheet border class="pa-4 text-center" rounded="lg">
              <div class="text-h4" :class="`text-${state.color}`">
                {{ machineCounts[state.value] }}
              </div>
              <div class="text-caption text-medium-emphasis">{{ state.title }}</div>
            </v-sheet>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <v-card-title>Sessions</v-card-title>
        <v-card-subtitle>{{ sessionsStore.total }} total</v-card-subtitle>
      </v-card-item>
      <v-card-text>
        <v-row dense>
          <v-col v-for="state in SESSION_STATES" :key="state.value" cols="6" md="3" sm="3">
            <v-sheet border class="pa-4 text-center" rounded="lg">
              <div class="text-h4" :class="`text-${state.color}`">
                {{ sessionCounts[state.value] }}
              </div>
              <div class="text-caption text-medium-emphasis">{{ state.title }}</div>
            </v-sheet>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <v-card-title>Recent sessions</v-card-title>
        <template #append>
          <v-btn append-icon="mdi-arrow-right" size="small" :to="{ name: 'sessions' }" variant="text">
            View all
          </v-btn>
        </template>
      </v-card-item>
      <v-divider />
      <v-list v-if="recentSessions.length > 0" lines="two">
        <v-list-item
          v-for="session in recentSessions"
          :key="session.id"
          :subtitle="`${session.machine_mac} · started ${formatTime(session.started_at)}`"
          :title="session.machine_name || session.machine_id"
          :to="{ name: 'sessions' }"
        >
          <template #append>
            <SessionStateChip :state="session.state" />
          </template>
        </v-list-item>
      </v-list>
      <v-card-text v-else class="text-medium-emphasis">No provisioning sessions yet.</v-card-text>
    </v-card>
  </div>
</template>
