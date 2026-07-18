package service

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "universe/backend/api/netboot/v1"
	"universe/backend/internal/biz"
)

type DhcpService struct {
	v1.UnimplementedDhcpServiceServer
	dhcp *biz.DhcpConfigUsecase
}

func NewDhcpService(dhcp *biz.DhcpConfigUsecase) *DhcpService {
	return &DhcpService{dhcp: dhcp}
}

func toDhcpConfigReply(c *biz.DhcpConfig) *v1.DhcpConfig {
	reply := &v1.DhcpConfig{
		Enabled: c.Enabled, Version: int32(c.Version),
		LeaseTtlSeconds: int32(c.LeaseTTLSeconds),
	}
	for _, s := range c.Subnets {
		reply.Subnets = append(reply.Subnets, &v1.DhcpSubnet{
			Id: s.ID, Network: s.Network, RangeStart: s.RangeStart,
			RangeEnd: s.RangeEnd, Gateway: s.Gateway, Dns: s.DNS,
		})
	}
	return reply
}

func (s *DhcpService) GetDhcpConfig(ctx context.Context, _ *emptypb.Empty) (*v1.DhcpConfig, error) {
	c, err := s.dhcp.Get(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return toDhcpConfigReply(c), nil
}

func (s *DhcpService) UpdateDhcpConfig(ctx context.Context, req *v1.UpdateDhcpConfigRequest) (*v1.DhcpConfig, error) {
	subnets := make([]biz.DhcpSubnet, 0, len(req.GetSubnets()))
	for _, s := range req.GetSubnets() {
		subnets = append(subnets, biz.DhcpSubnet{
			Network: s.GetNetwork(), RangeStart: s.GetRangeStart(),
			RangeEnd: s.GetRangeEnd(), Gateway: s.GetGateway(), DNS: s.GetDns(),
		})
	}
	c, err := s.dhcp.Update(ctx, int(req.GetLeaseTtlSeconds()), subnets)
	if err != nil {
		return nil, mapErr(err)
	}
	return toDhcpConfigReply(c), nil
}

func (s *DhcpService) EnableDhcp(ctx context.Context, _ *emptypb.Empty) (*v1.DhcpConfig, error) {
	c, err := s.dhcp.Enable(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return toDhcpConfigReply(c), nil
}

func (s *DhcpService) DisableDhcp(ctx context.Context, _ *emptypb.Empty) (*v1.DhcpConfig, error) {
	c, err := s.dhcp.Disable(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return toDhcpConfigReply(c), nil
}

func (s *DhcpService) ListLeases(ctx context.Context, req *v1.PageRequest) (*v1.ListLeasesReply, error) {
	page, size := pageParams(req)
	leases, total, err := s.dhcp.ListLeases(ctx, page, size)
	if err != nil {
		return nil, mapErr(err)
	}
	reply := &v1.ListLeasesReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, l := range leases {
		lease := &v1.Lease{Ip: l.IP, Mac: l.MAC, MachineId: l.MachineID, MachineName: l.MachineName}
		if l.ExpiresAt > 0 {
			lease.ExpiresAt = timestamppb.New(unixToTime(l.ExpiresAt))
		}
		reply.Leases = append(reply.Leases, lease)
	}
	return reply, nil
}

func (s *DhcpService) ListForeignServers(ctx context.Context, req *v1.PageRequest) (*v1.ListForeignServersReply, error) {
	page, size := pageParams(req)
	servers, total, err := s.dhcp.ListForeignServers(ctx, page, size)
	if err != nil {
		return nil, mapErr(err)
	}
	reply := &v1.ListForeignServersReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, f := range servers {
		reply.Servers = append(reply.Servers, &v1.ForeignServer{
			ServerId: f.ServerID, LastSeen: timestamppb.New(unixToTime(f.LastSeen)),
			OffersSeen: f.OffersSeen,
		})
	}
	return reply, nil
}
