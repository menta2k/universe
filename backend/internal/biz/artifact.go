package biz

import (
	"context"
	"errors"
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

// ErrArtifactInUse blocks deletion while a profile still needs the artifact's
// release/kind (FR-017). The service maps it to HTTP 409.
var ErrArtifactInUse = errors.New("artifact is referenced by a profile release set")

// ArtifactRepo persists artifact metadata and file content.
// US1 subset (Save/GetByReleaseKind/GetByFilename/Open/List) plus the US4
// full-lifecycle additions (GetByID/Delete/ReferencedByRelease).
type ArtifactRepo interface {
	Save(ctx context.Context, meta *Artifact, content io.Reader) (*Artifact, error)
	GetByReleaseKind(ctx context.Context, release UbuntuRelease, kind ArtifactKind) (*Artifact, error)
	GetByFilename(ctx context.Context, filename string) (*Artifact, error)
	Open(ctx context.Context, a *Artifact) (io.ReadCloser, error)
	List(ctx context.Context, page, pageSize int) ([]*Artifact, int64, error)

	// GetByID fetches a single artifact, returning ErrEntityNotFound if absent.
	GetByID(ctx context.Context, id string) (*Artifact, error)
	// Delete removes the DB row and the backing file (file-missing is tolerated).
	Delete(ctx context.Context, id string) error
	// ReferencedByRelease reports whether any profile requires this release/kind.
	ReferencedByRelease(ctx context.Context, release UbuntuRelease, kind ArtifactKind) (bool, error)
}

// Transfer is one unified file-serving record (TFTP or HTTP) for FR-011.
type Transfer struct {
	Time      time.Time
	ClientIP  string
	Filename  string
	BytesSent int64
	Success   bool
	Error     string
	Protocol  string // tftp | http
}

// TransferReader lists unified transfer activity, newest first, optionally
// filtered by filename. Implemented by the data layer over the TFTP transfer
// log unioned with file_served provisioning events.
type TransferReader interface {
	ListTransfers(ctx context.Context, filename string, page, pageSize int) ([]Transfer, int64, error)
}
