package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"universe/backend/internal/biz"
)

// SessionQueryRepo reads sessions + timelines for the UI (biz.SessionQueryRepo).
type SessionQueryRepo struct {
	data *Data
}

func NewSessionQueryRepo(d *Data) *SessionQueryRepo { return &SessionQueryRepo{data: d} }

const sessionViewCols = `s.id, s.machine_id, s.profile_id, s.profile_version, s.state,
	s.started_at, coalesce(s.ended_at,'epoch'::timestamptz), coalesce(s.failure_phase,''),
	s.evidence, m.name, m.mac::text`

func scanSessionView(row pgx.Row) (*biz.SessionView, error) {
	var v biz.SessionView
	var evidence []byte
	err := row.Scan(&v.ID, &v.MachineID, &v.ProfileID, &v.ProfileVersion, &v.State,
		&v.StartedAt, &v.EndedAt, &v.FailurePhase, &evidence, &v.MachineName, &v.MachineMAC)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan session view: %w", err)
	}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &v.Evidence)
	}
	return &v, nil
}

func (r *SessionQueryRepo) List(ctx context.Context, f biz.SessionFilter) ([]*biz.SessionView, int64, error) {
	where := []string{"true"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.MachineID != "" {
		where = append(where, "s.machine_id = "+arg(f.MachineID)+"::uuid")
	}
	if f.State != "" {
		where = append(where, "s.state = "+arg(string(f.State))+"::session_state")
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.data.Pool.QueryRow(ctx,
		"SELECT count(*) FROM provisioning_sessions s WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}
	page, size := normalizePage(f.Page, f.PageSize)
	q := "SELECT " + sessionViewCols + " FROM provisioning_sessions s " +
		"JOIN machines m ON m.id = s.machine_id WHERE " + cond +
		" ORDER BY s.started_at DESC LIMIT " + arg(size) + " OFFSET " + arg((page-1)*size)
	rows, err := r.data.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []*biz.SessionView
	for rows.Next() {
		v, err := scanSessionView(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (r *SessionQueryRepo) Get(ctx context.Context, id string) (*biz.SessionView, error) {
	return scanSessionView(r.data.Pool.QueryRow(ctx,
		"SELECT "+sessionViewCols+" FROM provisioning_sessions s "+
			"JOIN machines m ON m.id = s.machine_id WHERE s.id = $1", id))
}

func (r *SessionQueryRepo) Timeline(ctx context.Context, sessionID string) ([]biz.TimelineEvent, error) {
	rows, err := r.data.Pool.Query(ctx,
		`SELECT time, coalesce(session_id::text,''), coalesce(machine_mac::text,''),
		        phase, outcome, detail
		 FROM provisioning_events WHERE session_id = $1::uuid ORDER BY time ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()
	var out []biz.TimelineEvent
	for rows.Next() {
		var e biz.TimelineEvent
		var detail []byte
		if err := rows.Scan(&e.Time, &e.SessionID, &e.MachineMAC, &e.Phase, &e.Outcome, &detail); err != nil {
			return nil, fmt.Errorf("scan timeline event: %w", err)
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &e.Detail)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
