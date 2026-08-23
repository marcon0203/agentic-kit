// Package crypto adapts internal/crypto's AES-256-GCM helpers to the
// CredentialCipher port the resource context declares.
package crypto

import "github.com/marcon0203/agentic-kit/internal/crypto"

// Cipher encrypts single credential values with a fixed AES-256 key.
type Cipher struct{ key []byte }

func NewCipher(key []byte) *Cipher { return &Cipher{key: key} }

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	return crypto.Encrypt(c.key, []byte(plaintext))
}

func (c *Cipher) Decrypt(ciphertext string) (string, error) {
	plain, err := crypto.Decrypt(c.key, ciphertext)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
