package biz

import (
	"context"
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

func TestValidationErrorMessage(t *testing.T) {
	e := &ValidationError{Fields: map[string]string{"mac": "bad"}}
	if e.Error() == "" {
		t.Error("ValidationError.Error() should be non-empty")
	}
}
