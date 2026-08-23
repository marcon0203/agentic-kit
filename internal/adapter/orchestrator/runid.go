package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
)

// RunIDGenerator produces run ids in the shape api/openapi.yaml documents:
// "run-" followed by 16 hex characters, e.g. "run-9e67931d5c38024e".
type RunIDGenerator struct{}

func NewRunIDGenerator() RunIDGenerator { return RunIDGenerator{} }

func (RunIDGenerator) NewRunID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(buf), nil
}
