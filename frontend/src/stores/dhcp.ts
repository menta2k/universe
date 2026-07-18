/**
 * DHCP store: server config (enabled flag + version + subnets), live leases,
 * and observed foreign servers. Components never call the DHCP API directly.
 * All state updates replace objects/arrays immutably.
 */
import { defineStore } from 'pinia'
import { ref } from 'vue'

import * as dhcpApi from '../api/dhcp'
import type { DhcpConfigInput, DhcpLeaseEntry, ForeignDhcpServer } from '../api/dhcp'
import type { DhcpConfig } from '../api/types'

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}

export const useDhcpStore = defineStore('dhcp', () => {
  const config = ref<DhcpConfig | null>(null)
  const leases = ref<readonly DhcpLeaseEntry[]>([])
  const leasesTotal = ref(0)
  const conflicts = ref<readonly ForeignDhcpServer[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchConfig(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      config.value = await dhcpApi.getConfig()
    } catch (e: unknown) {
      error.value = errorMessage(e)
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

  async function updateConfig(input: DhcpConfigInput): Promise<DhcpConfig> {
    const updated = await mutate(() => dhcpApi.updateConfig(input))
    config.value = updated
    return updated
  }

  async function enable(): Promise<DhcpConfig> {
    const updated = await mutate(() => dhcpApi.enableDhcp())
    config.value = updated
    return updated
  }

  async function disable(): Promise<DhcpConfig> {
    const updated = await mutate(() => dhcpApi.disableDhcp())
    config.value = updated
    return updated
  }

  async function fetchLeases(page = 1, pageSize = 50): Promise<void> {
    error.value = null
    try {
      const result = await dhcpApi.listLeases({ page, page_size: pageSize })
      leases.value = result.leases
      leasesTotal.value = result.meta.total
    } catch (e: unknown) {
      error.value = errorMessage(e)
    }
  }

  async function fetchConflicts(page = 1, pageSize = 50): Promise<void> {
    error.value = null
    try {
      const result = await dhcpApi.listConflicts({ page, page_size: pageSize })
      conflicts.value = result.servers
    } catch (e: unknown) {
      error.value = errorMessage(e)
    }
  }

  return {
    config,
    leases,
    leasesTotal,
    conflicts,
    loading,
    error,
    fetchConfig,
    updateConfig,
    enable,
    disable,
    fetchLeases,
    fetchConflicts,
  }
})
