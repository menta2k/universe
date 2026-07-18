/**
 * API entity types for Netboot Manager, mirroring
 * specs/001-netboot-manager/data-model.md. Field names follow the JSON
 * wire format (snake_case). `password_hash` is never returned by the API
 * and is intentionally absent from Operator.
 */

export interface Operator {
  readonly id: string
  readonly username: string
  readonly display_name: string
  readonly active: boolean
  readonly last_login_at: string | null
  readonly created_at?: string
  readonly updated_at?: string
}

export type Firmware = 'bios' | 'uefi_x64' | 'unknown'

export type ProvisionState = 'new' | 'ready' | 'installing' | 'installed' | 'failed'

export interface Machine {
  readonly id: string
  readonly mac: string
  readonly name: string
  readonly firmware: Firmware
  readonly profile_id: string | null
  readonly reservation_ip: string | null
  readonly provision_state: ProvisionState
  readonly notes: string
  readonly created_at: string
  readonly updated_at: string
}

export type UbuntuRelease = 'jammy' | 'noble'

export type StorageMode = 'lvm' | 'direct' | 'custom'

export interface StorageLayout {
  readonly mode: StorageMode
  readonly custom?: string
}

export interface Profile {
  readonly id: string
  readonly name: string
  readonly version: number
  readonly ubuntu_release: UbuntuRelease
  readonly storage_layout: StorageLayout
  readonly network_config: Record<string, unknown>
  readonly packages: readonly string[]
  readonly ssh_authorized_keys: readonly string[]
  readonly user_data_template: string | null
  readonly late_commands: readonly string[]
  readonly kernel_cmdline_extra: string
  readonly created_at: string
  readonly updated_at: string
}

export interface DhcpSubnet {
  readonly id: string
  readonly network: string
  readonly range_start: string
  readonly range_end: string
  readonly gateway: string
  readonly dns: readonly string[]
  readonly next_server: string
}

export interface DhcpReservation {
  readonly id: string
  readonly machine_id: string
  readonly ip: string
}

export interface DhcpConfig {
  readonly enabled: boolean
  readonly version: number
  readonly interface: string
  readonly lease_ttl_seconds: number
  readonly subnets: readonly DhcpSubnet[]
  readonly updated_by: string | null
  readonly updated_at: string
}

export interface DhcpLease {
  readonly ip: string
  readonly mac: string
  readonly machine_id: string | null
  readonly expires_at: string
}

export type ArtifactKind = 'kernel' | 'initrd' | 'ipxe_bin' | 'other'

export interface BootArtifact {
  readonly id: string
  readonly kind: ArtifactKind
  readonly ubuntu_release: UbuntuRelease | null
  readonly filename: string
  readonly size_bytes: number
  readonly sha256: string
  readonly uploaded_by: string
  readonly created_at: string
  readonly updated_at: string
}

export type SessionState = 'active' | 'completed' | 'failed' | 'stale'

export interface ProvisioningSession {
  readonly id: string
  readonly machine_id: string
  readonly profile_id: string
  readonly profile_version: number
  readonly state: SessionState
  readonly started_at: string
  readonly ended_at: string | null
  readonly failure_phase: string | null
  readonly evidence: Record<string, unknown>
}

export type EventPhase =
  | 'dhcp_discover'
  | 'dhcp_offer'
  | 'dhcp_ack'
  | 'tftp_transfer'
  | 'ipxe_script'
  | 'file_served'
  | 'seed_served'
  | 'install_report'
  | 'session_completed'
  | 'session_failed'
  | 'unknown_machine'
  | 'foreign_dhcp_detected'
  | 'config_change'

export type EventOutcome = 'ok' | 'error' | 'denied'

export interface ProvisioningEvent {
  readonly time: string
  readonly session_id: string | null
  readonly machine_mac: string
  readonly phase: EventPhase
  readonly outcome: EventOutcome
  readonly detail: Record<string, unknown>
}
