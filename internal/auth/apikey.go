package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// APIKeyPrefix marks a string as one of ours at a glance (same idea as
// GitHub's `ghp_` / Stripe's `sk_`), and lets a secret-scanner flag a leaked
// key without false-positiving on every random base64 string in a log.
const APIKeyPrefix = "agk_"

// GenerateAPIKey returns a new raw API key and the SHA-256 hex digest to
// store for it. Only the hash is ever persisted — the raw key is shown to
// the caller exactly once, at creation time.
func GenerateAPIKey() (rawKey string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate api key: %w", err)
	}
	rawKey = APIKeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return rawKey, HashAPIKey(rawKey), nil
}

// HashAPIKey returns the SHA-256 hex digest of a raw API key, for lookup
// against api_keys.key_hash.
func HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}
