package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// --- fakes -----------------------------------------------------------------

type fakeMachineRepo struct {
	byID    map[string]*Machine
	nextID  int
	deleted []string
}

func newFakeMachineRepo() *fakeMachineRepo {
	return &fakeMachineRepo{byID: map[string]*Machine{}}
}

func (f *fakeMachineRepo) GetByID(_ context.Context, id string) (*Machine, error) {
	m, ok := f.byID[id]
	if !ok {
		return nil, ErrEntityNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMachineRepo) GetByMAC(_ context.Context, mac string) (*Machine, error) {
	for _, m := range f.byID {
		if m.MAC == mac {
			cp := *m
			return &cp, nil
		}
	}
	return nil, ErrEntityNotFound
}

func (f *fakeMachineRepo) List(_ context.Context, _ MachineFilter) ([]*Machine, int64, error) {
	out := make([]*Machine, 0, len(f.byID))
	for _, m := range f.byID {
		cp := *m
		out = append(out, &cp)
	}
	return out, int64(len(out)), nil
}

func (f *fakeMachineRepo) Create(_ context.Context, m *Machine) (*Machine, error) {
	for _, ex := range f.byID {
		if ex.MAC == m.MAC || ex.Name == m.Name {
			return nil, errors.New("duplicate")
		}
	}
	f.nextID++
	cp := *m
	cp.ID = fmt.Sprintf("m%d", f.nextID)
	cp.CreatedAt = time.Now()
	f.byID[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeMachineRepo) Update(_ context.Context, id string, u MachineUpdate) (*Machine, error) {
	m, ok := f.byID[id]
	if !ok {
		return nil, ErrEntityNotFound
	}
	cp := *m
	if u.Name != nil {
		cp.Name = *u.Name
	}
	if u.ProfileID != nil {
		cp.ProfileID = *u.ProfileID
	}
	if u.ReservationIP != nil {
		cp.ReservationIP = *u.ReservationIP
	}
	if u.Notes != nil {
		cp.Notes = *u.Notes
	}
	if u.Firmware != nil {
		cp.Firmware = *u.Firmware
	}
	if u.State != nil {
		cp.State = *u.State
	}
	f.byID[id] = &cp
	out := cp
	return &out, nil
}

func (f *fakeMachineRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.byID[id]; !ok {
		return ErrEntityNotFound
	}
	delete(f.byID, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeMachineRepo) ListUnknownBoots(_ context.Context, _, _ int) ([]*UnknownBoot, int64, error) {
	return nil, 0, nil
}

type fakeSessionRepo struct {
	byID   map[string]*Session
	nextID int
}

func newFakeSessionRepo() *fakeSessionRepo {
	return &fakeSessionRepo{byID: map[string]*Session{}}
}

func (f *fakeSessionRepo) Create(_ context.Context, s *Session) (*Session, error) {
	for _, ex := range f.byID {
		if ex.MachineID == s.MachineID && ex.State == SessionActive {
			return nil, errors.New("active session exists")
		}
	}
	f.nextID++
	cp := *s
	cp.ID = fmt.Sprintf("s%d", f.nextID)
	cp.State = SessionActive
	cp.StartedAt = time.Now()
	f.byID[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeSessionRepo) GetByID(_ context.Context, id string) (*Session, error) {
	s, ok := f.byID[id]
	if !ok {
		return nil, ErrEntityNotFound
	}
	cp := *s
	return &cp, nil
}

func (f *fakeSessionRepo) GetActiveByMachine(_ context.Context, machineID string) (*Session, error) {
	for _, s := range f.byID {
		if s.MachineID == machineID && s.State == SessionActive {
			cp := *s
			return &cp, nil
		}
	}
	return nil, ErrEntityNotFound
}

func (f *fakeSessionRepo) Finish(_ context.Context, id string, state SessionState, phase string, _ map[string]any) error {
	s, ok := f.byID[id]
	if !ok {
		return ErrEntityNotFound
	}
	if s.State != SessionActive {
		return nil // idempotent
	}
	cp := *s
	cp.State = state
	cp.FailurePhase = phase
	cp.EndedAt = time.Now()
	f.byID[id] = &cp
	return nil
}

func (f *fakeSessionRepo) ListActiveOlderThan(_ context.Context, cutoff time.Time) ([]*Session, error) {
	var out []*Session
	for _, s := range f.byID {
		if s.State == SessionActive && s.StartedAt.Before(cutoff) {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

type fakeProfileRepo struct {
	byID      map[string]*Profile
	deleteErr error
}

func newFakeProfileRepo() *fakeProfileRepo {
	return &fakeProfileRepo{byID: map[string]*Profile{
		"p1": {ID: "p1", Name: "noble-default", Version: 3, UbuntuRelease: ReleaseNoble,
			StorageLayout: StorageLayout{Mode: "lvm"}, SSHAuthorizedKeys: []string{"ssh-ed25519 AAAA test"}},
	}}
}

func (f *fakeProfileRepo) GetByID(_ context.Context, id string) (*Profile, error) {
	p, ok := f.byID[id]
	if !ok {
		return nil, ErrEntityNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeProfileRepo) List(_ context.Context, _, _ int) ([]*Profile, int64, error) {
	return nil, 0, nil
}

func (f *fakeProfileRepo) Create(_ context.Context, p *Profile) (*Profile, error) {
	cp := *p
	cp.ID = "p-" + p.Name
	if cp.Version == 0 {
		cp.Version = 1
	}
	f.byID[cp.ID] = &cp
	return &cp, nil
}

func (f *fakeProfileRepo) Update(_ context.Context, p *Profile) (*Profile, error) {
	if _, ok := f.byID[p.ID]; !ok {
		return nil, ErrEntityNotFound
	}
	cp := *p
	f.byID[cp.ID] = &cp
	return &cp, nil
}

func (f *fakeProfileRepo) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.byID[id]; !ok {
		return ErrEntityNotFound
	}
	delete(f.byID, id)
	return nil
}

type fakeGate struct{ enabled bool }

func (f *fakeGate) Enabled(context.Context) (bool, error) { return f.enabled, nil }

func newMachineUC(t *testing.T, dhcpEnabled bool) (*MachineUsecase, *fakeMachineRepo, *fakeSessionRepo) {
	t.Helper()
	machines := newFakeMachineRepo()
	sessions := newFakeSessionRepo()
	events := NewEventRecorder(&fakeEventRepo{}, &fakePublisher{}, testLogger())
	uc := NewMachineUsecase(machines, sessions, newFakeProfileRepo(),
		&fakeGate{enabled: dhcpEnabled}, events, testLogger())
	return uc, machines, sessions
}

// --- tests -----------------------------------------------------------------

func TestRegisterValidation(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	cases := []struct {
		name  string
		in    RegisterInput
		field string
	}{
		{"bad mac", RegisterInput{MAC: "zz:zz", Name: "host1"}, "mac"},
		{"bad name", RegisterInput{MAC: "52:54:00:aa:bb:01", Name: "UPPER_CASE"}, "name"},
		{"bad ip", RegisterInput{MAC: "52:54:00:aa:bb:02", Name: "host2", ReservationIP: "999.1.1.1"}, "reservation_ip"},
		{"bad firmware", RegisterInput{MAC: "52:54:00:aa:bb:03", Name: "host3", Firmware: "efi128"}, "firmware"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Register(context.Background(), tc.in)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			if ve.Fields[tc.field] == "" {
				t.Errorf("expected field %q in %v", tc.field, ve.Fields)
			}
		})
	}
}

func TestRegisterStates(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	noProfile, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:01", Name: "bare"})
	if err != nil {
		t.Fatal(err)
	}
	if noProfile.State != StateNew {
		t.Errorf("state without profile = %s, want new", noProfile.State)
	}
	withProfile, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:02", Name: "assigned", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if withProfile.State != StateReady {
		t.Errorf("state with profile = %s, want ready", withProfile.State)
	}
	// Unknown profile is rejected.
	if _, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:03", Name: "ghost", ProfileID: "nope"}); err == nil {
		t.Error("unknown profile must fail registration")
	}
}

func TestProvisionLifecycle(t *testing.T) {
	uc, _, sessions := newMachineUC(t, true)
	m, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:10", Name: "target", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}

	armed, err := uc.Provision(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if armed.State != StateInstalling {
		t.Errorf("state = %s, want installing", armed.State)
	}
	if armed.ActiveSessionID == "" {
		t.Error("no session id returned")
	}

	// Second provision must conflict (single active session).
	if _, err := uc.Provision(context.Background(), m.ID); !errors.Is(err, ErrSessionConflict) {
		t.Errorf("expected ErrSessionConflict, got %v", err)
	}

	// BootInfo resolves the armed machine.
	dec, err := uc.BootInfo(context.Background(), "52:54:00:aa:bb:10")
	if err != nil {
		t.Fatalf("bootinfo: %v", err)
	}
	if dec.Session.ID != armed.ActiveSessionID || dec.Profile.ID != "p1" {
		t.Errorf("wrong decision: %+v", dec)
	}
	if dec.Session.ProfileVersion != 3 {
		t.Errorf("profile version snapshot = %d, want 3", dec.Session.ProfileVersion)
	}

	// Cancel fails the session and machine.
	cancelled, err := uc.Cancel(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.State != StateFailed {
		t.Errorf("state after cancel = %s", cancelled.State)
	}
	if _, err := sessions.GetActiveByMachine(context.Background(), m.ID); !errors.Is(err, ErrEntityNotFound) {
		t.Error("active session should be gone after cancel")
	}
}

func TestProvisionPreconditions(t *testing.T) {
	ucDisabled, _, _ := newMachineUC(t, false)
	m, err := ucDisabled.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:20", Name: "blocked", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ucDisabled.Provision(context.Background(), m.ID); !errors.Is(err, ErrDhcpDisabled) {
		t.Errorf("expected ErrDhcpDisabled, got %v", err)
	}

	ucEnabled, _, _ := newMachineUC(t, true)
	bare, err := ucEnabled.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:21", Name: "noprofile"})
	if err != nil {
		t.Fatal(err)
	}
	var ve *ValidationError
	if _, err := ucEnabled.Provision(context.Background(), bare.ID); !errors.As(err, &ve) {
		t.Errorf("expected validation error for missing profile, got %v", err)
	}
}

func TestBootInfoUnknownAndUnarmed(t *testing.T) {
	uc, _, _ := newMachineUC(t, true)
	if _, err := uc.BootInfo(context.Background(), "52:54:00:ff:ff:ff"); !errors.Is(err, ErrEntityNotFound) {
		t.Errorf("unknown mac: expected ErrEntityNotFound, got %v", err)
	}
	m, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:30", Name: "idle", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = m
	if _, err := uc.BootInfo(context.Background(), "52:54:00:aa:bb:30"); !errors.Is(err, ErrNoActiveSession) {
		t.Errorf("unarmed machine: expected ErrNoActiveSession, got %v", err)
	}
}

func TestDeleteBlockedDuringInstall(t *testing.T) {
	uc, machines, _ := newMachineUC(t, true)
	m, err := uc.Register(context.Background(),
		RegisterInput{MAC: "52:54:00:aa:bb:40", Name: "busy", ProfileID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Provision(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if err := uc.Delete(context.Background(), m.ID); !errors.Is(err, ErrSessionConflict) {
		t.Errorf("expected ErrSessionConflict, got %v", err)
	}
	if len(machines.deleted) != 0 {
		t.Error("machine must not be deleted during active session")
	}
}
