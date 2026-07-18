<script setup lang="ts">
/**
 * Subnet / range editor: a table of DHCP subnets with add/remove rows plus a
 * lease TTL field. Validates CIDR, IPv4 ranges, and range ordering locally,
 * renders server-side 422 field errors inline (keys like
 * `subnets[0].range`, `lease_ttl_seconds`), and emits a serialized
 * DhcpConfigInput payload on save. The parent owns the PUT call.
 */
import { computed, ref, watch } from 'vue'

import type { DhcpConfigInput, DhcpSubnetInput } from '../api/dhcp'
import type { DhcpSubnet } from '../api/types'

interface SubnetRow {
  network: string
  range_start: string
  range_end: string
  gateway: string
  dns: string[]
}

const props = defineProps<{
  subnets?: readonly DhcpSubnet[]
  leaseTtlSeconds?: number
  serverErrors?: Readonly<Record<string, string>>
  saving?: boolean
}>()

const emit = defineEmits<{
  save: [values: DhcpConfigInput]
}>()

const IPV4_REGEX = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/
const CIDR_REGEX =
  /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\/(3[0-2]|[12]?\d)$/

function toRow(subnet: DhcpSubnet): SubnetRow {
  return {
    network: subnet.network,
    range_start: subnet.range_start,
    range_end: subnet.range_end,
    gateway: subnet.gateway,
    dns: [...subnet.dns],
  }
}

function emptyRow(): SubnetRow {
  return { network: '', range_start: '', range_end: '', gateway: '', dns: [] }
}

const rows = ref<SubnetRow[]>([])
const leaseTtl = ref<number | string>(3600)
const submitted = ref(false)

watch(
  () => [props.subnets, props.leaseTtlSeconds] as const,
  ([subnets, ttl]) => {
    rows.value = (subnets ?? []).map(toRow)
    leaseTtl.value = ttl ?? 3600
    submitted.value = false
  },
  { immediate: true },
)

function ipToNumber(ip: string): number {
  return ip.split('.').reduce((acc, part) => acc * 256 + Number(part), 0)
}

/** Local validation keyed by `${rowIndex}.${field}` plus `lease_ttl_seconds`. */
const localErrors = computed<Readonly<Record<string, string>>>(() => {
  const errors: Record<string, string> = {}
  rows.value.forEach((row, i) => {
    if (!row.network.trim()) errors[`${i}.network`] = 'Network is required'
    else if (!CIDR_REGEX.test(row.network.trim()))
      errors[`${i}.network`] = 'Enter CIDR like 10.0.0.0/24'
    if (!row.range_start.trim()) errors[`${i}.range_start`] = 'Start is required'
    else if (!IPV4_REGEX.test(row.range_start.trim()))
      errors[`${i}.range_start`] = 'Enter a valid IPv4'
    if (!row.range_end.trim()) errors[`${i}.range_end`] = 'End is required'
    else if (!IPV4_REGEX.test(row.range_end.trim()))
      errors[`${i}.range_end`] = 'Enter a valid IPv4'
    if (
      !errors[`${i}.range_start`] &&
      !errors[`${i}.range_end`] &&
      ipToNumber(row.range_start.trim()) > ipToNumber(row.range_end.trim())
    )
      errors[`${i}.range_end`] = 'End must be >= start'
    if (row.gateway.trim() && !IPV4_REGEX.test(row.gateway.trim()))
      errors[`${i}.gateway`] = 'Enter a valid IPv4'
    if (row.dns.some((d) => d.trim() && !IPV4_REGEX.test(d.trim())))
      errors[`${i}.dns`] = 'DNS entries must be IPv4'
  })
  const ttl = Number(leaseTtl.value)
  if (!Number.isInteger(ttl) || ttl <= 0)
    errors.lease_ttl_seconds = 'Lease TTL must be a positive integer'
  return errors
})

const isValid = computed(() => Object.keys(localErrors.value).length === 0)

/** Map a server error key (`subnets[i].field` / `range`) onto a field slot. */
function serverError(rowIndex: number, field: string): string | undefined {
  const errs = props.serverErrors
  if (!errs) return undefined
  const direct = errs[`subnets[${rowIndex}].${field}`]
  if (direct) return direct
  // The backend reports range violations under `range`; surface on both ends.
  if (field === 'range_start' || field === 'range_end') {
    return errs[`subnets[${rowIndex}].range`]
  }
  return undefined
}

