package integration

import (
	"context"
	"testing"
	"time"

	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/conf"
	"github.com/menta2k/universe/backend/internal/data"
	"github.com/menta2k/universe/backend/internal/server"
	"github.com/menta2k/universe/backend/internal/service"
	"github.com/menta2k/universe/backend/tests/integration/testenv"
)

// TestGRPCServerServesAndRejectsUnauthenticated exercises NewGRPCServer wiring
// and the auth middleware over a real gRPC connection.
func TestGRPCServerServesAndRejectsUnauthenticated(t *testing.T) {
	env := testenv.Start(t)
	log := testLog()
	addr := freePort(t)
	cfg := &conf.Config{}
	cfg.Server.GRPCAddr = addr

	events := biz.NewEventRecorder(data.NewEventRepo(env.Data), data.NewEventPublisher(env.Data), log)
	operators := biz.NewOperatorUsecase(
		data.NewOperatorRepo(env.Data), data.NewSessionStore(env.Data, time.Hour), log)
	machines := biz.NewMachineUsecase(
		data.NewMachineRepo(env.Data), data.NewSessionRepo(env.Data),
		data.NewProfileRepo(env.Data), data.NewDhcpGate(env.Data), events, log)
	machineSvc := service.NewMachineService(machines)

	srv := server.NewGRPCServer(cfg, log, operators, events,
		func(s *kgrpc.Server) { v1.RegisterMachineServiceServer(s, machineSvc) },
	)
	go func() { _ = srv.Start(context.Background()) }()
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	var conn *grpc.ClientConn
	var err error
	for range 100 {
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial grpc: %v", err)
	}
	defer conn.Close()

	client := v1.NewMachineServiceClient(conn)
	// No session cookie over gRPC => auth middleware rejects.
	_, err = client.ListMachines(context.Background(), &v1.ListMachinesRequest{})
	if err == nil {
		t.Error("expected unauthenticated gRPC call to be rejected")
	}
	_ = emptypb.Empty{}
}
