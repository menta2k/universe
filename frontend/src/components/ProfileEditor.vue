<script setup lang="ts">
/**
 * Create / edit profile dialog. Validates locally (name, >=1 SSH key, custom
 * storage body, no newline in kernel cmdline, JSON network config) and renders
 * server-side 422 field errors inline. Serialises storage_layout and
 * network_config to JSON strings; the parent owns the API call and passes
 * failures back via `serverErrors`.
 */
import { computed, ref, watch } from 'vue'

import type { ProfileInput } from '../api/profiles'
import type { Profile, StorageMode, UbuntuRelease } from '../api/types'

export type ProfileEditorMode = 'create' | 'edit'

interface ProfileFormState {
  name: string
  ubuntu_release: UbuntuRelease
  storageMode: StorageMode
  storageCustom: string
  networkConfig: string
  packages: string[]
  sshKeys: string[]
  lateCommands: string[]
  kernelCmdlineExtra: string
  userDataTemplate: string
}

const props = defineProps<{
  modelValue: boolean
  mode: ProfileEditorMode
  initial?: Profile | null
  serverErrors?: Readonly<Record<string, string>>
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [values: ProfileInput]
}>()

const RELEASE_OPTIONS: readonly { title: string; value: UbuntuRelease }[] = [
  { title: 'Ubuntu 22.04 (jammy)', value: 'jammy' },
  { title: 'Ubuntu 24.04 (noble)', value: 'noble' },
]

const STORAGE_OPTIONS: readonly { title: string; value: StorageMode }[] = [
  { title: 'LVM', value: 'lvm' },
  { title: 'Direct', value: 'direct' },
  { title: 'Custom', value: 'custom' },
]

function emptyForm(): ProfileFormState {
  return {
    name: '',
    ubuntu_release: 'noble',
    storageMode: 'lvm',
    storageCustom: '',
    networkConfig: '',
    packages: [],
    sshKeys: [''],
    lateCommands: [],
    kernelCmdlineExtra: '',
    userDataTemplate: '',
  }
}

function fromProfile(profile: Profile): ProfileFormState {
  const network = profile.network_config
  const hasNetwork = network && Object.keys(network).length > 0
  return {
    name: profile.name,
    ubuntu_release: profile.ubuntu_release,
    storageMode: profile.storage_layout.mode,
    storageCustom: profile.storage_layout.custom ?? '',
    networkConfig: hasNetwork ? JSON.stringify(network, null, 2) : '',
    packages: [...profile.packages],
    sshKeys: profile.ssh_authorized_keys.length > 0 ? [...profile.ssh_authorized_keys] : [''],
    lateCommands: [...profile.late_commands],
    kernelCmdlineExtra: profile.kernel_cmdline_extra,
    userDataTemplate: profile.user_data_template ?? '',
  }
}

const form = ref<ProfileFormState>(emptyForm())
const submitted = ref(false)

const title = computed(() => (props.mode === 'edit' ? 'Edit profile' : 'New profile'))

const localErrors = computed<Readonly<Record<string, string>>>(() => {
  const errors: Record<string, string> = {}
  if (!form.value.name.trim()) errors.name = 'Name is required'

  const keys = form.value.sshKeys.map((k) => k.trim()).filter(Boolean)
  if (keys.length === 0) errors.ssh_authorized_keys = 'At least one SSH authorized key is required'

  if (form.value.storageMode === 'custom' && !form.value.storageCustom.trim())
    errors.storage_layout = 'Custom storage layout requires a configuration body'

  if (form.value.kernelCmdlineExtra.includes('\n'))
    errors.kernel_cmdline_extra = 'Kernel cmdline must not contain newlines'

  const network = form.value.networkConfig.trim()
  if (network) {
    try {
      JSON.parse(network)
    } catch {
      errors.network_config = 'Network config must be valid JSON'
    }
  }
  return errors
})

const isValid = computed(() => Object.keys(localErrors.value).length === 0)

function fieldErrors(field: string): readonly string[] {
  const messages: string[] = []
  if (submitted.value && localErrors.value[field]) messages.push(localErrors.value[field])
  const server = props.serverErrors?.[field]
  if (server) messages.push(server)
  return messages
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    submitted.value = false
    form.value = props.initial ? fromProfile(props.initial) : emptyForm()
  },
  { immediate: true },
)

function addSshKey(): void {
  form.value.sshKeys = [...form.value.sshKeys, '']
}

function removeSshKey(index: number): void {
  form.value.sshKeys = form.value.sshKeys.filter((_, i) => i !== index)
}

function addLateCommand(): void {
  form.value.lateCommands = [...form.value.lateCommands, '']
}

function removeLateCommand(index: number): void {
  form.value.lateCommands = form.value.lateCommands.filter((_, i) => i !== index)
}

function serializeStorage(): string {
  if (form.value.storageMode === 'custom')
    return JSON.stringify({ mode: 'custom', custom: form.value.storageCustom.trim() })
  return JSON.stringify({ mode: form.value.storageMode })
}

function serializeNetwork(): string {
  const raw = form.value.networkConfig.trim()
  if (!raw) return ''
  return JSON.stringify(JSON.parse(raw))
}

function close(): void {
  emit('update:modelValue', false)
}

