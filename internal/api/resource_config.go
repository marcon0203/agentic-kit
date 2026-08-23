package api

import (
	adaptercrypto "github.com/marcon0203/agentic-kit/internal/adapter/crypto"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// DecryptConfigCredentials reverses the credential encryption the resource
// context applies on write. Still here because the run engine (spec-10)
// needs a decrypted config to build a real tool; it moves into the run
// context's own ports when that module migrates. The *rule* — which keys
// count as credentials — lives in internal/domain/resource, not here.
func DecryptConfigCredentials(key []byte, config map[string]any) (map[string]any, error) {
	cipher := adaptercrypto.NewCipher(key)
	out := make(map[string]any, len(config))
	for k, v := range config {
		if !resource.IsCredentialKey(k) {
			out[k] = v
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		plaintext, err := cipher.Decrypt(s)
		if err != nil {
			return nil, err
		}
		out[k] = plaintext
	}
	return out, nil
}
