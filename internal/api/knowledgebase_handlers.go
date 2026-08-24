package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/knowledgebase"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// KnowledgeBaseHandlers is the HTTP transport for 知识库's document
// ingestion and search — the real retrieval that makes a knowledge_base
// resource more than a labeled config record.
type KnowledgeBaseHandlers struct {
	svc *knowledgebase.Service
}

func NewKnowledgeBaseHandlers(svc *knowledgebase.Service) *KnowledgeBaseHandlers {
	return &KnowledgeBaseHandlers{svc: svc}
}

// knowledgeBaseID decodes {id} into a resource id, rejecting anything that
// isn't a knowledge_base resource — the same opaque id every /resources/{id}
// endpoint uses, kept consistent so a client never needs a second id shape
// for the same resource.
func knowledgeBaseID(r *http.Request) (int64, bool) {
	kind, id, err := decodeResourceID(chi.URLParam(r, "id"))
	if err != nil || kind != resource.KindKnowledgeBase {
		return 0, false
	}
	return id, true
}

type ingestDocumentRequest struct {
	SourceRef string `json:"source_ref"`
	Content   string `json:"content"`
}

type ingestDocumentResponse struct {
	ChunkCount int `json:"chunk_count"`
}

// IngestDocument handles POST /resources/{id}/kb/documents.
func (h *KnowledgeBaseHandlers) IngestDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	kbID, ok := knowledgeBaseID(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}
	var req ingestDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	n, err := h.svc.Ingest(r.Context(), userID, kbID, req.SourceRef, req.Content)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, ingestDocumentResponse{ChunkCount: n})
}

type sourceDTO struct {
	SourceRef  string    `json:"source_ref"`
	ChunkCount int       `json:"chunk_count"`
	IngestedAt time.Time `json:"ingested_at"`
}

// ListDocuments handles GET /resources/{id}/kb/documents.
func (h *KnowledgeBaseHandlers) ListDocuments(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	kbID, ok := knowledgeBaseID(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	sources, err := h.svc.Sources(r.Context(), userID, kbID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]sourceDTO, 0, len(sources))
	for _, s := range sources {
		items = append(items, sourceDTO{SourceRef: s.SourceRef, ChunkCount: s.ChunkCount, IngestedAt: s.IngestedAt})
	}
	writeJSON(w, r, http.StatusOK, items)
}

// DeleteDocument handles DELETE /resources/{id}/kb/documents/{source_ref}.
func (h *KnowledgeBaseHandlers) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	kbID, ok := knowledgeBaseID(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}

	if err := h.svc.DeleteSource(r.Context(), userID, kbID, chi.URLParam(r, "source_ref")); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type searchKBRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type searchResultDTO struct {
	SourceRef string  `json:"source_ref"`
	Content   string  `json:"content"`
	Score     float64 `json:"score"`
}

// Search handles POST /resources/{id}/kb/search — a manual preview of what
// an Agent referencing this knowledge base would get back, useful for
// checking the ingest actually retrieves something sensible before wiring
// it into an Agent.
func (h *KnowledgeBaseHandlers) Search(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	kbID, ok := knowledgeBaseID(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
		return
	}
	var req searchKBRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	results, err := h.svc.Search(r.Context(), userID, kbID, req.Query, req.TopK)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	items := make([]searchResultDTO, 0, len(results))
	for _, res := range results {
		items = append(items, searchResultDTO{SourceRef: res.SourceRef, Content: res.Content, Score: res.Score})
	}
	writeJSON(w, r, http.StatusOK, items)
}
