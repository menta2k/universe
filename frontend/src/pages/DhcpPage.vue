<script setup lang="ts">
/**
 * DHCP management page: authoritative-server enable/disable toggle (guarded by
 * a confirmation dialog), a foreign-server conflict banner, the subnet / lease
 * TTL editor with inline 422 handling, and a live leases table that auto-polls
 * every 5 seconds while mounted.
 */
import { storeToRefs } from 'pinia'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type { DhcpConfigInput } from '../api/dhcp'
import { ApiError } from '../api/http'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import DhcpSubnetEditor from '../components/DhcpSubnetEditor.vue'
import { useDhcpStore } from '../stores/dhcp'

const LEASE_POLL_MS = 5000

const store = useDhcpStore()
const { config, leases, conflicts, loading, error } = storeToRefs(store)

const enabled = computed(() => config.value?.enabled ?? false)
const version = computed(() => config.value?.version ?? 0)

const LEASE_HEADERS = [
  { title: 'IP', key: 'ip' },
  { title: 'MAC', key: 'mac' },
  { title: 'Machine', key: 'machine_name' },
  { title: 'Expires', key: 'expires_at' },
] as const

// --- Enable / disable ---
const confirmEnableOpen = ref(false)
const toggleBusy = ref(false)

function onToggle(value: boolean | null): void {
  if (value) {
    confirmEnableOpen.value = true
  } else {
    void doDisable()
  }
}

async function confirmEnable(): Promise<void> {
  toggleBusy.value = true
  try {
    await store.enable()
    confirmEnableOpen.value = false
  } catch {
    // Surfaced via the error snackbar.
  } finally {
    toggleBusy.value = false
  }
}

async function doDisable(): Promise<void> {
  toggleBusy.value = true
  try {
    await store.disable()
  } catch {
    // Surfaced via the error snackbar.
  } finally {
    toggleBusy.value = false
  }
}

// --- Subnet editor / save ---
const serverErrors = ref<Readonly<Record<string, string>>>({})
const saving = ref(false)
const successSnackbar = ref(false)

async function onSaveConfig(values: DhcpConfigInput): Promise<void> {
  saving.value = true
  serverErrors.value = {}
  try {
    await store.updateConfig(values)
    successSnackbar.value = true
  } catch (e: unknown) {
    if (e instanceof ApiError && e.details) {
      // Keep editing state so the operator can correct the flagged fields.
      serverErrors.value = e.details
    }
    // Non-field errors surface via the error snackbar.
  } finally {
    saving.value = false
  }
}

// --- Live leases: manual refresh + auto-poll while mounted ---
function refreshLeases(): void {
  void store.fetchLeases()
}

let pollTimer: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  void store.fetchConfig()
  void store.fetchConflicts()
  refreshLeases()
  pollTimer = setInterval(refreshLeases, LEASE_POLL_MS)
})

onBeforeUnmount(() => {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})

// --- Error snackbar ---
const errorSnackbar = ref(false)
watch(error, (message) => {
  if (message !== null) errorSnackbar.value = true
})
</script>

<template>
  <div class="d-flex flex-column ga-4">
    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <v-card-title>DHCP</v-card-title>
        <v-card-subtitle>Authoritative DHCP for the provisioning network</v-card-subtitle>
      </v-card-item>
      <v-divider />
      <v-card-text class="d-flex align-center">
        <v-switch
          color="primary"
          data-testid="enable-switch"
          density="comfortable"
          hide-details
          :label="enabled ? 'DHCP enabled' : 'DHCP disabled'"
          :loading="toggleBusy || loading"
          :model-value="enabled"
          @update:model-value="onToggle"
        />
        <v-spacer />
        <div class="text-right">
          <v-chip :color="enabled ? 'success' : 'default'" size="small" variant="tonal">
            {{ enabled ? 'Running' : 'Stopped' }}
          </v-chip>
          <div class="text-caption text-medium-emphasis mt-1">Config version {{ version }}</div>
        </div>
      </v-card-text>
    </v-card>

    <v-alert
      v-if="conflicts.length > 0"
      data-testid="conflicts-banner"
      type="warning"
      variant="tonal"
    >
      <div class="font-weight-medium">
        {{ conflicts.length }} foreign DHCP server(s) detected on the segment
      </div>
      <v-expansion-panels class="mt-2" variant="accordion">
        <v-expansion-panel title="Details">
          <template #text>
            <v-table density="compact">
              <thead>
                <tr>
                  <th>Server</th>
                  <th>Last seen</th>
                  <th>Offers seen</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="server in conflicts" :key="server.server_id">
                  <td class="font-monospace">{{ server.server_id }}</td>
                  <td>{{ server.last_seen }}</td>
                  <td>{{ server.offers_seen }}</td>
                </tr>
              </tbody>
            </v-table>
          </template>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-alert>

    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <v-card-title>Subnets &amp; leases policy</v-card-title>
        <v-card-subtitle>Ranges served to provisioned machines</v-card-subtitle>
      </v-card-item>
      <v-divider />
      <v-card-text>
        <DhcpSubnetEditor
          :lease-ttl-seconds="config?.lease_ttl_seconds"
          :saving="saving"
          :server-errors="serverErrors"
          :subnets="config?.subnets"
          @save="onSaveConfig"
        />
      </v-card-text>
    </v-card>

    <v-card border rounded="lg" variant="flat">
      <v-card-item>
        <template #append>
          <v-btn
            data-testid="refresh-leases-btn"
            prepend-icon="mdi-refresh"
            variant="text"
            @click="refreshLeases"
          >
            Refresh
          </v-btn>
        </template>
        <v-card-title>Active leases</v-card-title>
        <v-card-subtitle>Live from the lease store (auto-refreshes every 5s)</v-card-subtitle>
      </v-card-item>
      <v-divider />
      <v-data-table
        :headers="LEASE_HEADERS"
        item-value="ip"
        :items="leases"
        no-data-text="No active leases"
      >
        <template #[`item.mac`]="{ item }">
          <span class="font-monospace">{{ item.mac }}</span>
        </template>
        <template #[`item.machine_name`]="{ item }">
          {{ item.machine_name ?? '—' }}
        </template>
      </v-data-table>
    </v-card>
  </div>

  <ConfirmDialog
    :busy="toggleBusy"
    confirm-color="primary"
    confirm-label="Enable DHCP"
    message="This starts the authoritative DHCP server on the provisioning network."
    :model-value="confirmEnableOpen"
    title="Enable DHCP server"
    @confirm="confirmEnable"
    @update:model-value="confirmEnableOpen = $event"
  />

  <v-snackbar v-model="successSnackbar" color="success" timeout="4000">
    DHCP configuration saved.
  </v-snackbar>

  <v-snackbar v-model="errorSnackbar" color="error" timeout="5000">
    {{ error }}
  </v-snackbar>
</template>

<style scoped>
.font-monospace {
  font-family: monospace;
}
</style>
