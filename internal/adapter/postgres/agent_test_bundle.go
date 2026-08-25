package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/domain/bundle"
	"github.com/marcon0203/agentic-kit/internal/domain/run"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// agentTestBundleVersion never changes: there is exactly one placeholder row
// per owner and nothing ever reads its definition, so versioning it would be
// bookkeeping with no reader.
const agentTestBundleVersion = "1.0"

// AgentTestBundleProvider lazily creates (and thereafter returns) the one
// hidden Bundle row per owner that 草稿试运行 runs hang off — see
// run.AgentTestBundleProvider for why a bundle row has to exist at all.
type AgentTestBundleProvider struct{ q store.Querier }

func NewAgentTestBundleProvider(q store.Querier) *AgentTestBundleProvider {
	return &AgentTestBundleProvider{q: q}
}

var _ run.AgentTestBundleProvider = (*AgentTestBundleProvider)(nil)

func (p *AgentTestBundleProvider) Ensure(ctx context.Context, ownerID int64) (int64, string, string, error) {
	ref := bundle.SystemAgentTestRef

	existing, err := p.q.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{
		OwnerUserID: ownerID, BundleRef: ref, Version: agentTestBundleVersion,
	})
	if err == nil {
		return existing.ID, ref, agentTestBundleVersion, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", "", err
	}

	// The stored definition is a placeholder that is never executed —
	// StartAgentTest replaces it with the draft Agent before compiling.
	// It's still written in the Bundle DSL's own shape so anything that
	// reads bundles generically doesn't trip over a half-formed document.
	definition, err := json.Marshal(map[string]any{
		"bundle": ref, "version": agentTestBundleVersion, "type": "single", "agents": []any{},
	})
	if err != nil {
		return 0, "", "", err
	}
	displayMeta, err := json.Marshal(map[string]any{"display_name": "智能体试运行（系统）"})
	if err != nil {
		return 0, "", "", err
	}

	created, err := p.q.CreateBundle(ctx, store.CreateBundleParams{
		OwnerUserID: ownerID, BundleRef: ref, Version: agentTestBundleVersion,
		Definition: definition, DisplayMeta: displayMeta,
	})
	if err != nil {
		// Two concurrent first-ever test runs race here; the unique
		// (owner, ref, version) index picks a winner and the loser just
		// reads the row the winner wrote.
		existing, getErr := p.q.GetBundleForOwner(ctx, store.GetBundleForOwnerParams{
			OwnerUserID: ownerID, BundleRef: ref, Version: agentTestBundleVersion,
		})
		if getErr == nil {
			return existing.ID, ref, agentTestBundleVersion, nil
		}
		return 0, "", "", err
	}
	return created.ID, ref, agentTestBundleVersion, nil
}
