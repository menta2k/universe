<script setup lang="ts">
/**
 * Register / edit machine dialog. Validates locally (MAC, hostname, IPv4)
 * and renders server-side 422 field errors inline. The parent owns the API
 * call and passes failures back via `serverErrors`.
 */
import { computed, ref, watch } from 'vue'

import { listProfiles } from '../api/profiles'
import type { Firmware, Profile } from '../api/types'
import {
  type NicForm,
  defaultNic,
  emptyNic,
  parseNetworkConfig,
  serializeNics,
  validateNics,
} from '../utils/netplan'

export type MachineDialogMode = 'create' | 'edit' | 'register-unknown'

export interface MachineFormValues {
  readonly mac: string
  readonly name: string
  readonly firmware: Firmware
  readonly profile_id: string
  readonly reservation_ip: string
  readonly notes: string
  /** Per-machine netplan override as a JSON string; empty means "inherit profile". */
  readonly network_config: string
}

/** Machine network override modes: inherit the profile, or override per-NIC / raw. */
type MachineNetworkMode = 'inherit' | 'static' | 'advanced'

/** Mutable local edit state; v-select `clearable` may set profile_id to null. */
interface MachineFormState {
  mac: string
  name: string
  firmware: Firmware
  profile_id: string | null
  reservation_ip: string
  notes: string
  networkMode: MachineNetworkMode
  nics: NicForm[]
  networkAdvanced: string
}

const props = defineProps<{
  modelValue: boolean
  mode: MachineDialogMode
  initial?: Partial<MachineFormValues>
  serverErrors?: Readonly<Record<string, string>>
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [values: MachineFormValues]
}>()

const MAC_REGEX = /^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/i
const HOSTNAME_REGEX =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i
const IPV4_REGEX = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/

const FIRMWARE_OPTIONS: readonly { title: string; value: Firmware }[] = [
  { title: 'BIOS', value: 'bios' },
  { title: 'UEFI x64', value: 'uefi_x64' },
  { title: 'Unknown', value: 'unknown' },
]

const NETWORK_MODE_OPTIONS: readonly { title: string; value: MachineNetworkMode }[] = [
  { title: 'Inherit from profile', value: 'inherit' },
  { title: 'Override interfaces', value: 'static' },
  { title: 'Advanced (netplan JSON)', value: 'advanced' },
]

function emptyForm(): MachineFormState {
  return {
    mac: '',
    name: '',
    firmware: 'uefi_x64',
    profile_id: '',
    reservation_ip: '',
    notes: '',
    networkMode: 'inherit',
    nics: [defaultNic()],
    networkAdvanced: '',
  }
}

/** Builds edit state from initial values, parsing the network override string. */
function formFromInitial(initial?: Partial<MachineFormValues>): MachineFormState {
  const base = emptyForm()
  if (!initial) return base
  const { network_config, ...rest } = initial
  const state: MachineFormState = { ...base, ...rest }
  const raw = (network_config ?? '').trim()
  if (raw) {
    try {
      const parsed = parseNetworkConfig(JSON.parse(raw))
      if (parsed.mode === 'static') {
        state.networkMode = 'static'
        state.nics = parsed.nics.length > 0 ? parsed.nics : [defaultNic()]
      } else if (parsed.mode === 'advanced') {
        state.networkMode = 'advanced'
        state.networkAdvanced = parsed.raw
      }
      // parsed.mode === 'dhcp' only for an empty object, i.e. no real override.
    } catch {
      state.networkMode = 'advanced'
      state.networkAdvanced = raw
    }
  }
  return state
}

/** Serialises the network override back to a JSON string ('' = inherit). */
function serializeNetworkOverride(state: MachineFormState): string {
  if (state.networkMode === 'inherit') return ''
  if (state.networkMode === 'advanced') return state.networkAdvanced.trim()
  return serializeNics(state.nics)
}

const form = ref<MachineFormState>(emptyForm())
const submitted = ref(false)

const profiles = ref<readonly Profile[]>([])
const profilesError = ref<string | null>(null)

const isEdit = computed(() => props.mode === 'edit')
const isRegisterUnknown = computed(() => props.mode === 'register-unknown')
const title = computed(() => {
  if (isEdit.value) return 'Edit machine'
  if (isRegisterUnknown.value) return 'Register unknown machine'
  return 'Register machine'
})

