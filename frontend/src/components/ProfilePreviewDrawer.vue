<script setup lang="ts">
/**
 * Profile preview drawer: renders the profile's autoinstall user-data and
 * kernel cmdline (credentials redacted server-side) in a readonly monospace
 * block. Loads via the profiles store when opened.
 */
import { ref, watch } from 'vue'

import { useProfilesStore } from '../stores/profiles'

const props = defineProps<{
  modelValue: boolean
  profileId: string | null
  profileName?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const store = useProfilesStore()

const loading = ref(false)
const errorText = ref<string | null>(null)
const userData = ref('')
const cmdline = ref('')

async function load(id: string): Promise<void> {
  loading.value = true
  errorText.value = null
  userData.value = ''
  cmdline.value = ''
  try {
    const preview = await store.previewProfile(id)
    userData.value = preview.user_data
    cmdline.value = preview.cmdline
  } catch (e: unknown) {
    errorText.value = e instanceof Error ? e.message : 'Failed to render preview'
  } finally {
    loading.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open && props.profileId) void load(props.profileId)
  },
)
</script>

<template>
  <v-navigation-drawer
    location="right"
    :model-value="modelValue"
    temporary
    width="640"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-toolbar color="transparent" density="comfortable">
      <v-toolbar-title>Preview {{ profileName ?? '' }}</v-toolbar-title>
      <v-btn icon="mdi-close" @click="emit('update:modelValue', false)" />
    </v-toolbar>
    <v-divider />

    <div class="pa-4">
      <v-progress-linear v-if="loading" color="primary" indeterminate />
      <v-alert v-else-if="errorText" class="mb-2" density="compact" type="error" variant="tonal">
        {{ errorText }}
      </v-alert>

      <template v-else>
        <div class="text-subtitle-2 mb-1">Kernel cmdline</div>
        <pre class="preview-block mb-4" data-testid="preview-cmdline">{{ cmdline || '—' }}</pre>

        <div class="text-subtitle-2 mb-1">Rendered user-data</div>
        <pre class="preview-block" data-testid="preview-user-data">{{ userData || '—' }}</pre>
      </template>
    </div>
  </v-navigation-drawer>
</template>

<style scoped>
.preview-block {
  background: rgba(var(--v-theme-on-surface), 0.05);
  border-radius: 6px;
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8rem;
  overflow-x: auto;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
