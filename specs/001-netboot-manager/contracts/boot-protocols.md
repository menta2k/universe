# Contract: Boot-Path Protocols (machine-facing)

**Feature**: 001-netboot-manager

The wire contract a booting machine experiences. Patterns and quirk handling are
sourced from smee/Pixiecore analysis ([research.md](../research.md)).

## 1. DHCP (UDP :67, authoritative reservation mode)

- Answer DISCOVER/REQUEST for machines on configured subnets; assign from the
  dynamic range or the machine's reservation. Lease written to Valkey (TTL) and
  mirrored to events.
- **Netboot decision** — only when the request is a valid netboot request
  (options 60 `PXEClient`/`HTTPClient`, 93, 94 present) **and** the MAC belongs to a
  machine with an `active` provisioning session. Otherwise: plain lease, no boot
  options; unknown MACs additionally logged as `unknown_machine` (denied, FR-005).
- **Arch → bootfile** (option 93): `0` → `undionly.kpxe` (BIOS); `7`/`9` →
  `ipxe.efi` (UEFI x64). Machine `firmware` field updated from observation.
- **Chainload loop-break** (option 77 user-class = `iPXE`): serve
  `http://<server>:<boot_port>/boot/ipxe/{mac}` as bootfile instead of the binary.
- Offer/ACK carry: option 54 server id, option 66/next-server = our IP, option 67 =
  bootfile. Vendor class `PXEClient` echoed; option 43 sub-option 6=0x08 for BIOS
  only (UEFI relies on filename, port-4011 flow added only if E2E shows need).
- Honor `giaddr` for relayed requests; broadcast replies when client has no IP.
- **Conflict watcher**: passively log OFFERs from other server IDs on the segment →
  `foreign_dhcp_detected` events (FR-016). Service refuses to start unless
  `dhcp_config.enabled=true`.

## 2. TFTP (UDP :69, read-only)

- Serves only: embedded iPXE binaries (ipxedust) and `ipxe_bin` artifacts.
  Filename allowlist — no path traversal, no directory listing, no writes.
- blksize/tsize negotiation enabled (large-file speed). Every RRQ logged to
  `tftp_transfers` with client IP/MAC-correlation, file, bytes, outcome.

## 3. Boot HTTP (":8082", machine-facing, plain HTTP)

| Endpoint | Method | Contract |
|---|---|---|
| `/boot/ipxe/{mac}` | GET | iPXE script for the machine's active session: `kernel`/`initrd` lines pointing at `/boot/file/...` with the session's cmdline: `... autoinstall ds=nocloud-net;s=http://<server>/boot/seed/{token}/ netboot-session={session_id}`. 404-with-`#!ipxe exit` script if no active session. Fetch marks phase `ipxe_script`. |
| `/boot/file/{release}/{kind}` | GET | Streams kernel/initrd artifact. MUST set Content-Length (iPXE perf). SHA-256 spot-verified against DB. Logged as `file_served`. |
| `/boot/seed/{token}/user-data` | GET | Rendered autoinstall YAML for the session bound to `token`. Token: single-use-per-boot, 30 min TTL, Valkey-backed; invalid/expired → 403 + `seed_served` denied event. Contains per-session one-time credentials (FR-018). |
| `/boot/seed/{token}/meta-data` | GET | `instance-id: {session_id}`, hostname. |
| `/boot/seed/{token}/vendor-data` | GET | Empty 200 (subiquity requires the endpoint). |
| `/boot/report/{token}` | POST | Called by autoinstall `late-commands` (success) or `error-commands` (failure): `{status: ok\|error, log_tail?}`. Transitions session → `completed`/`failed`, machine → `installed`/`failed`. Idempotent; token invalidated after terminal state. |

- All boot HTTP responses are attributable: session_id resolved from mac/token and
  stamped on every event (correlation through the whole boot — smee pattern).
- Rendered user-data is schema-validated at render time; a render/validation error
  returns 500 **and never a partial document**, and fails the session with evidence
  (FR-008).

## 4. Rendered autoinstall document (served, not stored)

```yaml
#cloud-config
autoinstall:
  version: 1
  identity: {hostname, username, password: <one-time argon2 hash>}   # or user-data section
  ssh: {install-server: true, authorized-keys: [...profile keys...], allow-pw: false}
  storage: <from profile.storage_layout>
  network: <from profile.network_config>
  packages: [...profile.packages...]
  late-commands:
    - curtin in-target -- ... (profile.late_commands)
    - wget -qO- --post-data 'status=ok' http://<server>/boot/report/{token}
  error-commands:
    - wget -qO- --post-data 'status=error' http://<server>/boot/report/{token}
```

Hard rules: `allow-pw: false`; one-time identity password rotated/disabled by
late-command when profile provides SSH keys; no plaintext long-lived secrets
(FR-018, Constitution IV).