function submit(): void {
  submitted.value = true
  if (!isValid.value) return
  emit('save', {
    name: form.value.name.trim(),
    ubuntu_release: form.value.ubuntu_release,
    storage_layout: serializeStorage(),
    network_config: serializeNetwork(),
    packages: form.value.packages.map((p) => p.trim()).filter(Boolean),
    ssh_authorized_keys: form.value.sshKeys.map((k) => k.trim()).filter(Boolean),
    user_data_template: form.value.userDataTemplate.trim() || null,
    late_commands: form.value.lateCommands.map((c) => c.trim()).filter(Boolean),
    kernel_cmdline_extra: form.value.kernelCmdlineExtra.trim(),
  })
}

defineExpose({ form, submit, localErrors })
</script>

<template>
  <v-dialog
    :model-value="modelValue"
    max-width="720"
    scrollable
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card rounded="lg">
      <v-card-title class="pt-4 px-6">{{ title }}</v-card-title>
      <v-card-text class="px-6" style="max-height: 70vh">
        <v-form @submit.prevent="submit">
          <v-text-field
            v-model="form.name"
            class="mb-2"
            data-testid="field-name"
            :error-messages="fieldErrors('name')"
            label="Name"
            placeholder="ubuntu-server"
            variant="outlined"
          />
          <v-select
            v-model="form.ubuntu_release"
            class="mb-2"
            data-testid="field-release"
            :error-messages="fieldErrors('ubuntu_release')"
            :items="RELEASE_OPTIONS"
            label="Ubuntu release"
            variant="outlined"
          />

          <v-select
            v-model="form.storageMode"
            class="mb-2"
            data-testid="field-storage-mode"
            :error-messages="form.storageMode === 'custom' ? [] : fieldErrors('storage_layout')"
            :items="STORAGE_OPTIONS"
            label="Storage layout"
            variant="outlined"
          />
          <v-textarea
            v-if="form.storageMode === 'custom'"
            v-model="form.storageCustom"
            class="mb-2 text-mono"
            data-testid="field-storage-custom"
            :error-messages="fieldErrors('storage_layout')"
            label="Custom storage config (curtin YAML / JSON)"
            rows="4"
            variant="outlined"
          />

          <v-textarea
            v-model="form.networkConfig"
            class="mb-2 text-mono"
            data-testid="field-network"
            :error-messages="fieldErrors('network_config')"
            label="Network config (optional JSON, netplan-shaped)"
            placeholder='{"version": 2, "ethernets": {}}'
            rows="3"
            variant="outlined"
          />

          <v-combobox
            v-model="form.packages"
            chips
            class="mb-2"
            closable-chips
            data-testid="field-packages"
            :error-messages="fieldErrors('packages')"
            label="Packages"
            multiple
            variant="outlined"
          />

          <div class="mb-1 text-subtitle-2">SSH authorized keys</div>
          <div
            v-for="(_, index) in form.sshKeys"
            :key="`ssh-${index}`"
            class="d-flex align-start ga-2 mb-2"
          >
            <v-textarea
              v-model="form.sshKeys[index]"
              auto-grow
              class="text-mono"
              :data-testid="`field-ssh-${index}`"
              density="compact"
              :error-messages="index === 0 ? fieldErrors('ssh_authorized_keys') : []"
              hide-details="auto"
              label="ssh-ed25519 / ssh-rsa ..."
              rows="1"
              variant="outlined"
            />
            <v-btn
              :disabled="form.sshKeys.length <= 1"
              icon="mdi-close"
              size="small"
              title="Remove key"
              variant="text"
              @click="removeSshKey(index)"
            />
          </div>
          <v-btn
            class="mb-4"
            prepend-icon="mdi-plus"
            size="small"
            variant="text"
            @click="addSshKey"
          >
            Add key
          </v-btn>

          <div class="mb-1 text-subtitle-2">Late commands</div>
          <div
            v-for="(_, index) in form.lateCommands"
            :key="`late-${index}`"
            class="d-flex align-start ga-2 mb-2"
          >
            <v-text-field
              v-model="form.lateCommands[index]"
              class="text-mono"
              density="compact"
              hide-details="auto"
              label="curtin in-target -- ..."
              variant="outlined"
            />
            <v-btn
              icon="mdi-close"
              size="small"
              title="Remove command"
              variant="text"
              @click="removeLateCommand(index)"
            />
          </div>
          <v-btn
            class="mb-4"
            prepend-icon="mdi-plus"
            size="small"
            variant="text"
            @click="addLateCommand"
          >
            Add command
          </v-btn>

          <v-text-field
            v-model="form.kernelCmdlineExtra"
            class="mb-2 text-mono"
            data-testid="field-cmdline"
            :error-messages="fieldErrors('kernel_cmdline_extra')"
            label="Kernel cmdline extra"
            placeholder="console=ttyS0"
            variant="outlined"
          />
          <v-textarea
            v-model="form.userDataTemplate"
            class="text-mono"
            data-testid="field-user-data"
            :error-messages="fieldErrors('user_data_template')"
            label="User-data template (optional autoinstall override)"
            rows="3"
            variant="outlined"
          />
        </v-form>
      </v-card-text>
      <v-card-actions class="px-6 pb-4">
        <v-spacer />
        <v-btn :disabled="saving" variant="text" @click="close">Cancel</v-btn>
        <v-btn color="primary" data-testid="save-btn" :loading="saving" @click="submit">
          Save
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.text-mono :deep(input),
.text-mono :deep(textarea) {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
}
</style>
