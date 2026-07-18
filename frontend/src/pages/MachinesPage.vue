<script setup lang="ts">
/**
 * Machines page: server-paginated machine list with provisioning actions,
 * plus a tab of unknown-MAC boot attempts that can be promoted to machines.
 */
import { storeToRefs } from 'pinia'
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { ApiError } from '../api/http'
import { listProfiles } from '../api/profiles'
import type { Machine, Profile, ProvisionState } from '../api/types'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import MachineDialog from '../components/MachineDialog.vue'
import type { MachineDialogMode, MachineFormValues } from '../components/MachineDialog.vue'
import MachinesTable from '../components/MachinesTable.vue'
import UnknownBootsTable from '../components/UnknownBootsTable.vue'
import { useMachinesStore } from '../stores/machines'
import { debounce } from '../utils/debounce'

const store = useMachinesStore()
const {
  machines,
  total,
  stateFilter,
  search,
  loading,
  error,
  unknownBoots,
  unknownTotal,
  unknownLoading,
} = storeToRefs(store)

const tab = ref<'machines' | 'unknown'>('machines')

const STATE_OPTIONS: readonly { title: string; value: ProvisionState }[] = [
  { title: 'New', value: 'new' },
  { title: 'Ready', value: 'ready' },
  { title: 'Installing', value: 'installing' },
  { title: 'Installed', value: 'installed' },
  { title: 'Failed', value: 'failed' },
]

// Profile names for the profile column.
const profiles = ref<readonly Profile[]>([])
const profileNames = computed<Readonly<Record<string, string>>>(() =>
  Object.fromEntries(profiles.value.map((p) => [p.id, p.name])),
)
void listProfiles().then(
  (result) => {
    profiles.value = result
  },
  () => {
    // Non-fatal: the profile column falls back to raw ids.
  },
)

function onTableOptions(options: { page: number; itemsPerPage: number }): void {
  store.page = options.page
  store.pageSize = options.itemsPerPage
  void store.fetchMachines()
}

const debouncedSearch = debounce(() => {
  store.page = 1
  void store.fetchMachines()
}, 300)
watch(search, () => debouncedSearch())
onBeforeUnmount(() => debouncedSearch.cancel())

watch(stateFilter, () => {
  store.page = 1
  void store.fetchMachines()
})

// --- Register / edit dialog ---
const dialogOpen = ref(false)
const dialogMode = ref<MachineDialogMode>('create')
const dialogInitial = ref<Partial<MachineFormValues>>({})
const dialogServerErrors = ref<Readonly<Record<string, string>>>({})
const dialogSaving = ref(false)
const editingId = ref<string | null>(null)

function openCreate(): void {
  dialogMode.value = 'create'
  dialogInitial.value = {}
  editingId.value = null
  dialogServerErrors.value = {}
  dialogOpen.value = true
}

function openEdit(machine: Machine): void {
  dialogMode.value = 'edit'
  dialogInitial.value = {
    mac: machine.mac,
    name: machine.name,
    firmware: machine.firmware,
    profile_id: machine.profile_id ?? '',
    reservation_ip: machine.reservation_ip ?? '',
    notes: machine.notes,
  }
  editingId.value = machine.id
  dialogServerErrors.value = {}
  dialogOpen.value = true
}

function openRegisterUnknown(mac: string): void {
  dialogMode.value = 'register-unknown'
  dialogInitial.value = { mac }
  editingId.value = null
  dialogServerErrors.value = {}
  dialogOpen.value = true
}

async function onDialogSave(values: MachineFormValues): Promise<void> {
  dialogSaving.value = true
  dialogServerErrors.value = {}
  try {
    if (dialogMode.value === 'edit' && editingId.value !== null) {
      await store.updateMachine(editingId.value, {
        name: values.name,
        profile_id: values.profile_id,
        reservation_ip: values.reservation_ip,
        notes: values.notes,
      })
    } else if (dialogMode.value === 'register-unknown') {
      await store.registerFromUnknown({
        mac: values.mac,
        name: values.name,
        profile_id: values.profile_id,
      })
      await store.fetchUnknownBoots()
    } else {
      await store.createMachine({
        mac: values.mac,
        name: values.name,
        firmware: values.firmware,
        profile_id: values.profile_id || undefined,
        reservation_ip: values.reservation_ip || undefined,
        notes: values.notes || undefined,
      })
    }
    dialogOpen.value = false
  } catch (e: unknown) {
    if (e instanceof ApiError && e.details) {
      dialogServerErrors.value = e.details
    }
    // Non-field errors surface via the store error snackbar.
  } finally {
    dialogSaving.value = false
  }
}

