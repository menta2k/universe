<script setup lang="ts">
/**
 * Boot Files page: kernel/initrd/iPXE artifact management plus a tab of
 * TFTP/HTTP transfer activity. The store owns all API access; this page wires
 * the upload/replace dialog, a delete confirmation (surfacing 409 "referenced
 * by a profile" conflicts), and the transfer filter.
 */
import { storeToRefs } from 'pinia'
import { computed, ref, watch } from 'vue'

import { ApiError } from '../api/http'
import type { ArtifactKind, BootArtifact } from '../api/types'
import ArtifactUploadDialog from '../components/ArtifactUploadDialog.vue'
import type {
  ArtifactDialogMode,
  ArtifactFormValues,
} from '../components/ArtifactUploadDialog.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useArtifactsStore } from '../stores/artifacts'
import { formatBytes } from '../utils/bytes'

const store = useArtifactsStore()
const {
  artifacts,
  total,
  loading,
  error,
  uploading,
  transfers,
  transfersFilename,
  transfersLoading,
} = storeToRefs(store)

const tab = ref<'artifacts' | 'transfers'>('artifacts')

const KIND_LABELS: Readonly<Record<ArtifactKind, string>> = {
  kernel: 'Kernel',
  initrd: 'Initrd',
  ipxe_bin: 'iPXE binary',
  other: 'Other',
}
const KIND_COLORS: Readonly<Record<ArtifactKind, string>> = {
  kernel: 'primary',
  initrd: 'indigo',
  ipxe_bin: 'teal',
  other: 'grey',
}

const artifactHeaders = [
  { title: 'Filename', key: 'filename', sortable: false },
  { title: 'Kind', key: 'kind', sortable: false },
  { title: 'Release', key: 'ubuntu_release', sortable: false },
  { title: 'Size', key: 'size_bytes', sortable: false },
  { title: 'SHA-256', key: 'sha256', sortable: false },
  { title: 'Actions', key: 'actions', sortable: false, align: 'end' as const },
]

const transferHeaders = [
  { title: 'Time', key: 'time', sortable: false },
  { title: 'Client IP', key: 'client_ip', sortable: false },
  { title: 'Filename', key: 'filename', sortable: false },
  { title: 'Bytes', key: 'bytes_sent', sortable: false },
  { title: 'Protocol', key: 'protocol', sortable: false },
  { title: 'Result', key: 'success', sortable: false },
]

function onArtifactOptions(options: { page: number; itemsPerPage: number }): void {
  store.page = options.page
  store.pageSize = options.itemsPerPage
  void store.fetchArtifacts()
}

function truncateHash(hash: string): string {
  return hash.length > 16 ? `${hash.slice(0, 16)}…` : hash
}

async function copyHash(hash: string): Promise<void> {
  try {
    await navigator.clipboard?.writeText(hash)
  } catch {
    // Clipboard access can be denied; failing silently is acceptable here.
  }
}

function formatTime(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString()
}

// --- Upload / replace dialog ---
const dialogOpen = ref(false)
const dialogMode = ref<ArtifactDialogMode>('upload')
const dialogInitialKind = ref<ArtifactKind>('kernel')
const dialogInitialRelease = ref<'jammy' | 'noble' | ''>('')
const dialogReplaceFilename = ref('')
const dialogServerErrors = ref<Readonly<Record<string, string>>>({})
const replacingId = ref<string | null>(null)

function openUpload(): void {
  dialogMode.value = 'upload'
  dialogInitialKind.value = 'kernel'
  dialogInitialRelease.value = ''
  replacingId.value = null
  dialogServerErrors.value = {}
  dialogOpen.value = true
}

function openReplace(artifact: BootArtifact): void {
  dialogMode.value = 'replace'
  dialogInitialKind.value = artifact.kind
  dialogInitialRelease.value = artifact.ubuntu_release ?? ''
  dialogReplaceFilename.value = artifact.filename
  replacingId.value = artifact.id
  dialogServerErrors.value = {}
  dialogOpen.value = true
}

async function onDialogSave(values: ArtifactFormValues): Promise<void> {
  dialogServerErrors.value = {}
  try {
    if (dialogMode.value === 'replace' && replacingId.value !== null) {
      await store.replaceArtifact(replacingId.value, values)
    } else {
      await store.uploadArtifact(values)
    }
    dialogOpen.value = false
  } catch (e: unknown) {
    if (e instanceof ApiError && e.details) {
      dialogServerErrors.value = e.details
    }
    // Non-field errors surface via the store error snackbar.
  }
}

// --- Delete confirmation ---
const deleteTarget = ref<BootArtifact | null>(null)
const deleteBusy = ref(false)

const deleteMessage = computed(() => {
  const target = deleteTarget.value
  if (!target) return ''
  return `Delete "${target.filename}"? This cannot be undone.`
})

async function onConfirmDelete(): Promise<void> {
  const target = deleteTarget.value
  if (!target) return
  deleteBusy.value = true
  try {
    await store.deleteArtifact(target.id)
    deleteTarget.value = null
  } catch (e: unknown) {
    if (e instanceof ApiError && e.status === 409) {
      store.error = 'Cannot delete: this file is referenced by a profile release set.'
    }
    // Other errors surface via the store error snackbar.
  } finally {
    deleteBusy.value = false
  }
}

// --- Transfers ---
function refreshTransfers(): void {
  void store.fetchTransfers()
}

