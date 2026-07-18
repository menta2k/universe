package biz

import (
	"context"
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
// US1 needs create/get/assign; full lifecycle management arrives with US2.
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

type ProfileRepo interface {
	GetByID(ctx context.Context, id string) (*Profile, error)
	List(ctx context.Context, page, pageSize int) ([]*Profile, int64, error)
	Create(ctx context.Context, p *Profile) (*Profile, error)
}
