<script setup lang="ts">
/**
 * Profiles page: server-paginated profile list with create/edit/clone/preview
 * and delete (blocked with a friendly 409 message while machines are assigned).
 */
import { storeToRefs } from 'pinia'
import { computed, ref, watch } from 'vue'

import { ApiError } from '../api/http'
import type { ProfileInput } from '../api/profiles'
import type { Profile } from '../api/types'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import ProfileEditor from '../components/ProfileEditor.vue'
import type { ProfileEditorMode } from '../components/ProfileEditor.vue'
import ProfilePreviewDrawer from '../components/ProfilePreviewDrawer.vue'
import { useProfilesStore } from '../stores/profiles'

const store = useProfilesStore()
const { profiles, total, loading, error } = storeToRefs(store)

const headers = [
  { title: 'Name', key: 'name', sortable: false },
  { title: 'Release', key: 'ubuntu_release', sortable: false },
  { title: 'Version', key: 'version', sortable: false },
  { title: 'Assigned machines', key: 'assigned_machines', sortable: false },
  { title: 'Actions', key: 'actions', sortable: false, align: 'end' as const },
]

function onTableOptions(options: { page: number; itemsPerPage: number }): void {
  store.page = options.page
  store.pageSize = options.itemsPerPage
  void store.fetchProfiles()
}

// --- Editor dialog ---
const editorOpen = ref(false)
const editorMode = ref<ProfileEditorMode>('create')
const editorInitial = ref<Profile | null>(null)
const editorServerErrors = ref<Readonly<Record<string, string>>>({})
const editorSaving = ref(false)
const editingId = ref<string | null>(null)

function openCreate(): void {
  editorMode.value = 'create'
  editorInitial.value = null
  editingId.value = null
  editorServerErrors.value = {}
  editorOpen.value = true
}

function openEdit(profile: Profile): void {
  editorMode.value = 'edit'
  editorInitial.value = profile
  editingId.value = profile.id
  editorServerErrors.value = {}
  editorOpen.value = true
}

async function onEditorSave(values: ProfileInput): Promise<void> {
  editorSaving.value = true
  editorServerErrors.value = {}
  try {
    if (editorMode.value === 'edit' && editingId.value !== null) {
      await store.updateProfile(editingId.value, values)
    } else {
      await store.createProfile(values)
    }
    editorOpen.value = false
  } catch (e: unknown) {
    if (e instanceof ApiError && e.details) editorServerErrors.value = e.details
    // Non-field errors surface via the store error snackbar.
  } finally {
    editorSaving.value = false
  }
}

// --- Clone dialog ---
const cloneTarget = ref<Profile | null>(null)
const cloneName = ref('')
const cloneBusy = ref(false)
const cloneError = ref<string | null>(null)

function openClone(profile: Profile): void {
  cloneTarget.value = profile
  cloneName.value = `${profile.name}-copy`
  cloneError.value = null
}

async function onCloneConfirm(): Promise<void> {
  const target = cloneTarget.value
  if (!target || !cloneName.value.trim()) return
  cloneBusy.value = true
  cloneError.value = null
  try {
    await store.cloneProfile(target.id, cloneName.value.trim())
    cloneTarget.value = null
  } catch (e: unknown) {
    cloneError.value = e instanceof Error ? e.message : 'Clone failed'
  } finally {
    cloneBusy.value = false
  }
}

// --- Preview drawer ---
const previewOpen = ref(false)
const previewProfile = ref<Profile | null>(null)

function openPreview(profile: Profile): void {
  previewProfile.value = profile
  previewOpen.value = true
}

// --- Delete confirmation ---
const deleteTarget = ref<Profile | null>(null)
const deleteBusy = ref(false)

const deleteMessage = computed(() => {
  const target = deleteTarget.value
  if (!target) return ''
  if (target.assigned_machines > 0)
    return `"${target.name}" is assigned to ${target.assigned_machines} machine(s). Reassign them before deleting.`
  return `Delete "${target.name}"? This cannot be undone.`
})

async function onDeleteConfirm(): Promise<void> {
  const target = deleteTarget.value
  if (!target) return
  deleteBusy.value = true
  try {
    await store.removeProfile(target.id)
    deleteTarget.value = null
  } catch {
    // 409 / other errors surface via the store error snackbar.
  } finally {
    deleteBusy.value = false
  }
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
      <v-card-title>Profiles</v-card-title>
      <v-card-subtitle>Installation profiles served to machines</v-card-subtitle>
    </v-card-item>

    <v-toolbar color="transparent" density="comfortable">
      <v-spacer />
      <v-btn
        class="mr-4"
        color="primary"
        data-testid="new-profile-btn"
        prepend-icon="mdi-plus"
        @click="openCreate"
      >
        New profile
      </v-btn>
    </v-toolbar>

    <v-data-table-server
      :headers="headers"
      item-value="id"
      :items="profiles"
      :items-length="total"
      :loading="loading"
      @update:options="onTableOptions"
    >
      <template #[`item.assigned_machines`]="{ item }">
        <v-chip v-if="item.assigned_machines > 0" color="primary" size="small" variant="tonal">
          {{ item.assigned_machines }}
        </v-chip>
        <span v-else class="text-medium-emphasis">0</span>
      </template>
      <template #[`item.actions`]="{ item }">
        <v-btn
          icon="mdi-eye"
          size="small"
          title="Preview"
          variant="text"
          @click="openPreview(item)"
        />
        <v-btn
          icon="mdi-pencil"
          size="small"
          title="Edit"
          variant="text"
          @click="openEdit(item)"
        />
        <v-btn
          icon="mdi-content-copy"
          size="small"
          title="Clone"
          variant="text"
          @click="openClone(item)"
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
        <div class="py-6 text-medium-emphasis">No profiles yet.</div>
      </template>
    </v-data-table-server>
  </v-card>

  <ProfileEditor
    v-model="editorOpen"
    :initial="editorInitial"
    :mode="editorMode"
    :saving="editorSaving"
    :server-errors="editorServerErrors"
    @save="onEditorSave"
  />

  <ProfilePreviewDrawer
    v-model="previewOpen"
    :profile-id="previewProfile?.id ?? null"
    :profile-name="previewProfile?.name"
  />

  <v-dialog :model-value="cloneTarget !== null" max-width="440" @update:model-value="cloneTarget = null">
    <v-card rounded="lg">
      <v-card-title class="pt-4 px-6">Clone profile</v-card-title>
      <v-card-text class="px-6">
        <v-text-field
          v-model="cloneName"
          data-testid="clone-name"
          :error-messages="cloneError ? [cloneError] : []"
          label="New profile name"
          variant="outlined"
          @keyup.enter="onCloneConfirm"
        />
      </v-card-text>
      <v-card-actions class="px-6 pb-4">
        <v-spacer />
        <v-btn :disabled="cloneBusy" variant="text" @click="cloneTarget = null">Cancel</v-btn>
        <v-btn color="primary" :loading="cloneBusy" @click="onCloneConfirm">Clone</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <ConfirmDialog
    :busy="deleteBusy"
    confirm-color="red"
    confirm-label="Delete"
    :message="deleteMessage"
    :model-value="deleteTarget !== null"
    title="Delete profile"
    @confirm="onDeleteConfirm"
    @update:model-value="deleteTarget = null"
  />

  <v-snackbar v-model="snackbar" color="error" timeout="5000">
    {{ error }}
  </v-snackbar>
</template>
