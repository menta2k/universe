package biz

import (
	"context"
	"testing"
	"time"
)

func TestSweeperMarksStaleSessions(t *testing.T) {
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	// Create a machine and an active session, backdated past the timeout.
	m, _ := machines.Create(context.Background(), &Machine{
		MAC: "52:54:00:0a:0a:0a", Name: "stuck", State: StateInstalling})
	sess, _ := sessions.Create(context.Background(), &Session{MachineID: m.ID, ProfileID: "p1"})
	sessions.byID[sess.ID].StartedAt = time.Now().Add(-2 * time.Hour)

	sweeper := NewSessionSweeper(sessions, machines, events, time.Hour, testLogger())
	sweeper.sweep(context.Background())

	got, _ := sessions.GetByID(context.Background(), sess.ID)
	if got.State != SessionStale {
		t.Errorf("session state = %s, want stale", got.State)
	}
	gotM, _ := machines.GetByID(context.Background(), m.ID)
	if gotM.State != StateFailed {
		t.Errorf("machine state = %s, want failed", gotM.State)
	}
}

func TestSweeperLeavesFreshSessions(t *testing.T) {
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	m, _ := machines.Create(context.Background(), &Machine{
		MAC: "52:54:00:0b:0b:0b", Name: "fresh", State: StateInstalling})
	sess, _ := sessions.Create(context.Background(), &Session{MachineID: m.ID, ProfileID: "p1"})

	sweeper := NewSessionSweeper(sessions, machines, events, time.Hour, testLogger())
	sweeper.sweep(context.Background())

	got, _ := sessions.GetByID(context.Background(), sess.ID)
	if got.State != SessionActive {
		t.Errorf("fresh session should stay active, got %s", got.State)
	}
}