const profileItems = computed(() =>
  profiles.value.map((p) => ({ title: `${p.name} (${p.ubuntu_release})`, value: p.id })),
)

/** Local validation, evaluated per field (empty array = valid). */
const localErrors = computed<Readonly<Record<string, string>>>(() => {
  const errors: Record<string, string> = {}
  if (!form.value.mac.trim()) errors.mac = 'MAC address is required'
  else if (!MAC_REGEX.test(form.value.mac.trim()))
    errors.mac = 'Enter a MAC like aa:bb:cc:dd:ee:ff'
  if (!form.value.name.trim()) errors.name = 'Name is required'
  else if (!HOSTNAME_REGEX.test(form.value.name.trim()))
    errors.name = 'Enter a valid hostname (letters, digits, hyphens)'
  if (form.value.reservation_ip.trim() && !IPV4_REGEX.test(form.value.reservation_ip.trim()))
    errors.reservation_ip = 'Enter a valid IPv4 address'
  if (isRegisterUnknown.value && !form.value.profile_id)
    errors.profile_id = 'Profile is required when registering from an unknown boot'
  if (form.value.networkMode === 'static') {
    Object.assign(errors, validateNics(form.value.nics))
  } else if (form.value.networkMode === 'advanced' && form.value.networkAdvanced.trim()) {
    try {
      JSON.parse(form.value.networkAdvanced)
    } catch {
      errors.network_config = 'Network override must be valid JSON'
    }
  }
  return errors
})

const isValid = computed(() => Object.keys(localErrors.value).length === 0)

function fieldErrors(field: string): readonly string[] {
  const messages: string[] = []
  if (submitted.value && localErrors.value[field]) messages.push(localErrors.value[field])
  const server = props.serverErrors?.[field]
  if (server) messages.push(server)
  return messages
}

async function loadProfiles(): Promise<void> {
  profilesError.value = null
  try {
    profiles.value = await listProfiles()
  } catch (e: unknown) {
    profilesError.value = e instanceof Error ? e.message : 'Failed to load profiles'
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    submitted.value = false
    form.value = formFromInitial(props.initial)
    void loadProfiles()
  },
  { immediate: true },
)

function close(): void {
  emit('update:modelValue', false)
}

function addNic(): void {
  form.value.nics = [...form.value.nics, emptyNic()]
}

function removeNic(index: number): void {
  form.value.nics = form.value.nics.filter((_, i) => i !== index)
}

function updateNic(index: number, patch: Partial<NicForm>): void {
  form.value.nics = form.value.nics.map((nic, i) => (i === index ? { ...nic, ...patch } : nic))
}

function submit(): void {
  submitted.value = true
  if (!isValid.value) return
  emit('save', {
    mac: form.value.mac.trim().toLowerCase(),
    name: form.value.name.trim(),
    firmware: form.value.firmware,
    profile_id: form.value.profile_id ?? '',
    reservation_ip: form.value.reservation_ip.trim(),
    notes: form.value.notes.trim(),
    network_config: serializeNetworkOverride(form.value),
  })
}

defineExpose({ form, submit, localErrors })
</script>

