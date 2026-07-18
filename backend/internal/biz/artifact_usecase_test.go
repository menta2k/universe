package biz

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeArtifactRepo is an in-memory ArtifactRepo for usecase unit tests.
type fakeArtifactRepo struct {
	byID       map[string]*Artifact
	saved      *Artifact
	saveErr    error
	referenced bool
	refErr     error
	deleted    []string
	deleteErr  error
}

func newFakeArtifactRepo() *fakeArtifactRepo {
	return &fakeArtifactRepo{byID: map[string]*Artifact{}}
}

func (f *fakeArtifactRepo) Save(_ context.Context, meta *Artifact, content io.Reader) (*Artifact, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	b, _ := io.ReadAll(content)
	out := *meta
	out.ID = "id-" + meta.Filename
	out.SizeBytes = int64(len(b))
	out.SHA256 = "deadbeef"
	f.saved = &out
	f.byID[out.ID] = &out
	return &out, nil
}

func (f *fakeArtifactRepo) GetByReleaseKind(context.Context, UbuntuRelease, ArtifactKind) (*Artifact, error) {
	return nil, ErrEntityNotFound
}
func (f *fakeArtifactRepo) GetByFilename(context.Context, string) (*Artifact, error) {
	return nil, ErrEntityNotFound
}
func (f *fakeArtifactRepo) Open(context.Context, *Artifact) (io.ReadCloser, error) { return nil, nil }
func (f *fakeArtifactRepo) List(context.Context, int, int) ([]*Artifact, int64, error) {
	out := make([]*Artifact, 0, len(f.byID))
	for _, a := range f.byID {
		out = append(out, a)
	}
	return out, int64(len(out)), nil
}

func (f *fakeArtifactRepo) GetByID(_ context.Context, id string) (*Artifact, error) {
	a, ok := f.byID[id]
	if !ok {
		return nil, ErrEntityNotFound
	}
	return a, nil
}

func (f *fakeArtifactRepo) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.byID, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeArtifactRepo) ReferencedByRelease(context.Context, UbuntuRelease, ArtifactKind) (bool, error) {
	return f.referenced, f.refErr
}

func TestArtifactUsecaseUploadValidation(t *testing.T) {
	tests := []struct {
		name     string
		in       UploadInput
		body     string
		wantErr  bool
		wantKind ArtifactKind
	}{
		{
			name:    "bad kind",
			in:      UploadInput{Kind: "bogus", Filename: "x.bin"},
			body:    "data",
			wantErr: true,
		},
		{
			name:    "kernel without release",
			in:      UploadInput{Kind: ArtifactKernel, Filename: "vmlinuz"},
			body:    "data",
			wantErr: true,
		},
		{
			name:    "bad filename charset",
			in:      UploadInput{Kind: ArtifactOther, Filename: "bad/name"},
			body:    "data",
			wantErr: true,
		},
		{
			name:     "good kernel",
			in:       UploadInput{Kind: ArtifactKernel, UbuntuRelease: ReleaseNoble, Filename: "vmlinuz-noble"},
			body:     "kernelbytes",
			wantKind: ArtifactKernel,
		},
		{
			name:     "good other without release",
			in:       UploadInput{Kind: ArtifactOther, Filename: "grub.cfg"},
			body:     "cfg",
			wantKind: ArtifactOther,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeArtifactRepo()
			u := NewArtifactUsecase(repo, nil, testLogger())
			got, err := u.Upload(context.Background(), tt.in, strings.NewReader(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("expected ValidationError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind != tt.wantKind {
				t.Errorf("kind = %s, want %s", got.Kind, tt.wantKind)
			}
			if got.SHA256 == "" {
				t.Error("expected sha256 to be set")
			}
		})
	}
}

func TestArtifactUsecaseDelete(t *testing.T) {
	t.Run("blocked when referenced", func(t *testing.T) {
		repo := newFakeArtifactRepo()
		repo.byID["k1"] = &Artifact{ID: "k1", Kind: ArtifactKernel, UbuntuRelease: ReleaseNoble}
		repo.referenced = true
		u := NewArtifactUsecase(repo, nil, testLogger())
		err := u.Delete(context.Background(), "k1")
		if !errors.Is(err, ErrArtifactInUse) {
			t.Fatalf("err = %v, want ErrArtifactInUse", err)
		}
		if len(repo.deleted) != 0 {
			t.Errorf("delete should not have been called")
		}
	})

	t.Run("ok when unreferenced", func(t *testing.T) {
		repo := newFakeArtifactRepo()
		repo.byID["k1"] = &Artifact{ID: "k1", Kind: ArtifactKernel, UbuntuRelease: ReleaseNoble}
		repo.referenced = false
		u := NewArtifactUsecase(repo, nil, testLogger())
		if err := u.Delete(context.Background(), "k1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if len(repo.deleted) != 1 {
			t.Errorf("expected delete to be called once, got %d", len(repo.deleted))
		}
	})

	t.Run("other kind skips reference check", func(t *testing.T) {
		repo := newFakeArtifactRepo()
		repo.byID["o1"] = &Artifact{ID: "o1", Kind: ArtifactOther}
		repo.referenced = true // would block if checked
		u := NewArtifactUsecase(repo, nil, testLogger())
		if err := u.Delete(context.Background(), "o1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := newFakeArtifactRepo()
		u := NewArtifactUsecase(repo, nil, testLogger())
		if err := u.Delete(context.Background(), "missing"); !errors.Is(err, ErrEntityNotFound) {
			t.Fatalf("err = %v, want ErrEntityNotFound", err)
		}
	})
}
