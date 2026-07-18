package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jackc/pgx/v5"

	"universe/backend/internal/biz"
)

var artifactNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ArtifactStore keeps file bytes on disk under root and metadata in the DB.
type ArtifactStore struct {
	data *Data
	root string
	max  int64
}

func NewArtifactStore(d *Data, root string, maxBytes int64) (*ArtifactStore, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	return &ArtifactStore{data: d, root: root, max: maxBytes}, nil
}

const artifactCols = `id, kind, coalesce(ubuntu_release::text,''), filename, path,
	size_bytes, sha256, coalesce(uploaded_by::text,''), created_at, updated_at`

func scanArtifact(row pgx.Row) (*biz.Artifact, error) {
	var a biz.Artifact
	err := row.Scan(&a.ID, &a.Kind, &a.UbuntuRelease, &a.Filename, &a.Path,
		&a.SizeBytes, &a.SHA256, &a.UploadedBy, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan artifact: %w", err)
	}
	return &a, nil
}

// Save streams content to disk (size-capped), hashes it, and records metadata.
func (s *ArtifactStore) Save(ctx context.Context, meta *biz.Artifact, content io.Reader) (*biz.Artifact, error) {
	if !artifactNameRe.MatchString(meta.Filename) {
		return nil, &biz.ValidationError{Fields: map[string]string{
			"filename": "letters, digits, dot, dash and underscore only"}}
	}
	path := filepath.Join(s.root, meta.Filename)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640) // #nosec G304 -- name validated above
	if err != nil {
		return nil, fmt.Errorf("create artifact file: %w", err)
	}
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(content, s.max+1))
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write artifact: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close artifact: %w", closeErr)
	}
	if n > s.max {
		_ = os.Remove(path)
		return nil, &biz.ValidationError{Fields: map[string]string{
			"file": fmt.Sprintf("exceeds maximum size of %d bytes", s.max)}}
	}
	if n == 0 {
		_ = os.Remove(path)
		return nil, &biz.ValidationError{Fields: map[string]string{"file": "empty upload"}}
	}

	saved, err := scanArtifact(s.data.Pool.QueryRow(ctx,
		`INSERT INTO boot_artifacts (kind, ubuntu_release, filename, path, size_bytes, sha256, uploaded_by)
		 VALUES ($1::artifact_kind, NULLIF($2,'')::ubuntu_release, $3, $4, $5, $6, NULLIF($7,'')::uuid)
		 ON CONFLICT (filename) DO UPDATE
		   SET kind = EXCLUDED.kind, ubuntu_release = EXCLUDED.ubuntu_release,
		       path = EXCLUDED.path, size_bytes = EXCLUDED.size_bytes,
		       sha256 = EXCLUDED.sha256, uploaded_by = EXCLUDED.uploaded_by,
		       updated_at = now()
		 RETURNING `+artifactCols,
		string(meta.Kind), string(meta.UbuntuRelease), meta.Filename, path,
		n, hex.EncodeToString(hasher.Sum(nil)), meta.UploadedBy))
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("record artifact: %w", err)
	}
	return saved, nil
}

func (s *ArtifactStore) GetByReleaseKind(ctx context.Context, release biz.UbuntuRelease, kind biz.ArtifactKind) (*biz.Artifact, error) {
	return scanArtifact(s.data.Pool.QueryRow(ctx,
		`SELECT `+artifactCols+` FROM boot_artifacts
		 WHERE ubuntu_release = $1::ubuntu_release AND kind = $2::artifact_kind
		 ORDER BY updated_at DESC LIMIT 1`, string(release), string(kind)))
}

func (s *ArtifactStore) GetByFilename(ctx context.Context, filename string) (*biz.Artifact, error) {
	return scanArtifact(s.data.Pool.QueryRow(ctx,
		`SELECT `+artifactCols+` FROM boot_artifacts WHERE filename = $1`, filename))
}

func (s *ArtifactStore) Open(_ context.Context, a *biz.Artifact) (io.ReadCloser, error) {
	f, err := os.Open(a.Path) // #nosec G304 -- path was produced by Save from a validated name
	if err != nil {
		return nil, fmt.Errorf("open artifact %s: %w", a.Filename, err)
	}
	return f, nil
}

func (s *ArtifactStore) List(ctx context.Context, page, pageSize int) ([]*biz.Artifact, int64, error) {
	var total int64
	if err := s.data.Pool.QueryRow(ctx, `SELECT count(*) FROM boot_artifacts`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count artifacts: %w", err)
	}
	p, size := normalizePage(page, pageSize)
	rows, err := s.data.Pool.Query(ctx,
		`SELECT `+artifactCols+` FROM boot_artifacts ORDER BY filename LIMIT $1 OFFSET $2`,
		size, (p-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	var out []*biz.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}
