package postgres

import (
	"context"
	"sort"

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

// UsableProviders names the providers this owner can actually run with —
// Keys' own answer, with the credentials themselves dropped so the name
// list can travel out to the API layer and the frontend.
//
// It exists because "can this account run anything?" was previously asked
// two different ways: the run pre-flight and the compiler both ask Keys
// (personal connection *or* an admin's org-wide default), while the UI
// asked GET /model-providers, which only ever lists personal connections.
// An account running purely on an admin-configured org default therefore
// saw every 运行 button greyed out even though every run would have
// succeeded. One source of truth avoids re-introducing that skew.
func (s *ProviderKeyStore) UsableProviders(ctx context.Context, ownerID int64) ([]string, error) {
	keys, err := s.Keys(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
