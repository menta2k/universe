# Tasks: Netboot Manager

**Input**: Design documents from `/specs/001-netboot-manager/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: INCLUDED — TDD is NON-NEGOTIABLE per the project constitution (Principle III).
Every implementation task is preceded by its failing tests (RED → GREEN → REFACTOR).

**Organization**: Tasks are grouped by user story to enable independent implementation
and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US5)
- Paths follow plan.md: `backend/` (Go, Kratos) + `frontend/` (Vue 3 + Vuetify 3)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Create repository layout per plan.md: `backend/{api,cmd/netbootd,configs,internal/{conf,biz,data/migrations,service,server,netboot/{dhcp,tftp,bootsrv,autoinstall}},tests/{integration,e2e}}` and `frontend/` placeholder, plus root `README.md` and `deploy/docker-compose.dev.yml` (TimescaleDB + Valkey)
- [X] T002 Initialize Go module `backend/go.mod` (Go 1.24) with kratos v2, insomniacslk/dhcp, pin/tftp/v3, ipxedust (tinkerbell monorepo path), pgx/v5, golang-migrate, valkey-go, testify, testcontainers-go; create `backend/Makefile` (targets: proto, migrate, run, test, test-e2e, lint, coverage ≥80% gate)
- [X] T003 [P] Scaffold frontend with Vite: Vue 3 + TypeScript + Vuetify 3 + Pinia + vue-router in `frontend/` (`package.json`, `vite.config.ts` with `/api` proxy to :8080, `src/plugins/vuetify.ts`, `src/main.ts`)
- [X] T004 [P] Configure linting/formatting: `backend/.golangci.yml`, `frontend/eslint.config.js` + prettier; add `.editorconfig` at root
- [X] T005 [P] Add CI pipeline `.github/workflows/ci.yml`: backend lint+test+coverage gate, frontend lint+test, proto generation check

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T006 Define config schema + startup validation (fail-fast on missing secrets/paths per constitution) in `backend/internal/conf/conf.go` with unit tests in `backend/internal/conf/conf_test.go`; example config `backend/configs/netbootd.example.yaml` (interfaces, ports, artifact root, DB/Valkey DSNs, stale-timeout, bootstrap operator)
- [X] T007 Create initial migration set in `backend/internal/data/migrations/0001_init.sql`: operators, machines, profiles, profile_revisions, dhcp_config, dhcp_subnets, dhcp_reservations, boot_artifacts, provisioning_sessions (+ partial unique index on active), enums — per data-model.md
- [X] T008 [P] Create hypertable migration `backend/internal/data/migrations/0002_timeseries.sql`: provisioning_events, tftp_transfers, dhcp_offers_seen with 90-day retention policies
- [X] T009 Implement data plumbing in `backend/internal/data/data.go`: pgx pool, Valkey client, golang-migrate runner, health checks; testcontainers harness in `backend/tests/integration/testenv/env.go` (Timescale + Valkey containers, migration apply)
- [ ] T010 [P] Define proto contracts `backend/api/netboot/v1/{auth,machine,profile,dhcp,artifact,session}.proto` per contracts/admin-api.md (messages, services, HTTP annotations, field validation rules); generate via `make proto`
- [ ] T011 [P] Implement structured logging (slog JSON) + Prometheus metrics registry in `backend/internal/server/observability.go`; response envelope + typed error-reason → HTTP status mapping in `backend/internal/server/errors.go` with tests
- [ ] T012 Implement event recorder: `backend/internal/biz/event.go` (EventRecorder usecase: write provisioning_events + publish to Valkey `events` channel) with repo impl in `backend/internal/data/event_repo.go`; unit tests with mocked repo in `backend/internal/biz/event_test.go`
- [ ] T013 Implement operator auth: argon2id hashing + session store (Valkey `session:<operator>`) in `backend/internal/biz/operator.go` + `backend/internal/data/operator_repo.go`; Kratos HTTP middleware + Login/Logout/Me service in `backend/internal/server/auth_middleware.go` and `backend/internal/service/auth_service.go`; bootstrap operator creation on first start; mutation-audit middleware recording a `config_change` event with operator_id for every state-changing API call (FR-013); tests first in `backend/internal/biz/operator_test.go`
- [ ] T014 Wire application lifecycle in `backend/cmd/netbootd/main.go`: errgroup + signal.NotifyContext, per-service toggles (api-http, grpc, dhcp, tftp, boot-http), graceful shutdown; Kratos HTTP(:8080)/gRPC servers in `backend/internal/server/{http.go,grpc.go}`; `/healthz` + `/metrics`
- [ ] T015 [P] Embed iPXE binaries via ipxedust in `backend/internal/netboot/ipxe_binaries.go` (exposes undionly.kpxe / ipxe.efi as fs.FS for TFTP and HTTP) with a smoke test asserting binaries are non-empty
- [ ] T016 [P] Frontend foundation: `frontend/src/api/client.ts` (generated OpenAPI client + envelope unwrap + error toast handling), `frontend/src/stores/auth.ts`, login page `frontend/src/pages/LoginPage.vue`, authenticated layout + router guards in `frontend/src/plugins/router.ts`, app shell `frontend/src/App.vue` (Vuetify nav: Dashboard, Machines, Profiles, DHCP, Boot Files, Sessions); Vitest setup `frontend/tests/unit/auth.store.spec.ts`

**Checkpoint**: `make run` starts netbootd (DHCP off), operator can log into an empty UI shell; CI green

---

## Phase 3: User Story 1 - Unattended Ubuntu Install via Netboot (Priority: P1) 🎯 MVP

**Goal**: A registered machine with an assigned profile PXE-boots, chainloads iPXE,
fetches kernel/initrd + autoinstall seed over HTTP, installs Ubuntu unattended, and
reports completion. Unknown MACs are denied and recorded.

**Independent Test**: quickstart.md Scenario 1 — QEMU VM (BIOS and UEFI) completes a
fully unattended Ubuntu 24.04 install; machine shows `installed`; SSH with profile key works.

### Tests for User Story 1 (write FIRST, must FAIL) ⚠️

- [ ] T017 [P] [US1] Unit tests for DHCP packet decisions (netboot-client detection opts 60/93/94/97, arch→bootfile map, option 77 loop-break, giaddr/broadcast reply targeting, unknown-MAC denial) in `backend/internal/netboot/dhcp/handler_test.go`
- [ ] T018 [P] [US1] Unit tests for lease pool (allocate from range, reservation honored, TTL, no double-allocation, Valkey keys `lease:*`) in `backend/internal/netboot/dhcp/leasepool_test.go`
- [ ] T019 [P] [US1] Unit tests for autoinstall rendering (strict template, schema validation, one-time credentials injected, newline-rejection in cmdline, invalid profile → error not partial doc) in `backend/internal/netboot/autoinstall/render_test.go`
- [ ] T020 [P] [US1] Unit tests for machine + session usecases (register, assign profile, provision opens single active session, state transitions, cancel) in `backend/internal/biz/{machine_test.go,session_test.go}`
- [ ] T021 [P] [US1] Integration test: real DHCPv4 exchange over loopback using insomniacslk client (DISCOVER→OFFER→REQUEST→ACK with boot options for armed machine; no boot options for unarmed; nothing for unknown) in `backend/tests/integration/dhcp_test.go`
- [ ] T022 [P] [US1] Integration test: TFTP fetch of iPXE binary via pin/tftp client + transfer logged in `backend/tests/integration/tftp_test.go`
- [ ] T023 [P] [US1] Integration test: boot HTTP flow (ipxe script → files with Content-Length → seed with single-use token → report transitions session/machine states; expired token → 403) in `backend/tests/integration/bootflow_test.go`
- [ ] T024 [P] [US1] API contract tests for MachineService (CRUD, provision 409 on active session, provision 412 when DHCP disabled) in `backend/tests/integration/machine_api_test.go`

### Implementation for User Story 1

- [ ] T025 [P] [US1] Machine entity + repo interface in `backend/internal/biz/machine.go`; pgx repo in `backend/internal/data/machine_repo.go` (immutable update pattern, state machine per data-model.md)
- [ ] T026 [P] [US1] Session entity + usecase in `backend/internal/biz/session.go` (open/complete/fail/cancel, one-active enforcement, evidence jsonb); repo in `backend/internal/data/session_repo.go`
- [ ] T027 [P] [US1] Minimal profile entity + repo (create/get/assign only — full mgmt is US2) in `backend/internal/biz/profile.go` and `backend/internal/data/profile_repo.go`
- [ ] T028 [US1] Lease pool on Valkey in `backend/internal/netboot/dhcp/leasepool.go` (allocate/renew/release, reservation lookup, mirror grants to events)
- [ ] T029 [US1] DHCP server + reservation handler in `backend/internal/netboot/dhcp/{server.go,handler.go}`: insomniacslk server4 with x/net ipv4 control messages, netboot decision per contracts/boot-protocols.md §1, arch detection, machine firmware auto-update, enabled-flag gate (starts only when dhcp_config.enabled)
- [ ] T030 [US1] TFTP server in `backend/internal/netboot/tftp/server.go`: pin/tftp read-only, filename allowlist (embedded iPXE + ipxe_bin artifacts), blksize/tsize, RRQ logging to tftp_transfers
- [ ] T031 [US1] Autoinstall renderer in `backend/internal/netboot/autoinstall/render.go`: text/template `missingkey=error`, default document builder per contracts/boot-protocols.md §4, YAML parse + autoinstall schema validation, per-session one-time credentials
- [ ] T032 [US1] Seed token store (Valkey `seedtoken:*`, 30-min TTL, single-use semantics) in `backend/internal/netboot/bootsrv/token.go`
- [ ] T033 [US1] Boot HTTP server in `backend/internal/netboot/bootsrv/server.go` + handlers `{ipxe.go,files.go,seed.go,report.go}` per contracts/boot-protocols.md §3 (Content-Length, SHA-256 spot check, phase events with session correlation ID)
- [ ] T034 [US1] Minimal artifact store (filesystem write/read + sha256, seed via CLI/config for US1; full mgmt is US4) in `backend/internal/data/artifact_store.go` and `backend/internal/biz/artifact.go`
- [ ] T035 [US1] MachineService (Kratos) in `backend/internal/service/machine_service.go`: List/Get/Create/Update/Delete/Provision/Cancel/ListUnknownBoots/RegisterFromUnknown wired to biz; register in `backend/internal/server/http.go`
- [ ] T036 [US1] Unknown-boot capture: record `unknown_machine` events from DHCP handler and expose via ListUnknownBoots in `backend/internal/biz/machine.go` (query over provisioning_events)
- [ ] T037 [US1] Frontend Machines page in `frontend/src/pages/MachinesPage.vue` + `frontend/src/stores/machines.ts`: list with state chips, register dialog (MAC/name/profile/reservation), Provision/Cancel actions, unknown-boots tab with Register action; component test `frontend/tests/unit/machines.spec.ts`
- [ ] T038 [US1] E2E harness: QEMU scripts `backend/tests/e2e/scripts/{boot-vm.sh,netenv-up.sh}` (isolated bridge, BIOS + OVMF) and Go E2E test `backend/tests/e2e/install_test.go` asserting full unattended install + report callback (quickstart Scenario 1)

**Checkpoint**: MVP — quickstart Scenario 1 passes on BIOS and UEFI using API/CLI equivalents for steps 1–3 (upload/profile/DHCP UIs arrive in US2–US4); Scenario 3 passes in full

---

## Phase 4: User Story 2 - Manage Installation Profiles (Priority: P2)

**Goal**: Full profile lifecycle in the web UI: create/edit/clone/preview/delete with
validation-on-save, versioning + revisions, delete-blocked-while-assigned.

**Independent Test**: quickstart Scenario 2 (validation gates) + create/preview/clone
flows via UI; invalid profile never reaches a booting machine.

### Tests for User Story 2 (write FIRST, must FAIL) ⚠️

- [ ] T039 [P] [US2] Unit tests for profile validation rules (storage modes, netplan shape, ≥1 SSH key, template render check on save, version bump + revision row) in `backend/internal/biz/profile_full_test.go`
- [ ] T040 [P] [US2] API contract tests for ProfileService (422 field errors, 409 delete-while-assigned, clone, preview redacts credentials) in `backend/tests/integration/profile_api_test.go`

### Implementation for User Story 2

- [ ] T041 [US2] Extend profile usecase to full lifecycle in `backend/internal/biz/profile.go`: update-with-version-bump + revision write, clone, delete guard (FK RESTRICT → typed CONFLICT), save-time render validation via autoinstall renderer
- [ ] T042 [US2] Profile revisions repo + queries in `backend/internal/data/profile_repo.go`
- [ ] T043 [US2] ProfileService (Kratos) in `backend/internal/service/profile_service.go`: List/Get/Create/Update/Clone/Delete/Preview per contracts/admin-api.md
- [ ] T044 [US2] Frontend Profiles page in `frontend/src/pages/ProfilesPage.vue` + editor `frontend/src/components/ProfileEditor.vue` (release, storage layout, network, packages, SSH keys, late-commands) + `frontend/src/stores/profiles.ts`; preview drawer rendering user-data; field-error display from 422; component test `frontend/tests/unit/profiles.spec.ts`

**Checkpoint**: US1 + US2 work; operators manage the fleet with real profiles

---

## Phase 5: User Story 3 - Manage DHCP from the Web Interface (Priority: P3)

**Goal**: DHCP config (subnets/ranges/options/reservations) editable in UI with
transactional validation and last-valid-config semantics; live lease view; explicit
enable/disable; foreign-DHCP conflict visibility.

**Independent Test**: quickstart Scenario 2 (DHCP branch) + define range/reservation in
UI, boot a client, reserved IP honored and lease visible within seconds.

### Tests for User Story 3 (write FIRST, must FAIL) ⚠️

- [ ] T045 [P] [US3] Unit tests for DHCP config validation (range within subnet, no overlaps, reservation containment/uniqueness, TTL bounds) and last-valid-config swap in `backend/internal/biz/dhcpconfig_test.go`
- [ ] T046 [P] [US3] API contract tests for DhcpService (update 422 keeps old version, enable/disable events, leases from Valkey, conflicts list) in `backend/tests/integration/dhcp_api_test.go`

### Implementation for User Story 3

- [ ] T047 [US3] DHCP config usecase in `backend/internal/biz/dhcpconfig.go` (versioned transactional apply, hot-reload signal to running server, enable/disable) + repo in `backend/internal/data/dhcpconfig_repo.go`
- [ ] T048 [US3] Foreign-DHCP conflict watcher in `backend/internal/netboot/dhcp/conflict.go`: passively log OFFERs with foreign server-id → dhcp_offers_seen + `foreign_dhcp_detected` events; unit test in same package
- [ ] T049 [US3] DhcpService (Kratos) in `backend/internal/service/dhcp_service.go`: Get/UpdateConfig, Enable/Disable, ListLeases (Valkey scan), reservation CRUD, ListForeignServers
- [ ] T050 [US3] Frontend DHCP page in `frontend/src/pages/DhcpPage.vue` + `frontend/src/stores/dhcp.ts`: enable toggle with confirmation, subnet/range editor, reservations table (linked to machines), live leases table (poll/SSE), conflicts banner; component test `frontend/tests/unit/dhcp.spec.ts`

**Checkpoint**: Day-2 DHCP operation fully in the UI

---

## Phase 6: User Story 4 - Manage Boot Files from the Web Interface (Priority: P4)

**Goal**: Upload/replace/delete kernels, initrds and iPXE binaries via UI; transfer
activity view over TFTP/HTTP serving.

**Independent Test**: quickstart Scenario 1 step 1 done purely via UI + transfer visible
in activity view after a client boots (US4 acceptance scenarios).

### Tests for User Story 4 (write FIRST, must FAIL) ⚠️

- [ ] T051 [P] [US4] Unit tests for artifact validation (size limit, filename charset/no traversal, kind/release rules, sha256 computed) and delete-guard-while-referenced in `backend/internal/biz/artifact_full_test.go`
- [ ] T052 [P] [US4] API contract tests for ArtifactService (multipart upload, replace, 409 delete-referenced, transfers listing) in `backend/tests/integration/artifact_api_test.go`

### Implementation for User Story 4

- [ ] T053 [US4] Extend artifact usecase to full lifecycle (upload/replace/delete guards, release-set linkage to profiles) in `backend/internal/biz/artifact.go`
- [ ] T054 [US4] ArtifactService (Kratos) with multipart upload handling in `backend/internal/service/artifact_service.go`; transfers query (tftp_transfers + file_served events) in `backend/internal/data/transfer_repo.go`
- [ ] T055 [US4] Frontend Boot Files page in `frontend/src/pages/BootFilesPage.vue` + `frontend/src/stores/artifacts.ts`: upload dialog with progress, sha256 display, release grouping, transfer activity table; component test `frontend/tests/unit/bootfiles.spec.ts`

**Checkpoint**: No filesystem access needed to onboard a new Ubuntu release

---

## Phase 7: User Story 5 - Observe Provisioning Progress and History (Priority: P5)

**Goal**: Live per-machine provisioning timeline (SSE, no manual reload), session
history with failure evidence, stale-session detection.

**Independent Test**: quickstart Scenario 4 — timeline shows phases live; killed VM
becomes `stale/failed` with last completed phase and evidence, diagnosable from UI only.

### Tests for User Story 5 (write FIRST, must FAIL) ⚠️

- [ ] T056 [P] [US5] Unit tests for stale-session sweeper (timeout → stale, evidence snapshot, machine → failed) in `backend/internal/biz/session_sweeper_test.go`
- [ ] T057 [P] [US5] Integration test: SSE stream delivers published events filtered by machine/session within 5s (SC-004) in `backend/tests/integration/sse_test.go`
- [ ] T058 [P] [US5] API contract tests for SessionService (list filters, timeline ordering, evidence payload) in `backend/tests/integration/session_api_test.go`

### Implementation for User Story 5

- [ ] T059 [US5] Stale-session sweeper (ticker, configurable timeout) in `backend/internal/biz/session_sweeper.go` wired into cmd lifecycle
- [ ] T060 [US5] SessionService (Kratos) in `backend/internal/service/session_service.go`: ListSessions/GetSession (timeline from provisioning_events) per contracts/admin-api.md
- [ ] T061 [US5] SSE endpoint `/api/v1/events/stream` backed by Valkey pub/sub with filter params in `backend/internal/server/sse.go`
- [ ] T062 [US5] Frontend Sessions page + machine timeline: `frontend/src/pages/SessionsPage.vue`, `frontend/src/components/SessionTimeline.vue` (live phase stepper via SSE client `frontend/src/plugins/sse.ts`), evidence viewer; dashboard summary cards in `frontend/src/pages/DashboardPage.vue`; component test `frontend/tests/unit/sessions.spec.ts`

**Checkpoint**: All five user stories independently functional

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Hardening, performance proof, docs

- [ ] T063 [P] Fleet concurrency E2E `backend/tests/e2e/scripts/boot-fleet.sh` + `backend/tests/e2e/fleet_test.go`: 10 concurrent QEMU installs, assert zero identity/seed cross-contamination (SC-003, quickstart Scenario 5)
- [ ] T064 [P] Security hardening pass: rate limiting on auth + boot endpoints, security headers, session cookie flags, secrets-in-logs audit, dependency scan in CI (`backend/internal/server/hardening.go`, `.github/workflows/ci.yml`)
- [ ] T065 [P] Playwright E2E for critical UI flows (login → register machine → provision; profile validation error path) in `frontend/tests/e2e/{provision.spec.ts,profile-validation.spec.ts}`
- [ ] T066 Verify coverage ≥80% backend and frontend, fill unit-test gaps (`make coverage`, `npm run coverage`)
- [ ] T067 [P] Documentation: root `README.md` (architecture, deployment, port/capability requirements), `docs/operations.md` (DHCP enablement, conflict handling, retention tuning)
- [ ] T068 Run full quickstart.md validation (all 5 scenarios) and record results in `specs/001-netboot-manager/validation-report.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: none
- **Phase 2 (Foundational)**: after Phase 1 — BLOCKS all stories. Internal order: T006→T007→T008/T009; T010 anytime after T002; T012/T013 after T009+T010; T014 last (wires everything); T011/T015/T016 parallel
- **Phase 3–7 (US1–US5)**: all require Phase 2. US2–US5 are independently testable but build on entities introduced in US1 (machine/session/minimal profile/minimal artifact) — implement US1 first (it is the MVP); US2, US3, US4 can then proceed **in parallel**; US5 touches only new files plus events already emitted by US1
- **Phase 8 (Polish)**: after desired stories complete (T063/T068 need US1; T065 needs US1+US2)

