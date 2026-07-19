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
	"github.com/menta2k/universe/backend/internal/netboot/autoinstall"
	"github.com/menta2k/universe/backend/internal/service"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

func newProfileService(t *testing.T, env *testenv.Env) (*service.ProfileService, *biz.MachineUsecase) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	profileRepo := data.NewProfileRepo(env.Data)
	profiles := biz.NewProfileUsecase(profileRepo, autoinstall.NewValidator(), log)
	machines := biz.NewMachineUsecase(data.NewMachineRepo(env.Data), data.NewSessionRepo(env.Data),
		profileRepo, &gate{enabled: true}, events, log)
	return service.NewProfileService(profiles, machines), machines
}

func validProfileInput() *v1.ProfileInput {
	return &v1.ProfileInput{
		Name: "noble-web", UbuntuRelease: "noble",
		StorageLayout:     `{"mode":"lvm"}`,
		SshAuthorizedKeys: []string{"ssh-ed25519 AAAAC3Nz test@host"},
		Packages:          []string{"nginx"},
	}
}

func TestProfileServiceContract(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	svc, machines := newProfileService(t, env)

	// Create valid.
	p, err := svc.CreateProfile(ctx, validProfileInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Version != 1 {
		t.Errorf("version = %d, want 1", p.Version)
	}

	// Invalid: missing SSH key -> 422 with field details.
	bad := validProfileInput()
	bad.Name = "bad"
	bad.SshAuthorizedKeys = nil
	_, err = svc.CreateProfile(ctx, bad)
	if r := kerrors.FromError(err).Reason; r != "VALIDATION_FAILED" {
		t.Errorf("reason = %s, want VALIDATION_FAILED", r)
	}
	if kerrors.FromError(err).Metadata["ssh_authorized_keys"] == "" {
		t.Error("expected ssh_authorized_keys field error")
	}

	// Preview renders redacted user-data.
	prev, err := svc.PreviewProfile(ctx, &v1.PreviewProfileRequest{Id: p.Id})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !strings.Contains(prev.UserData, "autoinstall") || strings.Contains(prev.UserData, "PREVIEW-secret") {
		t.Errorf("preview user-data unexpected: %s", prev.UserData)
	}

	// Update bumps version and writes a revision.
	upd := validProfileInput()
	upd.Packages = []string{"nginx", "postgresql"}
	updated, err := svc.UpdateProfile(ctx, &v1.UpdateProfileRequest{Id: p.Id, Profile: upd})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("version after update = %d, want 2", updated.Version)
	}
	var revCount int
	if err := env.Data.Pool.QueryRow(ctx,
		`SELECT count(*) FROM profile_revisions WHERE profile_id = $1`, p.Id).Scan(&revCount); err != nil {
		t.Fatal(err)
	}
	if revCount != 1 {
		t.Errorf("revision rows = %d, want 1", revCount)
	}

	// Clone.
	clone, err := svc.CloneProfile(ctx, &v1.CloneProfileRequest{Id: p.Id, NewName: "noble-web-copy"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if clone.Name != "noble-web-copy" {
		t.Errorf("clone name = %s", clone.Name)
	}

	// Delete blocked while a machine is assigned (FR-009).
	if _, err := machines.Register(ctx, biz.RegisterInput{
		MAC: "52:54:00:aa:aa:aa", Name: "assigned-node", ProfileID: p.Id}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.DeleteProfile(ctx, &v1.GetProfileRequest{Id: p.Id})
	if r := kerrors.FromError(err).Reason; r != "CONFLICT" {
		t.Errorf("delete-in-use reason = %s, want CONFLICT", r)
	}

	// Unassigned clone deletes fine.
	if _, err := svc.DeleteProfile(ctx, &v1.GetProfileRequest{Id: clone.Id}); err != nil {
		t.Fatalf("delete clone: %v", err)
	}
}
