# Feature Specification: Netboot Manager

**Feature Branch**: `001-netboot-manager`

**Created**: 2026-07-18

**Status**: Draft

**Input**: User description: "You are a go lang programmer your task is to create a application that provide a netboot functionality. The core components are TFTP server DHCP server and a configuration managment for the installation itself. The target OS for the installation is ubuntu family. Application must have modern web based interface for tftp dhcp and configuration managment."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Unattended Ubuntu Install via Netboot (Priority: P1)

An infrastructure operator registers a target machine (by its network hardware address),
assigns it an installation profile for an Ubuntu-family release, and powers the machine on
with network boot enabled. The machine obtains a network address and boot instructions,
downloads its boot files, and completes a fully unattended Ubuntu installation without
anyone touching a console. When the install finishes, the operator can see that the
machine completed provisioning successfully.

**Why this priority**: This is the core value of the product — everything else (DHCP
management, TFTP management, profiles) exists to make this journey work. Delivering only
this story is already a usable MVP.

**Independent Test**: Boot a virtual machine on the same network segment as the
application with a registered hardware address and an assigned profile; verify the VM
installs Ubuntu end-to-end with zero manual input and is reachable afterwards.

**Acceptance Scenarios**:

1. **Given** a registered machine with an assigned installation profile, **When** the
   machine network-boots, **Then** it receives an address, boot files, and installation
   answers, and completes the Ubuntu installation without human interaction.
2. **Given** a machine whose hardware address is not registered, **When** it attempts to
   network-boot, **Then** it is not offered boot instructions and the attempt is recorded
   as an unknown-machine event visible to the operator.
3. **Given** a completed installation, **When** the operator views the machine in the
   interface, **Then** its status shows the install finished and when it finished.

---

### User Story 2 - Manage Installation Profiles (Priority: P2)

An operator creates, edits, clones, and deletes installation profiles through the web
interface. A profile captures everything the unattended Ubuntu installation needs: target
release, disk layout choice, initial user credentials or access keys, package selection,
network settings, and any post-install steps. Profiles are validated when saved so a
broken profile can never be served to a booting machine.

**Why this priority**: Without editable, validated profiles the P1 journey only works for
one hard-coded configuration; profiles make the product usable across a fleet.

**Independent Test**: Create a profile in the interface, save it with an intentional
error and confirm the save is rejected with a clear message; fix the error, save, and
confirm the profile's generated installation answers can be previewed.

**Acceptance Scenarios**:

1. **Given** a new profile form, **When** the operator saves a valid profile, **Then**
   it becomes available for assignment to machines and its rendered installation answers
   can be previewed.
2. **Given** a profile with invalid content, **When** the operator saves, **Then** the
   system rejects it, points to the offending fields, and no machine can receive it.
3. **Given** a profile assigned to machines, **When** the operator edits and saves it,
   **Then** subsequent boots receive the updated version and the change is recorded with
   who changed it and when.

---

### User Story 3 - Manage DHCP from the Web Interface (Priority: P3)

An operator configures the address-assignment service through the web interface: defines
the network range and lease options, creates static reservations for known machines,
views currently active leases, and starts/stops the service. The interface makes the
current state of the network visible at a glance.

**Why this priority**: P1 can ship with a file-seeded DHCP configuration; the web
management layer is what makes day-2 operation practical.

**Independent Test**: Define a range and a reservation in the interface, boot a client,
and confirm the client receives the reserved address and appears in the active lease
list.

**Acceptance Scenarios**:

1. **Given** a configured address range, **When** a client requests an address, **Then**
   it receives one from the range and the lease appears in the interface within seconds.
2. **Given** a static reservation for a hardware address, **When** that machine boots,
   **Then** it always receives the reserved address.
3. **Given** an invalid configuration (overlapping range, reservation outside the
   subnet), **When** the operator saves, **Then** the save is rejected with a specific
   error and the running service keeps its last valid configuration.

---

### User Story 4 - Manage Boot Files from the Web Interface (Priority: P4)

