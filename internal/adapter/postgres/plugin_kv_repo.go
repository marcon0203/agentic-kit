package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// PluginKVStore implements the kv.get/set host function's storage
// (spec-20 §4.3) against the plugin_kv table. namespace is
// "{plugin_id}:{owner_user_id}" — exactly the string
// internal/adapter/extism's callerNamespace derives from the call's own
// identity, never from anything a plugin's request claims.
type PluginKVStore struct{ q store.Querier }

func NewPluginKVStore(q store.Querier) *PluginKVStore { return &PluginKVStore{q: q} }

func (s *PluginKVStore) Get(ctx context.Context, namespace, key string) (string, bool, error) {
	pluginID, ownerID, err := splitNamespace(namespace)
	if err != nil {
		return "", false, err
	}
	row, err := s.q.GetPluginKV(ctx, store.GetPluginKVParams{PluginID: pluginID, OwnerUserID: ownerID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.Value, true, nil
}

func (s *PluginKVStore) Set(ctx context.Context, namespace, key, value string) error {
	pluginID, ownerID, err := splitNamespace(namespace)
	if err != nil {
		return err
	}
	_, err = s.q.UpsertPluginKV(ctx, store.UpsertPluginKVParams{PluginID: pluginID, OwnerUserID: ownerID, Key: key, Value: value})
	return err
}

func splitNamespace(namespace string) (pluginID string, ownerID int64, err error) {
	pluginID, ownerIDStr, ok := strings.Cut(namespace, ":")
	if !ok {
		return "", 0, errors.New("plugin kv: malformed namespace")
	}
	ownerID, err = strconv.ParseInt(ownerIDStr, 10, 64)
	if err != nil {
		return "", 0, errors.New("plugin kv: malformed namespace")
	}
	return pluginID, ownerID, nil
}
