package integration

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/service"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

func newArtifactStack(t *testing.T, env *testenv.Env) (*service.ArtifactService, *data.ArtifactStore, *biz.ArtifactUsecase) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := data.NewArtifactStore(env.Data, t.TempDir(), 1<<30)
	if err != nil {
		t.Fatalf("artifact store: %v", err)
	}
	usecase := biz.NewArtifactUsecase(store, data.NewTransferRepo(env.Data), log)
	return service.NewArtifactService(usecase, 1<<30), store, usecase
}

func TestArtifactServiceLifecycle(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	svc, _, usecase := newArtifactStack(t, env)

	// Upload a kernel (referenced release) and an unreferenced "other".
	kernel, err := usecase.Upload(ctx, biz.UploadInput{
		Kind: biz.ArtifactKernel, UbuntuRelease: biz.ReleaseNoble, Filename: "vmlinuz-noble",
	}, strings.NewReader("kernel-bytes"))
	if err != nil {
		t.Fatalf("upload kernel: %v", err)
	}
	if kernel.SHA256 == "" {
		t.Error("expected sha256 to be computed on upload")
	}
	other, err := usecase.Upload(ctx, biz.UploadInput{
		Kind: biz.ArtifactOther, Filename: "grub.cfg",
	}, strings.NewReader("cfg-bytes"))
	if err != nil {
		t.Fatalf("upload other: %v", err)
	}

	// List returns both via the service layer.
	list, err := svc.ListArtifacts(ctx, &v1.PageRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Meta.Total != 2 || len(list.Artifacts) != 2 {
		t.Fatalf("list total = %d (%d rows), want 2", list.Meta.Total, len(list.Artifacts))
	}

	// GetArtifact echoes the sha256.
	got, err := svc.GetArtifact(ctx, &v1.GetArtifactRequest{Id: kernel.ID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Sha256 != kernel.SHA256 {
		t.Errorf("get sha256 = %q, want %q", got.Sha256, kernel.SHA256)
	}

	// Create a profile using the noble release so the kernel becomes referenced.
	seedNobleProfile(t, env)

	// Delete of the referenced kernel is blocked with 409 CONFLICT.
	_, err = svc.DeleteArtifact(ctx, &v1.GetArtifactRequest{Id: kernel.ID})
	if r := kerrors.FromError(err).Reason; r != "CONFLICT" {
		t.Errorf("delete-in-use reason = %s, want CONFLICT", r)
	}

	// Deleting the unreferenced "other" artifact succeeds.
	if _, err := svc.DeleteArtifact(ctx, &v1.GetArtifactRequest{Id: other.ID}); err != nil {
		t.Fatalf("delete other: %v", err)
	}
	if _, err := svc.GetArtifact(ctx, &v1.GetArtifactRequest{Id: other.ID}); err == nil {
		t.Error("expected other artifact to be gone after delete")
	}
}

// seedNobleProfile inserts a minimal noble profile row so ReferencedByRelease
// reports the noble release as in use.
func seedNobleProfile(t *testing.T, env *testenv.Env) {
	t.Helper()
	_, err := env.Data.Pool.Exec(context.Background(),
		`INSERT INTO profiles (name, ubuntu_release, storage_layout, ssh_authorized_keys)
		 VALUES ('noble-ref', 'noble'::ubuntu_release, '{"mode":"lvm"}'::jsonb, ARRAY['ssh-ed25519 AAAA test'])`)
	if err != nil {
		t.Fatalf("seed profile: %v", err)
	}
}