An operator manages the boot-file service through the web interface: uploads or updates
boot loader files and installer kernels/images for Ubuntu releases, sees which files are
served, and watches recent transfer activity (which machine downloaded which file, when,
and whether it succeeded).

**Why this priority**: Boot files change rarely; initial seeding can be manual. The
management view matters for upgrades and troubleshooting.

**Independent Test**: Upload a boot file through the interface, boot a client, and
confirm the transfer appears in the activity view with a success status.

**Acceptance Scenarios**:

1. **Given** an uploaded boot file, **When** a machine requests it during netboot,
   **Then** the file is served and the transfer is logged with machine identity, file
   name, timestamp, and outcome.
2. **Given** a request for a missing file, **When** the transfer fails, **Then** the
   failure is visible in the activity view with an actionable error.
3. **Given** a new Ubuntu release, **When** the operator uploads its boot files and
   links them to a profile, **Then** machines using that profile boot the new release
   without any file-system access by the operator.

---

### User Story 5 - Observe Provisioning Progress and History (Priority: P5)

An operator watches a live timeline of a machine's provisioning: address assignment,
boot-file transfers, installation answer delivery, install completion. Past provisioning
runs are kept as an auditable history, including failures with enough detail to diagnose
them without physical access to the machine.

**Why this priority**: Builds directly on events already generated by P1–P4; it turns
raw logs into an operator experience.

**Independent Test**: Provision a machine and verify every phase appears on its timeline
in order; unplug the network mid-install and verify the run is shown as failed with the
last completed phase.

**Acceptance Scenarios**:

1. **Given** an in-progress installation, **When** the operator opens the machine's
   detail view, **Then** each completed phase is shown with timestamps and the current
   phase updates without a manual page reload.
2. **Given** a failed installation, **When** the operator reviews the run, **Then** the
   failure phase, time, and captured evidence are available.

---

### Edge Cases

- Another DHCP service already exists on the network segment: the system must make this
  detectable (log conflicting offers) and its own service must be explicitly enabled by
  the operator, never auto-started on install.
- A machine boots in UEFI mode while another boots in legacy BIOS mode: each must be
  offered the boot loader matching its firmware type.
- An unknown machine (unregistered hardware address) requests boot: no install is
  offered; the event is recorded so the operator can register the machine from it.
- A profile is deleted while machines are still assigned to it: deletion is blocked
  until assignments are removed or moved.
- Two machines are provisioned concurrently: sessions, leases, and logs must never mix
  identities.
- A boot-file transfer is interrupted (power loss, cable pull): the session is marked
  failed after a timeout, not left "in progress" forever.
- The operator saves a DHCP change while leases are active: existing leases remain valid;
  only new requests use the new configuration.
- Disk fills up on the host: uploads and log capture fail with clear errors; serving of
  existing boot files continues.

## Requirements *(mandatory)*

### Functional Requirements

**Netboot pipeline**

- **FR-001**: System MUST provide an address-assignment (DHCP) service that offers
  addresses, network-boot options, and boot-file locations to booting machines.
- **FR-002**: System MUST provide a boot-file (TFTP) service that serves boot loaders,
  kernels, and initial images to booting machines.
- **FR-003**: System MUST serve per-machine unattended installation answers for
  Ubuntu-family releases so the installation completes with zero human interaction.
- **FR-004**: System MUST support both UEFI and legacy BIOS clients, serving the boot
  loader appropriate to the client's firmware type.
- **FR-005**: System MUST only offer installation to machines whose hardware address is
  registered; unknown machines MUST be recorded but not provisioned.

**Machine & profile management**

- **FR-006**: Users MUST be able to register machines by hardware address and assign
  each machine exactly one installation profile.
- **FR-007**: Users MUST be able to create, edit, clone, preview, and delete
  installation profiles covering at minimum: Ubuntu release, disk layout, initial user
  access, package selection, network configuration, and post-install steps.
- **FR-008**: System MUST validate profiles and DHCP configuration on save and MUST
  refuse to serve any configuration that fails validation; the last valid configuration
  remains in effect.
- **FR-009**: System MUST block deletion of a profile while any machine is assigned
  to it.

