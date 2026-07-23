package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProfileUsecaseListAndGet(t *testing.T) {
	uc, _ := newProfileUC(t, nil)
	ctx := context.Background()
	if _, err := uc.Create(ctx, validInput()); err != nil {
		t.Fatal(err)
	}
	list, total, err := uc.List(ctx, 1, 50)
	if err != nil || total != 1 || len(list) != 1 {
		t.Errorf("list: %v total=%d", err, total)
	}
	if _, err := uc.Get(ctx, list[0].ID); err != nil {
		t.Errorf("get: %v", err)
	}
}

func TestSweeperRunStopsOnContextCancel(t *testing.T) {
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())
	sweeper := NewSessionSweeper(sessions, machines, events, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sweeper.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

// --- failure injection -------------------------------------------------------
//
// The integration suite runs against a real Postgres, so it can only exercise
// the happy paths and "not found". These fakes fail on demand to reach the
// storage-error branches, which are precisely the ones that must not panic or
// abort a sweep half-way.

type flakySessionRepo struct {
	*fakeSessionRepo
	listErr   error
	finishErr error
}

func (f *flakySessionRepo) ListActiveOlderThan(ctx context.Context, cutoff time.Time) ([]*Session, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.fakeSessionRepo.ListActiveOlderThan(ctx, cutoff)
}

func (f *flakySessionRepo) Finish(ctx context.Context, id string, state SessionState, phase string, ev map[string]any) error {
	if f.finishErr != nil {
		return f.finishErr
	}
	return f.fakeSessionRepo.Finish(ctx, id, state, phase, ev)
}

type flakyMachineRepo struct {
	*fakeMachineRepo
	updateErr error
}

func (f *flakyMachineRepo) Update(ctx context.Context, id string, u MachineUpdate) (*Machine, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.fakeMachineRepo.Update(ctx, id, u)
}

// staleSetup seeds one machine with an active session backdated past the
// timeout, so a sweep will always pick it up.
func staleSetup(t *testing.T, mac string) (*fakeMachineRepo, *fakeSessionRepo, *Machine, *Session) {
	t.Helper()
	ctx := context.Background()
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	m, err := machines.Create(ctx, &Machine{MAC: mac, Name: "stale-" + mac, State: StateInstalling})
	if err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	sess, err := sessions.Create(ctx, &Session{MachineID: m.ID, ProfileID: "p1"})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	sessions.byID[sess.ID].StartedAt = time.Now().Add(-2 * time.Hour)
	return machines, sessions, m, sess
}

func TestSweeperSurvivesListFailure(t *testing.T) {
	machines, sessions, m, sess := staleSetup(t, "52:54:00:0c:0c:0c")
	flaky := &flakySessionRepo{fakeSessionRepo: sessions, listErr: errors.New("db down")}
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	// Must return quietly, touching nothing, rather than panicking on nil stale.
	NewSessionSweeper(flaky, machines, events, time.Hour, testLogger()).sweep(context.Background())

	got, _ := sessions.GetByID(context.Background(), sess.ID)
	if got.State != SessionActive {
		t.Errorf("session state = %s, want it untouched (active) when the list fails", got.State)
	}
	gotM, _ := machines.GetByID(context.Background(), m.ID)
	if gotM.State != StateInstalling {
		t.Errorf("machine state = %s, want it untouched when the list fails", gotM.State)
	}
}

func TestSweeperSkipsSessionItCannotFinish(t *testing.T) {
	machines, sessions, m, _ := staleSetup(t, "52:54:00:0d:0d:0d")
	flaky := &flakySessionRepo{fakeSessionRepo: sessions, finishErr: errors.New("db down")}
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	NewSessionSweeper(flaky, machines, events, time.Hour, testLogger()).sweep(context.Background())

	// The machine must not be failed off the back of a session we could not
	// actually mark stale, or state would diverge from the session table.
	gotM, _ := machines.GetByID(context.Background(), m.ID)
	if gotM.State != StateInstalling {
		t.Errorf("machine state = %s, want installing when the session could not be finished", gotM.State)
	}
}

func TestSweeperSkipsSessionWithMissingMachine(t *testing.T) {
	ctx := context.Background()
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	sess, err := sessions.Create(ctx, &Session{MachineID: "ghost", ProfileID: "p1"})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	sessions.byID[sess.ID].StartedAt = time.Now().Add(-2 * time.Hour)
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	NewSessionSweeper(sessions, machines, events, time.Hour, testLogger()).sweep(ctx)

	// The session is still swept even though its machine is gone.
	got, _ := sessions.GetByID(ctx, sess.ID)
	if got.State != SessionStale {
		t.Errorf("session state = %s, want stale even with no machine to fail", got.State)
	}
}

func TestSweeperSweepsOnWhenMachineUpdateFails(t *testing.T) {
	machines, sessions, _, sess := staleSetup(t, "52:54:00:0e:0e:0e")
	flaky := &flakyMachineRepo{fakeMachineRepo: machines, updateErr: errors.New("db down")}
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())

	NewSessionSweeper(sessions, flaky, events, time.Hour, testLogger()).sweep(context.Background())

	// A failed machine update is logged, not fatal: the session is still stale.
	got, _ := sessions.GetByID(context.Background(), sess.ID)
	if got.State != SessionStale {
		t.Errorf("session state = %s, want stale despite the machine update failing", got.State)
	}
}
