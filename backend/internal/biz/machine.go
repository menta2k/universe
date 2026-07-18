package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"time"
)

// Firmware is the client firmware type observed via DHCP option 93.
type Firmware string

const (
	FirmwareBIOS    Firmware = "bios"
	FirmwareUEFI    Firmware = "uefi_x64"
	FirmwareUnknown Firmware = "unknown"
)

// ProvisionState is the machine lifecycle state (see data-model.md).
type ProvisionState string

const (
	StateNew        ProvisionState = "new"
	StateReady      ProvisionState = "ready"
	StateInstalling ProvisionState = "installing"
	StateInstalled  ProvisionState = "installed"
	StateFailed     ProvisionState = "failed"
)

// ErrNoActiveSession signals a boot request for a machine that is not armed.
var ErrNoActiveSession = errors.New("machine has no active provisioning session")

var hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Machine is a provisioning target identified by MAC address.
type Machine struct {
	ID              string
	MAC             string
	Name            string
	Firmware        Firmware
	ProfileID       string
	ReservationIP   string
	State           ProvisionState
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ActiveSessionID string
}

// MachineUpdate carries optional field changes (immutability: repos return
// fresh copies; nil pointer = leave unchanged).
type MachineUpdate struct {
	Name          *string
	ProfileID     *string
	ReservationIP *string
	Notes         *string
	Firmware      *Firmware
	State         *ProvisionState
}

// MachineFilter narrows List queries.
type MachineFilter struct {
	State     ProvisionState
	ProfileID string
	Query     string
	Page      int
	PageSize  int
}

// UnknownBoot aggregates boot attempts by unregistered MACs (FR-005).
type UnknownBoot struct {
	MAC      string
	LastSeen time.Time
	Attempts int64
}

type MachineRepo interface {
	GetByID(ctx context.Context, id string) (*Machine, error)
	GetByMAC(ctx context.Context, mac string) (*Machine, error)
	List(ctx context.Context, f MachineFilter) ([]*Machine, int64, error)
	Create(ctx context.Context, m *Machine) (*Machine, error)
	Update(ctx context.Context, id string, u MachineUpdate) (*Machine, error)
	Delete(ctx context.Context, id string) error
	ListUnknownBoots(ctx context.Context, page, pageSize int) ([]*UnknownBoot, int64, error)
}

// DhcpGate reports whether the DHCP service is enabled (FR-016: provisioning
// requires the address service to be on).
type DhcpGate interface {
	Enabled(ctx context.Context) (bool, error)
}

// ProfileLookup is the read-only slice of ProfileRepo the machine usecase
// needs (accept a small interface).
type ProfileLookup interface {
	GetByID(ctx context.Context, id string) (*Profile, error)
}

// MachineUsecase implements machine registration and provisioning arming.
type MachineUsecase struct {
	machines MachineRepo
	sessions SessionRepo
	profiles ProfileLookup
	gate     DhcpGate
	events   *EventRecorder
	log      *slog.Logger
}

func NewMachineUsecase(
	machines MachineRepo, sessions SessionRepo, profiles ProfileLookup,
	gate DhcpGate, events *EventRecorder, log *slog.Logger,
) *MachineUsecase {
	return &MachineUsecase{
		machines: machines, sessions: sessions, profiles: profiles,
		gate: gate, events: events, log: log,
	}
}

// RegisterInput is the validated payload for machine creation.
type RegisterInput struct {
	MAC           string
	Name          string
	Firmware      Firmware
	ProfileID     string
	ReservationIP string
	Notes         string
}

// ValidationError carries per-field messages to the API layer.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %v", e.Fields)
}

// asValidation reports whether err is (or wraps) a *ValidationError.
func asValidation(err error, target **ValidationError) bool {
	return errors.As(err, target)
}

