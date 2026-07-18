package bootsrv

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/alexedwards/argon2id"
)

// OneTimeCredentialMinter generates a random one-time password and returns its
// hash. The cleartext is discarded immediately: since profiles require SSH
// keys, the installed machine never needs the password (FR-018, Constitution
// IV — key-only access). The hash satisfies subiquity's identity requirement.
type OneTimeCredentialMinter struct{}

func NewOneTimeCredentialMinter() *OneTimeCredentialMinter { return &OneTimeCredentialMinter{} }

func (OneTimeCredentialMinter) MintOneTime() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate one-time password: %w", err)
	}
	cleartext := base64.RawStdEncoding.EncodeToString(buf)
	hash, err := argon2id.CreateHash(cleartext, argon2id.DefaultParams)
	if err != nil {
		return "", fmt.Errorf("hash one-time password: %w", err)
	}
	return hash, nil
}
