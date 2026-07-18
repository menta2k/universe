<script setup lang="ts">
/** Color-coded session-state chip; shows a spinner while the session is active. */
import { computed } from 'vue'

import type { SessionState } from '../api/types'

const props = defineProps<{ state: SessionState }>()

const STATE_COLORS: Readonly<Record<SessionState, string>> = {
  active: 'amber',
  completed: 'green',
  failed: 'red',
  stale: 'grey',
}

const color = computed(() => STATE_COLORS[props.state] ?? 'grey')
</script>

<template>
  <v-chip :color="color" label size="small" variant="tonal">
    <v-progress-circular
      v-if="props.state === 'active'"
      class="mr-1"
      indeterminate
      size="12"
      width="2"
    />
    {{ props.state }}
  </v-chip>
</template>
