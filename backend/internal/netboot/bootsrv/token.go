// Package bootsrv serves the machine-facing boot HTTP endpoints: iPXE script,
// kernel/initrd streaming, NoCloud seed, and the install-report callback.
package bootsrv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
)

// SeedPayload is the one-time credential bundle bound to a boot token.
// It holds only a password HASH, never cleartext (FR-018).
type SeedPayload struct {
	SessionID    string `json:"session_id"`
	MachineID    string `json:"machine_id"`
	PasswordHash string `json:"password_hash"`
}

// TokenStore issues and resolves single-use seed tokens (Valkey, TTL'd).
type TokenStore struct {
	vk  valkey.Client
	ttl time.Duration
}

func NewTokenStore(vk valkey.Client, ttl time.Duration) *TokenStore {
	return &TokenStore{vk: vk, ttl: ttl}
}

func seedKey(token string) string { return "seedtoken:" + token }

// Issue creates a token bound to the payload with the configured TTL.
func (s *TokenStore) Issue(ctx context.Context, p SeedPayload) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate seed token: %w", err)
	}
	token := hex.EncodeToString(buf)
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal seed payload: %w", err)
	}
	cmd := s.vk.B().Set().Key(seedKey(token)).Value(string(raw)).Ex(s.ttl).Build()
	if err := s.vk.Do(ctx, cmd).Error(); err != nil {
		return "", fmt.Errorf("store seed token: %w", err)
	}
	return token, nil
}

// Resolve returns the payload for a token, or an error if missing/expired.
// The token is NOT consumed here: subiquity fetches user-data, meta-data and
// vendor-data separately within one boot, so the token must survive the whole
// seed phase. It expires by TTL and is invalidated on terminal report.
func (s *TokenStore) Resolve(ctx context.Context, token string) (*SeedPayload, error) {
	cmd := s.vk.B().Get().Key(seedKey(token)).Build()
	raw, err := s.vk.Do(ctx, cmd).ToString()
	if err != nil {
		return nil, fmt.Errorf("seed token not found or expired")
	}
	var p SeedPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("decode seed payload: %w", err)
	}
	return &p, nil
}

// Invalidate deletes a token once its session reaches a terminal state.
func (s *TokenStore) Invalidate(ctx context.Context, token string) error {
	cmd := s.vk.B().Del().Key(seedKey(token)).Build()
	if err := s.vk.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("invalidate seed token: %w", err)
	}
	return nil
}
