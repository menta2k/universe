# Data Model: Netboot Manager

**Feature**: 001-netboot-manager | **Date**: 2026-07-18

Storage split (research D7): TimescaleDB is the system of record; Valkey holds
ephemeral/TTL state and pub/sub. Artifact file bytes live on disk; only metadata is
in the DB. All tables carry `created_at`/`updated_at`; all mutations are attributed
to an `operator_id` in `provisioning_events` or entity audit columns.

## Relational Entities (TimescaleDB)

### operators

| Field | Type | Rules |
|---|---|---|
| id | uuid PK | |
| username | text UNIQUE | 3–64 chars, `[a-z0-9._-]` |
| password_hash | text | argon2id; never returned by any API |
| display_name | text | |
| active | bool | inactive operators cannot log in |
| last_login_at | timestamptz NULL | |

### machines

| Field | Type | Rules |
|---|---|---|
| id | uuid PK | |
| mac | macaddr UNIQUE | validated MAC; identity key for boot lookups |
| name | text UNIQUE | 1–63 chars, hostname-safe |
| firmware | enum(`bios`,`uefi_x64`,`unknown`) | auto-updated from observed DHCP option 93 |
| profile_id | uuid FK → profiles NULL | exactly 0..1 profile (FR-006) |
| reservation_ip | inet NULL | must lie inside a defined subnet; unique |
| provision_state | enum(`new`,`ready`,`installing`,`installed`,`failed`) | derived from latest session |
| notes | text | |

State transitions (`provision_state`):
`new` → `ready` (profile assigned) → `installing` (session opened) →
`installed` (verified report received) \| `failed` (error or stale timeout);
`installed`/`failed` → `installing` on re-provision. Only `ready|installed|failed`
machines are served boot instructions; `new` (no profile) boots are logged as
unknown-profile events and denied (FR-005 variant).

### profiles

| Field | Type | Rules |
|---|---|---|
| id | uuid PK | |
| name | text UNIQUE | |
| version | int | incremented on every update (Principle I) |
| ubuntu_release | enum(`jammy`,`noble`) | maps to artifact set |
| storage_layout | jsonb | `{mode: lvm\|direct\|custom, custom: <curtin yaml>}` |
| network_config | jsonb | netplan-shaped; validated |
| packages | text[] | apt package names |
| ssh_authorized_keys | text[] | ≥1 required (key-only access, Constitution IV) |
| user_data_template | text NULL | optional full autoinstall override template |
| late_commands | text[] | post-install steps |
| kernel_cmdline_extra | text | no newlines; strict-template rendered |

Validation on save (FR-008): render with fixture machine → YAML parse → autoinstall
schema check; reject on any failure. Delete blocked while any machine references it
(FR-009, enforced by FK RESTRICT + friendly biz error).

Profile updates write the previous row to `profile_revisions` (same shape +
`revised_by`, `revised_at`) for auditability.

### dhcp_config (single row) & dhcp_subnets & dhcp_reservations

- `dhcp_config`: `enabled` (bool, default **false** — FR-016), `interface`,
  `lease_ttl_seconds` (300–86400), `updated_by`.
- `dhcp_subnets`: `network` (cidr), `range_start`/`range_end` (inet, inside network,
  start ≤ end), `gateway`, `dns` (inet[]), `next_server` (inet). Ranges must not
  overlap other subnets; boot options derived, not stored per subnet.
- `dhcp_reservations`: `machine_id` FK UNIQUE, `ip` (inet UNIQUE, inside a subnet,
  outside the dynamic range or excluded from the pool).

Save applies the whole DHCP config transactionally; on validation failure the
running service keeps the last valid version (kept in memory + `dhcp_config.version`).

### boot_artifacts

| Field | Type | Rules |
|---|---|---|
| id | uuid PK | |
| kind | enum(`kernel`,`initrd`,`ipxe_bin`,`other`) | |
| ubuntu_release | enum NULL | required for kernel/initrd |
| filename | text | served name; `[A-Za-z0-9._-]+`, no path traversal |
| path | text | absolute path under artifact root |
| size_bytes | bigint | enforced upload limit |
| sha256 | text | computed server-side on upload; verified on serve |
| uploaded_by | uuid FK → operators | |

### provisioning_sessions

| Field | Type | Rules |
|---|---|---|
| id | uuid PK | correlation ID carried through the whole boot |
| machine_id | uuid FK | |
| profile_id | uuid FK + profile_version | snapshot of what was served |
| state | enum(`active`,`completed`,`failed`,`stale`) | |
| started_at / ended_at | timestamptz | |
| failure_phase | text NULL | last completed phase on failure |
| evidence | jsonb | captured logs/artifacts refs for diagnosis |

One `active` session per machine (partial unique index). Stale timeout (default
60 min, configurable) moves `active` → `stale` (FR-015).

### provisioning_events (hypertable, partitioned on `time`)

| Field | Type |
|---|---|
| time | timestamptz |
| session_id | uuid NULL (null for unknown-machine events) |
| machine_mac | macaddr |
| phase | enum(`dhcp_discover`,`dhcp_offer`,`dhcp_ack`,`tftp_transfer`,`ipxe_script`,`file_served`,`seed_served`,`install_report`,`session_completed`,`session_failed`,`unknown_machine`,`foreign_dhcp_detected`,`config_change`) |
| outcome | enum(`ok`,`error`,`denied`) |
| detail | jsonb (file name, bytes, error text, operator_id for config_change…) |

Retention policy: 90 days default (config). `tftp_transfers` and
`dhcp_offers_seen` (foreign-server conflict evidence, FR-016) are additional narrow
hypertables feeding the activity views.

## Valkey Keys (ephemeral)

| Key pattern | Value | TTL |
|---|---|---|
| `lease:<ip>` | JSON {mac, machine_id, expires_at} | lease TTL (authoritative copy of active lease) |
| `lease:mac:<mac>` | ip | lease TTL (reverse index) |
| `seedtoken:<token>` | JSON {session_id, machine_id, credentials} | 30 min, **single-use** (GETDEL on seed fetch guarded per file set) |
| `events` (pub/sub channel) | provisioning_event JSON | — (SSE fanout to UI) |
| `session:<operator>` | web session data | idle timeout |

Lease grants/renewals/expiries are mirrored asynchronously into Timescale
(`provisioning_events`) for durable history; Valkey is the runtime source of truth
for what is currently leased.

## Relationships Overview

```text
operators ─┬─< profiles (audit)             machines >─── profiles (0..1)
           └─< boot_artifacts (uploaded_by) machines ──1:0..1── dhcp_reservations
provisioning_sessions >── machines          provisioning_sessions ──< provisioning_events
profiles ──< profile_revisions              dhcp_subnets ──< dhcp_reservations (containment)
```
