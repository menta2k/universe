<script setup lang="ts">
/** Unknown-MAC boot attempts table with per-row registration. */
import type { UnknownBoot } from '../api/machines'

defineProps<{
  boots: readonly UnknownBoot[]
  total: number
  loading: boolean
}>()

const emit = defineEmits<{
  'update:options': [options: { page: number; itemsPerPage: number }]
  register: [mac: string]
}>()

const headers = [
  { title: 'MAC address', key: 'mac', sortable: false },
  { title: 'Last seen', key: 'last_seen', sortable: false },
  { title: 'Attempts', key: 'attempts', sortable: false },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>

<template>
  <v-data-table-server
    :headers="headers"
    :items="boots"
    :items-length="total"
    :loading="loading"
    item-value="mac"
    @update:options="emit('update:options', $event)"
  >
    <template #[`item.mac`]="{ item }">
      <span class="text-mono">{{ item.mac }}</span>
    </template>
    <template #[`item.last_seen`]="{ item }">
      {{ formatTime(item.last_seen) }}
    </template>
    <template #[`item.actions`]="{ item }">
      <v-btn
        color="primary"
        prepend-icon="mdi-plus"
        size="small"
        variant="tonal"
        @click="emit('register', item.mac)"
      >
        Register
      </v-btn>
    </template>
    <template #no-data>
      <div class="py-6 text-medium-emphasis">No unknown boot attempts recorded.</div>
    </template>
  </v-data-table-server>
</template>

<style scoped>
.text-mono {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
