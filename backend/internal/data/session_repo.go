package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"universe/backend/internal/biz"
)

// SessionRepo is the pgx implementation of biz.SessionRepo.
type SessionRepo struct {
	data *Data
}

func NewSessionRepo(d *Data) *SessionRepo { return &SessionRepo{data: d} }

const sessionCols = `id, machine_id, profile_id, profile_version, state,
	started_at, coalesce(ended_at, 'epoch'::timestamptz), coalesce(failure_phase,''), evidence`

func scanSession(row pgx.Row) (*biz.Session, error) {
	var s biz.Session
	var evidence []byte
	err := row.Scan(&s.ID, &s.MachineID, &s.ProfileID, &s.ProfileVersion,
		&s.State, &s.StartedAt, &s.EndedAt, &s.FailurePhase, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	if len(evidence) > 0 {
		if err := json.Unmarshal(evidence, &s.Evidence); err != nil {
			return nil, fmt.Errorf("decode session evidence: %w", err)
		}
	}
	return &s, nil
}

func (r *SessionRepo) Create(ctx context.Context, s *biz.Session) (*biz.Session, error) {
	created, err := scanSession(r.data.Pool.QueryRow(ctx,
		`INSERT INTO provisioning_sessions (machine_id, profile_id, profile_version)
		 VALUES ($1, $2, $3) RETURNING `+sessionCols,
		s.MachineID, s.ProfileID, s.ProfileVersion))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return created, nil
}

func (r *SessionRepo) GetByID(ctx context.Context, id string) (*biz.Session, error) {
	return scanSession(r.data.Pool.QueryRow(ctx,
		`SELECT `+sessionCols+` FROM provisioning_sessions WHERE id = $1`, id))
}

func (r *SessionRepo) GetActiveByMachine(ctx context.Context, machineID string) (*biz.Session, error) {
	return scanSession(r.data.Pool.QueryRow(ctx,
		`SELECT `+sessionCols+` FROM provisioning_sessions
		 WHERE machine_id = $1 AND state = 'active'`, machineID))
}

// Finish transitions active -> terminal, merging evidence. Idempotent: rows
// already in a terminal state are left untouched.
func (r *SessionRepo) Finish(ctx context.Context, id string, state biz.SessionState, failurePhase string, evidence map[string]any) error {
	ev, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	_, err = r.data.Pool.Exec(ctx,
		`UPDATE provisioning_sessions
		 SET state = $2::session_state, ended_at = now(),
		     failure_phase = NULLIF($3,''), evidence = evidence || $4::jsonb
		 WHERE id = $1 AND state = 'active'`,
		id, string(state), failurePhase, ev)
	if err != nil {
		return fmt.Errorf("finish session: %w", err)
	}
	return nil
}

func (r *SessionRepo) ListActiveOlderThan(ctx context.Context, cutoff time.Time) ([]*biz.Session, error) {
	rows, err := r.data.Pool.Query(ctx,
		`SELECT `+sessionCols+` FROM provisioning_sessions
		 WHERE state = 'active' AND started_at < $1`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale sessions: %w", err)
	}
	defer rows.Close()
	var out []*biz.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
