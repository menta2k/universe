import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

import type { ProfileInput } from '../../src/api/profiles'
import ProfileEditor from '../../src/components/ProfileEditor.vue'

interface EditorVm {
  form: {
    name: string
    ubuntu_release: string
    keyboardLayout: string
    timezone: string
    storageMode: string
    storageCustom: string
    networkMode: string
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
  localErrors: Readonly<Record<string, string>>
  submit: () => void
}

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

function mountEditor(props: Partial<InstanceType<typeof ProfileEditor>['$props']> = {}) {
  const vuetify = createVuetify({ components, directives })
  return mount(ProfileEditor, {
    props: {
      modelValue: true,
      mode: 'create' as const,
      ...props,
    },
    global: { plugins: [vuetify] },
  })
}

describe('components/ProfileEditor', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('visualViewport', new EventTarget())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('blocks submit when no SSH key is provided', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['']
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.ssh_authorized_keys).toContain('SSH')
  })

  it('blocks submit when custom storage has no body', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.storageMode = 'custom'
    vm.form.storageCustom = '   '
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.storage_layout).toContain('Custom storage')
  })

  it('blocks submit when the kernel cmdline contains a newline', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.kernelCmdlineExtra = 'console=ttyS0\nquiet'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.kernel_cmdline_extra).toContain('newline')
  })

  it('blocks submit when the network config is not valid JSON', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.networkMode = 'advanced'
    vm.form.networkConfig = '{not json}'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.network_config).toContain('JSON')
  })

  it('emits save with serialized JSON strings for valid input', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = '  ubuntu-server  '
    vm.form.ubuntu_release = 'jammy'
    vm.form.keyboardLayout = 'gb'
    vm.form.timezone = 'Europe/London'
    vm.form.storageMode = 'lvm'
    vm.form.networkMode = 'advanced'
    vm.form.networkConfig = '{ "version": 2 }'
    vm.form.packages = ['vim', ' ']
    vm.form.sshKeys = ['ssh-ed25519 AAAA', '']
    vm.form.lateCommands = ['echo hi', '']
    vm.form.kernelCmdlineExtra = 'console=ttyS0'
    vm.submit()
    await wrapper.vm.$nextTick()

    const saved = wrapper.emitted('save')
    expect(saved).toHaveLength(1)
    const values = saved?.[0]?.[0] as ProfileInput
    expect(values.name).toBe('ubuntu-server')
    expect(values.ubuntu_release).toBe('jammy')
    expect(values.keyboard_layout).toBe('gb')
    expect(values.timezone).toBe('Europe/London')
    expect(values.storage_layout).toBe('{"mode":"lvm"}')
    expect(values.network_config).toBe('{"version":2}')
    expect(values.packages).toEqual(['vim'])
    expect(values.ssh_authorized_keys).toEqual(['ssh-ed25519 AAAA'])
    expect(values.late_commands).toEqual(['echo hi'])
    expect(values.user_data_template).toBeNull()
  })

  it('serializes a static network into netplan', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.networkMode = 'static'
    vm.form.staticAddress = '192.168.1.50/24'
    vm.form.staticGateway = '192.168.1.1'
    vm.form.staticDns = ['1.1.1.1']
    vm.submit()
    await wrapper.vm.$nextTick()

    const values = wrapper.emitted('save')?.[0]?.[0] as ProfileInput
    const netplan = JSON.parse(values.network_config)
    expect(netplan.ethernets.primary.addresses).toEqual(['192.168.1.50/24'])
    expect(netplan.ethernets.primary.routes).toEqual([{ to: 'default', via: '192.168.1.1' }])
    expect(netplan.ethernets.primary.nameservers).toEqual({ addresses: ['1.1.1.1'] })
  })

  it('blocks a static network with an invalid address', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.networkMode = 'static'
    vm.form.staticAddress = 'not-an-ip'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.network_config).toContain('192.168')
  })

  it('serializes custom storage with its body', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.form.name = 'ubuntu-server'
    vm.form.sshKeys = ['ssh-ed25519 AAAA']
    vm.form.storageMode = 'custom'
    vm.form.storageCustom = 'config: {}'
    vm.submit()
    await wrapper.vm.$nextTick()

    const values = wrapper.emitted('save')?.[0]?.[0] as ProfileInput
    expect(values.storage_layout).toBe('{"mode":"custom","custom":"config: {}"}')
  })
})
