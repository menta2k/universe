import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import * as components from 'vuetify/components'
import * as directives from 'vuetify/directives'

import type { ProvisioningEvent, ProvisioningSession } from '../../src/api/types'
import SessionTimeline from '../../src/components/SessionTimeline.vue'

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

class MockEventSource {
  constructor() {}
  addEventListener(): void {}
  removeEventListener(): void {}
  close(): void {}
}

function baseSession(overrides: Partial<ProvisioningSession> = {}): ProvisioningSession {
  return {
    id: 's-1',
    machine_id: 'm-1',
    machine_name: 'node-01',
    machine_mac: 'aa:bb:cc:dd:ee:ff',
    profile_id: 'p-1',
    profile_version: 1,
    state: 'completed',
    started_at: '2026-07-18T10:00:00Z',
    ended_at: '2026-07-18T10:05:00Z',
    failure_phase: null,
    ...overrides,
  }
}

function event(time: string, phase: string): ProvisioningEvent {
  return {
    time,
    session_id: 's-1',
    machine_mac: 'aa:bb:cc:dd:ee:ff',
    phase: phase as ProvisioningEvent['phase'],
    outcome: 'ok',
    detail: {},
  }
}

function mountTimeline(props: {
  session: ProvisioningSession | null
  timeline: readonly ProvisioningEvent[]
  evidence: Record<string, unknown>
}) {
  const vuetify = createVuetify({ components, directives })
  return mount(SessionTimeline, { props, global: { plugins: [vuetify] } })
}

describe('components/SessionTimeline', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
    vi.stubGlobal('EventSource', MockEventSource)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders phase events ordered oldest-first regardless of input order', () => {
    const wrapper = mountTimeline({
      session: baseSession(),
      timeline: [
        event('2026-07-18T10:00:03Z', 'tftp_transfer'),
        event('2026-07-18T10:00:01Z', 'dhcp_discover'),
        event('2026-07-18T10:00:02Z', 'dhcp_ack'),
      ],
      evidence: {},
    })

    const text = wrapper.text()
    const first = text.indexOf('dhcp discover')
    const second = text.indexOf('dhcp ack')
    const third = text.indexOf('tftp transfer')
    expect(first).toBeGreaterThanOrEqual(0)
    expect(first).toBeLessThan(second)
    expect(second).toBeLessThan(third)
  })

  it('shows the evidence viewer for a failed session', () => {
    const wrapper = mountTimeline({
      session: baseSession({ state: 'failed', failure_phase: 'install_report' }),
      timeline: [event('2026-07-18T10:00:01Z', 'install_report')],
      evidence: { last_error: 'disk full' },
    })

    expect(wrapper.text()).toContain('Failure evidence')
    expect(wrapper.text()).toContain('disk full')
  })

  it('hides the evidence viewer for a completed session', () => {
    const wrapper = mountTimeline({
      session: baseSession({ state: 'completed' }),
      timeline: [event('2026-07-18T10:00:01Z', 'session_completed')],
      evidence: { note: 'ok' },
    })

    expect(wrapper.text()).not.toContain('Failure evidence')
  })
})
