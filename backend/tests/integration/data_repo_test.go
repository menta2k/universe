package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

func TestMachineRepoDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	repo := data.NewMachineRepo(env.Data)
	profileID := seedProfile(t, env)

	created, err := repo.Create(ctx, &biz.Machine{
		MAC: "52:54:00:d0:0d:01", Name: "repo-a", Firmware: biz.FirmwareBIOS,
		ProfileID: profileID, State: biz.StateReady})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Duplicate MAC surfaces a field validation error (wrapConstraint).
	_, err = repo.Create(ctx, &biz.Machine{MAC: "52:54:00:d0:0d:01", Name: "dup",
		Firmware: biz.FirmwareUnknown, State: biz.StateNew})
	var ve *biz.ValidationError
	if !asVE(err, &ve) {
		t.Errorf("duplicate mac: expected ValidationError, got %v", err)
	}

	// Update every field-masked path.
	name, notes, ip := "repo-a2", "note", "192.168.90.50"
	fw := biz.FirmwareUEFI
	st := biz.StateInstalling
	updated, err := repo.Update(ctx, created.ID, biz.MachineUpdate{
		Name: &name, Notes: &notes, ReservationIP: &ip, Firmware: &fw, State: &st})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != name || updated.Firmware != fw || updated.State != st ||
		updated.ReservationIP != ip || updated.Notes != notes {
		t.Errorf("update did not apply all fields: %+v", updated)
	}

	// GetByMAC + filtered List.
	if _, err := repo.GetByMAC(ctx, "52:54:00:d0:0d:01"); err != nil {
		t.Errorf("get by mac: %v", err)
	}
	list, total, err := repo.List(ctx, biz.MachineFilter{State: biz.StateInstalling, Query: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("filtered list total=%d len=%d", total, len(list))
	}

	// Delete.
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); err != biz.ErrEntityNotFound {
		t.Errorf("delete missing: %v, want ErrEntityNotFound", err)
	}
}

func TestOperatorRepoDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	repo := data.NewOperatorRepo(env.Data)

	if n, _ := repo.Count(ctx); n != 0 {
		t.Fatalf("fresh db should have 0 operators, got %d", n)
	}
	op, err := repo.Create(ctx, &biz.Operator{Username: "alice", PasswordHash: "h", DisplayName: "Alice", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchLogin(ctx, op.ID); err != nil {
		t.Errorf("touch login: %v", err)
	}
	got, err := repo.GetByID(ctx, op.ID)
	if err != nil || got.Username != "alice" {
		t.Errorf("get by id: %v %+v", err, got)
	}
	if _, err := repo.GetByUsername(ctx, "nobody"); err != biz.ErrEntityNotFound {
		t.Errorf("missing user: %v", err)
	}
}

func TestTransferLoggerAndListDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	logger := data.NewTransferLogger(env.Data)
	logger.LogTransfer(ctx, "192.168.90.10", "undionly.kpxe", 12345, true, "")
	logger.LogTransfer(ctx, "192.168.90.11", "ipxe.efi", 0, false, "not found")

	transfers, total, err := data.NewTransferRepo(env.Data).ListTransfers(ctx, "", 1, 50)
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if total < 2 || len(transfers) < 2 {
		t.Errorf("expected >=2 transfers, got total=%d len=%d", total, len(transfers))
	}
	// Filter by filename.
	filtered, _, err := data.NewTransferRepo(env.Data).ListTransfers(ctx, "undionly.kpxe", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range filtered {
		if !strings.Contains(tr.Filename, "undionly") {
			t.Errorf("filter leaked: %s", tr.Filename)
		}
	}
}

func TestForeignOfferSinkDirect(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	sink := data.NewForeignOfferSink(env.Data)
	sink.RecordForeignOffer(ctx, "192.168.90.254", "52:54:00:ee:ee:ee", "192.168.90.77")

	repo := data.NewDhcpConfigRepo(env.Data)
	servers, total, err := repo.ListForeignServers(ctx, 1, 50)
	if err != nil {
		t.Fatalf("list foreign: %v", err)
	}
	if total != 1 || len(servers) != 1 || servers[0].ServerID != "192.168.90.254" {
		t.Errorf("unexpected foreign servers: %+v", servers)
	}
}

// asVE is a local errors.As wrapper for *biz.ValidationError.
func asVE(err error, target **biz.ValidationError) bool {
	for err != nil {
		if ve, ok := err.(*biz.ValidationError); ok {
			*target = ve
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
