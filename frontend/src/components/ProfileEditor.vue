<script setup lang="ts">
/**
 * Create / edit profile dialog. Organised into guided sections with the
 * advanced/raw fields tucked behind an expansion panel so the common path
 * stays simple. Network is captured with a friendly mode selector
 * (Automatic / Static / Advanced JSON) and serialised to netplan. Validates
 * locally and renders server-side 422 field errors inline; serialises
 * storage_layout and network_config to JSON strings for the parent's API call.
 */
import { computed, ref, watch } from 'vue'

import type { ProfileInput } from '../api/profiles'
import type { Profile, StorageMode, UbuntuRelease } from '../api/types'

export type ProfileEditorMode = 'create' | 'edit'
type NetworkMode = 'dhcp' | 'static' | 'advanced'

interface ProfileFormState {
  name: string
  ubuntu_release: UbuntuRelease
  keyboardLayout: string
  timezone: string
  storageMode: StorageMode
  storageCustom: string
  networkMode: NetworkMode
  staticAddress: string
  staticGateway: string
  staticDns: string[]
  networkConfig: string
  packages: string[]
  sshKeys: string[]
  lateCommands: string[]
  kernelCmdlineExtra: string
  userDataTemplate: string
}

const props = defineProps<{
  modelValue: boolean
  mode: ProfileEditorMode
  initial?: Profile | null
  serverErrors?: Readonly<Record<string, string>>
  saving?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [values: ProfileInput]
}>()

const RELEASE_OPTIONS: readonly {
  title: string
  subtitle: string
  value: UbuntuRelease
}[] = [
  { title: 'Ubuntu 24.04 LTS', subtitle: 'Noble Numbat · latest', value: 'noble' },
  { title: 'Ubuntu 22.04 LTS', subtitle: 'Jammy Jellyfish', value: 'jammy' },
]

// Common keyboard layouts (subiquity layout codes) with human labels.
const KEYBOARD_OPTIONS: readonly { title: string; value: string }[] = [
  { title: 'English (US)', value: 'us' },
  { title: 'English (UK)', value: 'gb' },
  { title: 'German', value: 'de' },
  { title: 'French', value: 'fr' },
  { title: 'Spanish', value: 'es' },
  { title: 'Italian', value: 'it' },
  { title: 'Portuguese', value: 'pt' },
  { title: 'Portuguese (Brazil)', value: 'br' },
  { title: 'Dutch', value: 'nl' },
  { title: 'Swedish', value: 'se' },
  { title: 'Norwegian', value: 'no' },
  { title: 'Danish', value: 'dk' },
  { title: 'Finnish', value: 'fi' },
  { title: 'Polish', value: 'pl' },
  { title: 'Czech', value: 'cz' },
  { title: 'Russian', value: 'ru' },
  { title: 'Turkish', value: 'tr' },
  { title: 'Japanese', value: 'jp' },
  { title: 'Bulgarian', value: 'bg' },
]

// A curated timezone list; the field also accepts a free-typed IANA name.
const TIMEZONE_OPTIONS: readonly string[] = [
  'Etc/UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Sao_Paulo',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Madrid',
  'Europe/Sofia',
  'Europe/Moscow',
  'Africa/Johannesburg',
  'Asia/Dubai',
  'Asia/Kolkata',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Australia/Sydney',
]

const STORAGE_OPTIONS: readonly {
  title: string
  description: string
  icon: string
  value: StorageMode
}[] = [
  {
    title: 'LVM',
    description: 'Flexible, resizable volumes on the whole disk. Recommended for servers.',
    icon: 'mdi-harddisk',
    value: 'lvm',
  },
  {
    title: 'Direct',
    description: 'Simple single partition using the entire disk. No volume manager.',
    icon: 'mdi-database',
    value: 'direct',
  },
  {
    title: 'Custom',
    description: 'Provide your own curtin storage configuration for full control.',
    icon: 'mdi-tune',
    value: 'custom',
  },
]

