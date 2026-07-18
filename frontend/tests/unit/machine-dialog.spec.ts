import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

import MachineDialog from '../../src/components/MachineDialog.vue'
import type { MachineFormValues } from '../../src/components/MachineDialog.vue'

vi.mock('../../src/api/profiles', () => ({
  listProfiles: vi.fn().mockResolvedValue([]),
}))

/** Shape exposed by the component via defineExpose for tests. */
interface DialogVm {
  form: {
    mac: string
    name: string
    firmware: string
    profile_id: string | null
    reservation_ip: string
    notes: string
  }
  localErrors: Readonly<Record<string, string>>
  submit: () => void
}

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

function mountDialog(props: Partial<InstanceType<typeof MachineDialog>['$props']> = {}) {
  const vuetify = createVuetify({ components, directives })
  return mount(MachineDialog, {
    props: {
      modelValue: true,
      mode: 'create' as const,
      ...props,
    },
    global: { plugins: [vuetify] },
  })
}

describe('components/MachineDialog', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    // Vuetify's VOverlay location strategy reads window.visualViewport.
    vi.stubGlobal('visualViewport', new EventTarget())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('blocks submit and reports an error for an invalid MAC', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.mac = 'not-a-mac'
    vm.form.name = 'node-01'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.mac).toContain('MAC')
  })

  it('blocks submit when required fields are empty', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.mac).toBe('MAC address is required')
    expect(vm.localErrors.name).toBe('Name is required')
  })

  it('blocks submit for an invalid hostname or reservation IP', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.mac = 'aa:bb:cc:dd:ee:ff'
    vm.form.name = 'bad name!'
    vm.form.reservation_ip = '999.1.1.1'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.name).toContain('hostname')
    expect(vm.localErrors.reservation_ip).toContain('IPv4')
  })

  it('emits save with normalized values for valid input', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.mac = 'AA:BB:CC:DD:EE:FF'
    vm.form.name = ' node-01 '
    vm.form.reservation_ip = '10.0.0.50'
    vm.form.notes = 'rack 3'
    vm.submit()
    await wrapper.vm.$nextTick()

    const saved = wrapper.emitted('save')
    expect(saved).toHaveLength(1)
    const values = saved?.[0]?.[0] as MachineFormValues
    expect(values.mac).toBe('aa:bb:cc:dd:ee:ff')
    expect(values.name).toBe('node-01')
    expect(values.firmware).toBe('uefi_x64')
    expect(values.reservation_ip).toBe('10.0.0.50')
    expect(values.notes).toBe('rack 3')
  })

  it('requires a profile in register-unknown mode', async () => {
    const wrapper = mountDialog({ mode: 'register-unknown', initial: { mac: '11:22:33:44:55:66' } })
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.name = 'node-02'
    vm.submit()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.profile_id).toContain('Profile is required')

    vm.form.profile_id = 'p-1'
    vm.submit()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('save')).toHaveLength(1)
  })
})
