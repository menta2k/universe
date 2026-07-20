package service

import (
	"context"
	"encoding/json"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
)

type SessionService struct {
	v1.UnimplementedSessionServiceServer
	sessions *biz.SessionQueryUsecase
}

func NewSessionService(sessions *biz.SessionQueryUsecase) *SessionService {
	return &SessionService{sessions: sessions}
}

func toSessionReply(v *biz.SessionView) *v1.ProvisioningSession {
	// #nosec G115 -- profile version is a small monotonic counter
	reply := &v1.ProvisioningSession{
		Id: v.ID, MachineId: v.MachineID, MachineName: v.MachineName, MachineMac: v.MachineMAC,
		ProfileId: v.ProfileID, ProfileVersion: int32(v.ProfileVersion),
		State: string(v.State), StartedAt: timestamppb.New(v.StartedAt),
		FailurePhase: v.FailurePhase,
	}
	if !v.EndedAt.IsZero() && v.EndedAt.Unix() > 0 {
		reply.EndedAt = timestamppb.New(v.EndedAt)
	}
	return reply
}

func (s *SessionService) ListSessions(ctx context.Context, req *v1.ListSessionsRequest) (*v1.ListSessionsReply, error) {
	page, size := pageParams(req.GetPage())
	views, total, err := s.sessions.List(ctx, biz.SessionFilter{
		MachineID: req.GetMachineId(), State: biz.SessionState(req.GetState()),
		Page: page, PageSize: size,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	reply := &v1.ListSessionsReply{Meta: pageMeta(total, page, size)}
	for _, v := range views {
		reply.Sessions = append(reply.Sessions, toSessionReply(v))
	}
	return reply, nil
}

func (s *SessionService) GetSession(ctx context.Context, req *v1.GetSessionRequest) (*v1.SessionDetail, error) {
	view, timeline, err := s.sessions.Get(ctx, req.GetId())
	if err != nil {
		return nil, mapErr(err)
	}
	detail := &v1.SessionDetail{Session: toSessionReply(view)}
	for _, e := range timeline {
		d, _ := json.Marshal(e.Detail)
		detail.Timeline = append(detail.Timeline, &v1.ProvisioningEvent{
			Time: timestamppb.New(e.Time), SessionId: e.SessionID, MachineMac: e.MachineMAC,
			Phase: e.Phase, Outcome: e.Outcome, Detail: string(d),
		})
	}
	if view.Evidence != nil {
		ev, _ := json.Marshal(view.Evidence)
		detail.Evidence = string(ev)
	}
	return detail, nil
}
