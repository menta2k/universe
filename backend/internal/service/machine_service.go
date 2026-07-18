package service

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
	"universe/backend/internal/server"
)

type MachineService struct {
	v1.UnimplementedMachineServiceServer
	machines *biz.MachineUsecase
}

func NewMachineService(machines *biz.MachineUsecase) *MachineService {
	return &MachineService{machines: machines}
}

func toMachineReply(m *biz.Machine) *v1.Machine {
	return &v1.Machine{
		Id: m.ID, Mac: m.MAC, Name: m.Name, Firmware: string(m.Firmware),
		ProfileId: m.ProfileID, ReservationIp: m.ReservationIP,
		ProvisionState: string(m.State), Notes: m.Notes,
		CreatedAt: timestamppb.New(m.CreatedAt), UpdatedAt: timestamppb.New(m.UpdatedAt),
		ActiveSessionId: m.ActiveSessionID,
	}
}

// mapErr converts biz/domain errors to typed API errors.
func mapErr(err error) error {
	var ve *biz.ValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ve):
		return server.ErrValidation("validation failed", ve.Fields)
	case errors.Is(err, biz.ErrEntityNotFound):
		return server.ErrNotFound("resource")
	case errors.Is(err, biz.ErrDhcpDisabled):
		return server.ErrDhcpDisabled()
	case errors.Is(err, biz.ErrSessionConflict):
		return server.ErrConflict("machine already has an active session")
	case errors.Is(err, biz.ErrNoActiveSession):
		return server.ErrConflict("machine has no active session")
	default:
		return err
	}
}

func pageParams(p *v1.PageRequest) (int, int) {
	if p == nil {
		return 1, 50
	}
	return int(p.Page), int(p.PageSize)
}

func (s *MachineService) ListMachines(ctx context.Context, req *v1.ListMachinesRequest) (*v1.ListMachinesReply, error) {
	page, size := pageParams(req.GetPage())
	filter := biz.MachineFilter{
		State: biz.ProvisionState(req.GetState()), ProfileID: req.GetProfileId(),
		Query: req.GetQ(), Page: page, PageSize: size,
	}
	machines, total, err := s.machines.List(ctx, filter)
	if err != nil {
		return nil, mapErr(err)
	}
	reply := &v1.ListMachinesReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, m := range machines {
		reply.Machines = append(reply.Machines, toMachineReply(m))
	}
	return reply, nil
}

func (s *MachineService) GetMachine(ctx context.Context, req *v1.GetMachineRequest) (*v1.Machine, error) {
	m, err := s.machines.Get(ctx, req.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}

func (s *MachineService) CreateMachine(ctx context.Context, req *v1.CreateMachineRequest) (*v1.Machine, error) {
	m, err := s.machines.Register(ctx, biz.RegisterInput{
		MAC: req.GetMac(), Name: req.GetName(), Firmware: biz.Firmware(req.GetFirmware()),
		ProfileID: req.GetProfileId(), ReservationIP: req.GetReservationIp(), Notes: req.GetNotes(),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}

func (s *MachineService) UpdateMachine(ctx context.Context, req *v1.UpdateMachineRequest) (*v1.Machine, error) {
	up := biz.MachineUpdate{}
	if req.Name != nil {
		up.Name = req.Name
	}
	if req.ProfileId != nil {
		up.ProfileID = req.ProfileId
	}
	if req.ReservationIp != nil {
		up.ReservationIP = req.ReservationIp
	}
	if req.Notes != nil {
		up.Notes = req.Notes
	}
	m, err := s.machines.Update(ctx, req.GetId(), up)
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}

func (s *MachineService) DeleteMachine(ctx context.Context, req *v1.GetMachineRequest) (*emptypb.Empty, error) {
	if err := s.machines.Delete(ctx, req.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *MachineService) Provision(ctx context.Context, req *v1.GetMachineRequest) (*v1.Machine, error) {
	m, err := s.machines.Provision(ctx, req.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}

func (s *MachineService) CancelProvision(ctx context.Context, req *v1.GetMachineRequest) (*v1.Machine, error) {
	m, err := s.machines.Cancel(ctx, req.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}

func (s *MachineService) ListUnknownBoots(ctx context.Context, req *v1.PageRequest) (*v1.ListUnknownBootsReply, error) {
	page, size := pageParams(req)
	boots, total, err := s.machines.ListUnknownBoots(ctx, page, size)
	if err != nil {
		return nil, mapErr(err)
	}
	reply := &v1.ListUnknownBootsReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, b := range boots {
		reply.Boots = append(reply.Boots, &v1.UnknownBoot{
			Mac: b.MAC, LastSeen: timestamppb.New(b.LastSeen), Attempts: b.Attempts,
		})
	}
	return reply, nil
}

func (s *MachineService) RegisterFromUnknown(ctx context.Context, req *v1.RegisterFromUnknownRequest) (*v1.Machine, error) {
	m, err := s.machines.Register(ctx, biz.RegisterInput{
		MAC: req.GetMac(), Name: req.GetName(), ProfileID: req.GetProfileId(),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return toMachineReply(m), nil
}
