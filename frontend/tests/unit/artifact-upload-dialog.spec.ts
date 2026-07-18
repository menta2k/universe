import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

import ArtifactUploadDialog from '../../src/components/ArtifactUploadDialog.vue'
import type { ArtifactFormValues } from '../../src/components/ArtifactUploadDialog.vue'

/** Shape exposed by the component via defineExpose for tests. */
interface DialogVm {
  form: {
    kind: string
    ubuntu_release: string
    file: File | null
  }
  localErrors: Readonly<Record<string, string>>
  submit: () => void
}

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

function mountDialog(props: Partial<InstanceType<typeof ArtifactUploadDialog>['$props']> = {}) {
  const vuetify = createVuetify({ components, directives })
  return mount(ArtifactUploadDialog, {
    props: {
      modelValue: true,
      mode: 'upload' as const,
      ...props,
    },
    global: { plugins: [vuetify] },
  })
}

function sampleFile(): File {
  return new File([new Uint8Array([1, 2, 3])], 'vmlinuz-noble', {
    type: 'application/octet-stream',
  })
}

describe('components/ArtifactUploadDialog', () => {
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('visualViewport', new EventTarget())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('requires an Ubuntu release for kernel artifacts', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.kind = 'kernel'
    vm.form.ubuntu_release = ''
    vm.form.file = sampleFile()
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.ubuntu_release).toContain('required')
  })

  it('does not require a release for kind "other"', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.kind = 'other'
    vm.form.file = sampleFile()
    await wrapper.vm.$nextTick()
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(vm.localErrors.ubuntu_release).toBeUndefined()
    expect(wrapper.emitted('save')).toHaveLength(1)
  })

  it('blocks submit when no file is selected', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm

    vm.form.kind = 'other'
    vm.form.file = null
    vm.submit()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('save')).toBeUndefined()
    expect(vm.localErrors.file).toBe('A file is required')
  })

  it('emits save with kind, release and file for valid input', async () => {
    const wrapper = mountDialog()
    const vm = wrapper.vm as unknown as DialogVm
    const file = sampleFile()

    vm.form.kind = 'kernel'
    vm.form.ubuntu_release = 'noble'
    vm.form.file = file
    vm.submit()
    await wrapper.vm.$nextTick()

    const saved = wrapper.emitted('save')
    expect(saved).toHaveLength(1)
    const values = saved?.[0]?.[0] as ArtifactFormValues
    expect(values.kind).toBe('kernel')
    expect(values.ubuntu_release).toBe('noble')
    expect(values.file).toBe(file)
  })
})
