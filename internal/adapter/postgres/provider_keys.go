package postgres

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/crypto"
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

func (s *ProviderKeyStore) Keys(ctx context.Context, ownerID int64) (map[string]string, error) {
	rows, err := s.q.ListModelProvidersForOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
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
		keys[row.Provider] = string(plaintext)
	}
	return keys, nil
}
