package postgres

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/crypto"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// ProviderKeyStore returns an owner's model-provider credentials as
// provider name -> plaintext key, ready to hand to the model gateway.
//
// Only the newest row per provider is kept: re-adding a provider is how a
// user rotates a key, and the older rows stay only as history.
type ProviderKeyStore struct {
	q      store.Querier
	aesKey []byte
}

func NewProviderKeyStore(q store.Querier, aesKey []byte) *ProviderKeyStore {
	return &ProviderKeyStore{q: q, aesKey: aesKey}
}

// Keys returns an owner's usable credentials, keyed by provider name: their
// own personal connection when they have one, falling back to an admin's
// org-wide default (系统配置 → 模型提供商) otherwise. A personal credential
// always wins — the org default only fills gaps.
func (s *ProviderKeyStore) Keys(ctx context.Context, ownerID int64) (map[string]modelgateway.Credential, error) {
	rows, err := s.q.ListModelProvidersForOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	keys := map[string]modelgateway.Credential{}
	for _, row := range rows {
		if _, ok := keys[row.Provider]; ok {
			continue // ListModelProvidersForOwner is newest-first
		}
		encrypted, err := s.q.GetModelProviderCredentials(ctx, store.GetModelProviderCredentialsParams{ID: row.ID, OwnerUserID: ownerID})
		if err != nil {
			return nil, err
		}
		plaintext, err := crypto.Decrypt(s.aesKey, string(encrypted))
		if err != nil {
			return nil, err
		}
		keys[row.Provider] = modelgateway.Credential{APIKey: string(plaintext), BaseURL: row.BaseUrl.String}
	}

	defaults, err := s.q.ListCatalogProviderDefaultCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range defaults {
		if _, ok := keys[row.ProviderKey]; ok {
			continue // personal credential takes precedence
		}
		plaintext, err := crypto.Decrypt(s.aesKey, row.DefaultApiKeyEncrypted.String)
		if err != nil {
			return nil, err
		}
		keys[row.ProviderKey] = modelgateway.Credential{APIKey: string(plaintext), BaseURL: row.BaseUrl.String}
	}
	return keys, nil
}
