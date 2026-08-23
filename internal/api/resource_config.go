package api

import (
	"fmt"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/crypto"
)

// credentialKeySubstrings marks a config field as a credential if its
// (lowercased) key contains any of these — covers the common shapes
// resource configs show up in (`api_key`, `apiKey`, `auth_token`, a plain
// `token`/`secret`/`password`, ...).
var credentialKeySubstrings = []string{
	"api_key", "apikey", "token", "secret", "password", "credential", "private_key",
}

func isCredentialKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range credentialKeySubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// EncryptConfigCredentials returns a copy of config with every
// credential-shaped value replaced by its AES-256-GCM ciphertext (spec-05:
// "config 中的凭证走 AES-256 加密存储"). Non-credential values pass through
// unchanged so the rest of the config stays queryable/inspectable.
func EncryptConfigCredentials(key []byte, config map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(config))
	for k, v := range config {
		if !isCredentialKey(k) {
			out[k] = v
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("resource config: credential field %q must be a string", k)
		}
		ciphertext, err := crypto.Encrypt(key, []byte(s))
		if err != nil {
			return nil, fmt.Errorf("resource config: encrypt %q: %w", k, err)
		}
		out[k] = ciphertext
	}
	return out, nil
}

// DecryptConfigCredentials reverses EncryptConfigCredentials — used only
// internally by the run engine to build a real tool.Tool (spec-10) from a
// resource's stored config; the decrypted result must never be returned
// from an HTTP handler.
func DecryptConfigCredentials(key []byte, config map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(config))
	for k, v := range config {
		if !isCredentialKey(k) {
			out[k] = v
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("resource config: credential field %q must be a string", k)
		}
		plaintext, err := crypto.Decrypt(key, s)
		if err != nil {
			return nil, fmt.Errorf("resource config: decrypt %q: %w", k, err)
		}
		out[k] = string(plaintext)
	}
	return out, nil
}

// RedactConfigForResponse returns a copy of config with every
// credential-shaped field removed entirely — spec-05's "凭证字段在任何 GET
// 响应中都不出现" means omitted, not masked or shown as ciphertext.
func RedactConfigForResponse(config map[string]any) map[string]any {
	out := make(map[string]any, len(config))
	for k, v := range config {
		if isCredentialKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}
