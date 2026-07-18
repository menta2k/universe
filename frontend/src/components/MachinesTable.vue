<script setup lang="ts">
/** Server-paginated machines table with per-row provisioning actions. */
import type { Machine } from '../api/types'
import MachineStateChip from './MachineStateChip.vue'

defineProps<{
  machines: readonly Machine[]
  total: number
  loading: boolean
  profileNames: Readonly<Record<string, string>>
}>()

const emit = defineEmits<{
  'update:options': [options: { page: number; itemsPerPage: number }]
  provision: [machine: Machine]
  cancel: [machine: Machine]
  edit: [machine: Machine]
  delete: [machine: Machine]
}>()

const headers = [
  { title: 'Name', key: 'name', sortable: false },
  { title: 'MAC address', key: 'mac', sortable: false },
  { title: 'State', key: 'provision_state', sortable: false },
  { title: 'Profile', key: 'profile_id', sortable: false },
  { title: 'Reservation IP', key: 'reservation_ip', sortable: false },
  { title: 'Actions', key: 'actions', sortable: false, align: 'end' as const },
]
</script>

<template>
  <v-data-table-server
    :headers="headers"
    item-value="id"
    :items="machines"
    :items-length="total"
    :loading="loading"
    @update:options="emit('update:options', $event)"
  >
    <template #[`item.mac`]="{ item }">
      <span class="text-mono">{{ item.mac }}</span>
    </template>
    <template #[`item.provision_state`]="{ item }">
      <MachineStateChip :state="item.provision_state" />
    </template>
    <template #[`item.profile_id`]="{ item }">
      {{ item.profile_id ? (profileNames[item.profile_id] ?? item.profile_id) : '—' }}
    </template>
    <template #[`item.reservation_ip`]="{ item }">
      <span class="text-mono">{{ item.reservation_ip || '—' }}</span>
    </template>
    <template #[`item.actions`]="{ item }">
      <v-btn
        v-if="item.provision_state === 'installing'"
        color="amber"
        icon="mdi-stop"
        size="small"
        title="Cancel provisioning"
        variant="text"
        @click="emit('cancel', item)"
      />
      <v-btn
        v-else
        color="primary"
        icon="mdi-play"
        size="small"
        title="Provision"
        variant="text"
        @click="emit('provision', item)"
      />
      <v-btn icon="mdi-pencil" size="small" title="Edit" variant="text" @click="emit('edit', item)" />
      <v-btn
        color="red"
        icon="mdi-delete"
        size="small"
        title="Delete"
        variant="text"
        @click="emit('delete', item)"
      />
    </template>
    <template #no-data>
      <div class="py-6 text-medium-emphasis">No machines registered yet.</div>
    </template>
  </v-data-table-server>
</template>

<style scoped>
.text-mono {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
