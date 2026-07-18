package biz

import (
	"context"
	"testing"
)

func newSessionUC(t *testing.T) (*SessionUsecase, *MachineUsecase, *fakeSessionRepo, *fakeMachineRepo) {
	t.Helper()
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())
	machineUC := NewMachineUsecase(machines, sessions, newFakeProfileRepo(),
		&fakeGate{enabled: true}, events, testLogger())
	return NewSessionUsecase(sessions, machines, events, testLogger()), machineUC, sessions, machines
}

func armMachine(t *testing.T, uc *MachineUsecase) *Machine {
	t.Helper()
	m, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:cc:dd:01", Name: "target", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	armed, err := uc.Provision(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return armed
}

func TestReportInstallSuccess(t *testing.T) {
	sessionUC, machineUC, sessions, machines := newSessionUC(t)
	armed := armMachine(t, machineUC)

	if err := sessionUC.ReportInstall(context.Background(), armed.ActiveSessionID, "ok", ""); err != nil {
		t.Fatalf("report: %v", err)
	}
	sess, err := sessions.GetByID(context.Background(), armed.ActiveSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.State != SessionCompleted {
		t.Errorf("session state = %s, want completed", sess.State)
	}
	m, err := machines.GetByID(context.Background(), armed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != StateInstalled {
		t.Errorf("machine state = %s, want installed", m.State)
	}
	// Idempotent: a second report must not error or change state.
	if err := sessionUC.ReportInstall(context.Background(), armed.ActiveSessionID, "error", ""); err != nil {
		t.Fatalf("second report: %v", err)
	}
	sess2, _ := sessions.GetByID(context.Background(), armed.ActiveSessionID)
	if sess2.State != SessionCompleted {
		t.Error("terminal state must not change on repeat report")
	}
}

func TestReportInstallFailure(t *testing.T) {
	sessionUC, machineUC, sessions, machines := newSessionUC(t)
	armed := armMachine(t, machineUC)

	if err := sessionUC.ReportInstall(context.Background(), armed.ActiveSessionID, "error", "curtin: disk not found"); err != nil {
		t.Fatalf("report: %v", err)
	}
	sess, _ := sessions.GetByID(context.Background(), armed.ActiveSessionID)
	if sess.State != SessionFailed || sess.FailurePhase == "" {
		t.Errorf("session = %+v, want failed with failure phase", sess)
	}
	m, _ := machines.GetByID(context.Background(), armed.ID)
	if m.State != StateFailed {
		t.Errorf("machine state = %s, want failed", m.State)
	}
}

func TestReportInstallUnknownSession(t *testing.T) {
	sessionUC, _, _, _ := newSessionUC(t)
	if err := sessionUC.ReportInstall(context.Background(), "nope", "ok", ""); err == nil {
		t.Error("unknown session must error")
	}
}
