package biz

import (
	"context"
	"errors"
	"testing"
)

func TestMachineListAndUpdate(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	ctx := context.Background()
	m, err := uc.Register(ctx, RegisterInput{MAC: "52:54:00:11:11:11", Name: "host-a"})
	if err != nil {
		t.Fatal(err)
	}

	// Update assigning a profile moves new -> ready.
	pid := "p1"
	updated, err := uc.Update(ctx, m.ID, MachineUpdate{ProfileID: &pid})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.State != StateReady || updated.ProfileID != "p1" {
		t.Errorf("assign profile: got state=%s profile=%s", updated.State, updated.ProfileID)
	}

	// Update with an unknown profile is rejected.
	bad := "ghost"
	if _, err := uc.Update(ctx, m.ID, MachineUpdate{ProfileID: &bad}); err == nil {
		t.Error("unknown profile update must fail")
	}

	// Rename.
	name := "host-a-renamed"
	renamed, err := uc.Update(ctx, m.ID, MachineUpdate{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "host-a-renamed" {
		t.Errorf("rename failed: %s", renamed.Name)
	}

	list, total, err := uc.List(ctx, MachineFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("list total=%d len=%d, want 1", total, len(list))
	}
}

func TestObserveFirmware(t *testing.T) {
	uc, repo, _ := newMachineUC(t, true)
	ctx := context.Background()
	m, _ := uc.Register(ctx, RegisterInput{MAC: "52:54:00:22:22:22", Name: "fw"})

	// Unknown firmware is not persisted.
	uc.ObserveFirmware(ctx, m.ID, FirmwareUnknown)
	if repo.byID[m.ID].Firmware != FirmwareUnknown {
		t.Error("unknown firmware should be a no-op")
	}
	// A concrete firmware is saved.
	uc.ObserveFirmware(ctx, m.ID, FirmwareUEFI)
	if repo.byID[m.ID].Firmware != FirmwareUEFI {
		t.Errorf("firmware = %s, want uefi_x64", repo.byID[m.ID].Firmware)
	}
}

func TestRecordUnknownBootEmitsEvent(t *testing.T) {
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	repo := &fakeEventRepo{}
	events := NewEventRecorder(repo, &fakePublisher{}, testLogger())
	uc := NewMachineUsecase(machines, sessions, newFakeProfileRepo(), &fakeGate{enabled: true}, events, testLogger())

	uc.RecordUnknownBoot(context.Background(), "52:54:00:ff:ff:fe")
	if len(repo.stored) != 1 || repo.stored[0].Phase != PhaseUnknownMachine {
		t.Errorf("expected one unknown_machine event, got %+v", repo.stored)
	}
	if repo.stored[0].Outcome != OutcomeDenied {
		t.Errorf("unknown boot outcome = %s, want denied", repo.stored[0].Outcome)
	}
}

// seedMachine registers one machine for the lifecycle tests below.
func seedMachine(t *testing.T, uc *MachineUsecase) *Machine {
	t.Helper()
	m, err := uc.Register(context.Background(), RegisterInput{MAC: "52:54:00:ab:cd:ef", Name: "node-a"})
	if err != nil {
		t.Fatalf("seed machine: %v", err)
	}
	return m
}

func TestCancelWithoutActiveSession(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	m := seedMachine(t, uc)
	if _, err := uc.Cancel(context.Background(), m.ID); !errors.Is(err, ErrNoActiveSession) {
		t.Errorf("cancel with no session: err = %v, want ErrNoActiveSession", err)
	}
}

func TestCancelUnknownMachine(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	if _, err := uc.Cancel(context.Background(), "nope"); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("cancel unknown machine: err = %v, want ErrEntityNotFound", err)
	}
}

func TestGetAttachesActiveSession(t *testing.T) {
	ctx := context.Background()
	uc, _, sessions := newMachineUC(t, true)
	m := seedMachine(t, uc)

	// With no session in flight the field stays empty rather than erroring.
	got, err := uc.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ActiveSessionID != "" {
		t.Errorf("active_session_id = %q, want empty with no session", got.ActiveSessionID)
	}

	sess, err := sessions.Create(ctx, &Session{MachineID: m.ID})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if got, err = uc.Get(ctx, m.ID); err != nil {
		t.Fatalf("get with session: %v", err)
	}
	if got.ActiveSessionID != sess.ID {
		t.Errorf("active_session_id = %q, want %q", got.ActiveSessionID, sess.ID)
	}

	if _, err := uc.Get(ctx, "nope"); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("get unknown machine: err = %v, want ErrEntityNotFound", err)
	}
}

func TestDeleteBlockedWhileInstalling(t *testing.T) {
	ctx := context.Background()
	uc, machines, sessions := newMachineUC(t, true)
	m := seedMachine(t, uc)

	if _, err := sessions.Create(ctx, &Session{MachineID: m.ID}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := uc.Delete(ctx, m.ID); !errors.Is(err, ErrSessionConflict) {
		t.Errorf("delete while installing: err = %v, want ErrSessionConflict", err)
	}
	if len(machines.deleted) != 0 {
		t.Errorf("machine must survive a blocked delete, got deleted=%v", machines.deleted)
	}

	// Once the session is finished the machine is deletable again.
	active, err := sessions.GetActiveByMachine(ctx, m.ID)
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if err := sessions.Finish(ctx, active.ID, SessionFailed, "cancelled", nil); err != nil {
		t.Fatalf("finish session: %v", err)
	}
	if err := uc.Delete(ctx, m.ID); err != nil {
		t.Errorf("delete after session finished: %v", err)
	}
	if len(machines.deleted) != 1 || machines.deleted[0] != m.ID {
		t.Errorf("deleted = %v, want [%s]", machines.deleted, m.ID)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	e := &ValidationError{Fields: map[string]string{"mac": "bad"}}
	if e.Error() == "" {
		t.Error("ValidationError.Error() should be non-empty")
	}
}
