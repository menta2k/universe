# Phase 0 Research: Netboot Manager

**Feature**: 001-netboot-manager | **Date**: 2026-07-18

Reference projects were cloned and analyzed in depth (ns1/waitron, danderson/netboot,
tinkerbell/smee, ggiamarchi/pxe-pilot). Findings below drive every technology decision.

## Reference Project Findings

### ns1/waitron (HTTP provisioning orchestrator)

- No DHCP/TFTP of its own — pure HTTP config service behind Pixiecore
  (`GET /v1/boot/{mac}` → kernel/initrd/cmdline; templates fetched by installer with a
  per-job token).
- Layered config merge: global → build type → machine → request override; pongo2 (Jinja2)
  templates rendered with `{job, machine, config, token}`.
- Job state machine (`pending → installing → preseed/finish → completed/failed`) with a
  stale-job ticker.
- **Pitfalls to avoid**: all state in-memory (lost on restart), YAML
  marshal/unmarshal merging (slow, lossy), state-changing GET endpoints, shell command
  hooks rendered from machine data (injection surface).
- **Adopt**: per-job one-time token auth for installer callbacks; layered
  defaults→profile→machine override; `_unknown_` machine capture; stale-job timeout.

### danderson/netboot (Pixiecore + dhcp4/tftp libraries)

- `dhcp4` package: clean DHCPv4 codec + Linux `NewSnooperConn` (AF_PACKET, coexists with
  a foreign DHCP server). `tftp` package: minimal read-only TFTP server.
  Both importable but **upstream is effectively frozen** — treat as reference code.
- ProxyDHCP-only: answers only DISCOVERs carrying option 93; never assigns IPs.
- Firmware/arch detection: option 93 (0=BIOS, 6=EFI32, 7=EFI64, 9=EFIBC); option 77
  user-class breaks the iPXE chainload loop (`iPXE`/sentinel user-class → serve HTTP
  script instead of re-serving the iPXE binary).
- UEFI quirk: many UEFI firmwares ignore option-43 "bypass", expect the port-4011
  PXE/BINL flow; BIOS gets option 43 sub-option 6 = 0x08.
- Other encoded gotchas: option 97 may be legally absent (buggy ROMs); vendor class in
  the offer must be `PXEClient`; always send Content-Length for boot files (iPXE is
  drastically slower without it); HMAC-signed file URLs; cmdline templates with
  `missingkey=error` and newline rejection.

### tinkerbell/smee (production PXE service — primary architectural model)

> **Successor note**: standalone smee has been superseded by the
> `github.com/tinkerbell/tinkerbell` monorepo, where smee continues as the DHCP/PXE
> service (`smee/` subtree). The architecture below is unchanged there; when borrowing
> code or tracking fixes, reference the monorepo, not the archived standalone repo.
> The same applies to `ipxedust` (iPXE binary embedding), which is maintained under the
> Tinkerbell org — verify its current module path at implementation time.

- Single binary; every service (DHCP, TFTP, HTTP, syslog) is a toggleable goroutine in
  one `errgroup` with `signal.NotifyContext` graceful shutdown.
- DHCP via **insomniacslk/dhcp** (`dhcpv4`, `server4`) with `x/net/ipv4` PacketConn +
  `SetControlMessage(FlagInterface)` for correct reply routing; three handler modes:
  reservation (authoritative), proxy, auto-proxy.
- Arch→bootfile map (`undionly.kpxe` BIOS, `ipxe.efi` EFI x86, `snp.efi` arm64);
  Raspberry Pi OUI workaround; `IsNetbootClient` validates options 60/93/94/97;
  UserClass + ClientType branching breaks chainload loops.
- TFTP via **tinkerbell/ipxedust** (embeds compiled iPXE binaries, serves them over
  TFTP and HTTP from one source; runtime byte-patching of embedded scripts).
