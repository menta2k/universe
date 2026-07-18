package biz

import (
	"context"
	"log/slog"
	"time"
)

// SessionSweeper marks long-running active sessions as stale (FR-015). It runs
// as a background ticker under the app lifecycle.
type SessionSweeper struct {
	sessions SessionRepo
	machines MachineRepo
	events   *EventRecorder
	timeout  time.Duration
	interval time.Duration
	log      *slog.Logger
}

func NewSessionSweeper(sessions SessionRepo, machines MachineRepo, events *EventRecorder, timeout time.Duration, log *slog.Logger) *SessionSweeper {
	interval := max(timeout/4, time.Minute)
	return &SessionSweeper{
		sessions: sessions, machines: machines, events: events,
		timeout: timeout, interval: interval, log: log,
	}
}

// Run sweeps until ctx is cancelled.
func (s *SessionSweeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// sweep marks sessions started before the cutoff as stale and fails their
// machines, capturing the last completed phase as evidence.
func (s *SessionSweeper) sweep(ctx context.Context) {
	cutoff := time.Now().Add(-s.timeout)
	stale, err := s.sessions.ListActiveOlderThan(ctx, cutoff)
	if err != nil {
		s.log.Error("sweep: list stale sessions failed", "err", err)
		return
	}
	for _, sess := range stale {
		evidence := map[string]any{"reason": "stale timeout", "timeout": s.timeout.String()}
		if err := s.sessions.Finish(ctx, sess.ID, SessionStale, string(PhaseSessionStale), evidence); err != nil {
			s.log.Error("sweep: finish session failed", "err", err, "session", sess.ID)
			continue
		}
		m, err := s.machines.GetByID(ctx, sess.MachineID)
		if err != nil {
			continue
		}
		failed := StateFailed
		if _, err := s.machines.Update(ctx, m.ID, MachineUpdate{State: &failed}); err != nil {
			s.log.Error("sweep: mark machine failed", "err", err, "machine", m.ID)
		}
		s.events.Record(ctx, Event{
			SessionID: sess.ID, MachineMAC: m.MAC, Phase: PhaseSessionStale, Outcome: OutcomeError,
			Detail: evidence,
		})
		s.log.Warn("session marked stale", "session", sess.ID, "machine", m.Name)
	}
}
