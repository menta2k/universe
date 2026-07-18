package biz

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// UbuntuRelease values match the ubuntu_release SQL enum.
type UbuntuRelease string

const (
	ReleaseJammy UbuntuRelease = "jammy"
	ReleaseNoble UbuntuRelease = "noble"
)

// StorageLayout selects the autoinstall storage strategy.
type StorageLayout struct {
	Mode   string `json:"mode"` // lvm | direct | custom
	Custom any    `json:"custom,omitempty"`
}

// Profile is a versioned, declarative definition of an unattended install.
type Profile struct {
	ID                 string
	Name               string
	Version            int
	UbuntuRelease      UbuntuRelease
	StorageLayout      StorageLayout
	NetworkConfig      map[string]any
	Packages           []string
	SSHAuthorizedKeys  []string
	UserDataTemplate   string
	LateCommands       []string
	KernelCmdlineExtra string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	AssignedMachines   int64
}

// ProfileRepo persists profiles and their revisions.
type ProfileRepo interface {
	GetByID(ctx context.Context, id string) (*Profile, error)
	List(ctx context.Context, page, pageSize int) ([]*Profile, int64, error)
	Create(ctx context.Context, p *Profile) (*Profile, error)
	// Update writes a new version and appends the prior state to revisions.
	Update(ctx context.Context, p *Profile) (*Profile, error)
	// Delete removes a profile; repos enforce the assigned-machine guard via
	// the FK RESTRICT and surface it as ErrProfileInUse.
	Delete(ctx context.Context, id string) error
}

// ErrProfileInUse blocks deletion while machines reference the profile (FR-009).
var ErrProfileInUse = fmt.Errorf("profile is assigned to one or more machines")

// AutoinstallValidator renders a profile against a fixture machine and returns
// an error if the produced document is invalid (FR-008). Implemented by the
// autoinstall package adapter to avoid a biz -> netboot import cycle.
type AutoinstallValidator interface {
	Validate(p *Profile) error
}

// ProfileUsecase implements the full profile lifecycle with save-time
// validation, versioning, and delete guards.
type ProfileUsecase struct {
	repo      ProfileRepo
	validator AutoinstallValidator
	log       *slog.Logger
}

func NewProfileUsecase(repo ProfileRepo, validator AutoinstallValidator, log *slog.Logger) *ProfileUsecase {
	return &ProfileUsecase{repo: repo, validator: validator, log: log}
}

// ProfileInput is the validated payload for create/update.
type ProfileInput struct {
	Name               string
	UbuntuRelease      UbuntuRelease
	StorageLayout      StorageLayout
	NetworkConfig      map[string]any
	Packages           []string
	SSHAuthorizedKeys  []string
	UserDataTemplate   string
	LateCommands       []string
	KernelCmdlineExtra string
}

func (in *ProfileInput) validate() error {
	fields := map[string]string{}
	if in.Name == "" {
		fields["name"] = "name is required"
	}
	switch in.UbuntuRelease {
	case ReleaseJammy, ReleaseNoble:
	default:
		fields["ubuntu_release"] = "must be jammy or noble"
	}
	switch in.StorageLayout.Mode {
	case "lvm", "direct", "custom":
	default:
		fields["storage_layout"] = "mode must be lvm, direct or custom"
	}
	if in.StorageLayout.Mode == "custom" && in.StorageLayout.Custom == nil {
		fields["storage_layout"] = "custom layout requires a config body"
	}
	if len(in.SSHAuthorizedKeys) == 0 {
		fields["ssh_authorized_keys"] = "at least one SSH key is required"
	}
	for _, k := range in.SSHAuthorizedKeys {
		if k == "" {
			fields["ssh_authorized_keys"] = "SSH keys must not be empty"
		}
	}
	if containsNewline(in.KernelCmdlineExtra) {
		fields["kernel_cmdline_extra"] = "must not contain newlines"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (in *ProfileInput) toProfile() *Profile {
	return &Profile{
		Name: in.Name, UbuntuRelease: in.UbuntuRelease, StorageLayout: in.StorageLayout,
		NetworkConfig: in.NetworkConfig, Packages: in.Packages,
		SSHAuthorizedKeys: in.SSHAuthorizedKeys, UserDataTemplate: in.UserDataTemplate,
		LateCommands: in.LateCommands, KernelCmdlineExtra: in.KernelCmdlineExtra,
	}
}

// Create validates and persists a new profile.
func (u *ProfileUsecase) Create(ctx context.Context, in ProfileInput) (*Profile, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	p := in.toProfile()
	if err := u.validator.Validate(p); err != nil {
		return nil, renderValidationError(err)
	}
	created, err := u.repo.Create(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("create profile: %w", err)
	}
	return created, nil
}

// Update validates, bumps the version, and archives the prior revision.
func (u *ProfileUsecase) Update(ctx context.Context, id string, in ProfileInput) (*Profile, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	current, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	p := in.toProfile()
	p.ID = current.ID
	p.Version = current.Version + 1
	if err := u.validator.Validate(p); err != nil {
		return nil, renderValidationError(err)
	}
	updated, err := u.repo.Update(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return updated, nil
}

// Clone copies an existing profile under a new name.
func (u *ProfileUsecase) Clone(ctx context.Context, id, newName string) (*Profile, error) {
	src, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if newName == "" {
		newName = src.Name + "-copy"
	}
	clone := *src
	clone.ID = ""
	clone.Name = newName
	clone.Version = 1
	created, err := u.repo.Create(ctx, &clone)
	if err != nil {
		return nil, fmt.Errorf("clone profile: %w", err)
	}
	return created, nil
}

// Delete removes a profile unless machines are assigned (FR-009).
func (u *ProfileUsecase) Delete(ctx context.Context, id string) error {
	if err := u.repo.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}

func (u *ProfileUsecase) Get(ctx context.Context, id string) (*Profile, error) {
	return u.repo.GetByID(ctx, id)
}

func (u *ProfileUsecase) List(ctx context.Context, page, pageSize int) ([]*Profile, int64, error) {
	return u.repo.List(ctx, page, pageSize)
}

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}

// renderValidationError normalizes an autoinstall render failure into a
// field-scoped validation error the API can surface.
func renderValidationError(err error) error {
	var ve *ValidationError
	if asValidation(err, &ve) {
		return ve
	}
	return &ValidationError{Fields: map[string]string{
		"user_data_template": "profile does not render to a valid autoinstall document: " + err.Error()}}
}
