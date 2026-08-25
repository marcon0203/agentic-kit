package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
	"github.com/marcon0203/agentic-kit/internal/store"
)

type BundleRepository struct{ q store.Querier }

func NewBundleRepository(q store.Querier) *BundleRepository { return &BundleRepository{q: q} }

var _ bundle.Repository = (*BundleRepository)(nil)

func toDomainBundle(row store.Bundle) (bundle.Bundle, error) {
	var def bundle.Definition
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return bundle.Bundle{}, err
	}
	return bundle.Bundle{
		ID: row.ID, OwnerID: row.OwnerUserID, Ref: row.BundleRef, Version: row.Version,
		Definition: def, Status: bundle.Status(row.Status), CreatedAt: row.CreatedAt.Time,
	}, nil
}

func (r *BundleRepository) ListLatestByOwner(ctx context.Context, ownerID int64, q domain.PageQuery) ([]bundle.Bundle, error) {
	rows, err := r.q.ListBundlesForOwnerLatestPage(ctx, store.ListBundlesForOwnerLatestPageParams{
		OwnerUserID: ownerID, BundleRef: q.After, Limit: int32(q.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]bundle.Bundle, 0, len(rows))
	for _, row := range rows {
		// The 草稿试运行 placeholder is platform plumbing, not one of the
		// user's applications — hidden here rather than in SQL because it
		// is at most one row per owner, and keeping the filter in Go means
		// every query that lists bundles doesn't have to remember it.
		if row.BundleRef == bundle.SystemAgentTestRef {
			continue
		}
		b, err := toDomainBundle(row)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *BundleRepository) Create(ctx context.Context, b bundle.Bundle) (bundle.Bundle, error) {
	defBytes, err := json.Marshal(b.Definition)
	if err != nil {
		return bundle.Bundle{}, err
	}
	row, err := r.q.CreateBundle(ctx, store.CreateBundleParams{
		OwnerUserID: b.OwnerID, BundleRef: b.Ref, Version: b.Version,
		Definition: defBytes, DisplayMeta: []byte("{}"),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return bundle.Bundle{}, bundle.ErrDuplicateVersion
		}
		return bundle.Bundle{}, err
	}
	return toDomainBundle(row)
}

// DeleteByRef maps any failure to ErrVersionLocked: migration 0006's
// immutable trigger is the only thing that refuses this delete.
func (r *BundleRepository) DeleteByRef(ctx context.Context, ownerID int64, ref string) error {
	if err := r.q.DeleteBundlesByRef(ctx, store.DeleteBundlesByRefParams{OwnerUserID: ownerID, BundleRef: ref}); err != nil {
		return bundle.ErrVersionLocked
	}
	return nil
}

func (r *BundleRepository) CountActiveSubscribedVersions(ctx context.Context, ownerID int64, ref string) (int64, error) {
	return r.q.CountActiveSubscribedListingsForBundleRef(ctx, store.CountActiveSubscribedListingsForBundleRefParams{
		OwnerUserID: ownerID, BundleRef: ref,
	})
}

// AgentHandoffs implements bundle.AgentHandoffs by reading the Agent DSL's
// advisory handoff block.
type AgentHandoffs struct{ q store.Querier }

func NewAgentHandoffs(q store.Querier) *AgentHandoffs { return &AgentHandoffs{q: q} }

var _ bundle.AgentHandoffs = (*AgentHandoffs)(nil)

func (a *AgentHandoffs) Lookup(ctx context.Context, ownerID int64, agentRef string) (bundle.Handoff, error) {
	if agentRef == "" {
		return bundle.Handoff{}, nil
	}
	row, err := a.q.GetAgentLatestByRef(ctx, store.GetAgentLatestByRefParams{OwnerUserID: ownerID, AgentRef: agentRef})
	if errors.Is(err, pgx.ErrNoRows) {
		// An unknown ref is not this check's problem — a zero Handoff means
		// "nothing declared", which never produces a warning.
		return bundle.Handoff{}, nil
	}
	if err != nil {
		return bundle.Handoff{}, err
	}

	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return bundle.Handoff{}, err
	}
	handoff, _ := def["handoff"].(map[string]any)
	return bundle.Handoff{
		AcceptsInputFrom: dslStringSlice(handoff, "accepts_input_from"),
		ProducesOutputTo: dslStringSlice(handoff, "produces_output_to"),
	}, nil
}
