# Validation Report: Netboot Manager

**Date**: 2026-07-18
**Branch**: `001-netboot-manager`

This records the results of running the quickstart validation scenarios and the
automated test suites. Scenarios that require real network/VM infrastructure
(QEMU/KVM, an isolated provisioning bridge) could not be executed in the
development sandbox and are marked accordingly; every check that can run against
real TimescaleDB + Valkey containers was executed and passed.

## Automated tests

| Suite | Result |
|-------|--------|
| Backend unit + integration (`make test`, `-race`) | PASS — **80.7% coverage** (gate: 80%) |
| Frontend unit (Vitest, 83 tests, 12 files) | PASS |
| Frontend lint (ESLint) + build (vue-tsc + vite) | PASS |
| Backend E2E (QEMU BIOS+UEFI) | NOT RUN — needs KVM + bridge (scaffold + scripts delivered) |

**Bugs surfaced and fixed during verification** (the test suite earned its keep):
- SSE handler deadlock — blocked on the Valkey subscribe before flushing
  response headers, hanging every event-stream client.
- Route collisions — `/machines/unknown` and `/artifacts/transfers` were
  shadowed by the `/{id}` routes and returned 500 (parsed the literal as a UUID).
- `inet`/`macaddr` columns leaked a `/32` suffix into API responses
  (reservation IP, DHCP subnet ranges, foreign server IDs).

Integration coverage spans: schema/migrations + hypertables, machine API
contract (CRUD, provision conflicts, DHCP-disabled precondition), full boot
flow (iPXE script → kernel/initrd with Content-Length → single-use seed token →
install report → machine installed), profile API (validation, versioning +
revision, clone, delete-in-use guard), DHCP API (validation, last-valid-config,
enable/disable, leases), SSE event delivery (< 5 s, filtered), and artifact
lifecycle (upload, sha256, delete reference guard).

## Quickstart scenarios

| # | Scenario | Status |
|---|----------|--------|
| 1 | Unattended install (BIOS+UEFI) | Boot-path verified via integration (HTTP sequence + report → installed); full VM install NOT RUN (needs KVM) |
| 2 | Validation gates (profile + DHCP) | PASS (profile & DHCP API contract tests) |
| 3 | Unknown machine denied | PASS (DHCP handler unit + machine API) |
| 4 | Failure diagnosis & staleness | PASS (sweeper unit + session timeline/evidence) |
| 5 | Concurrency (10 machines) | Harness delivered (`boot-fleet.sh`); NOT RUN (needs KVM) |

## Constitution compliance

- **Test-First**: every implementation task was preceded by failing tests.
- **Secure by Default**: DHCP disabled until explicit enable; one-time hashed
  seed credentials; argon2id operator auth; artifact SHA-256; rate-limited login.
- **Observability**: structured slog JSON, Prometheus metrics, per-boot
  correlation via session id, full event hypertable with retention.
- **Coverage**: see `make coverage` output; core biz/rendering logic is
  well-covered. The 80% whole-repo gate is enforced in `make test`.

## Environment limitations

- No KVM/QEMU bridge available: Scenarios 1 (full VM install) and 5, and task
  T038/T063 execution, are deferred. Scripts and a tagged Go harness are in
  `backend/tests/e2e/`.
- DHCP socket-level exchange (T021) needs a real interface; decision and handler
  logic are unit-tested instead.
