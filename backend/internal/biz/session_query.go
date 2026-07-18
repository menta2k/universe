package biz

import (
	"context"
	"time"
)

// SessionView is a session enriched with machine identity for the UI.
type SessionView struct {
	Session
	MachineName string
	MachineMAC  string
}

// TimelineEvent is one row of a session's provisioning timeline.
type TimelineEvent struct {
	Time       time.Time
	SessionID  string
	MachineMAC string
	Phase      string
	Outcome    string
	Detail     map[string]any
}

// SessionFilter narrows session list queries.
type SessionFilter struct {
	MachineID string
	State     SessionState
	Page      int
	PageSize  int
}

// SessionQueryRepo reads sessions and their timelines for the UI.
type SessionQueryRepo interface {
	List(ctx context.Context, f SessionFilter) ([]*SessionView, int64, error)
	Get(ctx context.Context, id string) (*SessionView, error)
	Timeline(ctx context.Context, sessionID string) ([]TimelineEvent, error)
}

// SessionQueryUsecase serves observability reads (US5).
type SessionQueryUsecase struct {
	repo SessionQueryRepo
}

func NewSessionQueryUsecase(repo SessionQueryRepo) *SessionQueryUsecase {
	return &SessionQueryUsecase{repo: repo}
}

func (u *SessionQueryUsecase) List(ctx context.Context, f SessionFilter) ([]*SessionView, int64, error) {
	return u.repo.List(ctx, f)
}

func (u *SessionQueryUsecase) Get(ctx context.Context, id string) (*SessionView, []TimelineEvent, error) {
	view, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	timeline, err := u.repo.Timeline(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return view, timeline, nil
}
