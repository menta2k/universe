/**
 * Netplan helpers for the profile editor's network section.
 *
 * The editor's "Static IP" mode is a list of per-interface (NIC) forms.
 * These helpers convert between that form model and the netplan-shaped
 * JSON stored in profile.network_config, and validate the form fields.
 * Configs that don't fit the form model (bonds, vlans, multiple addresses
 * per NIC, …) round-trip through the Advanced raw-JSON mode instead.
 */

export interface NicForm {
  /** Interface name — exact (eno1) or glob pattern (en*). */
  name: string
  /** Optional MAC address to match the interface by. */
  mac: string
  dhcp: boolean
  /** IPv4 address with prefix length, required when dhcp is false. */
  address: string
  gateway: string
  dns: string[]
}

export type ParsedNetwork =
  | { mode: 'dhcp' }
  | { mode: 'static'; nics: NicForm[] }
  | { mode: 'advanced'; raw: string }

export const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/
export const IPV4_RE = /^(\d{1,3}\.){3}\d{1,3}$/
export const MAC_RE = /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/i

const GLOB_RE = /[*?[\]]/

export function emptyNic(): NicForm {
  return { name: '', mac: '', dhcp: false, address: '', gateway: '', dns: [] }
}

/** The default first NIC mirrors the pre-multi-NIC behaviour: match any ethernet. */
export function defaultNic(): NicForm {
  return { ...emptyNic(), name: 'en*' }
}

interface NetplanRoute {
  to?: string
  via?: string
}

interface NetplanEthernet {
  match?: { name?: string; macaddress?: string }
  dhcp4?: boolean
  addresses?: string[]
  routes?: NetplanRoute[]
  gateway4?: string
  nameservers?: { addresses?: string[] }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function keysSubset(obj: Record<string, unknown>, allowed: readonly string[]): boolean {
  return Object.keys(obj).every((k) => allowed.includes(k))
}

// fitsNicForm reports whether one netplan ethernet entry can be represented by
// the per-NIC form without losing information.
function fitsNicForm(entry: unknown): entry is NetplanEthernet {
  if (!isPlainObject(entry)) return false
  if (!keysSubset(entry, ['match', 'dhcp4', 'addresses', 'routes', 'gateway4', 'nameservers']))
    return false

  const { match, dhcp4, addresses, routes, gateway4, nameservers } = entry
  if (match !== undefined && (!isPlainObject(match) || !keysSubset(match, ['name', 'macaddress'])))
    return false

  if (dhcp4 === true) {
    // A DHCP NIC in the form carries no other settings.
    return (
      addresses === undefined &&
      routes === undefined &&
      gateway4 === undefined &&
      nameservers === undefined
    )
  }
  if (dhcp4 !== undefined) return false

  if (!Array.isArray(addresses) || addresses.length !== 1 || typeof addresses[0] !== 'string')
    return false
  if (routes !== undefined) {
    if (!Array.isArray(routes) || routes.length !== 1) return false
    const route = routes[0]
    if (!isPlainObject(route) || !keysSubset(route, ['to', 'via'])) return false
    if (route.to !== 'default' || typeof route.via !== 'string') return false
  }
  if (routes !== undefined && gateway4 !== undefined) return false
  if (gateway4 !== undefined && typeof gateway4 !== 'string') return false
  if (nameservers !== undefined) {
    if (!isPlainObject(nameservers) || !keysSubset(nameservers, ['addresses'])) return false
    if (!Array.isArray(nameservers.addresses)) return false
  }
  return true
}

function nicFromEthernet(key: string, entry: NetplanEthernet): NicForm {
  const gateway =
    entry.gateway4 ?? entry.routes?.find((r) => r.to === 'default')?.via ?? ''
  return {
    name: entry.match?.name ?? (entry.match ? '' : key),
    mac: entry.match?.macaddress ?? '',
    dhcp: entry.dhcp4 === true,
    address: entry.addresses?.[0] ?? '',
    gateway,
    dns: entry.nameservers?.addresses ?? [],
  }
}

// parseNetworkConfig detects DHCP / Static (per-NIC) / Advanced from a stored
// netplan map so editing round-trips cleanly.
export function parseNetworkConfig(config: Record<string, unknown>): ParsedNetwork {
  if (!config || Object.keys(config).length === 0) return { mode: 'dhcp' }

  const advanced: ParsedNetwork = { mode: 'advanced', raw: JSON.stringify(config, null, 2) }
  if (!keysSubset(config, ['version', 'ethernets'])) return advanced

  const ethernets = config.ethernets
  if (!isPlainObject(ethernets) || Object.keys(ethernets).length === 0) return advanced

  const entries = Object.entries(ethernets)
  if (!entries.every(([, entry]) => fitsNicForm(entry))) return advanced

  return {
    mode: 'static',
    nics: entries.map(([key, entry]) => nicFromEthernet(key, entry as NetplanEthernet)),
  }
}

// serializeNics turns the per-NIC forms into a netplan JSON string. Exact
// interface names become the netplan key; glob patterns and MAC addresses go
// into a match block under a generated key.
export function serializeNics(nics: readonly NicForm[]): string {
  const ethernets: Record<string, unknown> = {}
  nics.forEach((nic, index) => {
    const name = nic.name.trim()
    const mac = nic.mac.trim().toLowerCase()
    const entry: Record<string, unknown> = {}

    const match: Record<string, string> = {}
    if (mac) match.macaddress = mac
    if (name && (mac || GLOB_RE.test(name))) match.name = name
    if (Object.keys(match).length > 0) entry.match = match
    const key = Object.keys(match).length === 0 ? name : `nic${index}`

    if (nic.dhcp) {
      entry.dhcp4 = true
    } else {
      entry.addresses = [nic.address.trim()]
      const gateway = nic.gateway.trim()
      if (gateway) entry.routes = [{ to: 'default', via: gateway }]
      const dns = nic.dns.map((d) => d.trim()).filter(Boolean)
      if (dns.length > 0) entry.nameservers = { addresses: dns }
    }
    ethernets[key] = entry
  })
  return JSON.stringify({ version: 2, ethernets })
}

// validateNics returns field errors keyed `nic_<index>_<field>`, plus
// `network_config` for list-level problems. Empty object means valid.
export function validateNics(nics: readonly NicForm[]): Record<string, string> {
  const errors: Record<string, string> = {}
  if (nics.length === 0) {
    return { network_config: 'Add at least one network interface' }
  }
  const seenNames = new Set<string>()
  nics.forEach((nic, index) => {
    const name = nic.name.trim()
    const mac = nic.mac.trim()
    if (!name && !mac) {
      errors[`nic_${index}_name`] = 'Enter an interface name (e.g. eno1 or en*) or a MAC address'
    } else if (name && !GLOB_RE.test(name)) {
      if (seenNames.has(name))
        errors[`nic_${index}_name`] = `Interface “${name}” is configured twice`
      seenNames.add(name)
    }
    if (mac && !MAC_RE.test(mac)) {
      errors[`nic_${index}_mac`] = 'MAC must look like aa:bb:cc:dd:ee:ff'
    }
    if (!nic.dhcp) {
      if (!CIDR_RE.test(nic.address.trim()))
        errors[`nic_${index}_address`] = 'Enter an IP address with prefix, e.g. 192.168.1.50/24'
      else if (nic.gateway.trim() && !IPV4_RE.test(nic.gateway.trim()))
        errors[`nic_${index}_gateway`] = 'Gateway must be a valid IPv4 address'
    }
  })
  return errors
}
