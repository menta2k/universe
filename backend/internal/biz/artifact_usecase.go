package biz

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
)

// artifactNameRe mirrors the store-side filename charset guard so the usecase
// can fail fast with a field-scoped error before touching disk (FR-017).
var artifactNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ArtifactUsecase implements the full artifact lifecycle: validated upload,
// listing, reference-guarded deletion, and unified transfer history.
type ArtifactUsecase struct {
	repo      ArtifactRepo
	transfers TransferReader
	log       *slog.Logger
}

// NewArtifactUsecase wires the repo and (optional) transfer reader. The
// transfer reader may be nil in unit tests that never call ListTransfers.
func NewArtifactUsecase(repo ArtifactRepo, transfers TransferReader, log *slog.Logger) *ArtifactUsecase {
	return &ArtifactUsecase{repo: repo, transfers: transfers, log: log}
}

// UploadInput is the validated metadata for an artifact upload/replace.
type UploadInput struct {
	Kind          ArtifactKind
	UbuntuRelease UbuntuRelease
	Filename      string
	UploadedBy    string
}

func (in UploadInput) validate() error {
	fields := map[string]string{}
	switch in.Kind {
	case ArtifactKernel, ArtifactInitrd, ArtifactIPXEBin, ArtifactOther:
	default:
		fields["kind"] = "must be kernel, initrd, ipxe_bin or other"
	}
	if in.Kind == ArtifactKernel || in.Kind == ArtifactInitrd {
		switch in.UbuntuRelease {
		case ReleaseJammy, ReleaseNoble:
		default:
			fields["ubuntu_release"] = "kernel and initrd require a valid ubuntu_release"
		}
	}
	if in.UbuntuRelease != "" {
		switch in.UbuntuRelease {
		case ReleaseJammy, ReleaseNoble:
		default:
			fields["ubuntu_release"] = "must be jammy or noble"
		}
	}
	if in.Filename == "" {
		fields["filename"] = "filename is required"
	} else if !artifactNameRe.MatchString(in.Filename) {
		fields["filename"] = "letters, digits, dot, dash and underscore only"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// Upload validates metadata then streams content through the repo, which caps
// size and computes the sha256. Returns the persisted artifact.
func (u *ArtifactUsecase) Upload(ctx context.Context, in UploadInput, content io.Reader) (*Artifact, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	meta := &Artifact{
		Kind:          in.Kind,
		UbuntuRelease: in.UbuntuRelease,
		Filename:      in.Filename,
		UploadedBy:    in.UploadedBy,
	}
	saved, err := u.repo.Save(ctx, meta, content)
	if err != nil {
		return nil, fmt.Errorf("save artifact: %w", err)
	}
	return saved, nil
}

// Delete removes an artifact, refusing to delete a kernel/initrd still needed
// by a profile's release set (returns ErrArtifactInUse).
func (u *ArtifactUsecase) Delete(ctx context.Context, id string) error {
	a, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if a.Kind == ArtifactKernel || a.Kind == ArtifactInitrd {
		used, err := u.repo.ReferencedByRelease(ctx, a.UbuntuRelease, a.Kind)
		if err != nil {
			return fmt.Errorf("check artifact references: %w", err)
		}
		if used {
			return ErrArtifactInUse
		}
	}
	if err := u.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

// Get fetches one artifact by ID.
func (u *ArtifactUsecase) Get(ctx context.Context, id string) (*Artifact, error) {
	return u.repo.GetByID(ctx, id)
}

// List returns a page of artifacts ordered by filename.
func (u *ArtifactUsecase) List(ctx context.Context, page, pageSize int) ([]*Artifact, int64, error) {
	return u.repo.List(ctx, page, pageSize)
}

// ListTransfers returns unified TFTP/HTTP transfer history, newest first.
func (u *ArtifactUsecase) ListTransfers(ctx context.Context, filename string, page, pageSize int) ([]Transfer, int64, error) {
	return u.transfers.ListTransfers(ctx, filename, page, pageSize)
}