**Web interface**

- **FR-010**: System MUST provide a web interface for managing the address-assignment
  service (ranges, options, static reservations, live lease view, service start/stop).
- **FR-011**: System MUST provide a web interface for managing boot files (upload,
  replace, delete, list) and viewing transfer activity.
- **FR-012**: System MUST provide a web interface for machines, profiles, and
  provisioning status, with in-progress installs updating without manual reload.
- **FR-013**: Web interface access MUST require authenticated operator login; all
  state-changing actions MUST be attributed to the logged-in operator.

**Operations & audit**

- **FR-014**: System MUST record every provisioning event (address offer, file
  transfer, answer delivery, install completion/failure) with machine identity,
  timestamp, and outcome, and retain history for later audit.
- **FR-015**: System MUST detect and surface provisioning failures, marking stalled
  sessions as failed after a defined timeout rather than leaving them in progress.
- **FR-016**: System MUST require explicit operator action to enable the
  address-assignment service and MUST surface evidence of competing address services on
  the network.
- **FR-017**: System MUST validate all uploaded boot files (size limits, expected file
  types) and all interface input before accepting them.
- **FR-018**: Installation answers containing credentials MUST be generated per machine
  at boot time and MUST NOT be stored or served as long-lived plaintext artifacts.

### Key Entities

- **Machine**: A provisioning target identified by hardware (MAC) address; carries a
  human-friendly name, firmware type, assigned profile, optional static address
  reservation, and current provisioning status.
- **Installation Profile**: A named, versioned definition of an unattended Ubuntu-family
  installation: release, disk layout, initial access, packages, network settings,
  post-install steps. Assigned to zero or more machines.
- **DHCP Configuration**: Network range(s), lease options, and boot options served by
  the address-assignment service; includes static **Reservations** tied to machines.
- **Lease**: A currently or recently assigned address with its machine identity and
  expiry.
- **Boot Artifact**: A served boot file (boot loader, kernel, initial image) with its
  Ubuntu release association and integrity information.
- **Provisioning Session**: One end-to-end bootstrap attempt for a machine — ordered
  events from first address request to install success/failure, with captured evidence.
- **Operator**: An authenticated user of the web interface; the actor recorded on all
  state-changing actions.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can register a new machine, assign an existing profile, and
  have the target machine's unattended installation underway within 5 minutes of
  starting, with zero interaction on the target's console.
- **SC-002**: A standard Ubuntu server installation driven by the system completes
  unattended in under 30 minutes on typical server hardware with local-network artifact
  delivery.
- **SC-003**: 10 machines can be provisioned concurrently with no cross-contamination
  of identities, addresses, or installation answers.
- **SC-004**: 100% of provisioning attempts (success or failure) are visible in the
  interface with a per-phase timeline; in-progress phases appear within 5 seconds of
  occurring.
- **SC-005**: An operator can diagnose a failed installation from the interface alone
  (no physical or console access to the target) in 95% of failure cases seeded during
  acceptance testing.
- **SC-006**: Invalid profile or DHCP configurations are rejected at save time in 100%
  of seeded-error test cases, and a booting machine is never served an invalid
  configuration.

## Assumptions

- The application runs on a Linux host attached to the same network segment as the
  target machines, operated by a technical infrastructure operator.
- The system's address-assignment service is authoritative on an isolated provisioning
  network; coexisting with a foreign DHCP service in proxy mode is out of scope for v1
  (conflict detection is in scope).
- Target OS scope is current Ubuntu-family LTS releases that support unattended
  installation (e.g., Ubuntu Server 22.04/24.04); other distributions are out of scope.
- Single-site, single-instance deployment; expected fleet size is up to a few hundred
  machines with tens of concurrent installs.
- Operator accounts are managed locally by the application (no external identity
  provider required for v1).
- Initial seeding of installer boot files may be performed by the operator via the web
  interface; the system does not need to download Ubuntu release media automatically
  in v1.
- Installed machines' post-install lifecycle (configuration management, monitoring) is
  out of scope; the product's responsibility ends at verified install completion.