watch(tab, (value) => {
  if (value === 'transfers' && transfers.value.length === 0) refreshTransfers()
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
      <v-card-title>Boot Files</v-card-title>
      <v-card-subtitle>Kernel, initrd and iPXE artifacts</v-card-subtitle>
    </v-card-item>

    <v-tabs v-model="tab" class="px-4">
      <v-tab value="artifacts">Artifacts</v-tab>
      <v-tab value="transfers">Transfer activity</v-tab>
    </v-tabs>
    <v-divider />

    <v-window v-model="tab">
      <v-window-item value="artifacts">
        <v-toolbar color="transparent" density="comfortable">
          <v-spacer />
          <v-btn
            class="mr-4"
            color="primary"
            data-testid="upload-btn"
            prepend-icon="mdi-upload"
            @click="openUpload"
          >
            Upload
          </v-btn>
        </v-toolbar>

        <v-data-table-server
          :headers="artifactHeaders"
          item-value="id"
          :items="artifacts"
          :items-length="total"
          :loading="loading"
          @update:options="onArtifactOptions"
        >
          <template #[`item.filename`]="{ item }">
            <span class="text-mono">{{ item.filename }}</span>
          </template>
          <template #[`item.kind`]="{ item }">
            <v-chip :color="KIND_COLORS[item.kind]" label size="small" variant="tonal">
              {{ KIND_LABELS[item.kind] }}
            </v-chip>
          </template>
          <template #[`item.ubuntu_release`]="{ item }">
            {{ item.ubuntu_release ?? '—' }}
          </template>
          <template #[`item.size_bytes`]="{ item }">
            {{ formatBytes(item.size_bytes) }}
          </template>
          <template #[`item.sha256`]="{ item }">
            <span class="text-mono" :title="item.sha256">{{ truncateHash(item.sha256) }}</span>
            <v-btn
              class="ml-1"
              icon="mdi-content-copy"
              size="x-small"
              title="Copy full SHA-256"
              variant="text"
              @click="copyHash(item.sha256)"
            />
          </template>
          <template #[`item.actions`]="{ item }">
            <v-btn
              icon="mdi-swap-horizontal"
              size="small"
              title="Replace"
              variant="text"
              @click="openReplace(item)"
            />
            <v-btn
              color="red"
              icon="mdi-delete"
              size="small"
              title="Delete"
              variant="text"
              @click="deleteTarget = item"
            />
          </template>
          <template #no-data>
            <div class="py-6 text-medium-emphasis">No boot files uploaded yet.</div>
          </template>
        </v-data-table-server>
      </v-window-item>

      <v-window-item value="transfers">
        <v-toolbar color="transparent" density="comfortable">
          <v-text-field
            v-model="transfersFilename"
            class="ml-4"
            clearable
            density="compact"
            hide-details
            label="Filter by filename"
            max-width="280"
            prepend-inner-icon="mdi-magnify"
            variant="outlined"
            @keyup.enter="refreshTransfers"
          />
          <v-spacer />
          <v-btn
            class="mr-4"
            data-testid="refresh-transfers-btn"
            prepend-icon="mdi-refresh"
            variant="tonal"
            @click="refreshTransfers"
          >
            Refresh
          </v-btn>
        </v-toolbar>

        <v-data-table
          :headers="transferHeaders"
          item-value="time"
          :items="transfers"
          :loading="transfersLoading"
        >
          <template #[`item.time`]="{ item }">
            {{ formatTime(item.time) }}
          </template>
          <template #[`item.client_ip`]="{ item }">
            <span class="text-mono">{{ item.client_ip }}</span>
          </template>
          <template #[`item.filename`]="{ item }">
            <span class="text-mono">{{ item.filename }}</span>
          </template>
          <template #[`item.bytes_sent`]="{ item }">
            {{ formatBytes(item.bytes_sent) }}
          </template>
          <template #[`item.protocol`]="{ item }">
            <v-chip :color="item.protocol === 'http' ? 'teal' : 'blue-grey'" label size="small">
              {{ item.protocol.toUpperCase() }}
            </v-chip>
          </template>
          <template #[`item.success`]="{ item }">
            <v-icon v-if="item.success" color="success" icon="mdi-check-circle" />
            <span v-else class="d-inline-flex align-center text-error">
              <v-icon class="mr-1" icon="mdi-close-circle" />
              <span v-if="item.error">{{ item.error }}</span>
            </span>
          </template>
          <template #no-data>
            <div class="py-6 text-medium-emphasis">No transfer activity recorded.</div>
          </template>
        </v-data-table>
      </v-window-item>
    </v-window>
  </v-card>

  <ArtifactUploadDialog
    v-model="dialogOpen"
    :initial-kind="dialogInitialKind"
    :initial-release="dialogInitialRelease"
    :mode="dialogMode"
    :replace-filename="dialogReplaceFilename"
    :saving="uploading"
    :server-errors="dialogServerErrors"
    @save="onDialogSave"
  />

  <ConfirmDialog
    :busy="deleteBusy"
    confirm-color="red"
    confirm-label="Delete"
    :message="deleteMessage"
    :model-value="deleteTarget !== null"
    title="Delete boot file"
    @confirm="onConfirmDelete"
    @update:model-value="deleteTarget = null"
  />

  <v-snackbar v-model="snackbar" color="error" timeout="6000">
    {{ error }}
  </v-snackbar>
</template>

<style scoped>
.text-mono {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
