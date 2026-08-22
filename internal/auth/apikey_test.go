package auth

import (
	"strings"
	"testing"
)

func TestGenerateAPIKey_HashMatchesHashAPIKey(t *testing.T) {
	raw, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(raw, APIKeyPrefix) {
		t.Fatalf("raw key %q missing prefix %q", raw, APIKeyPrefix)
	}
	if HashAPIKey(raw) != hash {
		t.Fatalf("HashAPIKey(raw) = %q, want %q", HashAPIKey(raw), hash)
	}
}

func TestGenerateAPIKey_UniquePerCall(t *testing.T) {
	raw1, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate 1: %v", err)
	}
	raw2, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate 2: %v", err)
	}
	if raw1 == raw2 {
		t.Fatal("two generated keys must not collide")
	}
}
