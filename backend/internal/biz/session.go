package biz

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SessionState mirrors the session_state SQL enum.
type SessionState string

const (
	SessionActive    SessionState = "active"
	SessionCompleted SessionState = "completed"
	SessionFailed    SessionState = "failed"
	SessionStale     SessionState = "stale"
)

// Session is one end-to-end bootstrap attempt (correlation ID for the boot).
type Session struct {
	ID             string
	MachineID      string
	ProfileID      string
	ProfileVersion int
	State          SessionState
	StartedAt      time.Time
	EndedAt        time.Time
	FailurePhase   string
	Evidence       map[string]any
}

type SessionRepo interface {
	Create(ctx context.Context, s *Session) (*Session, error)
	GetByID(ctx context.Context, id string) (*Session, error)
	GetActiveByMachine(ctx context.Context, machineID string) (*Session, error)
	// Finish transitions active -> terminal state, recording the failure
	// phase and merging evidence. Finishing a non-active session is a no-op
	// (idempotent report callbacks).
	Finish(ctx context.Context, id string, state SessionState, failurePhase string, evidence map[string]any) error
	// ListActiveOlderThan returns active sessions started before the cutoff.
	ListActiveOlderThan(ctx context.Context, cutoff time.Time) ([]*Session, error)
}

// SessionUsecase finalizes sessions from install reports (boot path).
type SessionUsecase struct {
	sessions SessionRepo
	machines MachineRepo
	events   *EventRecorder
	log      *slog.Logger
}

func NewSessionUsecase(sessions SessionRepo, machines MachineRepo, events *EventRecorder, log *slog.Logger) *SessionUsecase {
	return &SessionUsecase{sessions: sessions, machines: machines, events: events, log: log}
}

// ReportInstall handles the installer callback (contracts §3 /boot/report):
// status "ok" completes the session, anything else fails it. Idempotent.
func (u *SessionUsecase) ReportInstall(ctx context.Context, sessionID, status, logTail string) error {
	sess, err := u.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	m, err := u.machines.GetByID(ctx, sess.MachineID)
	if err != nil {
		return fmt.Errorf("load machine: %w", err)
	}

	ok := status == "ok"
	target, machineState, phase := SessionCompleted, StateInstalled, PhaseSessionCompleted
	if !ok {
		target, machineState, phase = SessionFailed, StateFailed, PhaseSessionFailed
	}
	evidence := map[string]any{"report_status": status}
	if logTail != "" {
		evidence["log_tail"] = logTail
	}
	failurePhase := ""
	if !ok {
		failurePhase = string(PhaseInstallReport)
	}
	if err := u.sessions.Finish(ctx, sess.ID, target, failurePhase, evidence); err != nil {
		return fmt.Errorf("finish session: %w", err)
	}
	if _, err := u.machines.Update(ctx, m.ID, MachineUpdate{State: &machineState}); err != nil {
		return fmt.Errorf("update machine state: %w", err)
	}
	outcome := OutcomeOK
	if !ok {
		outcome = OutcomeError
	}
	u.events.Record(ctx, Event{
		SessionID: sess.ID, MachineMAC: m.MAC, Phase: PhaseInstallReport, Outcome: outcome,
		Detail: map[string]any{"status": status},
	})
	u.events.Record(ctx, Event{
		SessionID: sess.ID, MachineMAC: m.MAC, Phase: phase, Outcome: OutcomeOK,
	})
	u.log.Info("install reported", "machine", m.Name, "session", sess.ID, "status", status)
	return nil
}
