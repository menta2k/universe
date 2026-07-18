package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
	"universe/backend/internal/data"
	"universe/backend/internal/service"
	"universe/backend/tests/integration/testenv"
)

func TestSessionServiceTimeline(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	profileID := seedProfile(t, env)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	machineRepo := data.NewMachineRepo(env.Data)
	sessionRepo := data.NewSessionRepo(env.Data)
	machines := biz.NewMachineUsecase(machineRepo, sessionRepo, data.NewProfileRepo(env.Data),
		&gate{enabled: true}, events, log)
	sessionsUC := biz.NewSessionUsecase(sessionRepo, machineRepo, events, log)

	m, err := machines.Register(ctx, biz.RegisterInput{
		MAC: "52:54:00:cc:cc:01", Name: "timeline-node", ProfileID: profileID})
	if err != nil {
		t.Fatal(err)
	}
	armed, err := machines.Provision(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Emit a couple of events across the boot, then complete.
	events.Record(ctx, biz.Event{SessionID: armed.ActiveSessionID, MachineMAC: m.MAC,
		Phase: biz.PhaseIPXEScript, Outcome: biz.OutcomeOK})
	events.Record(ctx, biz.Event{SessionID: armed.ActiveSessionID, MachineMAC: m.MAC,
		Phase: biz.PhaseSeedServed, Outcome: biz.OutcomeOK})
	if err := sessionsUC.ReportInstall(ctx, armed.ActiveSessionID, "ok", ""); err != nil {
		t.Fatal(err)
	}

	svc := service.NewSessionService(biz.NewSessionQueryUsecase(data.NewSessionQueryRepo(env.Data)))

	list, err := svc.ListSessions(ctx, &v1.ListSessionsRequest{MachineId: m.ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].State != "completed" {
		t.Fatalf("unexpected session list: %+v", list.Sessions)
	}
	if list.Sessions[0].MachineName != "timeline-node" {
		t.Errorf("machine name not enriched: %q", list.Sessions[0].MachineName)
	}

	detail, err := svc.GetSession(ctx, &v1.GetSessionRequest{Id: armed.ActiveSessionID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.Timeline) < 3 {
		t.Errorf("expected >=3 timeline events, got %d", len(detail.Timeline))
	}
	// Timeline must be time-ordered ascending.
	for i := 1; i < len(detail.Timeline); i++ {
		if detail.Timeline[i].Time.AsTime().Before(detail.Timeline[i-1].Time.AsTime()) {
			t.Error("timeline not ascending by time")
		}
	}
}

func TestSessionServiceFilters(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	svc := service.NewSessionService(biz.NewSessionQueryUsecase(data.NewSessionQueryRepo(env.Data)))

	// Empty DB: no sessions, no error.
	list, err := svc.ListSessions(ctx, &v1.ListSessionsRequest{State: "active"})
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list.Sessions) != 0 || list.Meta.Total != 0 {
		t.Errorf("expected empty result, got %+v", list)
	}
}
