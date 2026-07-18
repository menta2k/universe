// Package biz holds the domain layer: entities, use cases, and the repository
// interfaces implemented by internal/data (Constitution: repository pattern).
package biz

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Phase identifies a provisioning pipeline step (event_phase enum in SQL).
type Phase string

const (
	PhaseDHCPDiscover       Phase = "dhcp_discover"
	PhaseDHCPOffer          Phase = "dhcp_offer"
	PhaseDHCPAck            Phase = "dhcp_ack"
	PhaseLeaseGranted       Phase = "lease_granted"
	PhaseLeaseExpired       Phase = "lease_expired"
	PhaseTFTPTransfer       Phase = "tftp_transfer"
	PhaseIPXEScript         Phase = "ipxe_script"
	PhaseFileServed         Phase = "file_served"
	PhaseSeedServed         Phase = "seed_served"
	PhaseInstallReport      Phase = "install_report"
	PhaseSessionCompleted   Phase = "session_completed"
	PhaseSessionFailed      Phase = "session_failed"
	PhaseSessionStale       Phase = "session_stale"
	PhaseUnknownMachine     Phase = "unknown_machine"
	PhaseForeignDHCP        Phase = "foreign_dhcp_detected"
	PhaseConfigChange       Phase = "config_change"
)

// Outcome is the result of an event (event_outcome enum in SQL).
type Outcome string

const (
	OutcomeOK     Outcome = "ok"
	OutcomeError  Outcome = "error"
	OutcomeDenied Outcome = "denied"
)

var validPhases = map[Phase]bool{
	PhaseDHCPDiscover: true, PhaseDHCPOffer: true, PhaseDHCPAck: true,
	PhaseLeaseGranted: true, PhaseLeaseExpired: true, PhaseTFTPTransfer: true,
	PhaseIPXEScript: true, PhaseFileServed: true, PhaseSeedServed: true,
	PhaseInstallReport: true, PhaseSessionCompleted: true, PhaseSessionFailed: true,
	PhaseSessionStale: true, PhaseUnknownMachine: true, PhaseForeignDHCP: true,
	PhaseConfigChange: true,
}

var validOutcomes = map[Outcome]bool{OutcomeOK: true, OutcomeError: true, OutcomeDenied: true}

// Event is one durable, attributable provisioning record (FR-014).
type Event struct {
	Time       time.Time
	SessionID  string // empty for events with no session (unknown machine, config change)
	MachineMAC string
	Phase      Phase
	Outcome    Outcome
	Detail     map[string]any
}

// Validate checks enum membership before the event reaches storage.
func (e Event) Validate() error {
	if !validPhases[e.Phase] {
		return fmt.Errorf("unknown event phase %q", e.Phase)
	}
	if !validOutcomes[e.Outcome] {
		return fmt.Errorf("unknown event outcome %q", e.Outcome)
	}
	return nil
}

// EventRepo persists events to the hypertable.
type EventRepo interface {
	Store(ctx context.Context, e Event) error
}

// EventPublisher fans events out to live subscribers (SSE via Valkey pub/sub).
type EventPublisher interface {
	Publish(ctx context.Context, e Event) error
}

// EventRecorder is the single write path for provisioning events.
type EventRecorder struct {
	repo EventRepo
	pub  EventPublisher
	log  *slog.Logger
}

func NewEventRecorder(repo EventRepo, pub EventPublisher, log *slog.Logger) *EventRecorder {
	return &EventRecorder{repo: repo, pub: pub, log: log}
}

// Record stamps, validates, persists, and publishes an event. Storage or
// publish failures are logged, never propagated: recording evidence must not
// break the boot path itself.
func (r *EventRecorder) Record(ctx context.Context, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if err := e.Validate(); err != nil {
		r.log.Error("dropping invalid event", "err", err, "phase", e.Phase)
		return
	}
	if err := r.repo.Store(ctx, e); err != nil {
		r.log.Error("store event failed", "err", err, "phase", e.Phase, "mac", e.MachineMAC)
	}
	if err := r.pub.Publish(ctx, e); err != nil {
		r.log.Error("publish event failed", "err", err, "phase", e.Phase)
	}
}
