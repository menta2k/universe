<!--
Sync Impact Report
- Version change: (template) → 1.0.0
- Modified principles: all placeholders replaced (initial ratification)
  - I. Declarative, Reproducible Provisioning (new)
  - II. Unattended & Idempotent Netinstall (new)
  - III. Test-First & Verified Bootstrap (new, NON-NEGOTIABLE)
  - IV. Secure by Default (new)
  - V. Observability & Auditability (new)
- Added sections: Infrastructure & Technology Constraints; Development Workflow & Quality Gates
- Removed sections: none
- Templates requiring updates:
  - ✅ .specify/templates/plan-template.md (generic "Constitution Check" gate — derives gates from this file at plan time; no edit required)
  - ✅ .specify/templates/spec-template.md (no constitution-specific references; compatible)
  - ✅ .specify/templates/tasks-template.md (task categories already accommodate test-first and polish phases; compatible)
- Follow-up TODOs: none
-->

# Universe Constitution

Universe is an application that bootstraps Linux servers over the network using
distribution netinstall mechanisms (PXE/iPXE boot, DHCP/TFTP/HTTP delivery, and
distro-native unattended installers such as preseed, kickstart, and cloud-init /
autoinstall).

## Core Principles

### I. Declarative, Reproducible Provisioning

Every server MUST be fully described by a declarative definition (machine identity,
network, disk layout, target distribution/version, packages, post-install roles) stored
in version control. The same definition MUST produce a functionally identical server on
every run. Manual, undocumented changes to provisioning artifacts (boot images, installer
answer files, templates) are prohibited: artifacts MUST be generated from the definitions,
never hand-edited in place. All configuration data is treated as immutable — changes
create new versioned definitions rather than mutating deployed state.

Rationale: reproducibility is the entire value of automated bootstrapping; a server that
cannot be rebuilt from its definition is a liability.

### II. Unattended & Idempotent Netinstall

The bootstrap pipeline MUST run end-to-end without human interaction: from PXE/iPXE boot,
through installer answer-file execution, to first-boot configuration. Every step MUST be
idempotent — re-running a bootstrap against the same target either converges to the same
result or fails fast with a clear error; it MUST never leave a server in a partially
provisioned, ambiguous state. Failures MUST be recoverable by simply re-triggering the
bootstrap. Generated installer configurations (preseed/kickstart/autoinstall) MUST be
validated against their schema before being served to a target machine.

Rationale: netinstall targets are frequently remote and headless; any step that requires
a console or produces non-repeatable state defeats the purpose.

### III. Test-First & Verified Bootstrap (NON-NEGOTIABLE)

TDD is mandatory: tests are written first, fail (RED), then implementation makes them pass
(GREEN), then refactor. Minimum 80% coverage. Three test tiers are ALL required:
unit tests for template rendering, validation, and core logic; integration tests for the
API/DHCP/TFTP/HTTP serving paths; and end-to-end bootstrap tests that netinstall a real
target (VM-based) and assert the resulting server matches its definition. Every bootstrap
run in production MUST end with automated post-install verification (SSH reachability,
expected services, disk layout, network identity) before the server is marked ready.

Rationale: a provisioning bug multiplies across every server built; verification is the
only proof a bootstrap actually succeeded.

### IV. Secure by Default

No hardcoded secrets anywhere — credentials, tokens, and root passwords MUST come from
environment variables or a secret manager, be validated as present at startup, and be
injected into targets only at provision time. Installer answer files served over the
network MUST NOT contain long-lived plaintext credentials; per-provision one-time secrets
MUST be used and rotated after enrollment. All downloaded installation media and packages
MUST be verified against checksums/signatures before use. Bootstrapped servers MUST come
up hardened: SSH key-only authentication, no default passwords, minimal package set,
firewall enabled. All operator-facing endpoints MUST validate input, enforce
authentication/authorization, and apply rate limiting.

Rationale: the bootstrap system holds the keys to every server it builds; it is the
highest-value target in the fleet.

### V. Observability & Auditability

Every provisioning event (boot request, artifact served, install phase, verification
result) MUST be recorded as structured logs with the target machine identity, timestamp,
and outcome. Every state-changing action MUST be attributable: who or what triggered the
bootstrap, with which definition version. Failed bootstraps MUST capture enough evidence
(installer logs, last served artifacts) to diagnose without physical access to the target.
Error messages shown to operators MUST be actionable and MUST NOT leak secrets.

Rationale: netinstall failures happen on machines with no OS to inspect; the control plane
must carry the whole story.

## Infrastructure & Technology Constraints

- Boot chain: PXE/iPXE with DHCP/TFTP for initial boot and HTTP(S) for all subsequent
  artifact delivery; HTTPS MUST be used wherever target firmware supports it.
- Unattended install mechanisms MUST be distro-native (Debian preseed, RHEL-family
  kickstart, Ubuntu autoinstall/cloud-init) rather than post-hoc imaging hacks.
- Server definitions and templates live in the repository; rendered artifacts are
  build outputs and MUST NOT be committed.
- Data access goes through a repository-pattern interface; storage backends are
  swappable and mockable in tests.
- APIs use a consistent response envelope (status, data, error, pagination metadata).
- Code style follows the project's standing rules: immutable data patterns, small
  focused files (<800 lines) and functions (<50 lines), no silent error swallowing,
  validation at all system boundaries.

## Development Workflow & Quality Gates

- Research first: before implementing new functionality, search for existing
  battle-tested implementations and libraries; prefer adopting proven approaches.
- Plan before code: complex features require an implementation plan (spec → plan →
  tasks) before any code is written.
- Every change follows RED → GREEN → REFACTOR and must keep coverage ≥ 80%.
- Code review is mandatory for all changes; CRITICAL and HIGH findings block merge.
- Security review is mandatory before commit for any code touching secrets, served
  artifacts, authentication, or user input.
- Commits follow Conventional Commits (`feat:`, `fix:`, `refactor:`, ...).
- A change is not "done" until the end-to-end bootstrap test passes against a real
  (virtualized) netinstall target.

## Governance

This constitution supersedes all other practices for the Universe project. Amendments
require a documented rationale, a semantic version bump of this file, and propagation to
dependent templates (`plan`, `spec`, `tasks`) in the same change. Versioning policy:
MAJOR for principle removals or redefinitions, MINOR for new principles or materially
expanded guidance, PATCH for clarifications. All PRs and plan-phase Constitution Checks
MUST verify compliance with these principles; any violation must be explicitly justified
in the plan's Complexity Tracking section or the change rejected. Compliance is reviewed
at plan time and again at code review.

**Version**: 1.0.0 | **Ratified**: 2026-07-18 | **Last Amended**: 2026-07-18
