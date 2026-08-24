package adk

import (
	"context"
	"time"

	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// MemoryEntryInput is one remembered turn, ready to persist — the
// package-local, ADK-type-free shape MemoryStore deals in, so an
// implementation outside this package (internal/adapter/postgres) never
// needs to import google.golang.org/adk itself (spec-10: "所有 ADK 调用
// 收敛在 internal/orchestrator/adk 包内").
type MemoryEntryInput struct {
	Author  string
	Content string
}

// MemoryEntry is one remembered turn coming back out of a search.
type MemoryEntry struct {
	ID        string
	Author    string
	Content   string
	Timestamp time.Time
}

// MemoryStore persists and searches memory entries, scoped by the ADK
// identity triple every memory.Service call already carries (app, user,
// session) — real, restart-surviving storage behind capabilities.
// builtin_tools' load_memory/preload_memory, unlike ADK's own
// memory.InMemoryService which loses everything on restart.
type MemoryStore interface {
	AddEntries(ctx context.Context, appName, userID, sessionID string, entries []MemoryEntryInput) error
	Search(ctx context.Context, appName, userID, query string) ([]MemoryEntry, error)
}

// NewMemoryService adapts a MemoryStore to ADK's own memory.Service
// interface — the only place in this codebase that does, keeping every
// google.golang.org/adk/{memory,session} reference inside this package.
func NewMemoryService(store MemoryStore) memory.Service {
	return &memoryServiceAdapter{store: store}
}

type memoryServiceAdapter struct{ store MemoryStore }

func (a *memoryServiceAdapter) AddSessionToMemory(ctx context.Context, sess session.Session) error {
	var entries []MemoryEntryInput
	for ev := range sess.Events().All() {
		if ev.Content == nil {
			continue
		}
		text := contentText(ev.Content)
		if text == "" {
			continue
		}
		entries = append(entries, MemoryEntryInput{Author: ev.Author, Content: text})
	}
	if len(entries) == 0 {
		return nil
	}
	return a.store.AddEntries(ctx, sess.AppName(), sess.UserID(), sess.ID(), entries)
}

func (a *memoryServiceAdapter) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	results, err := a.store.Search(ctx, req.AppName, req.UserID, req.Query)
	if err != nil {
		return nil, err
	}
	out := make([]memory.Entry, len(results))
	for i, r := range results {
		out[i] = memory.Entry{
			ID:        r.ID,
			Content:   genai.NewContentFromText(r.Content, genai.RoleUser),
			Author:    r.Author,
			Timestamp: r.Timestamp,
		}
	}
	return &memory.SearchResponse{Memories: out}, nil
}