function fieldErrors(rowIndex: number, field: string): readonly string[] {
  const messages: string[] = []
  if (submitted.value && localErrors.value[`${rowIndex}.${field}`])
    messages.push(localErrors.value[`${rowIndex}.${field}`])
  const server = serverError(rowIndex, field)
  if (server) messages.push(server)
  return messages
}

const ttlErrors = computed<readonly string[]>(() => {
  const messages: string[] = []
  if (submitted.value && localErrors.value.lease_ttl_seconds)
    messages.push(localErrors.value.lease_ttl_seconds)
  const server = props.serverErrors?.lease_ttl_seconds
  if (server) messages.push(server)
  return messages
})

function addRow(): void {
  rows.value = [...rows.value, emptyRow()]
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index)
}

function serialize(): DhcpConfigInput {
  const subnets: DhcpSubnetInput[] = rows.value.map((row) => ({
    network: row.network.trim(),
    range_start: row.range_start.trim(),
    range_end: row.range_end.trim(),
    gateway: row.gateway.trim(),
    dns: row.dns.map((d) => d.trim()).filter((d) => d.length > 0),
  }))
  return { lease_ttl_seconds: Number(leaseTtl.value), subnets }
}

function submit(): void {
  submitted.value = true
  if (!isValid.value) return
  emit('save', serialize())
}

defineExpose({ rows, leaseTtl, localErrors, submit, addRow, removeRow })
</script>

<template>
  <div>
    <div class="d-flex align-center mb-3">
      <v-text-field
        v-model.number="leaseTtl"
        data-testid="field-lease-ttl"
        density="compact"
        :error-messages="ttlErrors"
        hide-details="auto"
        label="Lease TTL (seconds)"
        max-width="220"
        type="number"
        variant="outlined"
      />
      <v-spacer />
      <v-btn
        color="primary"
        data-testid="add-subnet-btn"
        prepend-icon="mdi-plus"
        variant="tonal"
        @click="addRow"
      >
        Add subnet
      </v-btn>
    </div>

    <v-alert v-if="rows.length === 0" class="mb-3" density="compact" type="info" variant="tonal">
      No subnets configured. Add at least one subnet before enabling DHCP.
    </v-alert>

    <v-card
      v-for="(row, i) in rows"
      :key="i"
      border
      class="mb-3 pa-3"
      :data-testid="`subnet-row-${i}`"
      variant="flat"
    >
      <div class="d-flex align-center mb-2">
        <span class="text-subtitle-2">Subnet {{ i + 1 }}</span>
        <v-spacer />
        <v-btn
          color="red"
          :data-testid="`remove-subnet-${i}`"
          density="comfortable"
          icon="mdi-delete"
          size="small"
          variant="text"
          @click="removeRow(i)"
        />
      </div>
      <v-row dense>
        <v-col cols="12" md="6">
          <v-text-field
            v-model="row.network"
            density="compact"
            :error-messages="fieldErrors(i, 'network')"
            hide-details="auto"
            label="Network (CIDR)"
            placeholder="10.0.0.0/24"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="6">
          <v-text-field
            v-model="row.gateway"
            density="compact"
            :error-messages="fieldErrors(i, 'gateway')"
            hide-details="auto"
            label="Gateway"
            placeholder="10.0.0.1"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="6">
          <v-text-field
            v-model="row.range_start"
            density="compact"
            :error-messages="fieldErrors(i, 'range_start')"
            hide-details="auto"
            label="Range start"
            placeholder="10.0.0.100"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="6">
          <v-text-field
            v-model="row.range_end"
            density="compact"
            :error-messages="fieldErrors(i, 'range_end')"
            hide-details="auto"
            label="Range end"
            placeholder="10.0.0.200"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12">
          <v-combobox
            v-model="row.dns"
            chips
            closable-chips
            density="compact"
            :error-messages="fieldErrors(i, 'dns')"
            hide-details="auto"
            label="DNS servers"
            multiple
            variant="outlined"
          />
        </v-col>
      </v-row>
    </v-card>

    <div class="d-flex justify-end">
      <v-btn
        color="primary"
        data-testid="save-config-btn"
        :loading="saving"
        prepend-icon="mdi-content-save"
        @click="submit"
      >
        Save
      </v-btn>
    </div>
  </div>
</template>
