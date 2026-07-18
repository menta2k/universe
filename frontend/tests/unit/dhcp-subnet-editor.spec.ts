import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

import type { DhcpConfigInput } from '../../src/api/dhcp'
import type { DhcpSubnet } from '../../src/api/types'
import DhcpSubnetEditor from '../../src/components/DhcpSubnetEditor.vue'

interface EditorVm {
  rows: Array<{
    network: string
    range_start: string
    range_end: string
    gateway: string
    dns: string[]
  }>
  leaseTtl: number | string
  localErrors: Readonly<Record<string, string>>
  submit: () => void
  addRow: () => void
  removeRow: (index: number) => void
}

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

const validSubnet: DhcpSubnet = {
  id: 's-1',
  network: '10.0.0.0/24',
  range_start: '10.0.0.100',
  range_end: '10.0.0.200',
  gateway: '10.0.0.1',
  dns: ['10.0.0.1'],
  next_server: '',
}

function mountEditor(props: Partial<InstanceType<typeof DhcpSubnetEditor>['$props']> = {}) {
  const vuetify = createVuetify({ components, directives })
  return mount(DhcpSubnetEditor, {
    props: { subnets: [validSubnet], leaseTtlSeconds: 3600, ...props },
    global: { plugins: [vuetify] },
  })
}

describe('components/DhcpSubnetEditor', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('visualViewport', new EventTarget())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('adds and removes subnet rows', async () => {
    const wrapper = mountEditor({ subnets: [] })
    const vm = wrapper.vm as unknown as EditorVm

    expect(vm.rows).toHaveLength(0)
    vm.addRow()
    vm.addRow()
    await wrapper.vm.$nextTick()
    expect(vm.rows).toHaveLength(2)

    vm.removeRow(0)
    await wrapper.vm.$nextTick()
    expect(vm.rows).toHaveLength(1)
  })

  it('blocks submit for an invalid CIDR', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.rows[0].network = '10.0.0.0'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors['0.network']).toContain('CIDR')
  })

  it('blocks submit when range end precedes range start', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.rows[0].range_start = '10.0.0.200'
    vm.rows[0].range_end = '10.0.0.100'
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors['0.range_end']).toContain('>= start')
  })

  it('blocks submit for a non-positive lease TTL', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.leaseTtl = 0
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.lease_ttl_seconds).toContain('positive')
  })

  it('emits a serialized DhcpConfigInput payload for valid input', async () => {
    const wrapper = mountEditor()
    const vm = wrapper.vm as unknown as EditorVm

    vm.leaseTtl = 7200
    vm.rows[0].dns = ['10.0.0.1', ' ']
    vm.submit()
    await wrapper.vm.$nextTick()

    const saved = wrapper.emitted('save')
    expect(saved).toHaveLength(1)
    const values = saved?.[0]?.[0] as DhcpConfigInput
    expect(values.lease_ttl_seconds).toBe(7200)
    expect(values.subnets).toEqual([
      {
        network: '10.0.0.0/24',
        range_start: '10.0.0.100',
        range_end: '10.0.0.200',
        gateway: '10.0.0.1',
        dns: ['10.0.0.1'],
      },
    ])
  })

  it('renders server-side 422 field errors mapped to the right row', async () => {
    const wrapper = mountEditor({
      serverErrors: { 'subnets[0].range': 'range overlaps subnet 1' },
    })
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('range overlaps subnet 1')
  })
})