<template>
  <v-dialog
    :model-value="modelValue"
    max-width="560"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card rounded="lg">
      <v-card-title class="pt-4 px-6">{{ title }}</v-card-title>
      <v-card-text class="px-6">
        <v-form @submit.prevent="submit">
          <v-text-field
            v-model="form.mac"
            class="mb-2"
            data-testid="field-mac"
            :disabled="isEdit || isRegisterUnknown"
            :error-messages="fieldErrors('mac')"
            label="MAC address"
            placeholder="aa:bb:cc:dd:ee:ff"
            variant="outlined"
          />
          <v-text-field
            v-model="form.name"
            class="mb-2"
            data-testid="field-name"
            :error-messages="fieldErrors('name')"
            label="Name"
            placeholder="node-01"
            variant="outlined"
          />
          <v-select
            v-if="!isEdit && !isRegisterUnknown"
            v-model="form.firmware"
            class="mb-2"
            data-testid="field-firmware"
            :error-messages="fieldErrors('firmware')"
            :items="FIRMWARE_OPTIONS"
            label="Firmware"
            variant="outlined"
          />
          <v-select
            v-model="form.profile_id"
            class="mb-2"
            clearable
            data-testid="field-profile"
            :error-messages="fieldErrors('profile_id')"
            :items="profileItems"
            label="Profile"
            :messages="profilesError ?? undefined"
            variant="outlined"
          />
          <template v-if="!isRegisterUnknown">
            <v-text-field
              v-model="form.reservation_ip"
              class="mb-2"
              data-testid="field-ip"
              :error-messages="fieldErrors('reservation_ip')"
              label="Reservation IP (optional)"
              placeholder="10.0.0.50"
              variant="outlined"
            />
            <v-textarea
              v-model="form.notes"
              class="mb-2"
              data-testid="field-notes"
              :error-messages="fieldErrors('notes')"
              label="Notes"
              rows="2"
              variant="outlined"
            />

            <!-- Per-machine network override -->
            <v-select
              v-model="form.networkMode"
              class="mb-2"
              data-testid="field-network-mode"
              hide-details="auto"
              :items="NETWORK_MODE_OPTIONS"
              label="Network"
              variant="outlined"
            />
            <div v-if="form.networkMode !== 'inherit'" class="text-caption text-medium-emphasis mb-2">
              Overrides the profile's network for this machine only.
            </div>

            <template v-if="form.networkMode === 'static'">
              <div
                v-for="(nic, index) in form.nics"
                :key="`nic-${index}`"
                class="pa-3 mb-2 rounded border"
              >
                <div class="d-flex align-center ga-2 mb-2">
                  <v-text-field
                    class="flex-grow-1"
                    density="compact"
                    hide-details="auto"
                    label="Interface (name or glob, e.g. en*)"
                    :model-value="nic.name"
                    variant="outlined"
                    @update:model-value="updateNic(index, { name: $event })"
                  />
                  <v-switch
                    color="primary"
                    density="compact"
                    hide-details
                    label="DHCP"
                    :model-value="nic.dhcp"
                    @update:model-value="updateNic(index, { dhcp: !!$event })"
                  />
                  <v-btn
                    :disabled="form.nics.length <= 1"
                    icon="mdi-close"
                    size="small"
                    title="Remove interface"
                    variant="text"
                    @click="removeNic(index)"
                  />
                </div>
                <template v-if="!nic.dhcp">
                  <v-text-field
                    class="mb-2"
                    density="compact"
                    :error-messages="fieldErrors(`nic_${index}_address`)"
                    hide-details="auto"
                    label="Address (CIDR, e.g. 10.0.0.5/24)"
                    :model-value="nic.address"
                    variant="outlined"
                    @update:model-value="updateNic(index, { address: $event })"
                  />
                  <div class="d-flex ga-2">
                    <v-text-field
                      density="compact"
                      :error-messages="fieldErrors(`nic_${index}_gateway`)"
                      hide-details="auto"
                      label="Gateway"
                      :model-value="nic.gateway"
                      variant="outlined"
                      @update:model-value="updateNic(index, { gateway: $event })"
                    />
                    <v-text-field
                      density="compact"
                      hide-details="auto"
                      label="DNS (comma-separated)"
                      :model-value="nic.dns.join(', ')"
                      variant="outlined"
                      @update:model-value="
                        updateNic(index, {
                          dns: $event
                            .split(/[,\s]+/)
                            .map((s: string) => s.trim())
                            .filter(Boolean),
                        })
                      "
                    />
                  </div>
                </template>
              </div>
              <v-btn prepend-icon="mdi-plus" size="small" variant="tonal" @click="addNic">
                Add interface
              </v-btn>
            </template>

            <v-textarea
              v-if="form.networkMode === 'advanced'"
              v-model="form.networkAdvanced"
              class="text-mono"
              data-testid="field-network-advanced"
              :error-messages="fieldErrors('network_config')"
              hide-details="auto"
              label="netplan JSON"
              placeholder='{"version":2,"ethernets":{"en*":{"dhcp4":true}}}'
              rows="4"
              variant="outlined"
            />
          </template>
        </v-form>
      </v-card-text>
      <v-card-actions class="px-6 pb-4">
        <v-spacer />
        <v-btn :disabled="saving" variant="text" @click="close">Cancel</v-btn>
        <v-btn color="primary" data-testid="save-btn" :loading="saving" @click="submit">
          Save
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
