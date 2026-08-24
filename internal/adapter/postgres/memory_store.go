package postgres

import (
	"context"
	"strconv"

	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// searchLimit bounds how many memory entries a single load_memory/
// preload_memory call gets back — enough to be useful in a prompt without
// ballooning it.
const searchLimit = 10

// MemoryStore implements adk.MemoryStore against Postgres, scoped to one
// registered "memory" resource per owner — real persistence, ranked by
// Postgres full-text search, behind capabilities.builtin_tools'
// load_memory/preload_memory.
type MemoryStore struct {
	q        store.Querier
	memoryID int64
	ownerID  int64
}

// NewMemoryStore scopes the store to one memory resource, owned by
// ownerID — a run gets one of these per invocation, matching how a fresh
// modelgateway.Gateway is built per run.
func NewMemoryStore(q store.Querier, memoryID, ownerID int64) *MemoryStore {
	return &MemoryStore{q: q, memoryID: memoryID, ownerID: ownerID}
}

var _ adk.MemoryStore = (*MemoryStore)(nil)

func (s *MemoryStore) AddEntries(ctx context.Context, appName, userID, sessionID string, entries []adk.MemoryEntryInput) error {
	for _, e := range entries {
		_, err := s.q.InsertMemoryEntry(ctx, store.InsertMemoryEntryParams{
			MemoryID: s.memoryID, OwnerUserID: s.ownerID,
			AppName: appName, AgentUserID: userID, SessionID: sessionID,
			Author: e.Author, Content: e.Content,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) Search(ctx context.Context, appName, userID, query string) ([]adk.MemoryEntry, error) {
	rows, err := s.q.SearchMemoryEntries(ctx, store.SearchMemoryEntriesParams{
		MemoryID: s.memoryID, OwnerUserID: s.ownerID,
		AppName: appName, AgentUserID: userID,
		PlaintoTsquery: query, Limit: searchLimit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]adk.MemoryEntry, len(rows))
	for i, row := range rows {
		out[i] = adk.MemoryEntry{
			ID: strconv.FormatInt(row.ID, 10), Author: row.Author,
			Content: row.Content, Timestamp: row.CreatedAt.Time,
		}
	}
	return out, nil
}