const NETWORK_OPTIONS: readonly {
  title: string
  description: string
  icon: string
  value: NetworkMode
}[] = [
  {
    title: 'Automatic (DHCP)',
    description: 'Get an address automatically on all interfaces. Best for most setups.',
    icon: 'mdi-lan',
    value: 'dhcp',
  },
  {
    title: 'Static IP',
    description: 'Assign a fixed address, gateway and DNS servers.',
    icon: 'mdi-ip-network',
    value: 'static',
  },
  {
    title: 'Advanced',
    description: 'Write raw netplan JSON for full control.',
    icon: 'mdi-code-json',
    value: 'advanced',
  },
]

const PACKAGE_SUGGESTIONS: readonly string[] = [
  'openssh-server',
  'curl',
  'vim',
  'htop',
  'git',
  'ca-certificates',
  'net-tools',
  'nginx',
  'docker.io',
  'ufw',
]

function emptyForm(): ProfileFormState {
  return {
    name: '',
    ubuntu_release: 'noble',
    keyboardLayout: 'us',
    timezone: '',
    storageMode: 'lvm',
    storageCustom: '',
    networkMode: 'dhcp',
    staticAddress: '',
    staticGateway: '',
    staticDns: [],
    networkConfig: '',
    packages: [],
    sshKeys: [''],
    lateCommands: [],
    kernelCmdlineExtra: '',
    userDataTemplate: '',
  }
}

// networkFromConfig detects DHCP / Static / Advanced from a stored netplan map
// so editing round-trips cleanly.
function networkFromConfig(config: Record<string, unknown>): Partial<ProfileFormState> {
  if (!config || Object.keys(config).length === 0) {
    return { networkMode: 'dhcp' }
  }
  const ethernets = (config as { ethernets?: Record<string, unknown> }).ethernets
  const entries = ethernets ? Object.values(ethernets) : []
  const first = entries[0] as
    | { addresses?: string[]; routes?: { to?: string; via?: string }[]; gateway4?: string; nameservers?: { addresses?: string[] } }
    | undefined
  if (entries.length === 1 && first?.addresses?.length) {
    const gateway =
      first.gateway4 ?? first.routes?.find((r) => r.to === 'default')?.via ?? ''
    return {
      networkMode: 'static',
      staticAddress: first.addresses[0] ?? '',
      staticGateway: gateway,
      staticDns: first.nameservers?.addresses ?? [],
    }
  }
  return { networkMode: 'advanced', networkConfig: JSON.stringify(config, null, 2) }
}

function fromProfile(profile: Profile): ProfileFormState {
  const base = emptyForm()
  return {
    ...base,
    name: profile.name,
    ubuntu_release: profile.ubuntu_release,
    keyboardLayout: profile.keyboard_layout || 'us',
    timezone: profile.timezone ?? '',
    storageMode: profile.storage_layout.mode,
    storageCustom: profile.storage_layout.custom ?? '',
    packages: [...profile.packages],
    sshKeys: profile.ssh_authorized_keys.length > 0 ? [...profile.ssh_authorized_keys] : [''],
    lateCommands: [...profile.late_commands],
    kernelCmdlineExtra: profile.kernel_cmdline_extra,
    userDataTemplate: profile.user_data_template ?? '',
    ...networkFromConfig(profile.network_config),
  }
}

const form = ref<ProfileFormState>(emptyForm())
const submitted = ref(false)
const advancedOpen = ref<number[]>([])

const title = computed(() => (props.mode === 'edit' ? 'Edit profile' : 'New profile'))

const SSH_KEY_RE = /^(ssh-(rsa|ed25519|dss)|ecdsa-sha2-\S+|sk-ssh-\S+)\s+\S+/
const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/
const IPV4_RE = /^(\d{1,3}\.){3}\d{1,3}$/

