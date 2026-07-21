package biz

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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
	KeyboardLayout     string // e.g. "us", "gb", "de"
	KeyboardVariant    string // optional, e.g. "dvorak"
	Locale             string // e.g. "en_US.UTF-8"
	Timezone           string // optional IANA tz, e.g. "Europe/Sofia"
	StorageLayout      StorageLayout
	NetworkConfig      map[string]any
	DefaultDNS         []string
	Packages           []string
	SSHAuthorizedKeys  []string
	UserDataTemplate   string
	LateCommands       []string
	KernelCmdlineExtra string
	// InstallUsername is the login account created on the installed OS; empty
	// means the default ("ubuntu").
	InstallUsername string
	// InstallPasswordHash is a sha512-crypt ($6$) hash for that account, or empty
	// for key-only access (the installer then gets a discarded one-time hash).
	InstallPasswordHash string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	AssignedMachines    int64
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

// PasswordHasher turns a plaintext install-user password into a crypt hash the
// installer and installed OS accept (sha512-crypt). Implemented in the data
// layer to keep the crypto dependency out of biz.
type PasswordHasher interface {
	Hash(plaintext string) (string, error)
}

// ProfileUsecase implements the full profile lifecycle with save-time
// validation, versioning, and delete guards.
type ProfileUsecase struct {
	repo      ProfileRepo
	validator AutoinstallValidator
	hasher    PasswordHasher
	log       *slog.Logger
}

func NewProfileUsecase(repo ProfileRepo, validator AutoinstallValidator, hasher PasswordHasher, log *slog.Logger) *ProfileUsecase {
	return &ProfileUsecase{repo: repo, validator: validator, hasher: hasher, log: log}
}

// ProfileInput is the validated payload for create/update.
type ProfileInput struct {
	Name               string
	UbuntuRelease      UbuntuRelease
	KeyboardLayout     string
	KeyboardVariant    string
	Locale             string
	Timezone           string
	StorageLayout      StorageLayout
	NetworkConfig      map[string]any
	DefaultDNS         []string
	Packages           []string
	SSHAuthorizedKeys  []string
	UserDataTemplate   string
	LateCommands       []string
	KernelCmdlineExtra string
	// InstallUsername overrides the default install account ("ubuntu"); empty
	// keeps the default.
	InstallUsername string
	// Password is the plaintext login password for the install account. It is
	// hashed (sha512-crypt) before storage and never persisted in the clear.
	// Empty on create means no password; empty on update preserves the existing
	// one (send ClearPassword to remove it).
	Password string
	// ClearPassword removes any stored password on update (ignored on create).
	ClearPassword bool
	// hasKeptPassword is set by Update when the target profile already has a
	// password that this input keeps (empty Password, no ClearPassword), so
	// validate() knows access is satisfied even without SSH keys.
	hasKeptPassword bool
}

var (
	keyboardLayoutRe  = regexp.MustCompile(`^[a-z]{2,}$`)
	installUsernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
)

// applyDefaults fills in the system settings that have safe defaults so the
// UI (and API clients) can omit them.
func (in *ProfileInput) applyDefaults() {
	if in.KeyboardLayout == "" {
		in.KeyboardLayout = "us"
	}
	if in.Locale == "" {
		in.Locale = "en_US.UTF-8"
	}
}

func (in *ProfileInput) validate() error {
	in.applyDefaults()
	fields := map[string]string{}
	if in.Name == "" {
		fields["name"] = "name is required"
	}
	switch in.UbuntuRelease {
	case ReleaseJammy, ReleaseNoble:
	default:
		fields["ubuntu_release"] = "must be jammy or noble"
	}
	if !keyboardLayoutRe.MatchString(in.KeyboardLayout) {
		fields["keyboard_layout"] = "must be a keyboard layout code such as us, gb or de"
	}
	switch in.StorageLayout.Mode {
	case "lvm", "direct", "custom":
	default:
		fields["storage_layout"] = "mode must be lvm, direct or custom"
	}
	if in.StorageLayout.Mode == "custom" && in.StorageLayout.Custom == nil {
		fields["storage_layout"] = "custom layout requires a config body"
	}
	for _, k := range in.SSHAuthorizedKeys {
		if k == "" {
			fields["ssh_authorized_keys"] = "SSH keys must not be empty"
		}
	}
	// Access requires SSH keys or a password: a machine with neither is
	// unreachable. On update, a stored password (kept because Password is empty
	// and ClearPassword is false) also satisfies this; that case is checked in
	// Update after the effective password is known.
	if len(in.SSHAuthorizedKeys) == 0 && in.Password == "" && !in.hasKeptPassword {
		fields["ssh_authorized_keys"] = "provide at least one SSH key or a login password"
	}
	if in.InstallUsername != "" && !installUsernameRe.MatchString(in.InstallUsername) {
		fields["install_username"] = "must start with a letter or underscore and use only a-z, 0-9, - or _"
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
		Name: in.Name, UbuntuRelease: in.UbuntuRelease,
		KeyboardLayout: in.KeyboardLayout, KeyboardVariant: in.KeyboardVariant,
		Locale: in.Locale, Timezone: in.Timezone,
		StorageLayout: in.StorageLayout,
		NetworkConfig: in.NetworkConfig, DefaultDNS: in.DefaultDNS, Packages: in.Packages,
		SSHAuthorizedKeys: in.SSHAuthorizedKeys, UserDataTemplate: in.UserDataTemplate,
		LateCommands: in.LateCommands, KernelCmdlineExtra: in.KernelCmdlineExtra,
		InstallUsername: in.InstallUsername,
	}
}

// hashPassword returns the sha512-crypt hash for a non-empty plaintext.
func (u *ProfileUsecase) hashPassword(plaintext string) (string, error) {
	hash, err := u.hasher.Hash(plaintext)
	if err != nil {
		return "", fmt.Errorf("hash install password: %w", err)
	}
	return hash, nil
}

// Create validates and persists a new profile.
func (u *ProfileUsecase) Create(ctx context.Context, in ProfileInput) (*Profile, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	p := in.toProfile()
	if in.Password != "" {
		hash, err := u.hashPassword(in.Password)
		if err != nil {
			return nil, err
		}
		p.InstallPasswordHash = hash
	}
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
	current, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// Decide the effective password before validation: a new one is hashed, an
	// empty input keeps the current hash unless ClearPassword was set.
	passwordHash := current.InstallPasswordHash
	switch {
	case in.ClearPassword:
		passwordHash = ""
	case in.Password != "":
		if passwordHash, err = u.hashPassword(in.Password); err != nil {
			return nil, err
		}
	}
	in.hasKeptPassword = passwordHash != ""
	if err := in.validate(); err != nil {
		return nil, err
	}
	p := in.toProfile()
	p.ID = current.ID
	p.Version = current.Version + 1
	p.InstallPasswordHash = passwordHash
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