### Within Each User Story

- Test tasks first and failing (RED) → models → services/protocol servers → Kratos endpoints → frontend → checkpoint (GREEN → REFACTOR)

### Parallel Opportunities

- Phase 1: T003, T004, T005 in parallel after T001/T002
- Phase 2: T008, T010, T011, T015, T016 parallel; T012+T013 parallel after T009
- US1: all test tasks T017–T024 in parallel; then T025/T026/T027 in parallel; T028–T034 mostly sequential within dhcp/bootsrv packages; T037 parallel with backend endpoint work
- After US1: US2 (T039–T044), US3 (T045–T050), US4 (T051–T055) by different developers in parallel — disjoint files
- Phase 8: T063, T064, T065, T067 in parallel

## Parallel Example: User Story 1

```bash
# RED phase — launch all US1 test tasks together:
Task: "Unit tests DHCP handler decisions in backend/internal/netboot/dhcp/handler_test.go"
Task: "Unit tests lease pool in backend/internal/netboot/dhcp/leasepool_test.go"
Task: "Unit tests autoinstall rendering in backend/internal/netboot/autoinstall/render_test.go"
Task: "Integration test DHCP exchange in backend/tests/integration/dhcp_test.go"
Task: "Integration test TFTP fetch in backend/tests/integration/tftp_test.go"
Task: "Integration test boot HTTP flow in backend/tests/integration/bootflow_test.go"

# GREEN phase — models in parallel:
Task: "Machine entity+repo in backend/internal/biz/machine.go"
Task: "Session entity+usecase in backend/internal/biz/session.go"
Task: "Minimal profile entity in backend/internal/biz/profile.go"
```

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 → Phase 2 (foundation, UI shell, auth)
2. Phase 3 (US1) — the QEMU E2E install passing on BIOS + UEFI **is** the MVP
3. STOP and VALIDATE: quickstart Scenarios 1 & 3; demo

### Incremental Delivery

1. + US2 (profiles UI) → validate Scenario 2 → deliver
2. + US3 (DHCP UI) and/or US4 (boot files UI) in parallel → deliver
3. + US5 (live timeline) → validate Scenario 4 → deliver
4. Phase 8 polish → Scenario 5 fleet test → release

### Notes

- Commit after each task or logical group (conventional commits)
- Constitution gates apply per task: immutability, files <800 lines, no silent errors,
  validation at boundaries; code review + security review before merge
