package service

import (
	"context"
	"encoding/json"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/netboot/autoinstall"
	"github.com/menta2k/universe/backend/internal/server"
)

type ProfileService struct {
	v1.UnimplementedProfileServiceServer
	profiles *biz.ProfileUsecase
	machines *biz.MachineUsecase
}

func NewProfileService(profiles *biz.ProfileUsecase, machines *biz.MachineUsecase) *ProfileService {
	return &ProfileService{profiles: profiles, machines: machines}
}

func mapProfileErr(err error) error {
	if errors.Is(err, biz.ErrProfileInUse) {
		return server.ErrConflict("profile is assigned to one or more machines")
	}
	return mapErr(err)
}

func toProfileReply(p *biz.Profile) *v1.Profile {
	storage, _ := json.Marshal(p.StorageLayout)
	network, _ := json.Marshal(p.NetworkConfig)
	if p.NetworkConfig == nil {
		network = []byte("{}")
	}
	return &v1.Profile{
		Id: p.ID, Name: p.Name, Version: int32(p.Version),
		UbuntuRelease: string(p.UbuntuRelease),
		StorageLayout: string(storage), NetworkConfig: string(network),
		Packages: p.Packages, SshAuthorizedKeys: p.SSHAuthorizedKeys,
		UserDataTemplate: p.UserDataTemplate, LateCommands: p.LateCommands,
		KernelCmdlineExtra: p.KernelCmdlineExtra,
		KeyboardLayout:     p.KeyboardLayout, KeyboardVariant: p.KeyboardVariant,
		Locale: p.Locale, Timezone: p.Timezone,
		CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
		AssignedMachines: p.AssignedMachines,
	}
}

// parseInput converts a proto ProfileInput into a validated biz.ProfileInput.
func parseInput(in *v1.ProfileInput) (biz.ProfileInput, error) {
	out := biz.ProfileInput{
		Name: in.GetName(), UbuntuRelease: biz.UbuntuRelease(in.GetUbuntuRelease()),
		Packages: in.GetPackages(), SSHAuthorizedKeys: in.GetSshAuthorizedKeys(),
		UserDataTemplate: in.GetUserDataTemplate(), LateCommands: in.GetLateCommands(),
		KernelCmdlineExtra: in.GetKernelCmdlineExtra(),
		KeyboardLayout:     in.GetKeyboardLayout(), KeyboardVariant: in.GetKeyboardVariant(),
		Locale: in.GetLocale(), Timezone: in.GetTimezone(),
	}
	if s := in.GetStorageLayout(); s != "" {
		if err := json.Unmarshal([]byte(s), &out.StorageLayout); err != nil {
			return out, server.ErrValidation("invalid storage_layout JSON",
				map[string]string{"storage_layout": err.Error()})
		}
	}
	if n := in.GetNetworkConfig(); n != "" && n != "{}" {
		if err := json.Unmarshal([]byte(n), &out.NetworkConfig); err != nil {
			return out, server.ErrValidation("invalid network_config JSON",
				map[string]string{"network_config": err.Error()})
		}
	}
	return out, nil
}

func (s *ProfileService) ListProfiles(ctx context.Context, req *v1.PageRequest) (*v1.ListProfilesReply, error) {
	page, size := pageParams(req)
	profiles, total, err := s.profiles.List(ctx, page, size)
	if err != nil {
		return nil, mapProfileErr(err)
	}
	reply := &v1.ListProfilesReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, p := range profiles {
		reply.Profiles = append(reply.Profiles, toProfileReply(p))
	}
	return reply, nil
}

func (s *ProfileService) GetProfile(ctx context.Context, req *v1.GetProfileRequest) (*v1.Profile, error) {
	p, err := s.profiles.Get(ctx, req.GetId())
	if err != nil {
		return nil, mapProfileErr(err)
	}
	return toProfileReply(p), nil
}

func (s *ProfileService) CreateProfile(ctx context.Context, req *v1.ProfileInput) (*v1.Profile, error) {
	in, err := parseInput(req)
	if err != nil {
		return nil, err
	}
	p, err := s.profiles.Create(ctx, in)
	if err != nil {
		return nil, mapProfileErr(err)
	}
	return toProfileReply(p), nil
}

func (s *ProfileService) UpdateProfile(ctx context.Context, req *v1.UpdateProfileRequest) (*v1.Profile, error) {
	in, err := parseInput(req.GetProfile())
	if err != nil {
		return nil, err
	}
	p, err := s.profiles.Update(ctx, req.GetId(), in)
	if err != nil {
		return nil, mapProfileErr(err)
	}
	return toProfileReply(p), nil
}

func (s *ProfileService) CloneProfile(ctx context.Context, req *v1.CloneProfileRequest) (*v1.Profile, error) {
	p, err := s.profiles.Clone(ctx, req.GetId(), req.GetNewName())
	if err != nil {
		return nil, mapProfileErr(err)
	}
	return toProfileReply(p), nil
}

func (s *ProfileService) DeleteProfile(ctx context.Context, req *v1.GetProfileRequest) (*emptypb.Empty, error) {
	if err := s.profiles.Delete(ctx, req.GetId()); err != nil {
		return nil, mapProfileErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ProfileService) PreviewProfile(ctx context.Context, req *v1.PreviewProfileRequest) (*v1.PreviewProfileReply, error) {
	p, err := s.profiles.Get(ctx, req.GetId())
	if err != nil {
		return nil, mapProfileErr(err)
	}
	var machine *biz.Machine
	if req.GetMachineId() != "" {
		machine, err = s.machines.Get(ctx, req.GetMachineId())
		if err != nil {
			return nil, mapErr(err)
		}
	}
	userData, cmdline, err := autoinstall.PreviewRedacted(machine, p)
	if err != nil {
		return nil, server.ErrValidation("profile does not render to a valid document",
			map[string]string{"user_data_template": err.Error()})
	}
	return &v1.PreviewProfileReply{UserData: userData, Cmdline: cmdline}, nil
}
