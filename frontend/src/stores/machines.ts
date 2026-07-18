/**
 * Machines store: server-paginated machine list, filters, and provisioning
 * actions. Components never call the machines API directly. All state
 * updates replace arrays/objects immutably.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

import * as machinesApi from '../api/machines'
import type {
  CreateMachineInput,
  RegisterFromUnknownInput,
  UnknownBoot,
  UpdateMachineInput,
} from '../api/machines'
import type { Machine, ProvisionState } from '../api/types'

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}

export const useMachinesStore = defineStore('machines', () => {
  const machines = ref<readonly Machine[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const stateFilter = ref<ProvisionState | null>(null)
  const search = ref('')
  const loading = ref(false)
  const error = ref<string | null>(null)

  const unknownBoots = ref<readonly UnknownBoot[]>([])
  const unknownTotal = ref(0)
  const unknownLoading = ref(false)

  function replaceMachine(updated: Machine): void {
    machines.value = machines.value.map((m) => (m.id === updated.id ? updated : m))
  }

  async function fetchMachines(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const result = await machinesApi.listMachines({
        page: page.value,
        page_size: pageSize.value,
        state: stateFilter.value ?? undefined,
        q: search.value.trim() || undefined,
      })
      machines.value = result.machines
      total.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      machines.value = []
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

  async function createMachine(input: CreateMachineInput): Promise<Machine> {
    const created = await mutate(() => machinesApi.createMachine(input))
    await fetchMachines()
    return created
  }

  async function updateMachine(id: string, input: UpdateMachineInput): Promise<Machine> {
    const updated = await mutate(() => machinesApi.updateMachine(id, input))
    replaceMachine(updated)
    return updated
  }

  async function deleteMachine(id: string): Promise<void> {
    await mutate(() => machinesApi.deleteMachine(id))
    await fetchMachines()
  }

  async function provision(id: string): Promise<Machine> {
    const updated = await mutate(() => machinesApi.provisionMachine(id))
    replaceMachine(updated)
    return updated
  }

  async function cancel(id: string): Promise<Machine> {
    const updated = await mutate(() => machinesApi.cancelProvision(id))
    replaceMachine(updated)
    return updated
  }

  async function fetchUnknownBoots(unknownPage = 1, unknownPageSize = 10): Promise<void> {
    unknownLoading.value = true
    error.value = null
    try {
      const result = await machinesApi.listUnknownBoots(unknownPage, unknownPageSize)
      unknownBoots.value = result.boots
      unknownTotal.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
      unknownBoots.value = []
      unknownTotal.value = 0
    } finally {
      unknownLoading.value = false
    }
  }

  async function registerFromUnknown(input: RegisterFromUnknownInput): Promise<Machine> {
    const created = await mutate(() => machinesApi.registerFromUnknown(input))
    unknownBoots.value = unknownBoots.value.filter((boot) => boot.mac !== input.mac)
    await fetchMachines()
    return created
  }

  return {
    machines,
    total,
    page,
    pageSize,
    stateFilter,
    search,
    loading,
    error,
    unknownBoots,
    unknownTotal,
    unknownLoading,
    fetchMachines,
    createMachine,
    updateMachine,
    deleteMachine,
    provision,
    cancel,
    fetchUnknownBoots,
    registerFromUnknown,
  }
})
