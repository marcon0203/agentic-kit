package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
)

// RunHandlers is the HTTP transport for the 编排运行时 context (spec-11).
// The launch chain, the black-box filter and the gate rules live in
// internal/domain/run; what is left here is JSON, status codes and the one
// endpoint that streams.
type RunHandlers struct {
	svc *run.Service
	// now is overridable in tests.
	now func() time.Time
}

func NewRunHandlers(svc *run.Service) *RunHandlers {
	return &RunHandlers{svc: svc, now: time.Now}
}

// ── DTOs ─────────────────────────────────────────────────────────────

type runSummaryDTO struct {
	RunID         string     `json:"run_id"`
	BundleRef     string     `json:"bundle_ref"`
	BundleVersion string     `json:"bundle_version"`
	Status        string     `json:"status"`
	Error         *string    `json:"error"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

type runUsageDTO struct {
	TotalTokens     int64   `json:"total_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	DurationSeconds int64   `json:"duration_seconds"`
}

type runDetailDTO struct {
	runSummaryDTO
	SharedState map[string]any `json:"shared_state"`
	IsOwner     bool           `json:"is_owner"`
	Usage       runUsageDTO    `json:"usage"`
}

func toRunSummaryDTO(r run.Run) runSummaryDTO {
	dto := runSummaryDTO{
		RunID: r.ID, BundleRef: r.BundleRef, BundleVersion: r.BundleVersion,
		Status: string(r.Status), CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
	}
	if r.Error != "" {
		errText := r.Error
		dto.Error = &errText
	}
	return dto
}

// ── Create ───────────────────────────────────────────────────────────

type createRunRequest struct {
	BundleRef     string         `json:"bundle_ref"`
	BundleVersion string         `json:"bundle_version"`
	Input         map[string]any `json:"input"`
}

// Create handles POST /runs. The run starts asynchronously — openapi's
// "异步启动，立即返回 run_id" — so the response is the freshly created run,
// still in `running`.
func (h *RunHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	created, err := h.svc.Start(r.Context(), userID, run.StartCommand{
		BundleRef: req.BundleRef, BundleVersion: req.BundleVersion, Input: req.Input,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toRunSummaryDTO(created))
}

// CreateAgentTest handles POST /runs/agent-test — the 智能体工作台's
// right-hand test panel. It takes a full Agent definition rather than a ref
// so the配置 being tested can be one the user has not saved yet, and returns
// the same RunSummary POST /runs does, so the caller consumes the result
// through the existing /runs/{id}/stream rather than a second event
// transport.
func (h *RunHandlers) CreateAgentTest(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req createAgentTestRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	created, err := h.svc.StartAgentTest(r.Context(), userID, run.AgentTestCommand{
		Definition: req.Definition, Input: req.Input,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toRunSummaryDTO(created))
}

type createAgentTestRunRequest struct {
	Definition map[string]any `json:"definition"`
	Input      map[string]any `json:"input"`
}

// ── List ─────────────────────────────────────────────────────────────

// List handles GET /runs.
func (h *RunHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	// Runs paginate by offset: run ids are random rather than monotonic,
	// so there is no keyset to resume from. The offset still travels as an
	// opaque cursor so the wire contract matches every other list.
	offset := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := decodeCursorString(cursor)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
			return
		}
		offset, _ = strconv.Atoi(decoded)
	}

	page, err := h.svc.List(r.Context(), userID, run.ListQuery{
		BundleRef: r.URL.Query().Get("bundle_ref"), Status: r.URL.Query().Get("status"),
		Limit: parseLimit(r.URL.Query().Get("limit")), Offset: offset,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toRunSummaryDTO))
}

// ── Get ──────────────────────────────────────────────────────────────

// Get handles GET /runs/{id}.
func (h *RunHandlers) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	detail, err := h.svc.Get(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, runDetailDTO{
		runSummaryDTO: toRunSummaryDTO(detail.Run),
		SharedState:   detail.SharedState,
		IsOwner:       detail.IsOwner,
		Usage: runUsageDTO{
			TotalTokens: detail.Run.Usage.TotalTokens, CostUSD: detail.Run.Usage.CostUSD,
			DurationSeconds: detail.Run.DurationSeconds(),
		},
	})
}

// ── Stream ───────────────────────────────────────────────────────────

// streamPollInterval is spec-12's "服务端检查间隔（300ms）" — how often the
// stream re-polls for new events and status once caught up, which bounds
// both end-to-end latency and how quickly the connection closes after the
// run finishes (spec-12's "~400ms 内自动关闭" acceptance check).
const streamPollInterval = 300 * time.Millisecond

type runEventDTO struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Node      *string        `json:"node,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

// Stream handles GET /runs/{id}/stream — NDJSON, and per openapi.yaml the
// one endpoint that does not use the unified envelope.
func (h *RunHandlers) Stream(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	afterID := int64(0)
	if v := r.URL.Query().Get("after_id"); v != "" {
		afterID, _ = strconv.ParseInt(v, 10, 64)
	}

	// The first fetch happens before any header is written, so an
	// unauthorized or missing run still gets a normal error envelope
	// rather than a 200 with an error line inside the stream.
	events, err := h.svc.EventsAfter(r.Context(), userID, id, afterID)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	status, err := h.svc.Status(r.Context(), id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	for {
		for _, ev := range events {
			dto := runEventDTO{ID: ev.ID, Type: ev.Type, RunID: id, Timestamp: ev.CreatedAt, Payload: ev.Payload}
			if ev.Node != "" {
				node := ev.Node
				dto.Node = &node
			}
			if err := writeNDJSONLine(w, dto); err != nil {
				return
			}
			afterID = ev.ID
		}
		if flusher != nil {
			flusher.Flush()
		}

		if status.Terminal() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(streamPollInterval):
		}

		if events, err = h.svc.EventsAfter(r.Context(), userID, id, afterID); err != nil {
			_ = writeNDJSONLine(w, runEventDTO{Type: "stream.error", RunID: id, Timestamp: h.now()})
			return
		}
		if status, err = h.svc.Status(r.Context(), id); err != nil {
			return
		}
	}
}

func writeNDJSONLine(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ── Cancel ───────────────────────────────────────────────────────────

// Cancel handles POST /runs/{id}/cancel.
func (h *RunHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	if err := h.svc.Cancel(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// compile-time guard: the page helper must keep producing a domain.Page.
var _ = domain.Page[run.Run]{}