- `BackendReader{GetByMac, GetByIP}` returning neutral DTOs — clean seam between data
  source and protocol handlers; typed `NotFound()` error interface.
- Observability: logr/slog JSON, Prometheus with pre-initialized labels, OpenTelemetry
  spans per packet; trace ID smuggled through boot for cross-phase correlation.
- **Pitfalls**: honor `giaddr` for relayed replies (RFC 2131); broadcast fallback when
  client has no IP; unbounded per-packet goroutines.

### ggiamarchi/pxe-pilot (PXE config manager)

- State = symlinks in the TFTP root (`pxelinux.cfg/01-<mac>` → config file); fragile and
  racy — confirms the need for a real datastore.
- Clean api/service/model layering; typed error-kind → HTTP status mapping; pluggable
  power-management adapter (IPMI via shell-out — avoid; use a Go IPMI lib if ever
  needed, out of scope v1).

## Decisions

### D1: Backend framework — go-kratos/kratos v2

- **Decision**: Kratos v2, proto-first API (gRPC + HTTP gateway), layered
  `service → biz → data` architecture.
- **Rationale**: user-mandated; proto contracts give schema-validated inputs
  (Constitution: validation at boundaries) and generated OpenAPI for the frontend.
- **Alternatives**: gin (pxe-pilot), httprouter (waitron) — rejected: no contract
  generation, weaker layering.

### D2: DHCP — insomniacslk/dhcp, own reservation handler

- **Decision**: `github.com/insomniacslk/dhcp` (`dhcpv4`/`server4`) with a smee-style
  handler in authoritative **reservation mode**; lease pool managed by us. Foreign-DHCP
  conflict detection by passively logging competing OFFERs. Proxy mode deferred to v2.
- **Rationale**: actively maintained, used in production by smee/CoreDHCP;
  danderson/dhcp4 is frozen. Authoritative mode matches the spec assumption (isolated
  provisioning network) and lets the DHCP UI manage ranges/reservations/leases.
- **Alternatives**: embed CoreDHCP (plugin model too rigid for per-machine DB lookups);
  danderson `dhcp4` (frozen); external dnsmasq (not manageable from our UI, violates
  "web interface for DHCP" requirement).

### D3: TFTP — pin/tftp + tinkerbell/ipxedust for iPXE binaries

- **Decision**: `github.com/pin/tftp/v3` server with a repository-backed read handler;
  iPXE binaries embedded via `tinkerbell/ipxedust` (also served over HTTP). Uploaded
  boot artifacts stored on disk with metadata + SHA-256 in the database.
- **Rationale**: pin/tftp is the maintained standard (blocksize/tsize negotiation —
  needed for large initrds); ipxedust removes the "where do iPXE binaries come from"
  problem and keeps TFTP/HTTP binary sources identical.
- **Alternatives**: danderson `tftp` (read-only, no option negotiation, frozen).

### D4: Boot flow — PXE → iPXE chainload → HTTP

- **Decision**: DHCP serves arch-appropriate iPXE binary over TFTP
  (BIOS `undionly.kpxe`, UEFI x64 `ipxe.efi`); iPXE (detected via option 77 user-class)
  is redirected to an HTTP iPXE script endpoint that serves per-machine
  kernel/initrd/cmdline; kernel+initrd+autoinstall seed delivered over HTTP.
- **Rationale**: TFTP only carries the small iPXE binary; everything heavy goes over
  HTTP (fast, observable, standard — pattern shared by all four references). Option
  93/77/60 handling copied from smee/pixiecore findings above, including the port-4011
  UEFI quirk if needed.

### D5: Ubuntu unattended install — autoinstall + NoCloud over HTTP

- **Decision**: Ubuntu Server live installer (subiquity) **autoinstall**, seeded via
  cloud-init NoCloud datasource over HTTP: kernel cmdline
  `autoinstall ds=nocloud-net;s=http://<server>/seed/<token>/` serving rendered
  `user-data`/`meta-data`/`vendor-data`. Install completion reported by a
  late-command phoning `POST /v1/boot/report/<token>`.
