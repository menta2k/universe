package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
	"universe/backend/internal/data"
	"universe/backend/internal/service"
	"universe/backend/tests/integration/testenv"
)

type gate struct{ enabled bool }

func (g *gate) Enabled(context.Context) (bool, error) { return g.enabled, nil }

func newMachineService(t *testing.T, env *testenv.Env, g biz.DhcpGate) *service.MachineService {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	machines := biz.NewMachineUsecase(
		data.NewMachineRepo(env.Data), data.NewSessionRepo(env.Data),
		data.NewProfileRepo(env.Data), g, events, log)
	return service.NewMachineService(machines)
}

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	return kerrors.FromError(err).Reason
}

func TestMachineServiceContract(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	g := &gate{enabled: true}
	svc := newMachineService(t, env, g)

	// Create with valid data.
	m, err := svc.CreateMachine(ctx, &v1.CreateMachineRequest{
		Mac: "52:54:00:01:02:03", Name: "node-a", ProfileId: profileID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.ProvisionState != "ready" {
		t.Errorf("state = %s, want ready", m.ProvisionState)
	}

	// Invalid MAC -> VALIDATION_FAILED with details.
	_, err = svc.CreateMachine(ctx, &v1.CreateMachineRequest{Mac: "bad", Name: "x"})
	if r := reasonOf(t, err); r != "VALIDATION_FAILED" {
		t.Errorf("reason = %s, want VALIDATION_FAILED", r)
	}
	if len(kerrors.FromError(err).Metadata) == 0 {
		t.Error("expected field details in validation error")
	}

	// Provision succeeds, second provision conflicts.
	if _, err := svc.Provision(ctx, &v1.GetMachineRequest{Id: m.Id}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	_, err = svc.Provision(ctx, &v1.GetMachineRequest{Id: m.Id})
	if r := reasonOf(t, err); r != "CONFLICT" {
		t.Errorf("second provision reason = %s, want CONFLICT", r)
	}

	// Delete blocked while installing.
	_, err = svc.DeleteMachine(ctx, &v1.GetMachineRequest{Id: m.Id})
	if r := reasonOf(t, err); r != "CONFLICT" {
		t.Errorf("delete-while-installing reason = %s, want CONFLICT", r)
	}

	// Cancel, then delete succeeds.
	if _, err := svc.CancelProvision(ctx, &v1.GetMachineRequest{Id: m.Id}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := svc.DeleteMachine(ctx, &v1.GetMachineRequest{Id: m.Id}); err != nil {
		t.Fatalf("delete after cancel: %v", err)
	}

	// Not found.
	if r := reasonOf(t, func() error {
		_, e := svc.GetMachine(ctx, &v1.GetMachineRequest{Id: "00000000-0000-0000-0000-000000000000"})
		return e
	}()); r != "NOT_FOUND" {
		t.Errorf("get missing reason = %s, want NOT_FOUND", r)
	}
}

func TestMachineServiceProvisionRequiresDhcp(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	svc := newMachineService(t, env, &gate{enabled: false})

	m, err := svc.CreateMachine(ctx, &v1.CreateMachineRequest{
		Mac: "52:54:00:0a:0b:0c", Name: "node-b", ProfileId: profileID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Provision(ctx, &v1.GetMachineRequest{Id: m.Id})
	if r := reasonOf(t, err); r != "DHCP_DISABLED" {
		t.Errorf("provision with dhcp off reason = %s, want DHCP_DISABLED", r)
	}
}
