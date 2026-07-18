package biz

import (
	"context"
	"io"
	"time"
)

// ArtifactKind mirrors the artifact_kind SQL enum.
type ArtifactKind string

const (
	ArtifactKernel  ArtifactKind = "kernel"
	ArtifactInitrd  ArtifactKind = "initrd"
	ArtifactIPXEBin ArtifactKind = "ipxe_bin"
	ArtifactOther   ArtifactKind = "other"
)

// Artifact is a served boot file with integrity metadata.
type Artifact struct {
	ID            string
	Kind          ArtifactKind
	UbuntuRelease UbuntuRelease
	Filename      string
	Path          string
	SizeBytes     int64
	SHA256        string
	UploadedBy    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ArtifactRepo persists artifact metadata and file content.
// US1 subset: store (seed via API/CLI), lookup, open. Full lifecycle in US4.
type ArtifactRepo interface {
	Save(ctx context.Context, meta *Artifact, content io.Reader) (*Artifact, error)
	GetByReleaseKind(ctx context.Context, release UbuntuRelease, kind ArtifactKind) (*Artifact, error)
	GetByFilename(ctx context.Context, filename string) (*Artifact, error)
	Open(ctx context.Context, a *Artifact) (io.ReadCloser, error)
	List(ctx context.Context, page, pageSize int) ([]*Artifact, int64, error)
}
