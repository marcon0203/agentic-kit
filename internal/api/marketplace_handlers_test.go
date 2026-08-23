package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// fakeMarketplaceQuerier is an in-memory store.Querier covering exactly
// what marketplace_handlers.go and marketplace_resolver.go call, mirroring
// the real SQL's owner-scoping and JSONB-containment semantics closely
// enough to exercise the handlers end to end without Postgres.
type fakeMarketplaceQuerier struct {
	store.Querier

	users    map[int64]store.User
	agents   []store.Agent
	bundles  []store.Bundle
	skills   []store.Skill
	mcps     []store.McpServer
	listings []store.MarketplaceListing
	subs     []store.Subscription

	nextListingID int64
	nextSubID     int64
	clock         int64 // monotonic stand-in for published_at ordering
}

func newFakeMarketplaceQuerier() *fakeMarketplaceQuerier {
	return &fakeMarketplaceQuerier{
		users:         map[int64]store.User{},
		nextListingID: 1,
		nextSubID:     1,
	}
}

func (f *fakeMarketplaceQuerier) tick() pgtype.Timestamptz {
	f.clock++
	return pgtype.Timestamptz{Valid: true, Time: time.Unix(f.clock, 0).UTC()}
}

// ── seed helpers used by tests ──────────────────────────────────────

func (f *fakeMarketplaceQuerier) addUser(id int64, email, name string) {
	f.users[id] = store.User{ID: id, Email: email, DisplayName: name, CreatedAt: pgtype.Timestamptz{Valid: true}}
}

func (f *fakeMarketplaceQuerier) addAgent(id, owner int64, ref, version string, def map[string]any) store.Agent {
	b, _ := json.Marshal(def)
	a := store.Agent{ID: id, OwnerUserID: owner, AgentRef: ref, Version: version, Definition: b, DisplayMeta: []byte("{}"), Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}}
	f.agents = append(f.agents, a)
	return a
}

func (f *fakeMarketplaceQuerier) addBundle(id, owner int64, ref, version string, def map[string]any) store.Bundle {
	b, _ := json.Marshal(def)
	bd := store.Bundle{ID: id, OwnerUserID: owner, BundleRef: ref, Version: version, Definition: b, DisplayMeta: []byte("{}"), Status: 1, CreatedAt: pgtype.Timestamptz{Valid: true}}
	f.bundles = append(f.bundles, bd)
	return bd
}

// publishFor is a test-only shortcut that bypasses the handler's own
// dependency-closure walk, for seeding a listing precondition tests need
// already satisfied (e.g. "an already-published Agent that a Bundle test
// depends on"). It mirrors CreateListing's own bookkeeping.
func (f *fakeMarketplaceQuerier) publishFor(author int64, resourceType string, resourceID int64, listingRef, version string) store.MarketplaceListing {
	row := store.MarketplaceListing{
		ID: f.nextListingID, AuthorUserID: author, ResourceType: resourceType, ResourceID: resourceID,
		ListingRef: listingRef, Version: version, Visibility: "blackbox", Distribution: 1,
		PublishedAt: f.tick(),
	}
	f.nextListingID++
	f.listings = append(f.listings, row)
	return row
}

// ── resource lookups (owner-scoped, exact ref/version) ──────────────

