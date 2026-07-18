<script setup lang="ts">
/**
 * Upload / replace boot-file dialog. Picks a kind and (for kernel/initrd) an
 * Ubuntu release, then a file. Validates locally and renders server-side 422
 * field errors inline. The parent owns the API call and passes failures back
 * via `serverErrors`; an indeterminate progress bar shows while `saving`.
 */
import { computed, ref, watch } from 'vue'

import type { ArtifactKind, UbuntuRelease } from '../api/types'

export type ArtifactDialogMode = 'upload' | 'replace'

export interface ArtifactFormValues {
  readonly kind: ArtifactKind
  readonly ubuntu_release: UbuntuRelease | ''
  readonly file: File
}

interface ArtifactFormState {
  kind: ArtifactKind
  ubuntu_release: UbuntuRelease | ''
  file: File | null
}

const props = defineProps<{
  modelValue: boolean
  mode: ArtifactDialogMode
  /** Kind/release to prefill when replacing an existing artifact. */
  initialKind?: ArtifactKind
  initialRelease?: UbuntuRelease | ''
  replaceFilename?: string
  serverErrors?: Readonly<Record<string, string>>
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [values: ArtifactFormValues]
}>()

const KIND_OPTIONS: readonly { title: string; value: ArtifactKind }[] = [
  { title: 'Kernel', value: 'kernel' },
  { title: 'Initrd', value: 'initrd' },
  { title: 'iPXE binary', value: 'ipxe_bin' },
  { title: 'Other', value: 'other' },
]

const RELEASE_OPTIONS: readonly { title: string; value: UbuntuRelease }[] = [
  { title: 'Jammy (22.04)', value: 'jammy' },
  { title: 'Noble (24.04)', value: 'noble' },
]

function emptyForm(): ArtifactFormState {
  return { kind: 'kernel', ubuntu_release: '', file: null }
}

const form = ref<ArtifactFormState>(emptyForm())
const submitted = ref(false)

const isReplace = computed(() => props.mode === 'replace')
const title = computed(() => (isReplace.value ? 'Replace boot file' : 'Upload boot file'))

/** Release is only meaningful (and required) for kernel/initrd artifacts. */
const releaseRequired = computed(
  () => form.value.kind === 'kernel' || form.value.kind === 'initrd',
)

const localErrors = computed<Readonly<Record<string, string>>>(() => {
  const errors: Record<string, string> = {}
  if (releaseRequired.value && !form.value.ubuntu_release)
    errors.ubuntu_release = 'Ubuntu release is required for kernel and initrd files'
  if (!form.value.file) errors.file = 'A file is required'
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
    form.value = {
      ...emptyForm(),
      kind: props.initialKind ?? 'kernel',
      ubuntu_release: props.initialRelease ?? '',
    }
  },
  { immediate: true },
)

// Clear the release when it becomes irrelevant (kind switched away from kernel/initrd).
watch(releaseRequired, (required) => {
  if (!required) form.value.ubuntu_release = ''
})

function close(): void {
  emit('update:modelValue', false)
}

function submit(): void {
  submitted.value = true
  if (!isValid.value || !form.value.file) return
  emit('save', {
    kind: form.value.kind,
    ubuntu_release: releaseRequired.value ? form.value.ubuntu_release : '',
    file: form.value.file,
  })
}

defineExpose({ form, submit, localErrors })
</script>

<template>
  <v-dialog
    :model-value="modelValue"
    max-width="560"
    persistent
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card rounded="lg">
      <v-card-title class="pt-4 px-6">{{ title }}</v-card-title>
      <v-card-subtitle v-if="isReplace && replaceFilename" class="px-6">
        Replacing {{ replaceFilename }}
      </v-card-subtitle>
      <v-progress-linear v-if="saving" color="primary" indeterminate />
      <v-card-text class="px-6">
        <v-form @submit.prevent="submit">
          <v-select
            v-model="form.kind"
            class="mb-2"
            data-testid="field-kind"
            :disabled="saving"
            :error-messages="fieldErrors('kind')"
            :items="KIND_OPTIONS"
            label="Kind"
            variant="outlined"
          />
          <v-select
            v-if="releaseRequired"
            v-model="form.ubuntu_release"
            class="mb-2"
            data-testid="field-release"
            :disabled="saving"
            :error-messages="fieldErrors('ubuntu_release')"
            :items="RELEASE_OPTIONS"
            label="Ubuntu release"
            variant="outlined"
          />
          <v-file-input
            v-model="form.file"
            data-testid="field-file"
            :disabled="saving"
            :error-messages="fieldErrors('file')"
            label="File"
            prepend-icon="mdi-file-upload"
            show-size
            variant="outlined"
          />
        </v-form>
      </v-card-text>
      <v-card-actions class="px-6 pb-4">
        <v-spacer />
        <v-btn :disabled="saving" variant="text" @click="close">Cancel</v-btn>
        <v-btn color="primary" data-testid="save-btn" :loading="saving" @click="submit">
          {{ isReplace ? 'Replace' : 'Upload' }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
