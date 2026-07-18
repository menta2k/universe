/**
 * Artifacts store: server-paginated boot-file list, TFTP/HTTP transfer
 * activity, and upload/replace/delete actions. Components never call the
 * artifacts API directly. All state updates replace arrays/objects immutably.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

import * as artifactsApi from '../api/artifacts'
import type { ArtifactUploadInput, Transfer } from '../api/artifacts'
import type { BootArtifact } from '../api/types'

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}

export const useArtifactsStore = defineStore('artifacts', () => {
  const artifacts = ref<readonly BootArtifact[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const uploading = ref(false)

  const transfers = ref<readonly Transfer[]>([])
  const transfersTotal = ref(0)
  const transfersFilename = ref('')
  const transfersLoading = ref(false)

  async function fetchArtifacts(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const result = await artifactsApi.listArtifacts({
        page: page.value,
        page_size: pageSize.value,
      })
      artifacts.value = result.artifacts
      total.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      artifacts.value = []
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

  async function uploadArtifact(input: ArtifactUploadInput): Promise<BootArtifact> {
    uploading.value = true
    try {
      const created = await mutate(() => artifactsApi.uploadArtifact(input))
      await fetchArtifacts()
      return created
    } finally {
      uploading.value = false
    }
  }

  async function replaceArtifact(id: string, input: ArtifactUploadInput): Promise<BootArtifact> {
    uploading.value = true
    try {
      const updated = await mutate(() => artifactsApi.replaceArtifact(id, input))
      artifacts.value = artifacts.value.map((a) => (a.id === updated.id ? updated : a))
      return updated
    } finally {
      uploading.value = false
    }
  }

  async function deleteArtifact(id: string): Promise<void> {
    await mutate(() => artifactsApi.deleteArtifact(id))
    await fetchArtifacts()
  }

  async function fetchTransfers(): Promise<void> {
    transfersLoading.value = true
    error.value = null
    try {
      const result = await artifactsApi.listTransfers({
        page: 1,
        page_size: 50,
        filename: transfersFilename.value.trim() || undefined,
      })
      transfers.value = result.transfers
      transfersTotal.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      transfers.value = []
      transfersTotal.value = 0
    } finally {
      transfersLoading.value = false
    }
  }

  return {
    artifacts,
    total,
    page,
    pageSize,
    loading,
    error,
    uploading,
    transfers,
    transfersTotal,
    transfersFilename,
    transfersLoading,
    fetchArtifacts,
    uploadArtifact,
    replaceArtifact,
    deleteArtifact,
    fetchTransfers,
  }
})
