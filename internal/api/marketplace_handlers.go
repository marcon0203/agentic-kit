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

	"github.com/marcon0203/agentic-kit/internal/depclosure"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// MarketplaceHandlers implements the publish/browse/detail/unpublish and
// subscribe/unsubscribe/upgrade endpoints of api/openapi.yaml's 应用广场
// group, per spec-08-marketplace.md.
type MarketplaceHandlers struct {
	Queries store.Querier
}

func NewMarketplaceHandlers(q store.Querier) *MarketplaceHandlers {
	return &MarketplaceHandlers{Queries: q}
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

func resourceTypeToDepKind(s string) (string, bool) {
	switch s {
	case DepKindAgent, DepKindBundle, DepKindSkill, DepKindMCP:
		return s, true
	default:
		return "", false
	}
}

func toUserDTO(u store.User) userDTO {
	return userDTO{
		ID:          strconv.FormatInt(u.ID, 10),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		CreatedAt:   u.CreatedAt.Time,
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

// Publish handles POST /marketplace/listings. Before creating the listing
// it walks the resource's full transitive dependency closure (spec-08 §1)
// — every Bundle -> Agent -> Skill/MCP layer must already be published, by
// exact version where the DSL pins one — and returns every problem found
// at once, not just the first.
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

	var details []FieldError
	kind, kindOK := resourceTypeToDepKind(req.ResourceType)
	if !kindOK {
		details = append(details, FieldError{Field: "resource_type", Reason: "must be one of agent, bundle, skill, mcp"})
	}
	if req.ResourceRef == "" {
		details = append(details, FieldError{Field: "resource_ref", Reason: "required"})
	}
	if req.Version == "" {
		details = append(details, FieldError{Field: "version", Reason: "required"})
	}
	if len(req.DisplayMeta) == 0 {
		details = append(details, FieldError{Field: "display_meta", Reason: "required"})
	} else {
		var meta map[string]any
		if err := json.Unmarshal(req.DisplayMeta, &meta); err != nil {
			details = append(details, FieldError{Field: "display_meta", Reason: "must be an object"})
		} else {
			if s, _ := meta["display_name"].(string); s == "" {
				details = append(details, FieldError{Field: "display_meta.display_name", Reason: "required"})
			}
			if s, _ := meta["description"].(string); s == "" {
				details = append(details, FieldError{Field: "display_meta.description", Reason: "required"})
			}
		}
	}
	if len(details) > 0 {
		writeErrDetails(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid publish request", details)
		return
	}

	resourceID, err := h.privateResourceID(r.Context(), userID, kind, req.ResourceRef, req.Version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "resource not found")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	root := depclosure.NodeKey{Kind: kind, Ref: req.ResourceRef, Version: req.Version}
	resolver := &storeResolver{ctx: r.Context(), q: h.Queries, ownerID: userID}
	issues, err := depclosure.Validate(root, resolver)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if len(issues) > 0 {
		code := ErrPublishUnpublishedDeps
		message := "存在未发布的依赖，无法发布"
		issueDetails := make([]FieldError, len(issues))
		hasCycle := false
		for i, iss := range issues {
			issueDetails[i] = FieldError{Field: iss.Field, Reason: iss.Reason}
			if iss.Kind == depclosure.ErrCycle {
				hasCycle = true
			}
		}
		if hasCycle {
			code = ErrCircularDependency
			message = "依赖图中存在循环依赖，无法发布"
		}
		writeErrDetails(w, r, http.StatusUnprocessableEntity, code, message, issueDetails)
		return
	}

	// listing_ref is globally unique across authors, like an npm package
	// name — reject publishing under one another author already owns.
	if existing, err := h.Queries.GetListingByListingRefLatestPublished(r.Context(), req.ResourceRef); err == nil {
		if existing.AuthorUserID != userID {
			writeErr(w, r, http.StatusConflict, ErrResourceRefDuplicate, "该 listing_ref 已被其他作者占用")
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	if err := h.setResourceDisplayMeta(r.Context(), kind, resourceID, req.DisplayMeta); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	changelog := pgtype.Text{}
	if req.Changelog != "" {
		changelog = pgtype.Text{String: req.Changelog, Valid: true}
	}

	listing, err := h.Queries.CreateListing(r.Context(), store.CreateListingParams{
		AuthorUserID: userID,
		ResourceType: req.ResourceType,
		ResourceID:   resourceID,
		ListingRef:   req.ResourceRef,
		Version:      req.Version,
		Changelog:    changelog,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, r, http.StatusConflict, ErrResourceRefDuplicate, "该资源的这个版本已经发布过")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	dto, err := h.toListingDetailDTO(r.Context(), listing, false)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusCreated, dto)
}

// privateResourceID fetches the owner's private resource row for the given
// kind/ref/version and returns its primary key.
func (h *MarketplaceHandlers) privateResourceID(ctx context.Context, userID int64, kind, ref, version string) (int64, error) {
	switch kind {
	case DepKindAgent:
		row, err := h.Queries.GetAgentForOwner(ctx, store.GetAgentForOwnerParams{OwnerUserID: userID, AgentRef: ref, Version: version})
		return row.ID, err
	case DepKindBundle:
		row, err := h.Queries.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{OwnerUserID: userID, BundleRef: ref, Version: version})
		return row.ID, err
	case DepKindSkill:
		row, err := h.Queries.GetSkillByRefVersionForOwner(ctx, store.GetSkillByRefVersionForOwnerParams{OwnerUserID: userID, Ref: ref, Version: version})
		return row.ID, err
	case DepKindMCP:
		row, err := h.Queries.GetMCPServerByRefVersionForOwner(ctx, store.GetMCPServerByRefVersionForOwnerParams{OwnerUserID: userID, Ref: ref, Version: version})
		return row.ID, err
	default:
		return 0, pgx.ErrNoRows
	}
}

func (h *MarketplaceHandlers) setResourceDisplayMeta(ctx context.Context, kind string, resourceID int64, displayMeta []byte) error {
	switch kind {
	case DepKindAgent:
		return h.Queries.SetAgentDisplayMeta(ctx, store.SetAgentDisplayMetaParams{ID: resourceID, DisplayMeta: displayMeta})
	case DepKindBundle:
		return h.Queries.SetBundleDisplayMeta(ctx, store.SetBundleDisplayMetaParams{ID: resourceID, DisplayMeta: displayMeta})
	case DepKindSkill:
		return h.Queries.SetSkillDisplayMeta(ctx, store.SetSkillDisplayMetaParams{ID: resourceID, DisplayMeta: displayMeta})
	case DepKindMCP:
		return h.Queries.SetMCPServerDisplayMeta(ctx, store.SetMCPServerDisplayMetaParams{ID: resourceID, DisplayMeta: displayMeta})
	default:
		return nil
	}
}

func (h *MarketplaceHandlers) listingDisplayMetaBytes(ctx context.Context, resourceType string, listingID int64) ([]byte, error) {
	switch resourceType {
	case DepKindAgent:
		row, err := h.Queries.GetAgentListingDisplayByListingID(ctx, listingID)
		return row.DisplayMeta, err
	case DepKindBundle:
		row, err := h.Queries.GetBundleListingDisplayByListingID(ctx, listingID)
		return row.DisplayMeta, err
	case DepKindSkill:
		row, err := h.Queries.GetSkillListingDisplayByListingID(ctx, listingID)
		return row.DisplayMeta, err
	case DepKindMCP:
		row, err := h.Queries.GetMCPServerListingDisplayByListingID(ctx, listingID)
		return row.DisplayMeta, err
	default:
		return nil, pgx.ErrNoRows
	}
}

// constraintsSummary is the blackbox rule's necessary exception (spec-08):
// no persona or orchestration graph is ever exposed, but a subscriber must
// be able to estimate run cost before subscribing. Skill/MCP have no
// constraints concept, so they return nil.
func (h *MarketplaceHandlers) constraintsSummary(ctx context.Context, resourceType string, listingID int64) (*constraintsSummaryDTO, error) {
	switch resourceType {
	case DepKindAgent:
		row, err := h.Queries.GetAgentConstraintsSummaryByListingID(ctx, listingID)
		if err != nil {
			return nil, err
		}
		dto := &constraintsSummaryDTO{MaxToolCalls: &row.MaxToolCalls, TimeoutSeconds: &row.TimeoutSeconds}
		if row.EstimatedTokensRange != "" {
			dto.EstimatedTokensRange = &row.EstimatedTokensRange
		}
		return dto, nil
	case DepKindBundle:
		row, err := h.Queries.GetBundleConstraintsSummaryByListingID(ctx, listingID)
		if err != nil {
			return nil, err
		}
		return &constraintsSummaryDTO{TimeoutSeconds: &row.TimeoutSeconds}, nil
	default:
		return nil, nil
	}
}

func (h *MarketplaceHandlers) toListingDetailDTO(ctx context.Context, listing store.MarketplaceListing, subscribed bool) (listingDetailDTO, error) {
	metaBytes, err := h.listingDisplayMetaBytes(ctx, listing.ResourceType, listing.ID)
	if err != nil {
		return listingDetailDTO{}, err
	}
	var meta any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return listingDetailDTO{}, err
	}

	author, err := h.Queries.GetUserByID(ctx, listing.AuthorUserID)
	if err != nil {
		return listingDetailDTO{}, err
	}

	constraints, err := h.constraintsSummary(ctx, listing.ResourceType, listing.ID)
	if err != nil {
		return listingDetailDTO{}, err
	}

	historyRows, err := h.Queries.ListListingVersionHistory(ctx, listing.ListingRef)
	if err != nil {
		return listingDetailDTO{}, err
	}
	versions := make([]listingVersionDTO, 0, len(historyRows))
	for _, hr := range historyRows {
		versions = append(versions, listingVersionDTO{Version: hr.Version, Changelog: hr.Changelog.String, PublishedAt: hr.PublishedAt.Time})
	}

	summary := listingSummaryDTO{
		ID:              strconv.FormatInt(listing.ID, 10),
		ListingRef:      listing.ListingRef,
		ResourceType:    listing.ResourceType,
		Version:         listing.Version,
		Visibility:      listing.Visibility,
		DisplayMeta:     meta,
		Author:          toUserDTO(author),
		SubscriberCount: listing.SubscriberCount,
		RunCount:        listing.RunCount,
		Subscribed:      subscribed,
		PublishedAt:     listing.PublishedAt.Time,
	}
	return listingDetailDTO{listingSummaryDTO: summary, ConstraintsSummary: constraints, Versions: versions}, nil
}

// ── Browse ───────────────────────────────────────────────────────────

type browseRow struct {
	ID              int64
	ListingRef      string
	ResourceType    string
	Version         string
	Visibility      string
	AuthorUserID    int64
	SubscriberCount int32
	RunCount        int64
	PublishedAt     time.Time
	DisplayMeta     []byte
}

// Browse handles GET /marketplace/listings. Only display_meta ever leaves
// Postgres here — never `definition` — per the blackbox rule (spec-08 §"黑
// 盒发布的实现要点"): the joined queries this reads from are dedicated
// SELECT lists that don't include the definition column at all.
func (h *MarketplaceHandlers) Browse(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	afterID := decodeCursor(r.URL.Query().Get("cursor"))
	resourceType := r.URL.Query().Get("resource_type")
	q := r.URL.Query().Get("q")

	var rows []browseRow
	kinds := []string{DepKindAgent, DepKindBundle, DepKindSkill, DepKindMCP}
	if resourceType != "" {
		if _, ok := resourceTypeToDepKind(resourceType); !ok {
			writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid resource_type")
			return
		}
		kinds = []string{resourceType}
	}

	for _, kind := range kinds {
		kindRows, err := h.browsePage(r.Context(), kind, afterID, int32(limit+1))
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		rows = append(rows, kindRows...)
	}
	// Every per-kind page is already sorted by id (marketplace_listings'
	// single shared sequence), so a stable sort by id merges them correctly
	// when browsing across all resource types at once.
	sortBrowseRowsByID(rows)

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]listingSummaryDTO, 0, len(rows))
	for _, row := range rows {
		var meta any
		if err := json.Unmarshal(row.DisplayMeta, &meta); err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		if q != "" && !displayMetaMatches(meta, q) {
			continue
		}
		author, err := h.Queries.GetUserByID(r.Context(), row.AuthorUserID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		subscribed := false
		if _, err := h.Queries.GetSubscriptionByListingAndSubscriber(r.Context(), store.GetSubscriptionByListingAndSubscriberParams{SubscriberID: userID, ListingID: row.ID}); err == nil {
			subscribed = true
		}
		items = append(items, listingSummaryDTO{
			ID:              strconv.FormatInt(row.ID, 10),
			ListingRef:      row.ListingRef,
			ResourceType:    row.ResourceType,
			Version:         row.Version,
			Visibility:      row.Visibility,
			DisplayMeta:     meta,
			Author:          toUserDTO(author),
			SubscriberCount: row.SubscriberCount,
			RunCount:        row.RunCount,
			Subscribed:      subscribed,
			PublishedAt:     row.PublishedAt,
		})
	}

	var nextCursor *string
	if len(rows) > 0 {
		c := encodeCursor(rows[len(rows)-1].ID)
		nextCursor = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

func (h *MarketplaceHandlers) browsePage(ctx context.Context, kind string, afterID int64, limit int32) ([]browseRow, error) {
	switch kind {
	case DepKindAgent:
		rows, err := h.Queries.ListPublishedAgentListingsPage(ctx, store.ListPublishedAgentListingsPageParams{ID: afterID, Limit: limit})
		out := make([]browseRow, len(rows))
		for i, row := range rows {
			out[i] = browseRow{ID: row.ID, ListingRef: row.ListingRef, ResourceType: row.ResourceType, Version: row.Version, Visibility: row.Visibility, AuthorUserID: row.AuthorUserID, SubscriberCount: row.SubscriberCount, RunCount: row.RunCount, PublishedAt: row.PublishedAt.Time, DisplayMeta: row.DisplayMeta}
		}
		return out, err
	case DepKindBundle:
		rows, err := h.Queries.ListPublishedBundleListingsPage(ctx, store.ListPublishedBundleListingsPageParams{ID: afterID, Limit: limit})
		out := make([]browseRow, len(rows))
		for i, row := range rows {
			out[i] = browseRow{ID: row.ID, ListingRef: row.ListingRef, ResourceType: row.ResourceType, Version: row.Version, Visibility: row.Visibility, AuthorUserID: row.AuthorUserID, SubscriberCount: row.SubscriberCount, RunCount: row.RunCount, PublishedAt: row.PublishedAt.Time, DisplayMeta: row.DisplayMeta}
		}
		return out, err
	case DepKindSkill:
		rows, err := h.Queries.ListPublishedSkillListingsPage(ctx, store.ListPublishedSkillListingsPageParams{ID: afterID, Limit: limit})
		out := make([]browseRow, len(rows))
		for i, row := range rows {
			out[i] = browseRow{ID: row.ID, ListingRef: row.ListingRef, ResourceType: row.ResourceType, Version: row.Version, Visibility: row.Visibility, AuthorUserID: row.AuthorUserID, SubscriberCount: row.SubscriberCount, RunCount: row.RunCount, PublishedAt: row.PublishedAt.Time, DisplayMeta: row.DisplayMeta}
		}
		return out, err
	case DepKindMCP:
		rows, err := h.Queries.ListPublishedMCPServerListingsPage(ctx, store.ListPublishedMCPServerListingsPageParams{ID: afterID, Limit: limit})
		out := make([]browseRow, len(rows))
		for i, row := range rows {
			out[i] = browseRow{ID: row.ID, ListingRef: row.ListingRef, ResourceType: row.ResourceType, Version: row.Version, Visibility: row.Visibility, AuthorUserID: row.AuthorUserID, SubscriberCount: row.SubscriberCount, RunCount: row.RunCount, PublishedAt: row.PublishedAt.Time, DisplayMeta: row.DisplayMeta}
		}
		return out, err
	default:
		return nil, nil
	}
}

func sortBrowseRowsByID(rows []browseRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].ID < rows[j-1].ID; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func displayMetaMatches(meta any, q string) bool {
	m, ok := meta.(map[string]any)
	if !ok {
		return false
	}
	name, _ := m["display_name"].(string)
	desc, _ := m["description"].(string)
	return containsFold(name, q) || containsFold(desc, q)
}

func containsFold(s, substr string) bool {
	sl, subl := []rune(toLower(s)), []rune(toLower(substr))
	if len(subl) == 0 {
		return true
	}
	if len(subl) > len(sl) {
		return false
	}
	for i := 0; i+len(subl) <= len(sl); i++ {
		match := true
		for j := range subl {
			if sl[i+j] != subl[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	out := []rune(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}

// ── Detail ───────────────────────────────────────────────────────────

// Detail handles GET /marketplace/listings/{ref}. `ref` is the listing_ref
// (spec-08: "广场内的稳定标识，跨版本不变") — the response is always the
// author's latest non-delisted version.
func (h *MarketplaceHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	ref := chi.URLParam(r, "ref")

	listing, err := h.Queries.GetListingByListingRefLatestPublished(r.Context(), ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrListingNotFound, "listing 不存在或已下架")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	subscribed := false
	if _, err := h.Queries.GetSubscriptionByListingAndSubscriber(r.Context(), store.GetSubscriptionByListingAndSubscriberParams{SubscriberID: userID, ListingID: listing.ID}); err == nil {
		subscribed = true
	}

	dto, err := h.toListingDetailDTO(r.Context(), listing, subscribed)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusOK, dto)
}

// ── Unpublish ────────────────────────────────────────────────────────

// Unpublish handles POST /marketplace/listings/{id}/unpublish. Stopping
// distribution never affects existing subscribers (spec-08: "无影响，继续
// 可用"), but is refused (409 + 70007) when another currently-published
// resource still depends on this one — otherwise that resource's own
// dependency closure would silently break for its subscribers.
func (h *MarketplaceHandlers) Unpublish(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}

	listing, err := h.Queries.GetListingByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrListingNotFound, "listing 不存在")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if listing.AuthorUserID != userID {
		writeErr(w, r, http.StatusForbidden, ErrForbidden, "forbidden")
		return
	}

	var dependedBy []FieldError
	switch listing.ResourceType {
	case DepKindAgent:
		rows, err := h.Queries.FindPublishedBundlesReferencingAgentRef(r.Context(), store.FindPublishedBundlesReferencingAgentRefParams{OwnerUserID: userID, AgentRef: listing.ListingRef})
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		for _, row := range rows {
			dependedBy = append(dependedBy, FieldError{Field: "depended_by", Reason: "Bundle " + row.BundleRef + "@" + row.Version + " 正在依赖"})
		}
	case DepKindSkill:
		rows, err := h.Queries.FindPublishedAgentsReferencingSkillRef(r.Context(), store.FindPublishedAgentsReferencingSkillRefParams{OwnerUserID: userID, SkillRef: listing.ListingRef})
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		for _, row := range rows {
			dependedBy = append(dependedBy, FieldError{Field: "depended_by", Reason: "Agent " + row.AgentRef + "@" + row.Version + " 正在依赖"})
		}
	case DepKindMCP:
		rows, err := h.Queries.FindPublishedAgentsReferencingToolRef(r.Context(), store.FindPublishedAgentsReferencingToolRefParams{OwnerUserID: userID, ToolRef: listing.ListingRef})
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		for _, row := range rows {
			dependedBy = append(dependedBy, FieldError{Field: "depended_by", Reason: "Agent " + row.AgentRef + "@" + row.Version + " 正在依赖"})
		}
	case DepKindBundle:
		// Nothing else in the DSL can reference a Bundle.
	}
	if len(dependedBy) > 0 {
		writeErrDetails(w, r, http.StatusConflict, ErrDependencyStillReferenced, "该资源正被其他已发布资源依赖，无法停止分发", dependedBy)
		return
	}

	if err := h.Queries.SetListingDistribution(r.Context(), store.SetListingDistributionParams{ID: id, Distribution: 2}); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Subscribe ────────────────────────────────────────────────────────

// Subscribe handles POST /marketplace/listings/{id}/subscribe. Snapshot
// isolation (spec-08's other non-negotiable rule): the subscription binds
// to this exact listing version, and the underlying resource version is
// marked immutable so the author can never change what a subscriber
// already committed to.
func (h *MarketplaceHandlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}

	listing, err := h.Queries.GetListingByID(r.Context(), id)
	if err != nil || listing.Distribution != 1 {
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrListingNotFound, "listing 不存在或已停止分发")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	if listing.AuthorUserID == userID {
		writeErr(w, r, http.StatusConflict, ErrAlreadySubscribed, "不能订阅自己发布的资源")
		return
	}

	if _, err := h.Queries.GetSubscriptionForSubscriberByListingRef(r.Context(), store.GetSubscriptionForSubscriberByListingRefParams{SubscriberID: userID, ListingRef: listing.ListingRef}); err == nil {
		writeErr(w, r, http.StatusConflict, ErrAlreadySubscribed, "已订阅该版本，如需切换版本请使用升级")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	sub, err := h.Queries.CreateSubscription(r.Context(), store.CreateSubscriptionParams{SubscriberID: userID, ListingID: id})
	if err != nil {
		if isUniqueViolation(err) {
			writeErr(w, r, http.StatusConflict, ErrAlreadySubscribed, "已订阅该版本")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.markResourceImmutable(r.Context(), listing.ResourceType, listing.ResourceID); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.Queries.IncrementListingSubscriberCount(r.Context(), id); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	// Reflect the just-applied increment without a second round-trip.
	listing.SubscriberCount++

	dto, err := h.toSubscriptionDTO(r.Context(), sub, listing)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusCreated, dto)
}

func (h *MarketplaceHandlers) markResourceImmutable(ctx context.Context, resourceType string, resourceID int64) error {
	switch resourceType {
	case DepKindAgent:
		return h.Queries.MarkAgentImmutable(ctx, resourceID)
	case DepKindBundle:
		return h.Queries.MarkBundleImmutable(ctx, resourceID)
	case DepKindSkill:
		return h.Queries.MarkSkillImmutable(ctx, resourceID)
	case DepKindMCP:
		return h.Queries.MarkMCPServerImmutable(ctx, resourceID)
	default:
		return nil
	}
}

func (h *MarketplaceHandlers) toSubscriptionDTO(ctx context.Context, sub store.Subscription, listing store.MarketplaceListing) (subscriptionDTO, error) {
	listingDTO, err := h.toListingDetailDTO(ctx, listing, true)
	if err != nil {
		return subscriptionDTO{}, err
	}

	latest, err := h.Queries.GetListingByListingRefLatestPublished(ctx, listing.ListingRef)
	var latestVersion, latestChangelog *string
	if err == nil {
		v := latest.Version
		latestVersion = &v
		if latest.Changelog.Valid {
			c := latest.Changelog.String
			latestChangelog = &c
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return subscriptionDTO{}, err
	}

	var localAlias *string
	if sub.LocalAlias.Valid {
		a := sub.LocalAlias.String
		localAlias = &a
	}

	return subscriptionDTO{
		ID:                strconv.FormatInt(sub.ID, 10),
		Listing:           listingDTO.listingSummaryDTO,
		SubscribedVersion: listing.Version,
		LocalAlias:        localAlias,
		LatestVersion:     latestVersion,
		LatestChangelog:   latestChangelog,
		CreatedAt:         sub.CreatedAt.Time,
	}, nil
}

// ── List subscriptions ───────────────────────────────────────────────

// ListSubscriptions handles GET /marketplace/subscriptions.
func (h *MarketplaceHandlers) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	afterID := decodeCursor(r.URL.Query().Get("cursor"))

	subs, err := h.Queries.ListSubscriptionsForUserPage(r.Context(), store.ListSubscriptionsForUserPageParams{SubscriberID: userID, ID: afterID, Limit: int32(limit + 1)})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	hasMore := len(subs) > limit
	if hasMore {
		subs = subs[:limit]
	}

	items := make([]subscriptionDTO, 0, len(subs))
	for _, sub := range subs {
		listing, err := h.Queries.GetListingByID(r.Context(), sub.ListingID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		dto, err := h.toSubscriptionDTO(r.Context(), sub, listing)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
			return
		}
		items = append(items, dto)
	}

	var nextCursor *string
	if len(subs) > 0 {
		c := encodeCursor(subs[len(subs)-1].ID)
		nextCursor = &c
	}
	writeJSON(w, r, http.StatusOK, NewPage(items, nextCursor, hasMore))
}

// ── Unsubscribe ──────────────────────────────────────────────────────

// Unsubscribe handles DELETE /marketplace/subscriptions/{id}. Run history
// stays queryable afterward (spec-08: "退订成功（历史运行记录仍可查）") —
// this only removes the subscription row and the marketplace listing's
// subscriber count.
func (h *MarketplaceHandlers) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}

	sub, err := h.Queries.GetSubscriptionByIDForSubscriber(r.Context(), store.GetSubscriptionByIDForSubscriberParams{ID: id, SubscriberID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "subscription not found")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	if err := h.Queries.DeleteSubscription(r.Context(), store.DeleteSubscriptionParams{ID: id, SubscriberID: userID}); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.Queries.DecrementListingSubscriberCount(r.Context(), sub.ListingID); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Upgrade ──────────────────────────────────────────────────────────

type upgradeRequest struct {
	TargetVersion string `json:"target_version"`
}

// Upgrade handles POST /marketplace/subscriptions/{id}/upgrade. Explicit
// only — spec-08's snapshot-isolation rule means a new author version
// never silently moves an existing subscriber.
func (h *MarketplaceHandlers) Upgrade(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeErr(w, r, http.StatusUnauthorized, ErrTokenInvalid, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "invalid id")
		return
	}
	var req upgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetVersion == "" {
		writeErr(w, r, http.StatusBadRequest, ErrValidationFailed, "target_version is required")
		return
	}

	sub, err := h.Queries.GetSubscriptionByIDForSubscriber(r.Context(), store.GetSubscriptionByIDForSubscriberParams{ID: id, SubscriberID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrResourceNotFound, "subscription not found")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	currentListing, err := h.Queries.GetListingByID(r.Context(), sub.ListingID)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	targetListing, err := h.Queries.GetListingByListingRefAndVersion(r.Context(), store.GetListingByListingRefAndVersionParams{ListingRef: currentListing.ListingRef, Version: req.TargetVersion})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, r, http.StatusNotFound, ErrListingNotFound, "目标版本不存在")
			return
		}
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}

	updatedSub, err := h.Queries.UpdateSubscriptionListing(r.Context(), store.UpdateSubscriptionListingParams{ID: id, SubscriberID: userID, ListingID: targetListing.ID})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.markResourceImmutable(r.Context(), targetListing.ResourceType, targetListing.ResourceID); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.Queries.IncrementListingSubscriberCount(r.Context(), targetListing.ID); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	if err := h.Queries.DecrementListingSubscriberCount(r.Context(), currentListing.ID); err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	targetListing.SubscriberCount++

	dto, err := h.toSubscriptionDTO(r.Context(), updatedSub, targetListing)
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, ErrInternal, "internal server error")
		return
	}
	writeJSON(w, r, http.StatusOK, dto)
}
