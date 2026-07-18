<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const username = ref('')
const password = ref('')
const error = ref<string | null>(null)
const submitting = ref(false)

const canSubmit = computed(
  () => username.value.trim().length > 0 && password.value.length > 0 && !submitting.value,
)

async function onSubmit(): Promise<void> {
  if (!canSubmit.value) return
  submitting.value = true
  error.value = null
  try {
    await auth.login(username.value, password.value)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.push(redirect)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Login failed'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <v-main>
    <v-container class="fill-height" fluid>
      <v-row align="center" justify="center">
        <v-col cols="12" sm="8" md="5" lg="4">
          <v-card class="pa-4" elevation="8" rounded="lg">
            <v-card-item class="text-center">
              <v-icon color="primary" icon="mdi-lan-connect" size="48" />
              <v-card-title class="text-h5 mt-2">Netboot Manager</v-card-title>
              <v-card-subtitle>Sign in to the provisioning console</v-card-subtitle>
            </v-card-item>
            <v-card-text>
              <v-form @submit.prevent="onSubmit">
                <v-text-field
                  v-model="username"
                  autocomplete="username"
                  autofocus
                  label="Username"
                  prepend-inner-icon="mdi-account-outline"
                  variant="outlined"
                />
                <v-text-field
                  v-model="password"
                  autocomplete="current-password"
                  class="mt-2"
                  label="Password"
                  prepend-inner-icon="mdi-lock-outline"
                  type="password"
                  variant="outlined"
                />
                <v-alert v-if="error" class="mb-4" density="compact" type="error" variant="tonal">
                  {{ error }}
                </v-alert>
                <v-btn
                  block
                  color="primary"
                  :disabled="!canSubmit"
                  :loading="submitting"
                  size="large"
                  type="submit"
                >
                  Sign in
                </v-btn>
              </v-form>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
    </v-container>
  </v-main>
</template>
