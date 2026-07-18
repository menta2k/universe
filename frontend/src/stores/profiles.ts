/**
 * Profiles store: server-paginated profile list plus create/update/clone/
 * delete/preview actions. Components never call the profiles API directly.
 * All state updates replace arrays immutably.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

import * as profilesApi from '../api/profiles'
import type { ProfileInput, ProfilePreview } from '../api/profiles'
import type { Profile } from '../api/types'

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}

export const useProfilesStore = defineStore('profiles', () => {
  const profiles = ref<readonly Profile[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const loading = ref(false)
  const error = ref<string | null>(null)

  function replaceProfile(updated: Profile): void {
    profiles.value = profiles.value.map((p) => (p.id === updated.id ? updated : p))
  }

  async function fetchProfiles(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const result = await profilesApi.listProfilesPage({
        page: page.value,
        page_size: pageSize.value,
      })
      profiles.value = result.profiles
      total.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      profiles.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
  }

  /** Run a mutation, surface its failure in `error`, and rethrow for callers. */
  async function mutate<T>(action: () => Promise<T>): Promise<T> {
    error.value = null
    try {
      return await action()
    } catch (e: unknown) {
      error.value = errorMessage(e)
      throw e
    }
  }

  async function createProfile(input: ProfileInput): Promise<Profile> {
    const created = await mutate(() => profilesApi.createProfile(input))
    await fetchProfiles()
    return created
  }

  async function updateProfile(id: string, input: ProfileInput): Promise<Profile> {
    const updated = await mutate(() => profilesApi.updateProfile(id, input))
    replaceProfile(updated)
    return updated
  }

  async function cloneProfile(id: string, newName: string): Promise<Profile> {
    const created = await mutate(() => profilesApi.cloneProfile(id, newName))
    await fetchProfiles()
    return created
  }

  async function removeProfile(id: string): Promise<void> {
    await mutate(() => profilesApi.removeProfile(id))
    await fetchProfiles()
  }

  async function previewProfile(id: string, machineId?: string): Promise<ProfilePreview> {
    return mutate(() => profilesApi.previewProfile(id, machineId))
  }

  return {
    profiles,
    total,
    page,
    pageSize,
    loading,
    error,
    fetchProfiles,
    createProfile,
    updateProfile,
    cloneProfile,
    removeProfile,
    previewProfile,
  }
})