function sshKeyState(key: string): 'empty' | 'valid' | 'invalid' {
  const trimmed = key.trim()
  if (!trimmed) return 'empty'
  return SSH_KEY_RE.test(trimmed) ? 'valid' : 'invalid'
}

const localErrors = computed<Readonly<Record<string, string>>>(() => {
  const errors: Record<string, string> = {}
  if (!form.value.name.trim()) errors.name = 'Name is required'

  const keys = form.value.sshKeys.map((k) => k.trim()).filter(Boolean)
  if (keys.length === 0) errors.ssh_authorized_keys = 'At least one SSH authorized key is required'

  if (form.value.storageMode === 'custom' && !form.value.storageCustom.trim())
    errors.storage_layout = 'Custom storage layout requires a configuration body'

  if (form.value.kernelCmdlineExtra.includes('\n'))
    errors.kernel_cmdline_extra = 'Kernel cmdline must not contain newlines'

  if (form.value.networkMode === 'static') {
    if (!CIDR_RE.test(form.value.staticAddress.trim()))
      errors.network_config = 'Enter an IP address with prefix, e.g. 192.168.1.50/24'
    else if (form.value.staticGateway.trim() && !IPV4_RE.test(form.value.staticGateway.trim()))
      errors.network_config = 'Gateway must be a valid IPv4 address'
  } else if (form.value.networkMode === 'advanced') {
    const network = form.value.networkConfig.trim()
    if (network) {
      try {
        JSON.parse(network)
      } catch {
        errors.network_config = 'Network config must be valid JSON'
      }
    }
  }
  return errors
})

const isValid = computed(() => Object.keys(localErrors.value).length === 0)
const errorCount = computed(() => (submitted.value ? Object.keys(localErrors.value).length : 0))

function fieldErrors(field: string): readonly string[] {
  const messages: string[] = []
  if (submitted.value && localErrors.value[field]) messages.push(localErrors.value[field])
  const server = props.serverErrors?.[field]
  if (server) messages.push(server)
  return messages
}

function hasAdvancedContent(state: ProfileFormState): boolean {
  return Boolean(
    state.lateCommands.some((c) => c.trim()) ||
      state.kernelCmdlineExtra.trim() ||
      state.userDataTemplate.trim(),
  )
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    submitted.value = false
    form.value = props.initial ? fromProfile(props.initial) : emptyForm()
    advancedOpen.value = hasAdvancedContent(form.value) ? [0] : []
  },
  { immediate: true },
)

function addSshKey(): void {
  form.value.sshKeys = [...form.value.sshKeys, '']
}

function removeSshKey(index: number): void {
  form.value.sshKeys = form.value.sshKeys.filter((_, i) => i !== index)
}

function addLateCommand(): void {
  form.value.lateCommands = [...form.value.lateCommands, '']
}

function removeLateCommand(index: number): void {
  form.value.lateCommands = form.value.lateCommands.filter((_, i) => i !== index)
}

function addSuggestedPackage(pkg: string): void {
  if (!form.value.packages.includes(pkg)) form.value.packages = [...form.value.packages, pkg]
}

function serializeStorage(): string {
  if (form.value.storageMode === 'custom')
    return JSON.stringify({ mode: 'custom', custom: form.value.storageCustom.trim() })
  return JSON.stringify({ mode: form.value.storageMode })
}

// serializeNetwork turns the friendly network form into a netplan JSON string.
// Automatic → empty (installer defaults to DHCP); Static → a single ethernet
// with the address/gateway/DNS; Advanced → the raw JSON verbatim.
function serializeNetwork(): string {
  if (form.value.networkMode === 'dhcp') return ''
  if (form.value.networkMode === 'advanced') {
    const raw = form.value.networkConfig.trim()
    return raw ? JSON.stringify(JSON.parse(raw)) : ''
  }
  const eth: Record<string, unknown> = {
    match: { name: 'en*' },
    addresses: [form.value.staticAddress.trim()],
  }
  const gateway = form.value.staticGateway.trim()
  if (gateway) eth.routes = [{ to: 'default', via: gateway }]
  const dns = form.value.staticDns.map((d) => d.trim()).filter(Boolean)
  if (dns.length) eth.nameservers = { addresses: dns }
  return JSON.stringify({ version: 2, ethernets: { primary: eth } })
}

