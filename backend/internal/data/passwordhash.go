package data

import (
	"fmt"

	"github.com/GehirnInc/crypt/sha512_crypt"
)

// Sha512Hasher hashes install-user passwords with sha512-crypt ($6$), the
// format Ubuntu's installer (subiquity identity.password) and libxcrypt on the
// installed system accept for console/login authentication. It implements
// biz.PasswordHasher.
type Sha512Hasher struct{}

func NewSha512Hasher() Sha512Hasher { return Sha512Hasher{} }

// Hash returns a $6$-prefixed sha512-crypt hash of plaintext with a random
// salt. Callers must not pass an empty password (that is a policy decision made
// by the profile use case, which stores an empty hash to mean "no password").
func (Sha512Hasher) Hash(plaintext string) (string, error) {
	hash, err := sha512_crypt.New().Generate([]byte(plaintext), nil)
	if err != nil {
		return "", fmt.Errorf("sha512-crypt password: %w", err)
	}
	return hash, nil
}
