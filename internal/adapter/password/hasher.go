// Package password adapts internal/auth's argon2id helpers to the
// PasswordHasher port the IAM context declares.
package password

import "github.com/marcon0203/agentic-kit/internal/auth"

// Hasher is argon2id.
type Hasher struct{ dummy string }

// NewHasher precomputes the dummy hash sign-in verifies against when no
// such account exists. Computing it once at startup rather than per
// request keeps the two paths the same cost without paying for a KDF run
// twice on a miss.
func NewHasher() (*Hasher, error) {
	dummy, err := auth.HashPassword("this-is-not-a-real-password-anyones-account-has")
	if err != nil {
		return nil, err
	}
	return &Hasher{dummy: dummy}, nil
}

func (h *Hasher) Hash(password string) (string, error) { return auth.HashPassword(password) }

func (h *Hasher) Verify(password, hash string) (bool, error) {
	return auth.VerifyPassword(password, hash)
}

func (h *Hasher) DummyHash() string { return h.dummy }