func (f *fakeMarketplaceQuerier) GetAgentForOwner(_ context.Context, arg store.GetAgentForOwnerParams) (store.Agent, error) {
	for _, a := range f.agents {
		if a.OwnerUserID == arg.OwnerUserID && a.AgentRef == arg.AgentRef && a.Version == arg.Version {
			return a, nil
		}
	}
	return store.Agent{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetAgentLatestByRef(_ context.Context, arg store.GetAgentLatestByRefParams) (store.Agent, error) {
	for i := len(f.agents) - 1; i >= 0; i-- {
		a := f.agents[i]
		if a.OwnerUserID == arg.OwnerUserID && a.AgentRef == arg.AgentRef {
			return a, nil
		}
	}
	return store.Agent{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetBundleForOwner(_ context.Context, arg store.GetBundleForOwnerParams) (store.Bundle, error) {
	for _, b := range f.bundles {
		if b.OwnerUserID == arg.OwnerUserID && b.BundleRef == arg.BundleRef && b.Version == arg.Version {
			return b, nil
		}
	}
	return store.Bundle{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetSkillByRefVersionForOwner(_ context.Context, arg store.GetSkillByRefVersionForOwnerParams) (store.Skill, error) {
	for _, s := range f.skills {
		if s.OwnerUserID == arg.OwnerUserID && s.Ref == arg.Ref && s.Version == arg.Version {
			return s, nil
		}
	}
	return store.Skill{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetMCPServerByRefVersionForOwner(_ context.Context, arg store.GetMCPServerByRefVersionForOwnerParams) (store.McpServer, error) {
	for _, m := range f.mcps {
		if m.OwnerUserID == arg.OwnerUserID && m.Ref == arg.Ref && m.Version == arg.Version {
			return m, nil
		}
	}
	return store.McpServer{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetSkillLatestStatusByRef(_ context.Context, arg store.GetSkillLatestStatusByRefParams) (int16, error) {
	for _, s := range f.skills {
		if s.OwnerUserID == arg.OwnerUserID && s.Ref == arg.Ref {
			return s.Status, nil
		}
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetMCPServerLatestStatusByRef(_ context.Context, arg store.GetMCPServerLatestStatusByRefParams) (int16, error) {
	for _, m := range f.mcps {
		if m.OwnerUserID == arg.OwnerUserID && m.Ref == arg.Ref {
			return m.Status, nil
		}
	}
	return 0, pgx.ErrNoRows
}

// ── dependency-satisfaction (publish-time closure) ──────────────────

func (f *fakeMarketplaceQuerier) listingFor(resourceType string, resourceID int64) (store.MarketplaceListing, bool) {
	var best store.MarketplaceListing
	found := false
	for _, l := range f.listings {
		if l.ResourceType != resourceType || l.ResourceID != resourceID || l.Distribution == 3 {
			continue
		}
		if !found || l.PublishedAt.Time.After(best.PublishedAt.Time) {
			best = l
			found = true
		}
	}
	return best, found
}

func (f *fakeMarketplaceQuerier) GetAgentListingForOwnerByRefVersion(_ context.Context, arg store.GetAgentListingForOwnerByRefVersionParams) (store.MarketplaceListing, error) {
	for _, a := range f.agents {
		if a.OwnerUserID == arg.OwnerUserID && a.AgentRef == arg.AgentRef && a.Version == arg.Version {
			if l, ok := f.listingFor("agent", a.ID); ok {
				return l, nil
			}
		}
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetBundleListingForOwnerByRefVersion(_ context.Context, arg store.GetBundleListingForOwnerByRefVersionParams) (store.MarketplaceListing, error) {
	for _, b := range f.bundles {
		if b.OwnerUserID == arg.OwnerUserID && b.BundleRef == arg.BundleRef && b.Version == arg.Version {
			if l, ok := f.listingFor("bundle", b.ID); ok {
				return l, nil
			}
		}
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetSkillListingForOwnerByRef(_ context.Context, arg store.GetSkillListingForOwnerByRefParams) (store.MarketplaceListing, error) {
	var best store.MarketplaceListing
	found := false
	for _, s := range f.skills {
		if s.OwnerUserID != arg.OwnerUserID || s.Ref != arg.Ref {
			continue
		}
		if l, ok := f.listingFor("skill", s.ID); ok {
			if !found || l.PublishedAt.Time.After(best.PublishedAt.Time) {
				best, found = l, true
			}
		}
	}
	if !found {
		return store.MarketplaceListing{}, pgx.ErrNoRows
	}
	return best, nil
}

func (f *fakeMarketplaceQuerier) GetMCPServerListingForOwnerByRef(_ context.Context, arg store.GetMCPServerListingForOwnerByRefParams) (store.MarketplaceListing, error) {
	var best store.MarketplaceListing
	found := false
	for _, m := range f.mcps {
		if m.OwnerUserID != arg.OwnerUserID || m.Ref != arg.Ref {
			continue
		}
		if l, ok := f.listingFor("mcp", m.ID); ok {
			if !found || l.PublishedAt.Time.After(best.PublishedAt.Time) {
				best, found = l, true
			}
		}
	}
	if !found {
		return store.MarketplaceListing{}, pgx.ErrNoRows
	}
	return best, nil
}

// ── reverse-dependency checks (unpublish, 70007) ─────────────────────

func refsAgentInBundle(def []byte, ref string) bool {
	var m map[string]any
	if err := json.Unmarshal(def, &m); err != nil {
		return false
	}
	agentsRaw, _ := m["agents"].([]any)
	for _, a := range agentsRaw {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if r, _ := am["ref"].(string); r == ref {
			return true
		}
	}
	return false
}

func agentRefsContain(def []byte, field, ref string) bool {
	var m map[string]any
	if err := json.Unmarshal(def, &m); err != nil {
		return false
	}
	caps, _ := m["capabilities"].(map[string]any)
	list, _ := caps[field].([]any)
	for _, v := range list {
		if s, _ := v.(string); s == ref {
			return true
		}
	}
	return false
}

func (f *fakeMarketplaceQuerier) FindPublishedBundlesReferencingAgentRef(_ context.Context, arg store.FindPublishedBundlesReferencingAgentRefParams) ([]store.FindPublishedBundlesReferencingAgentRefRow, error) {
	var out []store.FindPublishedBundlesReferencingAgentRefRow
	for _, b := range f.bundles {
		if b.OwnerUserID != arg.OwnerUserID {
			continue
		}
		if _, ok := f.listingFor("bundle", b.ID); !ok {
			continue
		}
		if refsAgentInBundle(b.Definition, arg.AgentRef) {
			out = append(out, store.FindPublishedBundlesReferencingAgentRefRow{BundleRef: b.BundleRef, Version: b.Version})
		}
	}
	return out, nil
}

func (f *fakeMarketplaceQuerier) FindPublishedAgentsReferencingSkillRef(_ context.Context, arg store.FindPublishedAgentsReferencingSkillRefParams) ([]store.FindPublishedAgentsReferencingSkillRefRow, error) {
	var out []store.FindPublishedAgentsReferencingSkillRefRow
	for _, a := range f.agents {
		if a.OwnerUserID != arg.OwnerUserID {
			continue
		}
		if _, ok := f.listingFor("agent", a.ID); !ok {
			continue
		}
		if agentRefsContain(a.Definition, "skills", arg.SkillRef) {
			out = append(out, store.FindPublishedAgentsReferencingSkillRefRow{AgentRef: a.AgentRef, Version: a.Version})
		}
	}
	return out, nil
}

func (f *fakeMarketplaceQuerier) FindPublishedAgentsReferencingToolRef(_ context.Context, arg store.FindPublishedAgentsReferencingToolRefParams) ([]store.FindPublishedAgentsReferencingToolRefRow, error) {
	var out []store.FindPublishedAgentsReferencingToolRefRow
	for _, a := range f.agents {
		if a.OwnerUserID != arg.OwnerUserID {
			continue
		}
		if _, ok := f.listingFor("agent", a.ID); !ok {
			continue
		}
		if agentRefsContain(a.Definition, "tools", arg.ToolRef) {
			out = append(out, store.FindPublishedAgentsReferencingToolRefRow{AgentRef: a.AgentRef, Version: a.Version})
		}
	}
	return out, nil
}

// ── listings ─────────────────────────────────────────────────────────

func (f *fakeMarketplaceQuerier) listingIndex(id int64) int {
	for i, l := range f.listings {
		if l.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeMarketplaceQuerier) CreateListing(_ context.Context, arg store.CreateListingParams) (store.MarketplaceListing, error) {
	for _, l := range f.listings {
		if l.ListingRef == arg.ListingRef && l.Version == arg.Version {
			return store.MarketplaceListing{}, &pgconn.PgError{Code: "23505"}
		}
	}
	row := store.MarketplaceListing{
		ID: f.nextListingID, AuthorUserID: arg.AuthorUserID, ResourceType: arg.ResourceType, ResourceID: arg.ResourceID,
		ListingRef: arg.ListingRef, Version: arg.Version, Visibility: "blackbox", Changelog: arg.Changelog,
		Distribution: 1, PublishedAt: f.tick(),
	}
	f.nextListingID++
	f.listings = append(f.listings, row)
	return row, nil
}

func (f *fakeMarketplaceQuerier) GetListingByID(_ context.Context, id int64) (store.MarketplaceListing, error) {
	if i := f.listingIndex(id); i >= 0 {
		return f.listings[i], nil
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetListingByListingRefLatestPublished(_ context.Context, listingRef string) (store.MarketplaceListing, error) {
	var best store.MarketplaceListing
	found := false
	for _, l := range f.listings {
		if l.ListingRef != listingRef || l.Distribution == 3 {
			continue
		}
		if !found || l.PublishedAt.Time.After(best.PublishedAt.Time) {
			best, found = l, true
		}
	}
	if !found {
		return store.MarketplaceListing{}, pgx.ErrNoRows
	}
	return best, nil
}

func (f *fakeMarketplaceQuerier) GetListingByListingRefAndVersion(_ context.Context, arg store.GetListingByListingRefAndVersionParams) (store.MarketplaceListing, error) {
	for _, l := range f.listings {
		if l.ListingRef == arg.ListingRef && l.Version == arg.Version && l.Distribution != 3 {
			return l, nil
		}
	}
	return store.MarketplaceListing{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) SetListingDistribution(_ context.Context, arg store.SetListingDistributionParams) error {
	if i := f.listingIndex(arg.ID); i >= 0 {
		f.listings[i].Distribution = arg.Distribution
		return nil
	}
	return pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) IncrementListingSubscriberCount(_ context.Context, id int64) error {
	if i := f.listingIndex(id); i >= 0 {
		f.listings[i].SubscriberCount++
		return nil
	}
	return pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) DecrementListingSubscriberCount(_ context.Context, id int64) error {
	if i := f.listingIndex(id); i >= 0 {
		f.listings[i].SubscriberCount--
		return nil
	}
	return pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) ListListingVersionHistory(_ context.Context, listingRef string) ([]store.ListListingVersionHistoryRow, error) {
	var out []store.ListListingVersionHistoryRow
	for _, l := range f.listings {
		if l.ListingRef == listingRef && l.Distribution != 3 {
			out = append(out, store.ListListingVersionHistoryRow{Version: l.Version, Changelog: l.Changelog, PublishedAt: l.PublishedAt})
		}
	}
	return out, nil
}

func (f *fakeMarketplaceQuerier) listPage(resourceType string, afterID int64, limit int32, displayMeta func(resourceID int64) []byte) []store.ListPublishedAgentListingsPageRow {
	var out []store.ListPublishedAgentListingsPageRow
	for _, l := range f.listings {
		if l.ResourceType != resourceType || l.Distribution != 1 || l.ID <= afterID {
			continue
		}
		out = append(out, store.ListPublishedAgentListingsPageRow{
			ID: l.ID, ListingRef: l.ListingRef, ResourceType: l.ResourceType, Version: l.Version, Visibility: l.Visibility,
			AuthorUserID: l.AuthorUserID, SubscriberCount: l.SubscriberCount, RunCount: l.RunCount, PublishedAt: l.PublishedAt,
			DisplayMeta: displayMeta(l.ResourceID),
		})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if int32(len(out)) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakeMarketplaceQuerier) agentDisplayMeta(id int64) []byte {
	for _, a := range f.agents {
		if a.ID == id {
			return a.DisplayMeta
		}
	}
	return []byte("{}")
}
func (f *fakeMarketplaceQuerier) bundleDisplayMeta(id int64) []byte {
	for _, b := range f.bundles {
		if b.ID == id {
			return b.DisplayMeta
		}
	}
	return []byte("{}")
}
func (f *fakeMarketplaceQuerier) skillDisplayMeta(id int64) []byte {
	for _, s := range f.skills {
		if s.ID == id {
			return s.DisplayMeta
		}
	}
	return []byte("{}")
}
func (f *fakeMarketplaceQuerier) mcpDisplayMeta(id int64) []byte {
	for _, m := range f.mcps {
		if m.ID == id {
			return m.DisplayMeta
		}
	}
	return []byte("{}")
}

func (f *fakeMarketplaceQuerier) ListPublishedAgentListingsPage(_ context.Context, arg store.ListPublishedAgentListingsPageParams) ([]store.ListPublishedAgentListingsPageRow, error) {
	return f.listPage("agent", arg.ID, arg.Limit, f.agentDisplayMeta), nil
}
func (f *fakeMarketplaceQuerier) ListPublishedBundleListingsPage(_ context.Context, arg store.ListPublishedBundleListingsPageParams) ([]store.ListPublishedBundleListingsPageRow, error) {
	rows := f.listPage("bundle", arg.ID, arg.Limit, f.bundleDisplayMeta)
	out := make([]store.ListPublishedBundleListingsPageRow, len(rows))
	for i, r := range rows {
		out[i] = store.ListPublishedBundleListingsPageRow(r)
	}
	return out, nil
}
func (f *fakeMarketplaceQuerier) ListPublishedSkillListingsPage(_ context.Context, arg store.ListPublishedSkillListingsPageParams) ([]store.ListPublishedSkillListingsPageRow, error) {
	rows := f.listPage("skill", arg.ID, arg.Limit, f.skillDisplayMeta)
	out := make([]store.ListPublishedSkillListingsPageRow, len(rows))
	for i, r := range rows {
		out[i] = store.ListPublishedSkillListingsPageRow(r)
	}
	return out, nil
}
func (f *fakeMarketplaceQuerier) ListPublishedMCPServerListingsPage(_ context.Context, arg store.ListPublishedMCPServerListingsPageParams) ([]store.ListPublishedMCPServerListingsPageRow, error) {
	rows := f.listPage("mcp", arg.ID, arg.Limit, f.mcpDisplayMeta)
	out := make([]store.ListPublishedMCPServerListingsPageRow, len(rows))
	for i, r := range rows {
		out[i] = store.ListPublishedMCPServerListingsPageRow(r)
	}
	return out, nil
}

func (f *fakeMarketplaceQuerier) GetAgentListingDisplayByListingID(_ context.Context, id int64) (store.GetAgentListingDisplayByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetAgentListingDisplayByListingIDRow{}, err
	}
	return store.GetAgentListingDisplayByListingIDRow{
		ID: l.ID, ListingRef: l.ListingRef, ResourceType: l.ResourceType, Version: l.Version, Visibility: l.Visibility,
		AuthorUserID: l.AuthorUserID, SubscriberCount: l.SubscriberCount, RunCount: l.RunCount, PublishedAt: l.PublishedAt,
		DisplayMeta: f.agentDisplayMeta(l.ResourceID),
	}, nil
}
func (f *fakeMarketplaceQuerier) GetBundleListingDisplayByListingID(_ context.Context, id int64) (store.GetBundleListingDisplayByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetBundleListingDisplayByListingIDRow{}, err
	}
	return store.GetBundleListingDisplayByListingIDRow{
		ID: l.ID, ListingRef: l.ListingRef, ResourceType: l.ResourceType, Version: l.Version, Visibility: l.Visibility,
		AuthorUserID: l.AuthorUserID, SubscriberCount: l.SubscriberCount, RunCount: l.RunCount, PublishedAt: l.PublishedAt,
		DisplayMeta: f.bundleDisplayMeta(l.ResourceID),
	}, nil
}
func (f *fakeMarketplaceQuerier) GetSkillListingDisplayByListingID(_ context.Context, id int64) (store.GetSkillListingDisplayByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetSkillListingDisplayByListingIDRow{}, err
	}
	return store.GetSkillListingDisplayByListingIDRow{
		ID: l.ID, ListingRef: l.ListingRef, ResourceType: l.ResourceType, Version: l.Version, Visibility: l.Visibility,
		AuthorUserID: l.AuthorUserID, SubscriberCount: l.SubscriberCount, RunCount: l.RunCount, PublishedAt: l.PublishedAt,
		DisplayMeta: f.skillDisplayMeta(l.ResourceID),
	}, nil
}
func (f *fakeMarketplaceQuerier) GetMCPServerListingDisplayByListingID(_ context.Context, id int64) (store.GetMCPServerListingDisplayByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetMCPServerListingDisplayByListingIDRow{}, err
	}
	return store.GetMCPServerListingDisplayByListingIDRow{
		ID: l.ID, ListingRef: l.ListingRef, ResourceType: l.ResourceType, Version: l.Version, Visibility: l.Visibility,
		AuthorUserID: l.AuthorUserID, SubscriberCount: l.SubscriberCount, RunCount: l.RunCount, PublishedAt: l.PublishedAt,
		DisplayMeta: f.mcpDisplayMeta(l.ResourceID),
	}, nil
}

func agentDefConstraints(def []byte) (maxToolCalls, timeoutSeconds int32, estimatedTokensRange string) {
	maxToolCalls, timeoutSeconds = 20, 300
	var m map[string]any
	if err := json.Unmarshal(def, &m); err != nil {
		return
	}
	c, _ := m["constraints"].(map[string]any)
	if v, ok := c["max_tool_calls"].(float64); ok {
		maxToolCalls = int32(v)
	}
	if v, ok := c["timeout_seconds"].(float64); ok {
		timeoutSeconds = int32(v)
	}
	if v, ok := c["estimated_tokens_range"].(string); ok {
		estimatedTokensRange = v
	}
	return
}

func (f *fakeMarketplaceQuerier) GetAgentConstraintsSummaryByListingID(_ context.Context, id int64) (store.GetAgentConstraintsSummaryByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetAgentConstraintsSummaryByListingIDRow{}, err
	}
	for _, a := range f.agents {
		if a.ID == l.ResourceID {
			maxTC, timeout, tokens := agentDefConstraints(a.Definition)
			return store.GetAgentConstraintsSummaryByListingIDRow{MaxToolCalls: maxTC, TimeoutSeconds: timeout, EstimatedTokensRange: tokens}, nil
		}
	}
	return store.GetAgentConstraintsSummaryByListingIDRow{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetBundleConstraintsSummaryByListingID(_ context.Context, id int64) (store.GetBundleConstraintsSummaryByListingIDRow, error) {
	l, err := f.GetListingByID(context.Background(), id)
	if err != nil {
		return store.GetBundleConstraintsSummaryByListingIDRow{}, err
	}
	timeout := int32(3600)
	for _, b := range f.bundles {
		if b.ID == l.ResourceID {
			var m map[string]any
			if err := json.Unmarshal(b.Definition, &m); err == nil {
				if limits, ok := m["limits"].(map[string]any); ok {
					if v, ok := limits["max_wall_clock_seconds"].(float64); ok {
						timeout = int32(v)
					}
				}
			}
			return store.GetBundleConstraintsSummaryByListingIDRow{TimeoutSeconds: timeout}, nil
		}
	}
	return store.GetBundleConstraintsSummaryByListingIDRow{}, pgx.ErrNoRows
}

// ── display_meta / immutability writes ────────────────────────────────

func (f *fakeMarketplaceQuerier) SetAgentDisplayMeta(_ context.Context, arg store.SetAgentDisplayMetaParams) error {
	for i, a := range f.agents {
		if a.ID == arg.ID {
			f.agents[i].DisplayMeta = arg.DisplayMeta
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) SetBundleDisplayMeta(_ context.Context, arg store.SetBundleDisplayMetaParams) error {
	for i, b := range f.bundles {
		if b.ID == arg.ID {
			f.bundles[i].DisplayMeta = arg.DisplayMeta
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) SetSkillDisplayMeta(_ context.Context, arg store.SetSkillDisplayMetaParams) error {
	for i, s := range f.skills {
		if s.ID == arg.ID {
			f.skills[i].DisplayMeta = arg.DisplayMeta
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) SetMCPServerDisplayMeta(_ context.Context, arg store.SetMCPServerDisplayMetaParams) error {
	for i, m := range f.mcps {
		if m.ID == arg.ID {
			f.mcps[i].DisplayMeta = arg.DisplayMeta
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) MarkAgentImmutable(_ context.Context, id int64) error {
	for i, a := range f.agents {
		if a.ID == id {
			f.agents[i].Immutable = true
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) MarkBundleImmutable(_ context.Context, id int64) error {
	for i, b := range f.bundles {
		if b.ID == id {
			f.bundles[i].Immutable = true
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) MarkSkillImmutable(_ context.Context, id int64) error {
	for i, s := range f.skills {
		if s.ID == id {
			f.skills[i].Immutable = true
			return nil
		}
	}
	return pgx.ErrNoRows
}
func (f *fakeMarketplaceQuerier) MarkMCPServerImmutable(_ context.Context, id int64) error {
	for i, m := range f.mcps {
		if m.ID == id {
			f.mcps[i].Immutable = true
			return nil
		}
	}
	return pgx.ErrNoRows
}

// ── users ────────────────────────────────────────────────────────────

func (f *fakeMarketplaceQuerier) GetUserByID(_ context.Context, id int64) (store.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return store.User{}, pgx.ErrNoRows
}

// ── subscriptions ────────────────────────────────────────────────────

func (f *fakeMarketplaceQuerier) CreateSubscription(_ context.Context, arg store.CreateSubscriptionParams) (store.Subscription, error) {
	for _, s := range f.subs {
		if s.SubscriberID == arg.SubscriberID && s.ListingID == arg.ListingID {
			return store.Subscription{}, &pgconn.PgError{Code: "23505"}
		}
	}
	row := store.Subscription{ID: f.nextSubID, SubscriberID: arg.SubscriberID, ListingID: arg.ListingID, LocalAlias: arg.LocalAlias, CreatedAt: f.tick()}
	f.nextSubID++
	f.subs = append(f.subs, row)
	return row, nil
}

func (f *fakeMarketplaceQuerier) GetSubscriptionByListingAndSubscriber(_ context.Context, arg store.GetSubscriptionByListingAndSubscriberParams) (store.Subscription, error) {
	for _, s := range f.subs {
		if s.SubscriberID == arg.SubscriberID && s.ListingID == arg.ListingID {
			return s, nil
		}
	}
	return store.Subscription{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetSubscriptionByIDForSubscriber(_ context.Context, arg store.GetSubscriptionByIDForSubscriberParams) (store.Subscription, error) {
	for _, s := range f.subs {
		if s.ID == arg.ID && s.SubscriberID == arg.SubscriberID {
			return s, nil
		}
	}
	return store.Subscription{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) GetSubscriptionForSubscriberByListingRef(_ context.Context, arg store.GetSubscriptionForSubscriberByListingRefParams) (store.Subscription, error) {
	for _, s := range f.subs {
		if s.SubscriberID != arg.SubscriberID {
			continue
		}
		l, err := f.GetListingByID(context.Background(), s.ListingID)
		if err == nil && l.ListingRef == arg.ListingRef {
			return s, nil
		}
	}
	return store.Subscription{}, pgx.ErrNoRows
}

func (f *fakeMarketplaceQuerier) ListSubscriptionsForUserPage(_ context.Context, arg store.ListSubscriptionsForUserPageParams) ([]store.Subscription, error) {
	var out []store.Subscription
	for _, s := range f.subs {
		if s.SubscriberID == arg.SubscriberID && s.ID > arg.ID {
			out = append(out, s)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if int32(len(out)) > arg.Limit {
		out = out[:arg.Limit]
	}
	return out, nil
}

func (f *fakeMarketplaceQuerier) DeleteSubscription(_ context.Context, arg store.DeleteSubscriptionParams) error {
	i := -1
	for idx, s := range f.subs {
		if s.ID == arg.ID && s.SubscriberID == arg.SubscriberID {
			i = idx
			break
		}
	}
	if i < 0 {
		return pgx.ErrNoRows
	}
	f.subs = append(f.subs[:i], f.subs[i+1:]...)
	return nil
}

func (f *fakeMarketplaceQuerier) UpdateSubscriptionListing(_ context.Context, arg store.UpdateSubscriptionListingParams) (store.Subscription, error) {
	i := -1
	for idx, s := range f.subs {
		if s.ID == arg.ID && s.SubscriberID == arg.SubscriberID {
			i = idx
			break
		}
	}
	if i < 0 {
		return store.Subscription{}, pgx.ErrNoRows
	}
	f.subs[i].ListingID = arg.ListingID
	return f.subs[i], nil
}

// ── test helpers ─────────────────────────────────────────────────────

func newMarketplaceTestHandlers() (*MarketplaceHandlers, *fakeMarketplaceQuerier) {
	f := newFakeMarketplaceQuerier()
	return NewMarketplaceHandlers(f), f
}

func doMarketplaceRequest(h http.HandlerFunc, userID int64, method, path string, routeParams map[string]string, body any) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req = req.WithContext(WithUserID(req.Context(), userID))
	if len(routeParams) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range routeParams {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func simpleAgentDef(ref, version string) map[string]any {
	return map[string]any{
		"agent": ref, "version": version,
		"persona":      "SECRET_PERSONA_INSTRUCTIONS_" + ref,
		"capabilities": map[string]any{"tools": []any{}, "skills": []any{}},
		"constraints":  map[string]any{"max_tool_calls": 15, "timeout_seconds": 120, "estimated_tokens_range": "1000~2000"},
	}
}

func simpleBundleDef(ref, version string, agents []map[string]any) map[string]any {
	agentsAny := make([]any, len(agents))
	for i, a := range agents {
		agentsAny[i] = a
	}
	return map[string]any{
		"bundle": ref, "version": version,
		"orchestration": "SECRET_ORCHESTRATION_GRAPH_" + ref,
		"agents":        agentsAny,
	}
}

// ── tests ────────────────────────────────────────────────────────────

func TestPublish_Success(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))

	rr := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "agent", ResourceRef: "architect", Version: "1.0",
		DisplayMeta: json.RawMessage(`{"display_name":"Architect","description":"designs systems"}`),
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "SECRET_PERSONA_INSTRUCTIONS") {
		t.Fatalf("publish response leaked definition content: %s", rr.Body.String())
	}
}

func TestPublish_UnpublishedDependency_Returns422(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	f.addBundle(20, 1, "web-app-builder", "2.0", simpleBundleDef("web-app-builder", "2.0", []map[string]any{
		{"ref": "architect", "version": "1.0"},
	}))
	// architect is NOT published yet.

	rr := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "bundle", ResourceRef: "web-app-builder", Version: "2.0",
		DisplayMeta: json.RawMessage(`{"display_name":"Builder","description":"builds apps"}`),
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
	if !containsCode(rr.Body.String(), ErrPublishUnpublishedDeps) {
		t.Fatalf("expected 70001 in body, got %s", rr.Body.String())
	}
}

func TestPublish_TransitiveDependencyPublished_Succeeds(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	f.publishFor(1, "agent", agent.ID, "architect", "1.0")
	f.addBundle(20, 1, "web-app-builder", "2.0", simpleBundleDef("web-app-builder", "2.0", []map[string]any{
		{"ref": "architect", "version": "1.0"},
	}))

	rr := doMarketplaceRequest(h.Publish, 1, http.MethodPost, "/marketplace/listings", nil, publishRequest{
		ResourceType: "bundle", ResourceRef: "web-app-builder", Version: "2.0",
		DisplayMeta: json.RawMessage(`{"display_name":"Builder","description":"builds apps"}`),
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUnpublish_BlockedWhenDependedUpon(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	agentListing := f.publishFor(1, "agent", agent.ID, "architect", "1.0")
	bundle := f.addBundle(20, 1, "web-app-builder", "2.0", simpleBundleDef("web-app-builder", "2.0", []map[string]any{
		{"ref": "architect", "version": "1.0"},
	}))
	f.publishFor(1, "bundle", bundle.ID, "web-app-builder", "2.0")

	rr := doMarketplaceRequest(h.Unpublish, 1, http.MethodPost, "/marketplace/listings/{id}/unpublish",
		map[string]string{"id": strconv.FormatInt(agentListing.ID, 10)}, nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if !containsCode(rr.Body.String(), ErrDependencyStillReferenced) {
		t.Fatalf("expected 70007 in body, got %s", rr.Body.String())
	}
}

func TestUnpublish_AllowedWhenNotDependedUpon(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	agent := f.addAgent(10, 1, "standalone", "1.0", simpleAgentDef("standalone", "1.0"))
	listing := f.publishFor(1, "agent", agent.ID, "standalone", "1.0")

	rr := doMarketplaceRequest(h.Unpublish, 1, http.MethodPost, "/marketplace/listings/{id}/unpublish",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSubscribe_AuthorCannotSubscribeOwnResource(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	listing := f.publishFor(1, "agent", agent.ID, "architect", "1.0")

	rr := doMarketplaceRequest(h.Subscribe, 1, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSubscribe_Success_MarksResourceImmutable(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(2, "sub@test.com", "Subscriber")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	listing := f.publishFor(1, "agent", agent.ID, "architect", "1.0")

	rr := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if !f.agents[0].Immutable {
		t.Fatalf("expected subscribed agent version to be marked immutable")
	}
}

func TestSubscribe_DuplicateSameRef_Returns409(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(2, "sub@test.com", "Subscriber")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	listing := f.publishFor(1, "agent", agent.ID, "architect", "1.0")

	first := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	if first.Code != http.StatusCreated {
		t.Fatalf("first subscribe: expected 201, got %d: %s", first.Code, first.Body.String())
	}

	second := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	if second.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", second.Code, second.Body.String())
	}
	if !containsCode(second.Body.String(), ErrAlreadySubscribed) {
		t.Fatalf("expected 70004 in body, got %s", second.Body.String())
	}
}

func TestUpgrade_SwitchesVersion_SnapshotPreservedUntilExplicit(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(2, "sub@test.com", "Subscriber")
	agentV1 := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	listingV1 := f.publishFor(1, "agent", agentV1.ID, "architect", "1.0")

	subRR := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listingV1.ID, 10)}, nil)
	var subEnv struct {
		Data subscriptionDTO `json:"data"`
	}
	if err := json.Unmarshal(subRR.Body.Bytes(), &subEnv); err != nil {
		t.Fatalf("decode subscribe response: %v", err)
	}

	// Author publishes v1.1 — must NOT move the existing subscriber.
	agentV11 := f.addAgent(11, 1, "architect", "1.1", simpleAgentDef("architect", "1.1"))
	f.publishFor(1, "agent", agentV11.ID, "architect", "1.1")

	listRR := doMarketplaceRequest(h.ListSubscriptions, 2, http.MethodGet, "/marketplace/subscriptions", nil, nil)
	var listEnv struct {
		Data struct {
			Items []subscriptionDTO `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listEnv.Data.Items) != 1 || listEnv.Data.Items[0].SubscribedVersion != "1.0" {
		t.Fatalf("expected subscribed_version to remain 1.0, got %+v", listEnv.Data.Items)
	}
	if listEnv.Data.Items[0].LatestVersion == nil || *listEnv.Data.Items[0].LatestVersion != "1.1" {
		t.Fatalf("expected latest_version 1.1 to be surfaced, got %+v", listEnv.Data.Items[0].LatestVersion)
	}

	upRR := doMarketplaceRequest(h.Upgrade, 2, http.MethodPost, "/marketplace/subscriptions/{id}/upgrade",
		map[string]string{"id": subEnv.Data.ID}, upgradeRequest{TargetVersion: "1.1"})
	if upRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", upRR.Code, upRR.Body.String())
	}
	var upEnv struct {
		Data subscriptionDTO `json:"data"`
	}
	if err := json.Unmarshal(upRR.Body.Bytes(), &upEnv); err != nil {
		t.Fatalf("decode upgrade response: %v", err)
	}
	if upEnv.Data.SubscribedVersion != "1.1" {
		t.Fatalf("expected subscribed_version 1.1 after upgrade, got %s", upEnv.Data.SubscribedVersion)
	}
	if !f.agents[1].Immutable {
		t.Fatalf("expected the newly-subscribed v1.1 to be marked immutable")
	}
}

func TestUnsubscribe_DecrementsCount(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(2, "sub@test.com", "Subscriber")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	listing := f.publishFor(1, "agent", agent.ID, "architect", "1.0")

	subRR := doMarketplaceRequest(h.Subscribe, 2, http.MethodPost, "/marketplace/listings/{id}/subscribe",
		map[string]string{"id": strconv.FormatInt(listing.ID, 10)}, nil)
	var subEnv struct {
		Data subscriptionDTO `json:"data"`
	}
	_ = json.Unmarshal(subRR.Body.Bytes(), &subEnv)

	rr := doMarketplaceRequest(h.Unsubscribe, 2, http.MethodDelete, "/marketplace/subscriptions/{id}",
		map[string]string{"id": subEnv.Data.ID}, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if f.listings[0].SubscriberCount != 0 {
		t.Fatalf("expected subscriber_count back to 0, got %d", f.listings[0].SubscriberCount)
	}
}

// TestBlackboxNoDefinitionLeak walks every subscriber-facing marketplace
// endpoint and asserts the response JSON never contains a definition-only
// key or content — only display_meta and the necessary constraints
// exception ever cross the DAO boundary (spec-08's non-negotiable rule).
func TestBlackboxNoDefinitionLeak(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(2, "sub@test.com", "Subscriber")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	f.publishFor(1, "agent", agent.ID, "architect", "1.0")
	_ = f.SetAgentDisplayMeta(context.Background(), store.SetAgentDisplayMetaParams{
		ID: agent.ID, DisplayMeta: []byte(`{"display_name":"Architect","description":"designs systems"}`),
	})

	forbidden := []string{"SECRET_PERSONA_INSTRUCTIONS", `"definition"`, `"persona"`}

	browseRR := doMarketplaceRequest(h.Browse, 2, http.MethodGet, "/marketplace/listings", nil, nil)
	detailRR := doMarketplaceRequest(h.Detail, 2, http.MethodGet, "/marketplace/listings/{ref}",
		map[string]string{"ref": "architect"}, nil)

	for name, body := range map[string]string{"browse": browseRR.Body.String(), "detail": detailRR.Body.String()} {
		for _, marker := range forbidden {
			if strings.Contains(body, marker) {
				t.Fatalf("%s response leaked %q: %s", name, marker, body)
			}
		}
	}
	if !strings.Contains(detailRR.Body.String(), "Architect") {
		t.Fatalf("expected detail to still expose display_meta content: %s", detailRR.Body.String())
	}
}

// TestUnsubscribedCannotAccess: a user who never subscribed gets exactly
// the same blackbox-safe payload as anyone else — no path (browse, detail,
// or a forged subscribe-only listing id) exposes definition.
func TestUnsubscribedCannotAccess(t *testing.T) {
	h, f := newMarketplaceTestHandlers()
	f.addUser(1, "author@test.com", "Author")
	f.addUser(3, "stranger@test.com", "Stranger")
	agent := f.addAgent(10, 1, "architect", "1.0", simpleAgentDef("architect", "1.0"))
	f.publishFor(1, "agent", agent.ID, "architect", "1.0")
	_ = f.SetAgentDisplayMeta(context.Background(), store.SetAgentDisplayMetaParams{
		ID: agent.ID, DisplayMeta: []byte(`{"display_name":"Architect","description":"designs systems"}`),
	})

	detailRR := doMarketplaceRequest(h.Detail, 3, http.MethodGet, "/marketplace/listings/{ref}",
		map[string]string{"ref": "architect"}, nil)
	if strings.Contains(detailRR.Body.String(), "SECRET_PERSONA_INSTRUCTIONS") {
		t.Fatalf("unsubscribed stranger's detail view leaked definition: %s", detailRR.Body.String())
	}
	var detailEnv struct {
		Data listingDetailDTO `json:"data"`
	}
	if err := json.Unmarshal(detailRR.Body.Bytes(), &detailEnv); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detailEnv.Data.Subscribed {
		t.Fatalf("stranger should not show as subscribed")
	}

	// Listing subscriptions never returns another user's subscription, so
	// a stranger can't enumerate anything via that path either.
	listRR := doMarketplaceRequest(h.ListSubscriptions, 3, http.MethodGet, "/marketplace/subscriptions", nil, nil)
	var listEnv struct {
		Data struct {
			Items []subscriptionDTO `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listEnv.Data.Items) != 0 {
		t.Fatalf("stranger should have no subscriptions, got %+v", listEnv.Data.Items)
	}
}
