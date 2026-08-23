package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RunHandlers implements the /runs endpoint group (spec-11).
type RunHandlers struct {
	Queries store.Querier
	Engine  *RunEngine
	// now is overridable in tests.
	now func() time.Time
}

func NewRunHandlers(q store.Querier, engine *RunEngine) *RunHandlers {
	return &RunHandlers{Queries: q, Engine: engine, now: time.Now}
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
	Usage       runUsageDTO    `json:"usage"`
}

func toRunSummaryDTO(id string, bundleRef, bundleVersion, status string, errText pgtype.Text, createdAt, finishedAt pgtype.Timestamptz) runSummaryDTO {
	dto := runSummaryDTO{RunID: id, BundleRef: bundleRef, BundleVersion: bundleVersion, Status: status, CreatedAt: createdAt.Time}
	if errText.Valid {
		dto.Error = &errText.String
	}
	if finishedAt.Valid {
		dto.FinishedAt = &finishedAt.Time
	}
	return dto
}

// ── Create ───────────────────────────────────────────────────────────

type createRunRequest struct {
	BundleRef     string         `json:"bundle_ref"`
	BundleVersion string         `json:"bundle_version"`
	Input         map[string]any `json:"input"`
}

// Create handles POST /runs — spec-11's full chain: auth (done by
// middleware) → subscription check → version resolution (snapshot
// isolation) → dependency recheck (sanitized errors) → compile → launch
// async, returning immediately per openapi's "异步启动，立即返回 run_id".
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
	if req.BundleRef == "" {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid request",
			[]FieldError{{Field: "bundle_ref", Reason: "required"}})
		return
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}

	resolved, rerr := h.Engine.ResolveBundle(r.Context(), userID, req.BundleRef, req.BundleVersion)
	if rerr != nil {
		writeErr(w, r, rerr.Status, rerr.Code, rerr.Message)
		return
	}

	var bundleDef map[string]any
	if err := json.Unmarshal(resolved.Row.Definition, &bundleDef); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	apiKeys, err := h.Engine.DecryptedAPIKeys(r.Context(), resolved.OwnerUserID)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	if rerr := h.Engine.CheckDependencies(r.Context(), resolved.OwnerUserID, bundleDef, apiKeys); rerr != nil {
		writeErr(w, r, rerr.Status, rerr.Code, rerr.Message)
		return
	}

	runID, err := newRunID()
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	gw := modelgateway.NewGateway(nil)
	root, err := h.Engine.Compile(r.Context(), runID, resolved.OwnerUserID, bundleDef, apiKeys, gw)
	if err != nil {
		writeErr(w, r, http.StatusUnprocessableEntity, ErrAgentVersionNotFound, "该 Bundle 当前无法编译执行")
		return
	}

	var viaListingID pgtype.Int8
	if resolved.ViaListingID != nil {
		viaListingID = pgtype.Int8{Valid: true, Int64: *resolved.ViaListingID}
	}
	runRow, err := h.Queries.CreateBundleRun(r.Context(), store.CreateBundleRunParams{
		ID: runID, BundleID: resolved.Row.ID, TriggeredBy: userID, ViaListingID: viaListingID, Status: "running",
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	limits := parseRunLimits(bundleDef)
	go h.Engine.Execute(runID, root, userID, req.Input, limits)

	writeJSON(w, r, http.StatusCreated, toRunSummaryDTO(runRow.ID, resolved.Row.BundleRef, resolved.Row.Version, runRow.Status, runRow.Error, runRow.CreatedAt, runRow.FinishedAt))
}

// ── List ─────────────────────────────────────────────────────────────

func (h *RunHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := decodeCursorString(cursor)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid cursor")
			return
		}
		offset, _ = strconv.Atoi(decoded)
	}

	rows, err := h.Queries.ListBundleRunsForUserFiltered(r.Context(), store.ListBundleRunsForUserFilteredParams{
		TriggeredBy: userID, BundleRef: r.URL.Query().Get("bundle_ref"), RunStatus: r.URL.Query().Get("status"),
		PageLimit: int32(limit + 1), PageOffset: int32(offset),
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]runSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toRunSummaryDTO(row.ID, row.BundleRef, row.BundleVersion, row.Status, row.Error, row.CreatedAt, row.FinishedAt))
	}

	var nextCursor *string
	if hasMore {
		c := encodeCursorString(strconv.Itoa(offset + limit))
		nextCursor = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

// ── Get ──────────────────────────────────────────────────────────────

// Get handles GET /runs/{id}. shared_state is filtered to only the
// output fields the resource's display_meta declares whenever the
// requester isn't the Bundle's own owner (spec-11: 黑盒 shared_state
// filtering; 架构文档's own clarification is that the filter keys off
// "requester != author", not merely "ran via a listing").
func (h *RunHandlers) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	run, bundle, apiErr := h.loadRunAndBundle(r.Context(), id, userID)
	if apiErr != nil {
		writeErr(w, r, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}

	var sharedState map[string]any
	if err := json.Unmarshal(run.SharedState, &sharedState); err != nil {
		sharedState = map[string]any{}
	}
	if bundle.OwnerUserID != userID {
		sharedState = filterSharedStateForSubscriber(sharedState, bundle.DisplayMeta)
	}

	duration := int64(0)
	if run.FinishedAt.Valid {
		duration = int64(run.FinishedAt.Time.Sub(run.CreatedAt.Time).Seconds())
	}
	dto := runDetailDTO{
		runSummaryDTO: toRunSummaryDTO(run.ID, bundle.BundleRef, bundle.Version, run.Status, run.Error, run.CreatedAt, run.FinishedAt),
		SharedState:   sharedState,
		Usage:         runUsageDTO{TotalTokens: run.TotalTokens, CostUSD: numericToFloat64(run.CostUsd), DurationSeconds: duration},
	}
	writeJSON(w, r, http.StatusOK, dto)
}

// filterSharedStateForSubscriber keeps only the keys the resource's own
// display_meta.io_description.outputs[] declares (spec-08/11's black-box
// boundary: intermediate nodes may have written internal prompt
// fragments into shared_state, and only the declared output surface is
// safe to expose).
func filterSharedStateForSubscriber(state map[string]any, displayMeta []byte) map[string]any {
	var meta struct {
		IODescription struct {
			Outputs []string `json:"outputs"`
		} `json:"io_description"`
	}
	_ = json.Unmarshal(displayMeta, &meta)

	out := map[string]any{}
	for _, key := range meta.IODescription.Outputs {
		if v, ok := state[key]; ok {
			out[key] = v
		}
	}
	return out
}

func (h *RunHandlers) loadRunAndBundle(ctx context.Context, runID string, userID int64) (store.BundleRun, store.Bundle, *runEngineError) {
	run, err := h.Queries.GetBundleRun(ctx, runID)
	if err != nil {
		if pgxErrNoRows(err) {
			return store.BundleRun{}, store.Bundle{}, newRunEngineError(http.StatusNotFound, ErrRunNotFound, "run not found")
		}
		return store.BundleRun{}, store.Bundle{}, errInternal
	}
	if run.TriggeredBy != userID {
		return store.BundleRun{}, store.Bundle{}, newRunEngineError(http.StatusNotFound, ErrRunNotFound, "run not found")
	}
	bundle, err := h.Queries.GetBundleByID(ctx, run.BundleID)
	if err != nil {
		return store.BundleRun{}, store.Bundle{}, errInternal
	}
	return run, bundle, nil
}

func pgxErrNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// ── Stream ───────────────────────────────────────────────────────────

const streamPollInterval = 500 * time.Millisecond

type runEventDTO struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Node      *string        `json:"node,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

// Stream handles GET /runs/{id}/stream — NDJSON, per openapi.yaml the one
// endpoint that doesn't use the unified envelope. is_internal events are
// only ever included for the Bundle's own owner (spec-10 §3 / 架构文档's
// "请求者非作者" rule); everyone else gets the black-box-safe subset.
func (h *RunHandlers) Stream(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id := chi.URLParam(r, "id")

	run, bundle, apiErr := h.loadRunAndBundle(r.Context(), id, userID)
	if apiErr != nil {
		writeErr(w, r, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}
	includeInternal := bundle.OwnerUserID == userID

	afterID := int64(0)
	if v := r.URL.Query().Get("after_id"); v != "" {
		afterID, _ = strconv.ParseInt(v, 10, 64)
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	status := run.Status
	for {
		events, err := h.fetchEvents(r.Context(), id, afterID, includeInternal)
		if err != nil {
			_ = writeNDJSONLine(w, runEventDTO{Type: "stream.error", RunID: id, Timestamp: h.now()})
			return
		}
		for _, ev := range events {
			var payload map[string]any
			_ = json.Unmarshal(ev.Payload, &payload)
			dto := runEventDTO{ID: ev.ID, Type: ev.Type, RunID: id, Timestamp: ev.CreatedAt.Time, Payload: payload}
			if ev.Node.Valid {
				dto.Node = &ev.Node.String
			}
			if err := writeNDJSONLine(w, dto); err != nil {
				return
			}
			afterID = ev.ID
		}
		if flusher != nil {
			flusher.Flush()
		}

		if status == "finished" || status == "failed" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(streamPollInterval):
		}
		if run, err := h.Queries.GetBundleRun(r.Context(), id); err == nil {
			status = run.Status
		}
	}
}

func (h *RunHandlers) fetchEvents(ctx context.Context, runID string, afterID int64, includeInternal bool) ([]store.BundleRunEvent, error) {
	if includeInternal {
		return h.Queries.ListBundleRunEventsAfter(ctx, store.ListBundleRunEventsAfterParams{RunID: runID, ID: afterID})
	}
	return h.Queries.ListBundleRunEventsAfterExternal(ctx, store.ListBundleRunEventsAfterExternalParams{RunID: runID, ID: afterID})
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
	id := chi.URLParam(r, "id")

	run, _, apiErr := h.loadRunAndBundle(r.Context(), id, userID)
	if apiErr != nil {
		writeErr(w, r, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}
	if run.Status != "running" {
		writeErr(w, r, http.StatusConflict, ErrRunAlreadyFinished, "运行已结束")
		return
	}

	_ = h.Queries.MarkBundleRunCancelRequested(r.Context(), id)
	h.Engine.Cancel(id)
	w.WriteHeader(http.StatusNoContent)
}
