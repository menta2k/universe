package biz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeEventRepo struct {
	mu     sync.Mutex
	stored []Event
	failed bool
}

func (f *fakeEventRepo) Store(_ context.Context, e Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return errors.New("db down")
	}
	f.stored = append(f.stored, e)
	return nil
}

type fakePublisher struct {
	mu        sync.Mutex
	published []Event
	failed    bool
}

func (f *fakePublisher) Publish(_ context.Context, e Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return errors.New("bus down")
	}
	f.published = append(f.published, e)
	return nil
}

// Recording evidence must never break the boot path, so a bad event or a dead
// store/bus is logged and swallowed rather than propagated. These cover that
// contract; the integration suite cannot, since its Postgres and bus are both
// healthy by construction.
func TestRecordDropsInvalidEvent(t *testing.T) {
	repo := &fakeEventRepo{}
	pub := &fakePublisher{}
	rec := NewEventRecorder(repo, pub, testLogger())

	// No phase => fails Validate => never reaches the store or the bus.
	rec.Record(context.Background(), Event{MachineMAC: "52:54:00:aa:bb:cc", Outcome: OutcomeOK})

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.stored) != 0 {
		t.Errorf("stored %d invalid events, want 0", len(repo.stored))
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.published) != 0 {
		t.Errorf("published %d invalid events, want 0", len(pub.published))
	}
}

func TestRecordSurvivesStoreAndPublishFailure(t *testing.T) {
	repo := &fakeEventRepo{failed: true}
	pub := &fakePublisher{failed: true}
	rec := NewEventRecorder(repo, pub, testLogger())

	// Both sinks are down: Record must still return normally.
	rec.Record(context.Background(), Event{
		SessionID: "s1", MachineMAC: "52:54:00:aa:bb:cc",
		Phase: PhaseDHCPDiscover, Outcome: OutcomeOK,
	})
}

func TestRecordStoresAndPublishes(t *testing.T) {
	repo := &fakeEventRepo{}
	pub := &fakePublisher{}
	rec := NewEventRecorder(repo, pub, testLogger())

	e := Event{
		SessionID:  "s1",
		MachineMAC: "52:54:00:aa:bb:cc",
		Phase:      PhaseDHCPDiscover,
		Outcome:    OutcomeOK,
		Detail:     map[string]any{"iface": "eth1"},
	}
	rec.Record(context.Background(), e)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.stored) != 1 {
		t.Fatalf("stored %d events, want 1", len(repo.stored))
	}
	if repo.stored[0].Time.IsZero() {
		t.Error("Record must stamp event time")
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.published) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.published))
	}
}

func TestRecordSurvivesStoreFailure(t *testing.T) {
	repo := &fakeEventRepo{failed: true}
	pub := &fakePublisher{}
	rec := NewEventRecorder(repo, pub, testLogger())

	// Must not panic and must still publish for live UI.
	rec.Record(context.Background(), Event{Phase: PhaseTFTPTransfer, Outcome: OutcomeError})

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.published) != 1 {
		t.Error("publish should happen even when store fails")
	}
}

func TestEventValidation(t *testing.T) {
	e := Event{Phase: "not-a-phase", Outcome: OutcomeOK, Time: time.Now()}
	if err := e.Validate(); err == nil {
		t.Error("expected error for unknown phase")
	}
	e = Event{Phase: PhaseInstallReport, Outcome: "nope"}
	if err := e.Validate(); err == nil {
		t.Error("expected error for unknown outcome")
	}
}