func (in *RegisterInput) validate() error {
	fields := map[string]string{}
	hw, err := net.ParseMAC(in.MAC)
	if err != nil {
		fields["mac"] = "invalid MAC address"
	} else {
		in.MAC = hw.String()
	}
	if !hostnameRe.MatchString(in.Name) {
		fields["name"] = "must be a valid hostname (lowercase, 1-63 chars)"
	}
	if in.ReservationIP != "" && net.ParseIP(in.ReservationIP) == nil {
		fields["reservation_ip"] = "invalid IP address"
	}
	switch in.Firmware {
	case "", FirmwareBIOS, FirmwareUEFI, FirmwareUnknown:
	default:
		fields["firmware"] = "must be bios, uefi_x64 or unknown"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// Register creates a machine; state is ready when a profile is assigned.
func (u *MachineUsecase) Register(ctx context.Context, in RegisterInput) (*Machine, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	if in.ProfileID != "" {
		if _, err := u.profiles.GetByID(ctx, in.ProfileID); err != nil {
			return nil, fmt.Errorf("profile %s: %w", in.ProfileID, err)
		}
	}
	fw := in.Firmware
	if fw == "" {
		fw = FirmwareUnknown
	}
	state := StateNew
	if in.ProfileID != "" {
		state = StateReady
	}
	m, err := u.machines.Create(ctx, &Machine{
		MAC: in.MAC, Name: in.Name, Firmware: fw,
		ProfileID: in.ProfileID, ReservationIP: in.ReservationIP,
		State: state, Notes: in.Notes,
	})
	if err != nil {
		return nil, fmt.Errorf("create machine: %w", err)
	}
	return m, nil
}

// Update applies changes; assigning a profile moves new -> ready.
func (u *MachineUsecase) Update(ctx context.Context, id string, up MachineUpdate) (*Machine, error) {
	current, err := u.machines.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if up.ProfileID != nil && *up.ProfileID != "" {
		if _, err := u.profiles.GetByID(ctx, *up.ProfileID); err != nil {
			return nil, fmt.Errorf("profile %s: %w", *up.ProfileID, err)
		}
		if current.State == StateNew {
			ready := StateReady
			up.State = &ready
		}
	}
	m, err := u.machines.Update(ctx, id, up)
	if err != nil {
		return nil, fmt.Errorf("update machine: %w", err)
	}
	return m, nil
}

// Provision arms the machine: validates preconditions and opens a session.
func (u *MachineUsecase) Provision(ctx context.Context, machineID string) (*Machine, error) {
	enabled, err := u.gate.Enabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("check dhcp state: %w", err)
	}
	if !enabled {
		return nil, ErrDhcpDisabled
	}
	m, err := u.machines.GetByID(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if m.ProfileID == "" {
		return nil, &ValidationError{Fields: map[string]string{
			"profile_id": "machine has no profile assigned"}}
	}
	profile, err := u.profiles.GetByID(ctx, m.ProfileID)
	if err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}
	if _, err := u.sessions.GetActiveByMachine(ctx, m.ID); err == nil {
		return nil, ErrSessionConflict
	} else if !errors.Is(err, ErrEntityNotFound) {
		return nil, fmt.Errorf("check active session: %w", err)
	}
	sess, err := u.sessions.Create(ctx, &Session{
		MachineID: m.ID, ProfileID: profile.ID, ProfileVersion: profile.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	installing := StateInstalling
	updated, err := u.machines.Update(ctx, m.ID, MachineUpdate{State: &installing})
	if err != nil {
		return nil, fmt.Errorf("mark installing: %w", err)
	}
	updated.ActiveSessionID = sess.ID
	u.log.Info("machine armed for provisioning",
		"machine", m.Name, "mac", m.MAC, "session", sess.ID)
	return updated, nil
}

// Cancel aborts the active session and marks the machine failed.
func (u *MachineUsecase) Cancel(ctx context.Context, machineID string) (*Machine, error) {
	m, err := u.machines.GetByID(ctx, machineID)
	if err != nil {
		return nil, err
	}
	sess, err := u.sessions.GetActiveByMachine(ctx, m.ID)
	if err != nil {
		if errors.Is(err, ErrEntityNotFound) {
			return nil, ErrNoActiveSession
		}
		return nil, err
	}
	if err := u.sessions.Finish(ctx, sess.ID, SessionFailed, "cancelled", nil); err != nil {
		return nil, fmt.Errorf("cancel session: %w", err)
	}
	failed := StateFailed
	updated, err := u.machines.Update(ctx, m.ID, MachineUpdate{State: &failed})
	if err != nil {
		return nil, fmt.Errorf("mark failed: %w", err)
	}
	u.events.Record(ctx, Event{
		SessionID: sess.ID, MachineMAC: m.MAC,
		Phase: PhaseSessionFailed, Outcome: OutcomeOK,
		Detail: map[string]any{"reason": "cancelled by operator"},
	})
	return updated, nil
}

// Get returns a machine with its active session attached.
func (u *MachineUsecase) Get(ctx context.Context, id string) (*Machine, error) {
	m, err := u.machines.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess, err := u.sessions.GetActiveByMachine(ctx, m.ID); err == nil {
		m.ActiveSessionID = sess.ID
	}
	return m, nil
}

func (u *MachineUsecase) List(ctx context.Context, f MachineFilter) ([]*Machine, int64, error) {
	return u.machines.List(ctx, f)
}

// Delete removes a machine unless an install is in flight.
func (u *MachineUsecase) Delete(ctx context.Context, id string) error {
	if _, err := u.sessions.GetActiveByMachine(ctx, id); err == nil {
		return ErrSessionConflict
	} else if !errors.Is(err, ErrEntityNotFound) {
		return err
	}
	return u.machines.Delete(ctx, id)
}

func (u *MachineUsecase) ListUnknownBoots(ctx context.Context, page, pageSize int) ([]*UnknownBoot, int64, error) {
	return u.machines.ListUnknownBoots(ctx, page, pageSize)
}

// RecordUnknownBoot logs a denied boot attempt from an unregistered MAC.
func (u *MachineUsecase) RecordUnknownBoot(ctx context.Context, mac string) {
	u.events.Record(ctx, Event{
		MachineMAC: mac, Phase: PhaseUnknownMachine, Outcome: OutcomeDenied,
		Detail: map[string]any{"reason": "mac not registered"},
	})
}

// BootDecision is what the DHCP/boot services need to serve a machine.
type BootDecision struct {
	Machine *Machine
	Session *Session
	Profile *Profile
}

// BootInfo resolves a booting MAC to its armed session. Returns
// ErrEntityNotFound for unknown MACs and ErrNoActiveSession for known
// machines that are not armed (both are non-netboot answers, FR-005).
func (u *MachineUsecase) BootInfo(ctx context.Context, mac string) (*BootDecision, error) {
	m, err := u.machines.GetByMAC(ctx, mac)
	if err != nil {
		return nil, err
	}
	sess, err := u.sessions.GetActiveByMachine(ctx, m.ID)
	if err != nil {
		if errors.Is(err, ErrEntityNotFound) {
			return nil, ErrNoActiveSession
		}
		return nil, err
	}
	profile, err := u.profiles.GetByID(ctx, m.ProfileID)
	if err != nil {
		return nil, fmt.Errorf("load profile for boot: %w", err)
	}
	return &BootDecision{Machine: m, Session: sess, Profile: profile}, nil
}

// BootInfoBySession resolves a boot decision directly from a session id,
// used by the seed endpoints which hold a token bound to a session.
func (u *MachineUsecase) BootInfoBySession(ctx context.Context, sessionID string) (*BootDecision, error) {
	sess, err := u.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	m, err := u.machines.GetByID(ctx, sess.MachineID)
	if err != nil {
		return nil, err
	}
	profile, err := u.profiles.GetByID(ctx, sess.ProfileID)
	if err != nil {
		return nil, fmt.Errorf("load profile for boot: %w", err)
	}
	return &BootDecision{Machine: m, Session: sess, Profile: profile}, nil
}

// ObserveFirmware persists the firmware type reported during PXE boot.
func (u *MachineUsecase) ObserveFirmware(ctx context.Context, machineID string, fw Firmware) {
	if fw == FirmwareUnknown {
		return
	}
	if _, err := u.machines.Update(ctx, machineID, MachineUpdate{Firmware: &fw}); err != nil {
		u.log.Warn("firmware observation not saved", "err", err, "machine", machineID)
	}
}

// ErrDhcpDisabled blocks provisioning while the DHCP service is off.
var ErrDhcpDisabled = errors.New("dhcp service is disabled")

// ErrSessionConflict signals an already-active provisioning session.
var ErrSessionConflict = errors.New("machine already has an active session")
