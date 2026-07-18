# Universe — Netboot Manager

Bootstraps Ubuntu-family Linux servers over the network: an authoritative DHCP
server and a TFTP server chainload machines into iPXE, which fetches a
per-machine kernel/initrd and a rendered Ubuntu **autoinstall** (cloud-init
NoCloud) seed over HTTP. Machines, installation profiles, DHCP configuration,
boot artifacts, and full provisioning history are managed through a Go
([Kratos](https://go-kratos.dev/)) API and a Vue 3 + Vuetify web interface.

See [`specs/001-netboot-manager/`](specs/001-netboot-manager/) for the full
spec, plan, data model, and API contracts, and
[`.specify/memory/constitution.md`](.specify/memory/constitution.md) for the
project's engineering principles.

## Architecture

```
                         ┌────────────── operator (browser) ──────────────┐
                         │  Vue 3 + Vuetify SPA  ──HTTPS──▶  Kratos API     │
                         └──────────────────────────────────────┬──────────┘
                                                                 │  (HTTP :8080 / gRPC :9090)
  ┌──────────── provisioning network (isolated) ───────────┐    │
  │  target machine                                        │    ▼
  │   │  1. DHCP DISCOVER ─────────────▶ DHCP server (:67) ─┼──▶ biz ── TimescaleDB
  │   │  2. TFTP get iPXE binary ──────▶ TFTP server (:69) ─┼──▶       (machines, profiles,
  │   │  3. HTTP iPXE script/kernel/   ▶ boot HTTP (:8082) ─┼──▶        dhcp, artifacts,
  │   │     initrd/seed + report                            │    │      sessions, events)
  │   └────────────────────────────────────────────────────┘    └──▶ Valkey
  └──────────────────────────────────────────────────────────┘        (leases, seed tokens,
                                                                        SSE pub/sub, sessions)
```

Component layout follows Kratos conventions: `service → biz → data`, with the
machine-facing protocol servers isolated under `internal/netboot/*` and talking
to the same `biz` use cases through repository interfaces.

- **Backend** (`backend/`): Go 1.24, Kratos v2, pgx, valkey-go, insomniacslk/dhcp,
  pin/tftp, embedded iPXE via tinkerbell/ipxedust.
- **Frontend** (`frontend/`): Vue 3 + TypeScript + Vuetify 3 + Pinia + Vite.
- **Storage**: TimescaleDB (relational + provisioning-event hypertables with
  retention), Valkey (active leases, single-use seed tokens, SSE fanout).

## Quick start (development)

Prerequisites: Go 1.24, Node 22, Docker, and (for end-to-end netboot) QEMU/OVMF.

```bash
# 1. Storage
docker compose -f deploy/docker-compose.dev.yml up -d --wait

# 2. Backend
cd backend
go run ./cmd/netbootd migrate -conf configs/netbootd.example.yaml   # apply migrations
go run ./cmd/netbootd -conf configs/netbootd.example.yaml           # start the daemon

# 3. Frontend (separate shell)
cd frontend
npm ci
npm run dev            # http://localhost:5173, proxies /api to :8080
```

The first operator account is created from `bootstrap_operator` in the config on
first start — **change its password immediately**.

### Ports & privileges

| Service        | Default | Notes                                              |
|----------------|---------|----------------------------------------------------|
| Admin API HTTP | 8080    | operator UI/API (serve behind TLS in production)   |
| Admin API gRPC | 9090    | internal tooling                                   |
| Boot HTTP      | 8082    | machine-facing; plain HTTP (see below)             |
| DHCP           | 67/udp  | needs `CAP_NET_BIND_SERVICE` or root; **off** until enabled |
| TFTP           | 69/udp  | needs `CAP_NET_BIND_SERVICE` or root               |

Bind privileged ports with `setcap 'cap_net_bind_service=+ep' bin/netbootd`
(preferred) rather than running as root. For local development the example
config uses unprivileged ports (`:6767`, `:6969`).

> **Boot-path HTTP is plain HTTP in v1.** The boot network is an isolated
> provisioning segment; seed URLs are single-use 30-minute tokens and any
> credentials in a seed are hashes, never cleartext. The operator UI/API is
> HTTPS. Full boot-path TLS (custom iPXE build with an embedded CA) is a
> planned follow-up — see the Complexity Tracking note in the plan.

## Testing

```bash
cd backend
make test        # unit + integration (testcontainers: TimescaleDB + Valkey), 80% gate
make test-e2e    # QEMU BIOS+UEFI netboot of Ubuntu (needs KVM)   [see docs/operations.md]

cd frontend
npm test         # vitest unit
npm run test:e2e # playwright   [critical UI flows]
```

## Repository layout

```
backend/    Go daemon (netbootd): API + DHCP/TFTP/boot-HTTP servers
frontend/   Vue 3 + Vuetify SPA
deploy/     docker-compose for local TimescaleDB + Valkey
specs/      Spec Kit artifacts (spec, plan, data-model, contracts, tasks)
docs/       operations guide
```

See [`docs/operations.md`](docs/operations.md) for day-2 operation: enabling
DHCP, handling foreign-server conflicts, and tuning event retention.
