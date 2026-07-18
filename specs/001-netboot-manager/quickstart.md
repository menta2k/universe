# Quickstart & Validation Guide: Netboot Manager

**Feature**: 001-netboot-manager

Runnable scenarios proving the feature end-to-end. Contracts:
[admin-api.md](./contracts/admin-api.md), [boot-protocols.md](./contracts/boot-protocols.md);
entities: [data-model.md](./data-model.md).

## Prerequisites

- Linux host (amd64), Go 1.24, Node 22, Docker (for TimescaleDB + Valkey), QEMU/OVMF
  (`qemu-system-x86_64`, `ovmf`) for E2E.
- Free ports: 67/69 (root or CAP_NET_BIND_SERVICE), 8080 (API/UI), 8082 (boot HTTP).
- Ubuntu 24.04 netboot kernel/initrd downloaded once for artifact upload.

## Setup

```bash
docker compose -f deploy/docker-compose.dev.yml up -d   # timescaledb + valkey
cd backend && make migrate && make run                  # netbootd, DHCP disabled by default
cd frontend && npm ci && npm run dev                    # UI on :5173 → proxies :8080
```

Bootstrap operator account is created from config on first start; login via UI.

## Scenario 1 — Unattended install (User Story 1, SC-001/SC-002)

1. UI → Boot Files: upload noble kernel + initrd (expect SHA-256 shown).
2. UI → Profiles: create profile (noble, lvm layout, 1 SSH key) — expect save OK and
   Preview renders redacted user-data.
3. UI → DHCP: define subnet + range, **Enable DHCP** (explicit action).
4. UI → Machines: register QEMU VM's MAC, assign profile, click **Provision**.
5. Boot the VM PXE-first on the isolated bridge:
   `tests/e2e/scripts/boot-vm.sh --mac <mac> --firmware uefi` (also run `--firmware bios`).
6. **Expected**: VM chainloads iPXE → Ubuntu installs unattended → VM reboots into
   installed OS; machine state `installed`; session timeline shows every phase
   (dhcp → tftp → ipxe_script → file_served → seed_served → install_report →
   session_completed); SSH into VM with the profile key succeeds; password login refused.

## Scenario 2 — Validation gates (SC-006)

- Save a profile with an invalid storage layout → expect 422 with field errors, no
  version bump.
- Save DHCP config with range outside subnet → expect 422; `GET /api/v1/dhcp/config`
  still returns previous version; running DHCP unaffected.

## Scenario 3 — Unknown machine denied (FR-005)

- Boot a VM whose MAC is not registered → expect no boot options offered, entry in
  UI → Machines → Unknown boots; use **Register** from that entry.

## Scenario 4 — Failure diagnosis (SC-005) & staleness (FR-015)

- Start a provision, kill the VM mid-install → after stale timeout session state is
  `stale`/`failed` with `failure_phase` = last completed phase and evidence attached;
  diagnose from UI only.

## Scenario 5 — Concurrency (SC-003)

- `tests/e2e/scripts/boot-fleet.sh --count 10` → 10 VMs install concurrently; assert
  each got its reserved/pool IP, its own seed (hostname matches), no cross-mixed
  events between sessions.

## Test commands

```bash
cd backend && make test          # unit + integration (testcontainers)
cd backend && make test-e2e      # QEMU BIOS+UEFI netboot (needs KVM)
cd frontend && npm test          # vitest
cd frontend && npm run test:e2e  # playwright
```

Coverage gate: ≥80% (constitution) — enforced in `make test` and CI.
