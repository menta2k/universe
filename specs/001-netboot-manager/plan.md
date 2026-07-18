# Implementation Plan: Netboot Manager

**Branch**: `001-netboot-manager` | **Date**: 2026-07-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-netboot-manager/spec.md`

## Summary

A single Go application that netboots and unattended-installs Ubuntu-family servers:
an authoritative DHCP server (insomniacslk/dhcp) and TFTP server (pin/tftp + embedded
iPXE binaries) chainload machines into iPXE, which fetches per-machine kernel/initrd
and a rendered Ubuntu autoinstall (cloud-init NoCloud) seed over HTTP. Machines,
installation profiles, DHCP configuration, boot artifacts, and full provisioning
history are managed through a Kratos (gRPC+HTTP) API consumed by a Vue 3 + Vuetify 3
web interface. TimescaleDB stores relational state and time-series provisioning
events; Valkey holds active leases, one-time install tokens, and pub/sub fanout for
live UI updates. Architecture patterns are adapted from tinkerbell/smee, Pixiecore,
and waitron (see [research.md](./research.md)).

## Technical Context

**Language/Version**: Go 1.24 (backend); TypeScript / Node 22 (frontend build)

**Primary Dependencies**: go-kratos/kratos v2 (API framework, proto-first),
insomniacslk/dhcp (DHCPv4 server), pin/tftp v3 (TFTP), tinkerbell/ipxedust (embedded
iPXE binaries, now under the tinkerbell monorepo org), pgx + golang-migrate
(TimescaleDB), valkey-go, Vue 3 + Vuetify 3 + Pinia + Vite (frontend)

**Storage**: TimescaleDB (machines, profiles, DHCP config, artifacts, operators;
hypertables for provisioning_events / tftp_transfers / dhcp_offers_seen with retention
policies); Valkey (active leases with TTL, one-time seed tokens, pub/sub for SSE);
local disk for boot artifact files (metadata + SHA-256 in DB)

**Testing**: go test + testify (unit, table-driven), testcontainers-go
(Timescale/Valkey integration), real DHCP/TFTP loopback exchanges (client libs),
QEMU BIOS+OVMF E2E netboot of Ubuntu 24.04 on an isolated bridge; Vitest + Vue Test
Utils, Playwright (frontend)

**Target Platform**: Linux server (amd64), single instance, attached to the
provisioning network segment; needs CAP_NET_BIND_SERVICE or root for ports 67/69

**Project Type**: Web application (Go backend + Vue frontend)

**Performance Goals**: 10 concurrent installs with zero identity cross-contamination
(SC-003); DHCP response < 100 ms; provisioning events visible in UI within 5 s
(SC-004); unattended install underway ≤ 5 min from operator action (SC-001)

**Constraints**: DHCP service disabled until explicit operator enablement (FR-016);
no long-lived plaintext credentials in served artifacts (FR-018); last-valid-config
semantics on validation failure (FR-008); UEFI + legacy BIOS clients (FR-004)

**Scale/Scope**: up to a few hundred registered machines, tens of concurrent
installs, single site; ~6 UI pages (dashboard, machines, profiles, DHCP, boot files,
sessions)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence in this plan |
|---|---|---|
| I. Declarative, Reproducible Provisioning | ✅ PASS | Profiles are versioned DB entities; autoinstall artifacts rendered from templates at boot time, never hand-edited; rendered outputs are not persisted as sources. |
| II. Unattended & Idempotent Netinstall | ✅ PASS | PXE→iPXE→autoinstall pipeline fully unattended; re-triggering a bootstrap creates a new session; rendered user-data schema-validated before serving (D5, D6). |
| III. Test-First & Verified Bootstrap | ✅ PASS | TDD with three tiers incl. QEMU E2E (D10); install completion verified via token callback + post-install report before a session is marked `completed`. |
| IV. Secure by Default | ✅ PASS | One-time TTL tokens for seed URLs (Valkey); per-machine credentials rendered at boot, never stored plaintext; argon2id operator auth; artifact SHA-256 verification; input validation via proto + biz-layer rules. |
| V. Observability & Auditability | ✅ PASS | Every DHCP/TFTP/HTTP/install event → `provisioning_events` hypertable with correlation ID carried through the whole boot; structured slog JSON; Prometheus metrics. |
| Infra constraints (PXE/iPXE, HTTPS, distro-native installers, repository pattern, API envelope) | ✅ PASS | D4/D5 use iPXE + autoinstall; artifact delivery over HTTP on the provisioning net (TFTP only for the iPXE binary — protocol limitation, HTTPS used for the operator UI/API); repository pattern in `internal/data`; Kratos errors + consistent response envelope. |
| Workflow gates (research-first, plan-first, conventional commits, E2E before done) | ✅ PASS | Four reference repos analyzed before design; this plan precedes code; E2E QEMU boot is the definition of done. |

**Post-Phase-1 re-check**: design artifacts (data-model.md, contracts/) introduce no
violations — no complexity tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/001-netboot-manager/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── admin-api.md     # Kratos gRPC/HTTP management API contract
│   └── boot-protocols.md# On-the-wire contracts: DHCP/TFTP/iPXE/seed/report
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
backend/
├── api/netboot/v1/            # .proto definitions + generated code (admin API)
├── cmd/netbootd/              # main: errgroup lifecycle, service toggles, wiring
├── configs/                   # example YAML config
├── internal/
│   ├── conf/                  # config schema + validation (startup fail-fast)
│   ├── biz/                   # domain: machine, profile, dhcpconfig, artifact,
│   │                          #   session, operator usecases + repo interfaces
│   ├── data/                  # repositories: Timescale (pgx), Valkey, artifact store
│   │   └── migrations/        # golang-migrate SQL (incl. hypertables)
│   ├── service/               # Kratos service layer (proto ⇄ biz mapping)
│   ├── server/                # Kratos HTTP/gRPC servers, auth middleware, SSE
│   └── netboot/
│       ├── dhcp/              # insomniacslk-based server, reservation handler,
│       │                      #   arch detection, lease pool, conflict watcher
│       ├── tftp/              # pin/tftp server, artifact + ipxedust handlers
│       ├── bootsrv/           # HTTP boot endpoints: ipxe script, kernel/initrd,
│       │                      #   NoCloud seed, install report callback
│       └── autoinstall/       # template rendering + autoinstall schema validation
└── tests/
    ├── integration/           # DHCP/TFTP loopback, testcontainers DB/KV, API
    └── e2e/                   # QEMU BIOS+UEFI netboot harness

frontend/
├── src/
│   ├── api/                   # generated client from Kratos OpenAPI
│   ├── stores/                # Pinia stores
│   ├── pages/                 # dashboard, machines, profiles, dhcp, bootfiles,
│   │                          #   sessions (+ session detail timeline)
│   ├── components/
│   └── plugins/               # vuetify, router, sse client
└── tests/                     # Vitest unit + Playwright e2e
```

**Structure Decision**: Web application layout (backend/ + frontend/). The backend
follows the standard Kratos layering (`service → biz → data`) with the
protocol-facing netboot servers isolated under `internal/netboot/*`, talking to the
same `biz` usecases through repo interfaces — the smee `BackendReader` seam adapted
to Kratos.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Boot-path endpoints (iPXE script, kernel/initrd, NoCloud seed, report) served over plain HTTP in v1, despite "HTTPS wherever firmware supports it" | Stock iPXE binaries shipped by ipxedust do not embed our CA; HTTPS on the boot path requires a custom iPXE build with an embedded trust anchor and cert lifecycle management — meaningful scope for v1 | Full boot-path TLS deferred to a follow-up feature ("boot-path TLS with cluster CA"). Risk contained: boot network is an isolated provisioning segment (spec assumption), seed URLs are single-use 30-min tokens, credentials in seeds are hashes, and the operator UI/API remains HTTPS |
