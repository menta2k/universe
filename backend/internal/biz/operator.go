package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alexedwards/argon2id"
)

// ErrEntityNotFound is the repository-level "no rows" sentinel shared by all
// repos; the service layer converts it to a typed 404.
var ErrEntityNotFound = errors.New("entity not found")

// ErrInvalidCredentials covers unknown user, wrong password, and inactive
// accounts uniformly so responses don't reveal which one happened.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Operator is an authenticated user of the admin interface.
type Operator struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	Active       bool
	LastLoginAt  time.Time
}

// OperatorRepo persists operators.
type OperatorRepo interface {
	GetByUsername(ctx context.Context, username string) (*Operator, error)
	GetByID(ctx context.Context, id string) (*Operator, error)
	Create(ctx context.Context, op *Operator) (*Operator, error)
	Count(ctx context.Context) (int, error)
	TouchLogin(ctx context.Context, id string) error
}

// SessionStore keeps opaque web session tokens (Valkey-backed, TTL'd).
type SessionStore interface {
	Create(ctx context.Context, operatorID string) (token string, err error)
	Get(ctx context.Context, token string) (operatorID string, err error)
	Delete(ctx context.Context, token string) error
}

// OperatorUsecase implements login, session authentication, and bootstrap.
type OperatorUsecase struct {
	repo     OperatorRepo
	sessions SessionStore
	log      *slog.Logger
}

func NewOperatorUsecase(repo OperatorRepo, sessions SessionStore, log *slog.Logger) *OperatorUsecase {
	return &OperatorUsecase{repo: repo, sessions: sessions, log: log}
}

// EnsureBootstrap creates the configured operator if none exist yet.
func (u *OperatorUsecase) EnsureBootstrap(ctx context.Context, username, password string) error {
	n, err := u.repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("count operators: %w", err)
	}
	if n > 0 {
		return nil
	}
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	if _, err := u.repo.Create(ctx, &Operator{
		Username:     username,
		PasswordHash: hash,
		DisplayName:  username,
		Active:       true,
	}); err != nil {
		return fmt.Errorf("create bootstrap operator: %w", err)
	}
	u.log.Info("bootstrap operator created", "username", username)
	return nil
}

// Login verifies credentials and opens a session.
func (u *OperatorUsecase) Login(ctx context.Context, username, password string) (*Operator, string, error) {
	op, err := u.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrEntityNotFound) {
			// Burn comparable time to avoid a user-enumeration timing oracle.
			_, _ = argon2id.ComparePasswordAndHash(password,
				"$argon2id$v=19$m=65536,t=1,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
			return nil, "", ErrInvalidCredentials
		}
		return nil, "", fmt.Errorf("lookup operator: %w", err)
	}
	match, err := argon2id.ComparePasswordAndHash(password, op.PasswordHash)
	if err != nil || !match || !op.Active {
		return nil, "", ErrInvalidCredentials
	}
	token, err := u.sessions.Create(ctx, op.ID)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	if err := u.repo.TouchLogin(ctx, op.ID); err != nil {
		u.log.Warn("touch last_login_at failed", "err", err, "operator", op.Username)
	}
	return op, token, nil
}

// AuthenticateSession resolves a session token to its operator.
func (u *OperatorUsecase) AuthenticateSession(ctx context.Context, token string) (*Operator, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	id, err := u.sessions.Get(ctx, token)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	op, err := u.repo.GetByID(ctx, id)
	if err != nil || !op.Active {
		return nil, ErrInvalidCredentials
	}
	return op, nil
}

// Logout destroys the session.
func (u *OperatorUsecase) Logout(ctx context.Context, token string) error {
	if err := u.sessions.Delete(ctx, token); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
