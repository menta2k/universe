<script setup lang="ts">
/** Generic confirmation dialog used for destructive / provisioning actions. */
defineProps<{
  modelValue: boolean
  title: string
  message: string
  confirmLabel?: string
  confirmColor?: string
  busy?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  confirm: []
}>()
</script>

<template>
  <v-dialog
    :model-value="modelValue"
    max-width="440"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card rounded="lg">
      <v-card-title class="pt-4 px-6">{{ title }}</v-card-title>
      <v-card-text class="px-6">{{ message }}</v-card-text>
      <v-card-actions class="px-6 pb-4">
        <v-spacer />
        <v-btn :disabled="busy" variant="text" @click="emit('update:modelValue', false)">
          Cancel
        </v-btn>
        <v-btn :color="confirmColor ?? 'primary'" :loading="busy" @click="emit('confirm')">
          {{ confirmLabel ?? 'Confirm' }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
