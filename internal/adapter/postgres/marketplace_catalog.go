package postgres

import (
	"context"
	"encoding/json"

	"github.com/marcon0203/agentic-kit/internal/domain/marketplace"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// MarketplaceCatalog implements marketplace.ResourceCatalog. Every method
// fans out over the four resource kinds, which each live in their own table
// — that fan-out is precisely the storage detail the port hides.
type MarketplaceCatalog struct{ q store.Querier }

func NewMarketplaceCatalog(q store.Querier) *MarketplaceCatalog { return &MarketplaceCatalog{q: q} }

var _ marketplace.ResourceCatalog = (*MarketplaceCatalog)(nil)

func (c *MarketplaceCatalog) ResolvePrivateID(ctx context.Context, ownerID int64, kind marketplace.Kind, ref, version string) (int64, error) {
	var (
		id  int64
		err error
	)
	switch kind {
	case marketplace.KindAgent:
		row, e := c.q.GetAgentForOwner(ctx, store.GetAgentForOwnerParams{OwnerUserID: ownerID, AgentRef: ref, Version: version})
		id, err = row.ID, e
	case marketplace.KindBundle:
		row, e := c.q.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{OwnerUserID: ownerID, BundleRef: ref, Version: version})
		id, err = row.ID, e
	case marketplace.KindSkill:
		row, e := c.q.GetSkillByRefVersionForOwner(ctx, store.GetSkillByRefVersionForOwnerParams{OwnerUserID: ownerID, Ref: ref, Version: version})
		id, err = row.ID, e
	case marketplace.KindMCP:
		row, e := c.q.GetMCPServerByRefVersionForOwner(ctx, store.GetMCPServerByRefVersionForOwnerParams{OwnerUserID: ownerID, Ref: ref, Version: version})
		id, err = row.ID, e
	default:
		return 0, marketplace.ErrNotFound
	}
	if err != nil {
		return 0, translateNotFound(err)
	}
	return id, nil
}

func (c *MarketplaceCatalog) SetDisplayMeta(ctx context.Context, kind marketplace.Kind, resourceID int64, meta marketplace.DisplayMeta) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	switch kind {
	case marketplace.KindAgent:
		return c.q.SetAgentDisplayMeta(ctx, store.SetAgentDisplayMetaParams{ID: resourceID, DisplayMeta: raw})
	case marketplace.KindBundle:
		return c.q.SetBundleDisplayMeta(ctx, store.SetBundleDisplayMetaParams{ID: resourceID, DisplayMeta: raw})
	case marketplace.KindSkill:
		return c.q.SetSkillDisplayMeta(ctx, store.SetSkillDisplayMetaParams{ID: resourceID, DisplayMeta: raw})
	case marketplace.KindMCP:
		return c.q.SetMCPServerDisplayMeta(ctx, store.SetMCPServerDisplayMetaParams{ID: resourceID, DisplayMeta: raw})
	default:
		return nil
	}
}

func (c *MarketplaceCatalog) DisplayMetaForListing(ctx context.Context, kind marketplace.Kind, listingID int64) (marketplace.DisplayMeta, error) {
	var (
		raw []byte
		err error
	)
	switch kind {
	case marketplace.KindAgent:
		row, e := c.q.GetAgentListingDisplayByListingID(ctx, listingID)
		raw, err = row.DisplayMeta, e
	case marketplace.KindBundle:
		row, e := c.q.GetBundleListingDisplayByListingID(ctx, listingID)
		raw, err = row.DisplayMeta, e
	case marketplace.KindSkill:
		row, e := c.q.GetSkillListingDisplayByListingID(ctx, listingID)
		raw, err = row.DisplayMeta, e
	case marketplace.KindMCP:
		row, e := c.q.GetMCPServerListingDisplayByListingID(ctx, listingID)
		raw, err = row.DisplayMeta, e
	default:
		return nil, marketplace.ErrNotFound
	}
	if err != nil {
		return nil, translateNotFound(err)
	}
	var meta marketplace.DisplayMeta
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, err
		}
	}
	return meta, nil
}

func (c *MarketplaceCatalog) ConstraintsForListing(ctx context.Context, kind marketplace.Kind, listingID int64) (*marketplace.ConstraintsSummary, error) {
	switch kind {
	case marketplace.KindAgent:
		row, err := c.q.GetAgentConstraintsSummaryByListingID(ctx, listingID)
		if err != nil {
			return nil, translateNotFound(err)
		}
		maxCalls, timeout := row.MaxToolCalls, row.TimeoutSeconds
		return &marketplace.ConstraintsSummary{
			MaxToolCalls: &maxCalls, TimeoutSeconds: &timeout,
			EstimatedTokensRange: row.EstimatedTokensRange,
		}, nil
	case marketplace.KindBundle:
		row, err := c.q.GetBundleConstraintsSummaryByListingID(ctx, listingID)
		if err != nil {
			return nil, translateNotFound(err)
		}
		timeout := row.TimeoutSeconds
		return &marketplace.ConstraintsSummary{TimeoutSeconds: &timeout}, nil
	default:
		// Skills and MCP servers have no execution-constraints concept.
		return nil, nil
	}
}

func (c *MarketplaceCatalog) Freeze(ctx context.Context, kind marketplace.Kind, resourceID int64) error {
	switch kind {
	case marketplace.KindAgent:
		return c.q.MarkAgentImmutable(ctx, resourceID)
	case marketplace.KindBundle:
		return c.q.MarkBundleImmutable(ctx, resourceID)
	case marketplace.KindSkill:
		return c.q.MarkSkillImmutable(ctx, resourceID)
	case marketplace.KindMCP:
		return c.q.MarkMCPServerImmutable(ctx, resourceID)
	default:
		return nil
	}
}

func (c *MarketplaceCatalog) PublishedDependents(ctx context.Context, ownerID int64, kind marketplace.Kind, ref string) ([]marketplace.Dependent, error) {
	var out []marketplace.Dependent

	switch kind {
	case marketplace.KindAgent:
		rows, err := c.q.FindPublishedBundlesReferencingAgentRef(ctx, store.FindPublishedBundlesReferencingAgentRefParams{OwnerUserID: ownerID, AgentRef: ref})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, marketplace.Dependent{Kind: marketplace.KindBundle, Ref: row.BundleRef, Version: row.Version})
		}
	case marketplace.KindSkill:
		rows, err := c.q.FindPublishedAgentsReferencingSkillRef(ctx, store.FindPublishedAgentsReferencingSkillRefParams{OwnerUserID: ownerID, SkillRef: ref})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, marketplace.Dependent{Kind: marketplace.KindAgent, Ref: row.AgentRef, Version: row.Version})
		}
	case marketplace.KindMCP:
		rows, err := c.q.FindPublishedAgentsReferencingToolRef(ctx, store.FindPublishedAgentsReferencingToolRefParams{OwnerUserID: ownerID, ToolRef: ref})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, marketplace.Dependent{Kind: marketplace.KindAgent, Ref: row.AgentRef, Version: row.Version})
		}
	case marketplace.KindBundle:
		// Nothing in the DSL can reference a Bundle — it is always a root.
	}
	return out, nil
}
