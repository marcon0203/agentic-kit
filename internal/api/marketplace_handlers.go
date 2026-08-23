package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
)

// MarketplaceHandlers is the HTTP transport for the 应用广场 context. The
// blackbox rule, snapshot isolation, the dependency-closure gate and every
// business code live in internal/domain/marketplace; this file is DTOs and
// wiring.
type MarketplaceHandlers struct {
	svc *marketplace.Service
}

func NewMarketplaceHandlers(svc *marketplace.Service) *MarketplaceHandlers {
	return &MarketplaceHandlers{svc: svc}
}

// ── DTOs ─────────────────────────────────────────────────────────────

type listingSummaryDTO struct {
	ID              string    `json:"id"`
	ListingRef      string    `json:"listing_ref"`
	ResourceType    string    `json:"resource_type"`
	Version         string    `json:"version"`
	Visibility      string    `json:"visibility"`
	DisplayMeta     any       `json:"display_meta"`
	Author          userDTO   `json:"author"`
	SubscriberCount int32     `json:"subscriber_count"`
	RunCount        int64     `json:"run_count"`
	Subscribed      bool      `json:"subscribed"`
	PublishedAt     time.Time `json:"published_at"`
}

type listingVersionDTO struct {
	Version     string    `json:"version"`
	Changelog   string    `json:"changelog"`
	PublishedAt time.Time `json:"published_at"`
}

type constraintsSummaryDTO struct {
	MaxToolCalls         *int32  `json:"max_tool_calls,omitempty"`
	TimeoutSeconds       *int32  `json:"timeout_seconds,omitempty"`
	EstimatedTokensRange *string `json:"estimated_tokens_range,omitempty"`
}

type listingDetailDTO struct {
	listingSummaryDTO
	ConstraintsSummary *constraintsSummaryDTO `json:"constraints_summary,omitempty"`
	Versions           []listingVersionDTO    `json:"versions"`
}

type subscriptionDTO struct {
	ID                string            `json:"id"`
	Listing           listingSummaryDTO `json:"listing"`
	SubscribedVersion string            `json:"subscribed_version"`
	LocalAlias        *string           `json:"local_alias"`
	LatestVersion     *string           `json:"latest_version"`
	LatestChangelog   *string           `json:"latest_changelog"`
	CreatedAt         time.Time         `json:"created_at"`
}

func toAuthorDTO(a marketplace.Author) userDTO {
	return userDTO{
		ID:          strconv.FormatInt(a.ID, 10),
		Email:       a.Email,
		DisplayName: a.DisplayName,
		IsAdmin:     a.IsAdmin,
		CreatedAt:   a.CreatedAt,
	}
}

func toListingSummaryDTO(v marketplace.ListingView) listingSummaryDTO {
	l := v.Listing
	var meta any
	if l.DisplayMeta != nil {
		meta = map[string]any(l.DisplayMeta)
	}
	return listingSummaryDTO{
		ID:              strconv.FormatInt(l.ID, 10),
		ListingRef:      l.Ref,
		ResourceType:    string(l.Kind),
		Version:         l.Version,
		Visibility:      l.Visibility,
		DisplayMeta:     meta,
		Author:          toAuthorDTO(v.Author),
		SubscriberCount: l.SubscriberCount,
		RunCount:        l.RunCount,
		Subscribed:      v.Subscribed,
		PublishedAt:     l.PublishedAt,
	}
}

func toListingDetailDTO(v marketplace.ListingView) listingDetailDTO {
	versions := make([]listingVersionDTO, 0, len(v.Versions))
	for _, ver := range v.Versions {
		versions = append(versions, listingVersionDTO{
			Version: ver.Version, Changelog: ver.Changelog, PublishedAt: ver.PublishedAt,
		})
	}

	var constraints *constraintsSummaryDTO
	if c := v.ConstraintsSummary; c != nil {
		constraints = &constraintsSummaryDTO{MaxToolCalls: c.MaxToolCalls, TimeoutSeconds: c.TimeoutSeconds}
		if c.EstimatedTokensRange != "" {
			r := c.EstimatedTokensRange
			constraints.EstimatedTokensRange = &r
		}
	}

	return listingDetailDTO{
		listingSummaryDTO:  toListingSummaryDTO(v),
		ConstraintsSummary: constraints,
		Versions:           versions,
	}
}

