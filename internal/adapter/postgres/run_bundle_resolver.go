package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// RunBundleResolver implements run.BundleResolver.
type RunBundleResolver struct{ q store.Querier }

func NewRunBundleResolver(q store.Querier) *RunBundleResolver { return &RunBundleResolver{q: q} }

// Resolve tries ownership first, then subscription.
//
// The second path deliberately ignores the caller's bundle_version: the
// version that runs is the one the subscription is bound to (spec-08's
// snapshot isolation). Accepting a caller-supplied version here would let
// a subscriber run a newer, unsubscribed version of somebody's Bundle.
func (r *RunBundleResolver) Resolve(ctx context.Context, userID int64, bundleRef, bundleVersion string) (run.ResolvedBundle, error) {
	var row store.Bundle
	var err error
	if bundleVersion != "" {
		row, err = r.q.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{OwnerUserID: userID, BundleRef: bundleRef, Version: bundleVersion})
	} else {
		row, err = r.q.GetBundleLatestByRef(ctx, store.GetBundleLatestByRefParams{OwnerUserID: userID, BundleRef: bundleRef})
	}
	if err == nil {
		return toResolvedBundle(row, nil)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return run.ResolvedBundle{}, err
	}

	sub, err := r.q.GetSubscriptionForSubscriberByListingRef(ctx, store.GetSubscriptionForSubscriberByListingRefParams{
		SubscriberID: userID, ListingRef: bundleRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ResolvedBundle{}, run.ErrNotSubscribed
	}
	if err != nil {
		return run.ResolvedBundle{}, err
	}

	listing, err := r.q.GetListingByID(ctx, sub.ListingID)
	if err != nil {
		return run.ResolvedBundle{}, err
	}
	// A subscription to an Agent or a Tool is not a licence to run
	// something: only a bundle listing is executable.
	if listing.ResourceType != "bundle" {
		return run.ResolvedBundle{}, run.ErrNotSubscribed
	}

	bundleRow, err := r.q.GetBundleByID(ctx, listing.ResourceID)
	if err != nil {
		return run.ResolvedBundle{}, err
	}
	listingID := listing.ID
	return toResolvedBundle(bundleRow, &listingID)
}

func (r *RunBundleResolver) LoadForRun(ctx context.Context, bundleID int64) (run.ResolvedBundle, error) {
	row, err := r.q.GetBundleByID(ctx, bundleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.ResolvedBundle{}, run.ErrNotFound
	}
	if err != nil {
		return run.ResolvedBundle{}, err
	}
	return toResolvedBundle(row, nil)
}

func toResolvedBundle(row store.Bundle, viaListingID *int64) (run.ResolvedBundle, error) {
	var def map[string]any
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return run.ResolvedBundle{}, err
	}
	return run.ResolvedBundle{
		BundleID: row.ID, Ref: row.BundleRef, Version: row.Version, Definition: def,
		OwnerUserID: row.OwnerUserID, ViaListingID: viaListingID,
		DeclaredOutputs: declaredOutputs(row.DisplayMeta),
	}, nil
}

// declaredOutputs reads display_meta.io_description.outputs — the Bundle's
// published output surface, and the whole of what a non-author may see of
// a run's shared_state.
func declaredOutputs(displayMeta []byte) []string {
	var meta struct {
		IODescription struct {
			Outputs []string `json:"outputs"`
		} `json:"io_description"`
	}
	_ = json.Unmarshal(displayMeta, &meta)
	return meta.IODescription.Outputs
}
