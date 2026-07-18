package data

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"universe/backend/internal/biz"
)

// OperatorRepo is the pgx implementation of biz.OperatorRepo.
type OperatorRepo struct {
	data *Data
}

func NewOperatorRepo(d *Data) *OperatorRepo { return &OperatorRepo{data: d} }

const operatorCols = `id, username, password_hash, display_name, active, coalesce(last_login_at, 'epoch'::timestamptz)`

func scanOperator(row pgx.Row) (*biz.Operator, error) {
	var op biz.Operator
	err := row.Scan(&op.ID, &op.Username, &op.PasswordHash, &op.DisplayName, &op.Active, &op.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, biz.ErrEntityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan operator: %w", err)
	}
	return &op, nil
}

func (r *OperatorRepo) GetByUsername(ctx context.Context, username string) (*biz.Operator, error) {
	return scanOperator(r.data.Pool.QueryRow(ctx,
		`SELECT `+operatorCols+` FROM operators WHERE username = $1`, username))
}

func (r *OperatorRepo) GetByID(ctx context.Context, id string) (*biz.Operator, error) {
	return scanOperator(r.data.Pool.QueryRow(ctx,
		`SELECT `+operatorCols+` FROM operators WHERE id = $1`, id))
}

func (r *OperatorRepo) Create(ctx context.Context, op *biz.Operator) (*biz.Operator, error) {
	return scanOperator(r.data.Pool.QueryRow(ctx,
		`INSERT INTO operators (username, password_hash, display_name, active)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+operatorCols, op.Username, op.PasswordHash, op.DisplayName, op.Active))
}

func (r *OperatorRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.data.Pool.QueryRow(ctx, `SELECT count(*) FROM operators`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count operators: %w", err)
	}
	return n, nil
}

func (r *OperatorRepo) TouchLogin(ctx context.Context, id string) error {
	if _, err := r.data.Pool.Exec(ctx,
		`UPDATE operators SET last_login_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("touch login: %w", err)
	}
	return nil
}

// SessionStore keeps opaque session tokens in Valkey with an idle TTL.
type SessionStore struct {
	data *Data
	ttl  time.Duration
}

func NewSessionStore(d *Data, ttl time.Duration) *SessionStore {
	return &SessionStore{data: d, ttl: ttl}
}

func sessionKey(token string) string { return "session:" + token }

func (s *SessionStore) Create(ctx context.Context, operatorID string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(buf)
	cmd := s.data.Valkey.B().Set().Key(sessionKey(token)).Value(operatorID).
		Ex(s.ttl).Build()
	if err := s.data.Valkey.Do(ctx, cmd).Error(); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return token, nil
}

func (s *SessionStore) Get(ctx context.Context, token string) (string, error) {
	// GETEX refreshes the idle timeout on every authenticated request.
	cmd := s.data.Valkey.B().Getex().Key(sessionKey(token)).Ex(s.ttl).Build()
	res, err := s.data.Valkey.Do(ctx, cmd).ToString()
	if err != nil {
		return "", biz.ErrEntityNotFound
	}
	return res, nil
}

func (s *SessionStore) Delete(ctx context.Context, token string) error {
	cmd := s.data.Valkey.B().Del().Key(sessionKey(token)).Build()
	if err := s.data.Valkey.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
