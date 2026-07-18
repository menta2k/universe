package biz

import "context"

// BootFacade adapts the machine and session use cases into the single
// interface the boot HTTP server consumes (BootInfo + BootInfoBySession +
// ReportInstall). It keeps the protocol layer decoupled from two use cases.
type BootFacade struct {
	machines *MachineUsecase
	sessions *SessionUsecase
}

func NewBootFacade(machines *MachineUsecase, sessions *SessionUsecase) *BootFacade {
	return &BootFacade{machines: machines, sessions: sessions}
}

func (f *BootFacade) BootInfo(ctx context.Context, mac string) (*BootDecision, error) {
	return f.machines.BootInfo(ctx, mac)
}

func (f *BootFacade) BootInfoBySession(ctx context.Context, sessionID string) (*BootDecision, error) {
	return f.machines.BootInfoBySession(ctx, sessionID)
}

func (f *BootFacade) ReportInstall(ctx context.Context, sessionID, status, logTail string) error {
	return f.sessions.ReportInstall(ctx, sessionID, status, logTail)
}
