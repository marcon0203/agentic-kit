package api

import (
	"testing"

	"github.com/marcon0203/agentic-kit/internal/crypto"
)

func testAESKey() []byte {
	return []byte("01234567890123456789012345678901"[:32])
}

func TestRedactConfigForResponse_RemovesCredentialFields(t *testing.T) {
	config := map[string]any{
		"endpoint":  "https://mcp.internal/search",
		"api_key":   "sk-secret",
		"auth_type": "bearer",
		"token":     "another-secret",
	}

	redacted := RedactConfigForResponse(config)

	if _, ok := redacted["api_key"]; ok {
		t.Fatal("api_key must not appear in the redacted config")
	}
	if _, ok := redacted["token"]; ok {
		t.Fatal("token must not appear in the redacted config")
	}
	if redacted["endpoint"] != "https://mcp.internal/search" {
		t.Fatalf("non-credential field endpoint was dropped or altered: %v", redacted["endpoint"])
	}
	if redacted["auth_type"] != "bearer" {
		t.Fatalf("non-credential field auth_type was dropped or altered: %v", redacted["auth_type"])
	}
}

func TestEncryptConfigCredentials_EncryptsOnlyCredentialFields(t *testing.T) {
	key := testAESKey()
	config := map[string]any{
		"endpoint": "https://mcp.internal/search",
		"api_key":  "sk-secret",
	}

	encrypted, err := EncryptConfigCredentials(key, config)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if encrypted["endpoint"] != "https://mcp.internal/search" {
		t.Fatalf("non-credential field must pass through unchanged, got %v", encrypted["endpoint"])
	}

	ciphertext, ok := encrypted["api_key"].(string)
	if !ok || ciphertext == "sk-secret" {
		t.Fatalf("api_key must be replaced with ciphertext, got %v", encrypted["api_key"])
	}

	plaintext, err := crypto.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt round trip: %v", err)
	}
	if string(plaintext) != "sk-secret" {
		t.Fatalf("decrypted = %q, want sk-secret", plaintext)
	}
}

func TestEncryptConfigCredentials_RejectsNonStringCredential(t *testing.T) {
	_, err := EncryptConfigCredentials(testAESKey(), map[string]any{"api_key": 12345})
	if err == nil {
		t.Fatal("expected an error when a credential field isn't a string")
	}
}
