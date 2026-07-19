import { describe, expect, it } from 'vitest'

import {
  defaultNic,
  emptyNic,
  parseNetworkConfig,
  serializeNics,
  validateNics,
} from '../../src/utils/netplan'

describe('utils/netplan', () => {
  it('parses an empty config as DHCP', () => {
    expect(parseNetworkConfig({})).toEqual({ mode: 'dhcp' })
  })

  it('parses the legacy single-NIC shape into one static NIC', () => {
    const parsed = parseNetworkConfig({
      version: 2,
      ethernets: {
        primary: {
          match: { name: 'en*' },
          addresses: ['192.168.1.50/24'],
          routes: [{ to: 'default', via: '192.168.1.1' }],
          nameservers: { addresses: ['1.1.1.1'] },
        },
      },
    })
    expect(parsed).toEqual({
      mode: 'static',
      nics: [
        {
          name: 'en*',
          mac: '',
          dhcp: false,
          address: '192.168.1.50/24',
          gateway: '192.168.1.1',
          dns: ['1.1.1.1'],
        },
      ],
    })
  })

  it('parses gateway4 as the gateway', () => {
    const parsed = parseNetworkConfig({
      version: 2,
      ethernets: { eno1: { addresses: ['10.0.0.5/24'], gateway4: '10.0.0.1' } },
    })
    expect(parsed.mode).toBe('static')
    if (parsed.mode === 'static') expect(parsed.nics[0].gateway).toBe('10.0.0.1')
  })

  it('falls back to advanced for shapes the form cannot represent', () => {
    const configs: Record<string, unknown>[] = [
      { version: 2, bonds: {} },
      { version: 2, ethernets: { eno1: { addresses: ['10.0.0.5/24', '10.0.0.6/24'] } } },
      { version: 2, ethernets: { eno1: { dhcp4: true, nameservers: { addresses: ['1.1.1.1'] } } } },
      { version: 2, ethernets: { eno1: { addresses: ['10.0.0.5/24'], mtu: 9000 } } },
      { version: 2, ethernets: {} },
    ]
    for (const config of configs) {
      expect(parseNetworkConfig(config).mode).toBe('advanced')
    }
  })

  it('serialize/parse round-trips multiple NICs', () => {
    const nics = [
      { ...emptyNic(), name: 'eno1', address: '10.0.0.5/24', gateway: '10.0.0.1', dns: ['1.1.1.1'] },
      { ...emptyNic(), name: 'en*', mac: 'aa:bb:cc:dd:ee:ff', dhcp: true },
    ]
    const parsed = parseNetworkConfig(JSON.parse(serializeNics(nics)))
    expect(parsed).toEqual({ mode: 'static', nics })
  })

  it('validates required fields per NIC', () => {
    expect(validateNics([]).network_config).toContain('at least one')
    const errors = validateNics([
      { ...emptyNic() },
      { ...defaultNic(), address: '10.0.0.5/24', gateway: 'bad' },
    ])
    expect(errors.nic_0_name).toContain('name')
    expect(errors.nic_0_address).toContain('prefix')
    expect(errors.nic_1_gateway).toContain('IPv4')
  })

  it('accepts a valid multi-NIC form', () => {
    expect(
      validateNics([
        { ...emptyNic(), name: 'eno1', address: '10.0.0.5/24' },
        { ...emptyNic(), name: 'eno2', dhcp: true },
      ]),
    ).toEqual({})
  })
})