- **Rationale**: autoinstall is the only supported unattended mechanism for
  Ubuntu ≥ 20.04 (preseed is legacy); NoCloud-net is the standard netboot seed path;
  waitron's token-callback pattern gives us FR-014/FR-015 completion tracking.
- **Alternatives**: legacy preseed (deprecated), full ISO repack per machine (slow,
  violates SC-001).

### D6: Templating — Go text/template with strict mode

- **Decision**: `text/template` with `missingkey=error` for autoinstall user-data and
  cmdline rendering; rendered output YAML-validated against the autoinstall schema
  before serving; per-machine one-time credentials injected at render time (FR-018).
- **Rationale**: stdlib, no new dependency, strict-mode failures satisfy FR-008
  (never serve invalid config). Pongo2 (waitron) rejected: unmaintained pace, non-Go
  semantics.

### D7: Storage — TimescaleDB (primary) + Valkey (KV/ephemeral)

- **Decision**: TimescaleDB for all relational state (machines, profiles, DHCP config,
  reservations, artifacts, operators) and hypertables for time-series
  (`provisioning_events`, `tftp_transfers`, `dhcp_offers_seen`). Valkey for: active
  leases (TTL-keyed), one-time seed tokens/credentials (TTL), and pub/sub fanout of
  provisioning events to the UI (SSE).
- **Rationale**: user-mandated; the event/audit requirements (FR-014, SC-004) are
  time-series shaped — hypertables + retention policies fit exactly; Valkey TTL
  semantics model DHCP lease expiry naturally, with Timescale keeping the durable
  lease history. Fixes waitron's in-memory-state pitfall.
- **Access**: repository pattern in `internal/data` (Constitution), pgx driver,
  golang-migrate migrations.

### D8: Frontend — Vue 3 + Vuetify 3

- **Decision**: Vue 3 (Composition API, `<script setup>`) + Vuetify 3, Vite, Pinia,
  vue-router; API client generated from Kratos OpenAPI output; live provisioning
  timeline via SSE (Valkey pub/sub → Kratos HTTP SSE endpoint). Session-cookie auth
  against local operator accounts (argon2id hashes).
- **Rationale**: user-mandated stack; SSE (not websockets) is sufficient for
  one-way status streams and simpler to secure.

### D9: Lifecycle & observability

- **Decision**: smee's pattern — every network service (DHCP, TFTP, boot-HTTP, API-HTTP)
  is a toggleable goroutine under one `errgroup` with `signal.NotifyContext`; DHCP is
  **disabled by default** and enabled by explicit operator action (FR-016). Structured
  slog JSON logging, Prometheus `/metrics`, per-boot correlation ID carried from DHCP
  through install completion into `provisioning_events`.
- **Rationale**: satisfies Constitution Principle V and FR-016 directly; correlation-ID
  through-boot trick lifted from smee.

### D10: Testing strategy

- **Decision**: TDD per constitution. Unit: table-driven Go tests (protocol codecs,
  template rendering, validation, biz logic with mocked repos). Integration: real
  DHCP/TFTP exchanges over loopback/veth using client libs (`insomniacslk/dhcp` client
  side, `pin/tftp` client), API contract tests against the proto definitions,
  Timescale/Valkey via testcontainers. E2E: QEMU VM (BIOS + UEFI/OVMF) PXE-booting on
  an isolated bridge, asserting full unattended Ubuntu 24.04 install + report callback.
  Frontend: Vitest + Vue Test Utils; Playwright for critical UI flows.
- **Rationale**: SC-001..SC-006 are only provable with a real VM boot; QEMU on a
  host-only bridge makes that CI-able.

## Resolved Clarifications

All Technical Context unknowns are resolved by D1–D10; no NEEDS CLARIFICATION remains.
