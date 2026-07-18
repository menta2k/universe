# Contract: Admin API (Kratos gRPC + HTTP)

**Feature**: 001-netboot-manager

Proto package `netboot.v1` (files under `backend/api/netboot/v1/`), served as gRPC
and HTTP (Kratos gateway) under `/api/v1`. All HTTP responses use the standard
envelope `{success, data, error, meta?}`; errors are Kratos errors with typed
reasons mapped to HTTP status (pattern from pxe-pilot's error-kind mapping).
All endpoints except `AuthService.Login` and `/healthz` require an authenticated
operator session; every mutating call is recorded as a `config_change` event.

## Services

### AuthService

| RPC | HTTP | Notes |
|---|---|---|
| Login | POST /api/v1/auth/login | {username, password} → session cookie |
| Logout | POST /api/v1/auth/logout | |
| Me | GET /api/v1/auth/me | current operator |

### MachineService

| RPC | HTTP | Notes |
|---|---|---|
| ListMachines | GET /api/v1/machines | filter: state, profile_id, q; paginated |
| GetMachine | GET /api/v1/machines/{id} | includes current session summary |
| CreateMachine | POST /api/v1/machines | {mac, name, firmware?, profile_id?, reservation_ip?} |
| UpdateMachine | PATCH /api/v1/machines/{id} | field mask |
| DeleteMachine | DELETE /api/v1/machines/{id} | blocked while session active |
| Provision | POST /api/v1/machines/{id}:provision | opens session, arms next boot; 409 if active session |
| CancelProvision | POST /api/v1/machines/{id}:cancel | active session → failed(cancelled) |
| ListUnknownBoots | GET /api/v1/machines/unknown | recent unknown-MAC boot attempts (FR-005) |
| RegisterFromUnknown | POST /api/v1/machines:register-unknown | promote logged MAC to machine |

### ProfileService

| RPC | HTTP | Notes |
|---|---|---|
| ListProfiles / GetProfile | GET /api/v1/profiles[/{id}] | |
| CreateProfile | POST /api/v1/profiles | full validation (FR-008); 422 with field errors |
| UpdateProfile | PUT /api/v1/profiles/{id} | bumps version, writes revision |
| CloneProfile | POST /api/v1/profiles/{id}:clone | |
| DeleteProfile | DELETE /api/v1/profiles/{id} | 409 while machines assigned (FR-009) |
| PreviewProfile | POST /api/v1/profiles/{id}:preview | body: {machine_id?} → rendered user-data (credentials redacted) |

### DhcpService

| RPC | HTTP | Notes |
|---|---|---|
| GetDhcpConfig | GET /api/v1/dhcp/config | includes enabled flag + version |
| UpdateDhcpConfig | PUT /api/v1/dhcp/config | subnets+options transactionally; 422 on overlap/containment errors; running service keeps last valid on failure |
| EnableDhcp / DisableDhcp | POST /api/v1/dhcp:enable / :disable | explicit action (FR-016) |
| ListLeases | GET /api/v1/dhcp/leases | live from Valkey |
| ListReservations (+CRUD) | /api/v1/dhcp/reservations | tied to machines |
| ListForeignServers | GET /api/v1/dhcp/conflicts | observed competing OFFERs (FR-016) |

### ArtifactService

| RPC | HTTP | Notes |
|---|---|---|
| ListArtifacts | GET /api/v1/artifacts | |
| UploadArtifact | POST /api/v1/artifacts (multipart) | kind, release; size/type validated (FR-017); sha256 returned |
| ReplaceArtifact | PUT /api/v1/artifacts/{id} | |
| DeleteArtifact | DELETE /api/v1/artifacts/{id} | 409 if referenced by a profile's release set |
| ListTransfers | GET /api/v1/artifacts/transfers | TFTP/HTTP transfer activity (FR-011) |

### SessionService (provisioning observability)

| RPC | HTTP | Notes |
|---|---|---|
| ListSessions | GET /api/v1/sessions | filter: machine, state, time range |
| GetSession | GET /api/v1/sessions/{id} | ordered phase timeline + evidence |
| StreamEvents | GET /api/v1/events/stream (SSE) | filters: machine_id?, session_id?; backed by Valkey pub/sub; UI live updates (FR-012, SC-004) |

## Common error reasons

`VALIDATION_FAILED` (422, field details), `NOT_FOUND` (404), `CONFLICT` (409 —
active session, assigned profile, referenced artifact), `UNAUTHENTICATED` (401),
`PERMISSION_DENIED` (403), `DHCP_DISABLED` (412 on provision when DHCP off).

## Pagination

`page`, `page_size` (≤200) request params; `meta: {total, page, page_size}` in
envelope.
