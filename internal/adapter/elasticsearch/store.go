// Package elasticsearch implements knowledgebase.KeywordStore over a
// single shared Elasticsearch index — the BM25 keyword-search leg of
// 多路召回, run alongside Milvus's vector search and merged by
// internal/domain/knowledgebase's Reciprocal Rank Fusion. One index
// ("kb_chunks") holds every knowledge base's chunks; owner_id/
// knowledge_base_id term filters do the per-KB/per-owner scoping,
// mirroring internal/adapter/milvus's single-collection design.
//
// Named `esstore` rather than `elasticsearch` (despite the directory name
// matching the rest of this repo's adapter packages) because the official
// client package is itself called `elasticsearch` — importing both under
// the same identifier would force an alias at every call site anyway, so
// the package declares its own name instead.
package esstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	es "github.com/elastic/go-elasticsearch/v8"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
)

const indexName = "kb_chunks"

// Config is Elasticsearch connection settings — mirrors
// internal/config.Config's ELASTICSEARCH_* fields so main.go can pass them
// straight through. Set either APIKey or Username/Password, not both; all
// empty means no auth (a local dev instance with security disabled).
type Config struct {
	Addr     string
	Username string
	Password string
	APIKey   string
}

type Store struct {
	client *es.Client
}

var _ knowledgebase.KeywordStore = (*Store)(nil)

// NewStore connects to Elasticsearch and ensures the shared kb_chunks
// index exists with its mapping — idempotent, safe to call on every
// server startup.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	esCfg := es.Config{Addresses: []string{cfg.Addr}}
	switch {
	case cfg.APIKey != "":
		esCfg.APIKey = cfg.APIKey
	case cfg.Username != "":
		esCfg.Username = cfg.Username
		esCfg.Password = cfg.Password
	}

	client, err := es.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch: build client: %w", err)
	}
	s := &Store{client: client}
	if err := s.ensureIndex(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureIndex(ctx context.Context) error {
	existsRes, err := s.client.Indices.Exists([]string{indexName}, s.client.Indices.Exists.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("elasticsearch: check index: %w", err)
	}
	defer existsRes.Body.Close()
	if existsRes.StatusCode == 200 {
		return nil
	}
	if existsRes.StatusCode != 404 {
		return fmt.Errorf("elasticsearch: check index: unexpected status %s", existsRes.Status())
	}

	mapping := `{
		"mappings": {
			"properties": {
				"owner_id":          {"type": "long"},
				"knowledge_base_id": {"type": "long"},
				"source_ref":        {"type": "keyword"},
				"chunk_index":       {"type": "integer"},
				"content":           {"type": "text"},
				"created_at":        {"type": "date"}
			}
		}
	}`
	createRes, err := s.client.Indices.Create(indexName,
		s.client.Indices.Create.WithBody(bytes.NewReader([]byte(mapping))),
		s.client.Indices.Create.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("elasticsearch: create index: %w", err)
	}
	defer createRes.Body.Close()
	if createRes.IsError() {
		return fmt.Errorf("elasticsearch: create index: %s", createRes.String())
	}
	return nil
}

func chunkDocID(ownerID, knowledgeBaseID int64, sourceRef string, chunkIndex int) string {
	return fmt.Sprintf("%d_%d_%s_%d", ownerID, knowledgeBaseID, sourceRef, chunkIndex)
}

// Index writes chunks via the Bulk API — one round trip for however many
// chunks a document split into, rather than one Index call per chunk.
func (s *Store) Index(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string, chunks []knowledgebase.ChunkInsert) error {
	if len(chunks) == 0 {
		return nil
	}
	var buf bytes.Buffer
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range chunks {
		meta := map[string]any{"index": map[string]any{
			"_index": indexName,
			"_id":    chunkDocID(ownerID, knowledgeBaseID, sourceRef, c.ChunkIndex),
		}}
		if err := json.NewEncoder(&buf).Encode(meta); err != nil {
			return fmt.Errorf("elasticsearch: encode bulk meta: %w", err)
		}
		doc := map[string]any{
			"owner_id": ownerID, "knowledge_base_id": knowledgeBaseID,
			"source_ref": sourceRef, "chunk_index": c.ChunkIndex,
			"content": c.Content, "created_at": now,
		}
		if err := json.NewEncoder(&buf).Encode(doc); err != nil {
			return fmt.Errorf("elasticsearch: encode bulk doc: %w", err)
		}
	}

	res, err := s.client.Bulk(bytes.NewReader(buf.Bytes()),
		s.client.Bulk.WithContext(ctx), s.client.Bulk.WithRefresh("true"))
	if err != nil {
		return fmt.Errorf("elasticsearch: bulk index: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("elasticsearch: bulk index: %s", res.String())
	}
	var parsed struct {
		Errors bool `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err == nil && parsed.Errors {
		return fmt.Errorf("elasticsearch: one or more bulk index operations failed")
	}
	return nil
}

func (s *Store) DeleteSource(ctx context.Context, ownerID, knowledgeBaseID int64, sourceRef string) error {
	query := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []map[string]any{
					{"term": map[string]any{"owner_id": ownerID}},
					{"term": map[string]any{"knowledge_base_id": knowledgeBaseID}},
					{"term": map[string]any{"source_ref": sourceRef}},
				},
			},
		},
	}
	body, err := json.Marshal(query)
	if err != nil {
		return fmt.Errorf("elasticsearch: encode delete query: %w", err)
	}

	res, err := s.client.DeleteByQuery([]string{indexName}, bytes.NewReader(body),
		s.client.DeleteByQuery.WithContext(ctx),
		s.client.DeleteByQuery.WithRefresh(true),
		s.client.DeleteByQuery.WithConflicts("proceed"),
	)
	if err != nil {
		return fmt.Errorf("elasticsearch: delete by query: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("elasticsearch: delete by query: %s", res.String())
	}
	return nil
}

// SearchKeyword runs BM25 full-text match over the content field, scoped
// to this owner/knowledge base — the keyword leg of 多路召回.
func (s *Store) SearchKeyword(ctx context.Context, ownerID, knowledgeBaseID int64, query string, topK int) ([]knowledgebase.SearchResult, error) {
	body := map[string]any{
		"size": topK,
		"query": map[string]any{
			"bool": map[string]any{
				"must": map[string]any{
					"match": map[string]any{"content": query},
				},
				"filter": []map[string]any{
					{"term": map[string]any{"owner_id": ownerID}},
					{"term": map[string]any{"knowledge_base_id": knowledgeBaseID}},
				},
			},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch: encode search query: %w", err)
	}

	res, err := s.client.Search(
		s.client.Search.WithContext(ctx),
		s.client.Search.WithIndex(indexName),
		s.client.Search.WithBody(bytes.NewReader(encoded)),
	)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch: search: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch: search: %s", res.String())
	}

	var parsed struct {
		Hits struct {
			Hits []struct {
				Score  float64 `json:"_score"`
				Source struct {
					SourceRef  string `json:"source_ref"`
					ChunkIndex int    `json:"chunk_index"`
					Content    string `json:"content"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("elasticsearch: decode search response: %w", err)
	}

	out := make([]knowledgebase.SearchResult, 0, len(parsed.Hits.Hits))
	for _, h := range parsed.Hits.Hits {
		out = append(out, knowledgebase.SearchResult{
			Chunk: knowledgebase.Chunk{SourceRef: h.Source.SourceRef, ChunkIndex: h.Source.ChunkIndex, Content: h.Source.Content},
			Score: h.Score,
		})
	}
	return out, nil
}