// --- Provision / delete confirmations ---
const confirmTarget = ref<{ action: 'provision' | 'delete'; machine: Machine } | null>(null)
const confirmBusy = ref(false)

const confirmTitle = computed(() =>
  confirmTarget.value?.action === 'delete' ? 'Delete machine' : 'Provision machine',
)
const confirmMessage = computed(() => {
  const target = confirmTarget.value
  if (!target) return ''
  return target.action === 'delete'
    ? `Delete "${target.machine.name}" (${target.machine.mac})? This cannot be undone.`
    : `Provision "${target.machine.name}"? The machine will be reinstalled on its next boot.`
})

async function onConfirm(): Promise<void> {
  const target = confirmTarget.value
  if (!target) return
  confirmBusy.value = true
  try {
    if (target.action === 'delete') await store.deleteMachine(target.machine.id)
    else await store.provision(target.machine.id)
    confirmTarget.value = null
  } catch {
    // Error is surfaced via the store error snackbar.
  } finally {
    confirmBusy.value = false
  }
}

async function onCancelProvision(machine: Machine): Promise<void> {
  try {
    await store.cancel(machine.id)
  } catch {
    // Error is surfaced via the store error snackbar.
  }
}

function onUnknownOptions(options: { page: number; itemsPerPage: number }): void {
  void store.fetchUnknownBoots(options.page, options.itemsPerPage)
}

// --- Error snackbar ---
const snackbar = ref(false)
watch(error, (message) => {
  if (message !== null) snackbar.value = true
})
</script>

<template>
  <v-card border rounded="lg" variant="flat">
    <v-card-item>
      <v-card-title>Machines</v-card-title>
      <v-card-subtitle>Registered machines and provisioning state</v-card-subtitle>
    </v-card-item>

    <v-tabs v-model="tab" class="px-4">
      <v-tab value="machines">Machines</v-tab>
      <v-tab value="unknown">Unknown boots</v-tab>
    </v-tabs>
    <v-divider />

    <v-window v-model="tab">
      <v-window-item value="machines">
        <v-toolbar color="transparent" density="comfortable">
          <v-text-field
            v-model="search"
            class="ml-4"
            clearable
            density="compact"
            hide-details
            label="Search name or MAC"
            max-width="280"
            prepend-inner-icon="mdi-magnify"
            variant="outlined"
          />
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
          <v-btn class="mr-4" color="primary" prepend-icon="mdi-plus" @click="openCreate">
            Register machine
          </v-btn>
        </v-toolbar>

        <MachinesTable
          :loading="loading"
          :machines="machines"
          :profile-names="profileNames"
          :total="total"
          @cancel="onCancelProvision"
          @delete="confirmTarget = { action: 'delete', machine: $event }"
          @edit="openEdit"
          @provision="confirmTarget = { action: 'provision', machine: $event }"
          @update:options="onTableOptions"
        />
      </v-window-item>

      <v-window-item value="unknown">
        <UnknownBootsTable
          :boots="unknownBoots"
          :loading="unknownLoading"
          :total="unknownTotal"
          @register="openRegisterUnknown"
          @update:options="onUnknownOptions"
        />
      </v-window-item>
    </v-window>
  </v-card>

  <MachineDialog
    v-model="dialogOpen"
    :initial="dialogInitial"
    :mode="dialogMode"
    :saving="dialogSaving"
    :server-errors="dialogServerErrors"
    @save="onDialogSave"
  />

  <ConfirmDialog
    :busy="confirmBusy"
    :confirm-color="confirmTarget?.action === 'delete' ? 'red' : 'primary'"
    :confirm-label="confirmTarget?.action === 'delete' ? 'Delete' : 'Provision'"
    :message="confirmMessage"
    :model-value="confirmTarget !== null"
    :title="confirmTitle"
    @confirm="onConfirm"
    @update:model-value="confirmTarget = null"
  />

  <v-snackbar v-model="snackbar" color="error" timeout="5000">
    {{ error }}
  </v-snackbar>
</template>
