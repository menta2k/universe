package biz

import (
	"context"
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