function close(): void {
  emit('update:modelValue', false)
}

function submit(): void {
  submitted.value = true
  if (!isValid.value) {
    if (localErrors.value.kernel_cmdline_extra) advancedOpen.value = [0]
    return
  }
  emit('save', {
    name: form.value.name.trim(),
    ubuntu_release: form.value.ubuntu_release,
    keyboard_layout: form.value.keyboardLayout,
    keyboard_variant: '',
    locale: '',
    timezone: form.value.timezone.trim(),
    storage_layout: serializeStorage(),
    network_config: serializeNetwork(),
    packages: form.value.packages.map((p) => p.trim()).filter(Boolean),
    ssh_authorized_keys: form.value.sshKeys.map((k) => k.trim()).filter(Boolean),
    user_data_template: form.value.userDataTemplate.trim() || null,
    late_commands: form.value.lateCommands.map((c) => c.trim()).filter(Boolean),
    kernel_cmdline_extra: form.value.kernelCmdlineExtra.trim(),
  })
}

defineExpose({ form, submit, localErrors })
</script>

<template>
  <v-dialog
    :model-value="modelValue"
    max-width="820"
    scrollable
    @update:model-value="emit('update:modelValue', $event)"
  >
    <v-card rounded="lg">
      <v-card-item class="pt-5 px-6 pb-2">
        <v-card-title class="d-flex align-center ga-2">
          <v-icon color="primary" icon="mdi-file-cog-outline" />
          {{ title }}
        </v-card-title>
        <v-card-subtitle class="text-wrap">
          A profile is a reusable recipe for an unattended Ubuntu install — pick a release,
          set the system basics, grant SSH access, and choose disk and network.
        </v-card-subtitle>
      </v-card-item>

      <v-divider />

      <v-card-text class="px-6 py-4" style="max-height: 68vh">
        <v-form @submit.prevent="submit">
          <!-- 1. Basics -->
          <div class="section-label">
            <v-icon icon="mdi-tag-outline" size="18" />
            <span>Basics</span>
          </div>
          <v-text-field
            v-model="form.name"
            class="mb-1"
            data-testid="field-name"
            :error-messages="fieldErrors('name')"
            hint="A short name to recognise this profile, e.g. “web-server” or “db-noble”."
            label="Profile name"
            persistent-hint
            placeholder="web-server"
            prepend-inner-icon="mdi-rename"
            variant="outlined"
          />

          <div class="mt-4 mb-2 text-body-2 text-medium-emphasis">Ubuntu release</div>
          <v-btn-toggle
            v-model="form.ubuntu_release"
            class="mb-2 release-toggle"
            color="primary"
            data-testid="field-release"
            divided
            mandatory
            variant="outlined"
          >
            <v-btn
              v-for="opt in RELEASE_OPTIONS"
              :key="opt.value"
              class="flex-grow-1 py-6"
              :value="opt.value"
            >
              <div class="d-flex flex-column align-start">
                <span class="text-body-1 font-weight-medium">{{ opt.title }}</span>
                <span class="text-caption text-medium-emphasis">{{ opt.subtitle }}</span>
              </div>
            </v-btn>
          </v-btn-toggle>

          <v-alert
            class="mt-3"
            density="compact"
            icon="mdi-server"
            variant="tonal"
          >
            <span class="text-body-2">
              Each machine's <strong>hostname</strong> is its name — you set that when you
              register the machine, so one profile can serve many hosts.
            </span>
          </v-alert>

          <!-- 2. System -->
          <div class="section-label mt-6">
            <v-icon icon="mdi-cog-outline" size="18" />
            <span>System settings</span>
          </div>
          <div class="d-flex ga-3 flex-wrap">
            <v-select
              v-model="form.keyboardLayout"
              class="flex-grow-1"
              data-testid="field-keyboard"
              hint="Keyboard layout for the console."
              item-title="title"
              item-value="value"
              :items="KEYBOARD_OPTIONS"
              label="Keyboard layout"
              persistent-hint
              prepend-inner-icon="mdi-keyboard-outline"
              style="min-width: 220px"
              variant="outlined"
            />
            <v-combobox
              v-model="form.timezone"
              class="flex-grow-1"
              data-testid="field-timezone"
              hint="Leave blank to keep UTC."
              :items="TIMEZONE_OPTIONS"
              label="Time zone"
              persistent-hint
              prepend-inner-icon="mdi-clock-outline"
              style="min-width: 220px"
              variant="outlined"
            />
          </div>

          <!-- 3. Access -->
          <div class="section-label mt-6">
            <v-icon icon="mdi-key-chain" size="18" />
            <span>SSH access</span>
          </div>
          <v-alert
            class="mb-3"
            density="compact"
            icon="mdi-shield-key-outline"
            text="Installed servers accept key-only login — no passwords. Paste at least one public key."
            type="info"
            variant="tonal"
          />
          <div
            v-for="(_, index) in form.sshKeys"
            :key="`ssh-${index}`"
            class="d-flex align-start ga-2 mb-2"
          >
            <v-textarea
              v-model="form.sshKeys[index]"
              auto-grow
              class="text-mono"
              :data-testid="`field-ssh-${index}`"
              density="compact"
              :error-messages="index === 0 ? fieldErrors('ssh_authorized_keys') : []"
              hide-details="auto"
              placeholder="ssh-ed25519 AAAAC3Nza... user@laptop"
              rows="1"
              variant="outlined"
            >
              <template #prepend-inner>
                <v-icon
                  v-if="sshKeyState(form.sshKeys[index]) === 'valid'"
                  color="success"
                  icon="mdi-check-circle"
                  size="18"
                  title="Looks like a valid key"
                />
                <v-icon
                  v-else-if="sshKeyState(form.sshKeys[index]) === 'invalid'"
                  color="warning"
                  icon="mdi-alert-circle-outline"
                  size="18"
                  title="This doesn't look like an SSH public key"
                />
                <v-icon v-else color="disabled" icon="mdi-key-outline" size="18" />
              </template>
            </v-textarea>
            <v-btn
              :disabled="form.sshKeys.length <= 1"
              icon="mdi-close"
              size="small"
              title="Remove key"
              variant="text"
              @click="removeSshKey(index)"
            />
          </div>
          <v-btn prepend-icon="mdi-plus" size="small" variant="tonal" @click="addSshKey">
            Add another key
          </v-btn>

          <!-- 4. Storage -->
          <div class="section-label mt-6">
            <v-icon icon="mdi-harddisk" size="18" />
            <span>Disk layout</span>
          </div>
          <v-radio-group
            v-model="form.storageMode"
            class="choice-group"
            data-testid="field-storage-mode"
            hide-details
          >
            <v-sheet
              v-for="opt in STORAGE_OPTIONS"
              :key="opt.value"
              border
              class="choice-card mb-2 pa-3"
              :class="{ 'choice-card--active': form.storageMode === opt.value }"
              rounded="lg"
              @click="form.storageMode = opt.value"
            >
              <div class="d-flex align-center ga-3">
                <v-radio :value="opt.value" />
                <v-icon :icon="opt.icon" />
                <div>
                  <div class="text-body-1 font-weight-medium">{{ opt.title }}</div>
                  <div class="text-caption text-medium-emphasis">{{ opt.description }}</div>
                </div>
              </div>
            </v-sheet>
          </v-radio-group>
          <v-textarea
            v-if="form.storageMode === 'custom'"
            v-model="form.storageCustom"
            class="mt-2 text-mono"
            data-testid="field-storage-custom"
            :error-messages="fieldErrors('storage_layout')"
            hint="curtin storage config (YAML or JSON) served verbatim to the installer."
            label="Custom storage configuration"
            persistent-hint
            rows="5"
            variant="outlined"
          />

          <!-- 5. Network -->
          <div class="section-label mt-6">
            <v-icon icon="mdi-ip-network-outline" size="18" />
            <span>Network</span>
          </div>
          <v-radio-group
            v-model="form.networkMode"
            class="choice-group"
            data-testid="field-network-mode"
            hide-details
          >
            <v-sheet
              v-for="opt in NETWORK_OPTIONS"
              :key="opt.value"
              border
              class="choice-card mb-2 pa-3"
              :class="{ 'choice-card--active': form.networkMode === opt.value }"
              rounded="lg"
              @click="form.networkMode = opt.value"
            >
              <div class="d-flex align-center ga-3">
                <v-radio :value="opt.value" />
                <v-icon :icon="opt.icon" />
                <div>
                  <div class="text-body-1 font-weight-medium">{{ opt.title }}</div>
                  <div class="text-caption text-medium-emphasis">{{ opt.description }}</div>
                </div>
              </div>
            </v-sheet>
          </v-radio-group>

          <div v-if="form.networkMode === 'static'" class="mt-2">
            <v-text-field
              v-model="form.staticAddress"
              class="mb-2 text-mono"
              data-testid="field-static-address"
              :error-messages="fieldErrors('network_config')"
              hint="IP address with prefix length."
              label="IP address / CIDR"
              persistent-hint
              placeholder="192.168.1.50/24"
              prepend-inner-icon="mdi-ip"
              variant="outlined"
            />
            <v-text-field
              v-model="form.staticGateway"
              class="mb-2 text-mono"
              data-testid="field-static-gateway"
              label="Gateway (optional)"
              placeholder="192.168.1.1"
              prepend-inner-icon="mdi-router-network"
              variant="outlined"
            />
            <v-combobox
              v-model="form.staticDns"
              chips
              closable-chips
              data-testid="field-static-dns"
              hint="DNS servers. Press Enter to add each one."
              label="DNS servers (optional)"
              multiple
              persistent-hint
              prepend-inner-icon="mdi-dns"
              variant="outlined"
            />
          </div>
          <v-textarea
            v-else-if="form.networkMode === 'advanced'"
            v-model="form.networkConfig"
            class="mt-2 text-mono"
            data-testid="field-network"
            :error-messages="fieldErrors('network_config')"
            hint="Netplan-shaped JSON served under autoinstall.network."
            label="Netplan configuration (JSON)"
            persistent-hint
            placeholder='{ "version": 2, "ethernets": { "any": { "match": {}, "dhcp4": true } } }'
            rows="4"
            variant="outlined"
          />

          <!-- 6. Software -->
          <div class="section-label mt-6">
            <v-icon icon="mdi-package-variant-closed" size="18" />
            <span>Packages</span>
          </div>
          <v-combobox
            v-model="form.packages"
            chips
            closable-chips
            data-testid="field-packages"
            :error-messages="fieldErrors('packages')"
            hint="apt packages to install. Press Enter to add a custom one."
            label="Packages to install"
            multiple
            persistent-hint
            prepend-inner-icon="mdi-magnify"
            variant="outlined"
          />
          <div class="mt-2 d-flex flex-wrap ga-1 align-center">
            <span class="text-caption text-medium-emphasis mr-1">Suggestions:</span>
            <v-chip
              v-for="pkg in PACKAGE_SUGGESTIONS"
              :key="pkg"
              :disabled="form.packages.includes(pkg)"
              size="small"
              variant="outlined"
              @click="addSuggestedPackage(pkg)"
            >
              <v-icon icon="mdi-plus" size="14" start />
              {{ pkg }}
            </v-chip>
          </div>

          <!-- 7. Advanced (collapsed) -->
          <v-expansion-panels v-model="advancedOpen" class="mt-6" multiple variant="accordion">
            <v-expansion-panel elevation="0">
              <v-expansion-panel-title>
                <v-icon class="mr-2" icon="mdi-cog-outline" size="18" />
                Advanced options
                <span class="text-caption text-medium-emphasis ml-2">
                  late commands, kernel cmdline, raw template
                </span>
              </v-expansion-panel-title>
              <v-expansion-panel-text>
                <div class="mb-1 text-body-2 text-medium-emphasis">
                  Late commands — run in the installed system at the end of install
                </div>
                <div
                  v-for="(_, index) in form.lateCommands"
                  :key="`late-${index}`"
                  class="d-flex align-start ga-2 mb-2"
                >
                  <v-text-field
                    v-model="form.lateCommands[index]"
                    class="text-mono"
                    density="compact"
                    hide-details="auto"
                    placeholder="systemctl enable my-service"
                    variant="outlined"
                  />
                  <v-btn
                    icon="mdi-close"
                    size="small"
                    title="Remove command"
                    variant="text"
                    @click="removeLateCommand(index)"
                  />
                </div>
                <v-btn
                  class="mb-4"
                  prepend-icon="mdi-plus"
                  size="small"
                  variant="tonal"
                  @click="addLateCommand"
                >
                  Add command
                </v-btn>

                <v-text-field
                  v-model="form.kernelCmdlineExtra"
                  class="mb-3 text-mono"
                  data-testid="field-cmdline"
                  :error-messages="fieldErrors('kernel_cmdline_extra')"
                  hint="Extra kernel parameters appended at boot. Single line."
                  label="Kernel cmdline extra (optional)"
                  persistent-hint
                  placeholder="console=ttyS0"
                  variant="outlined"
                />
                <v-textarea
                  v-model="form.userDataTemplate"
                  class="text-mono"
                  data-testid="field-user-data"
                  :error-messages="fieldErrors('user_data_template')"
                  hint="Overrides the generated autoinstall document entirely. Leave empty unless you know you need it."
                  label="User-data template (optional autoinstall override)"
                  persistent-hint
                  rows="3"
                  variant="outlined"
                />
              </v-expansion-panel-text>
            </v-expansion-panel>
          </v-expansion-panels>
        </v-form>
      </v-card-text>

      <v-divider />

      <v-card-actions class="px-6 py-3">
        <v-fade-transition>
          <div v-if="errorCount > 0" class="text-caption text-error d-flex align-center ga-1">
            <v-icon icon="mdi-alert-circle-outline" size="16" />
            {{ errorCount }} field{{ errorCount > 1 ? 's' : '' }} need attention
          </div>
        </v-fade-transition>
        <v-spacer />
        <v-btn :disabled="saving" variant="text" @click="close">Cancel</v-btn>
        <v-btn
          color="primary"
          data-testid="save-btn"
          :loading="saving"
          prepend-icon="mdi-content-save-outline"
          variant="flat"
          @click="submit"
        >
          {{ mode === 'edit' ? 'Save changes' : 'Create profile' }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.section-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.95rem;
  font-weight: 600;
  margin-bottom: 12px;
  color: rgb(var(--v-theme-primary));
}

.release-toggle {
  width: 100%;
  height: auto;
}

.choice-card {
  cursor: pointer;
  transition:
    border-color 0.15s ease,
    background-color 0.15s ease;
}

.choice-card--active {
  border-color: rgb(var(--v-theme-primary)) !important;
  background-color: rgba(var(--v-theme-primary), 0.06);
}

.text-mono :deep(input),
.text-mono :deep(textarea) {
  font-family: 'Roboto Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
}
</style>
