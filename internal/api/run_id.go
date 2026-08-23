package api

import (
	"crypto/rand"
	"encoding/hex"
)

// newRunID generates a run_id matching api/openapi.yaml's example shape
// ("run-9e67931d5c38024e" — "run-" + 16 hex chars from 8 random bytes).
func newRunID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(buf), nil
}