func toSubscriptionDTO(v marketplace.SubscriptionView) subscriptionDTO {
	optional := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return subscriptionDTO{
		ID:                strconv.FormatInt(v.Subscription.ID, 10),
		Listing:           toListingSummaryDTO(v.Listing),
		SubscribedVersion: v.Listing.Listing.Version,
		LocalAlias:        optional(v.Subscription.LocalAlias),
		LatestVersion:     optional(v.LatestVersion),
		LatestChangelog:   optional(v.LatestChangelog),
		CreatedAt:         v.Subscription.CreatedAt,
	}
}

// ── Publish ──────────────────────────────────────────────────────────

type publishRequest struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  string          `json:"resource_ref"`
	Version      string          `json:"version"`
	DisplayMeta  json.RawMessage `json:"display_meta"`
	Changelog    string          `json:"changelog"`
}

// Publish handles POST /marketplace/listings.
func (h *MarketplaceHandlers) Publish(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "malformed request body")
		return
	}

	// display_meta is free-form JSON on the wire. Only its *shape* is a
	// transport concern; whether the required keys are present is a business
	// rule and stays in the service.
	var meta marketplace.DisplayMeta
	if len(req.DisplayMeta) > 0 {
		if err := json.Unmarshal(req.DisplayMeta, &meta); err != nil {
			writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid publish request",
				[]FieldError{{Field: "display_meta", Reason: "must be an object"}})
			return
		}
	}

	view, err := h.svc.Publish(r.Context(), userID, marketplace.PublishCommand{
		Kind: req.ResourceType, Ref: req.ResourceRef, Version: req.Version,
		DisplayMeta: meta, Changelog: req.Changelog,
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toListingDetailDTO(view))
}

// ── Browse & detail ──────────────────────────────────────────────────

// Browse handles GET /marketplace/listings.
func (h *MarketplaceHandlers) Browse(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	page, err := h.svc.Browse(r.Context(), userID, marketplace.BrowseQuery{
		Kind:   r.URL.Query().Get("resource_type"),
		Search: r.URL.Query().Get("q"),
		Limit:  parseLimit(r.URL.Query().Get("limit")),
		After:  decodeCursor(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toListingSummaryDTO))
}

// Detail handles GET /marketplace/listings/{ref}.
func (h *MarketplaceHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	view, err := h.svc.Detail(r.Context(), userID, chi.URLParam(r, "ref"))
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toListingDetailDTO(view))
}

// ── Unpublish ────────────────────────────────────────────────────────

// Unpublish handles POST /marketplace/listings/{id}/unpublish.
func (h *MarketplaceHandlers) Unpublish(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	if err := h.svc.Unpublish(r.Context(), userID, id); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Subscribe ────────────────────────────────────────────────────────

// Subscribe handles POST /marketplace/listings/{id}/subscribe.
func (h *MarketplaceHandlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	view, err := h.svc.Subscribe(r.Context(), userID, id)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toSubscriptionDTO(view))
}

// ListSubscriptions handles GET /marketplace/subscriptions.
func (h *MarketplaceHandlers) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	page, err := h.svc.ListSubscriptions(r.Context(), userID, domain.PageQuery{
		Limit: parseLimit(r.URL.Query().Get("limit")),
		After: strconv.FormatInt(decodeCursor(r.URL.Query().Get("cursor")), 10),
	})
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeDomainPage(w, r, mapPage(page, toSubscriptionDTO))
}

// Unsubscribe handles DELETE /marketplace/subscriptions/{id}.
func (h *MarketplaceHandlers) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	if err := h.svc.Unsubscribe(r.Context(), userID, id); err != nil {
		writeDomainErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Upgrade ──────────────────────────────────────────────────────────

type upgradeRequest struct {
	TargetVersion string `json:"target_version"`
}

// Upgrade handles POST /marketplace/subscriptions/{id}/upgrade.
func (h *MarketplaceHandlers) Upgrade(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req upgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "target_version is required")
		return
	}

	view, err := h.svc.Upgrade(r.Context(), userID, id, req.TargetVersion)
	if err != nil {
		writeDomainErr(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toSubscriptionDTO(view))
}

// pathID parses a numeric path parameter, writing the 400 itself so each
// handler stays a straight line.
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid "+name)
		return 0, false
	}
	return id, true
}
