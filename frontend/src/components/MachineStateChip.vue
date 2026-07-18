<script setup lang="ts">
/** Color-coded provision-state chip; shows a spinner while installing. */
import { computed } from 'vue'

import type { ProvisionState } from '../api/types'

const props = defineProps<{ state: ProvisionState }>()

const STATE_COLORS: Readonly<Record<ProvisionState, string>> = {
  new: 'grey',
  ready: 'blue',
  installing: 'amber',
  installed: 'green',
  failed: 'red',
}

const color = computed(() => STATE_COLORS[props.state] ?? 'grey')
</script>

<template>
  <v-chip :color="color" label size="small" variant="tonal">
    <v-progress-circular
      v-if="props.state === 'installing'"
      class="mr-1"
      indeterminate
      size="12"
      width="2"
    />
    {{ props.state }}
  </v-chip>
</template>
