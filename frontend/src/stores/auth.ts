/**
 * Auth store: holds the current operator session. All auth API access goes
 * through this store; components never call the auth API directly.
 */
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import * as authApi from '../api/auth'
import type { Operator } from '../api/types'

export const useAuthStore = defineStore('auth', () => {
  const operator = ref<Operator | null>(null)
  /** True once the initial session check (fetchMe) has completed. */
  const checked = ref(false)

  const isAuthenticated = computed(() => operator.value !== null)
  const displayName = computed(
    () => operator.value?.display_name || operator.value?.username || '',
  )

  async function login(username: string, password: string): Promise<void> {
    const trimmedUsername = username.trim()
    if (trimmedUsername.length === 0 || password.length === 0) {
      throw new Error('Username and password are required')
    }
    operator.value = await authApi.login({ username: trimmedUsername, password })
    checked.value = true
  }

  async function logout(): Promise<void> {
    try {
      await authApi.logout()
    } catch {
      // The server session may already be gone; always clear local state.
    } finally {
      operator.value = null
    }
  }

  async function fetchMe(): Promise<void> {
    try {
      operator.value = await authApi.me()
    } catch {
      operator.value = null
    } finally {
      checked.value = true
    }
  }

  return { operator, checked, isAuthenticated, displayName, login, logout, fetchMe }
})
