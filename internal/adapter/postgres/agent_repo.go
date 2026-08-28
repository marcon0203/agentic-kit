// Package postgres holds the sqlc-backed adapters implementing the
// repository ports the domain contexts declare. This is the only layer that
// knows about internal/store, pgx or pgtype: a domain package importing any
// of them means a port was skipped.
package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/agent"
	"github.com/marcon0203/agentic-kit/internal/store"
)

// AgentRepository implements agent.Repository over the sqlc queries.
type AgentRepository struct {
	q store.Querier
}

func NewAgentRepository(q store.Querier) *AgentRepository { return &AgentRepository{q: q} }

var _ agent.Repository = (*AgentRepository)(nil)

// isUniqueViolation reports a Postgres 23505. Translating it here — rather
// than letting a *pgconn.PgError reach a service — is what keeps the driver
// out of the domain.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func toDomainAgent(row store.Agent) (agent.Agent, error) {
	var def agent.Definition
	if err := json.Unmarshal(row.Definition, &def); err != nil {
		return agent.Agent{}, err
	}
	return agent.Agent{
		ID:         row.ID,
		OwnerID:    row.OwnerUserID,
		Ref:        row.AgentRef,
		Version:    row.Version,
		Definition: def,
		Status:     agent.Status(row.Status),
		CreatedAt:  row.CreatedAt.Time,
	}, nil
}

func toDomainAgents(rows []store.Agent) ([]agent.Agent, error) {
	out := make([]agent.Agent, 0, len(rows))
	for _, row := range rows {
		a, err := toDomainAgent(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (r *AgentRepository) GetByID(ctx context.Context, ownerID, id int64) (agent.Agent, error) {
	row, err := r.q.GetAgentByID(ctx, store.GetAgentByIDParams{ID: id, OwnerUserID: ownerID})
	if err != nil {
		return agent.Agent{}, err
	}
	return toDomainAgent(row)
}

func (r *AgentRepository) ListLatestByOwner(ctx context.Context, ownerID int64, q domain.PageQuery) ([]agent.Agent, error) {
	rows, err := r.q.ListAgentsForOwnerLatestPage(ctx, store.ListAgentsForOwnerLatestPageParams{
		OwnerUserID: ownerID, AgentRef: q.After, Limit: int32(q.Limit),
	})
	if err != nil {
		return nil, err
	}
	return toDomainAgents(rows)
}

func (r *AgentRepository) ListVersions(ctx context.Context, ownerID int64, ref string) ([]agent.Agent, error) {
	rows, err := r.q.ListAgentVersionsForOwner(ctx, store.ListAgentVersionsForOwnerParams{
		OwnerUserID: ownerID, AgentRef: ref,
	})
	if err != nil {
		return nil, err
	}
	return toDomainAgents(rows)
}

func (r *AgentRepository) Create(ctx context.Context, a agent.Agent) (agent.Agent, error) {
	defBytes, err := json.Marshal(a.Definition)
	if err != nil {
		return agent.Agent{}, err
	}
	row, err := r.q.CreateAgent(ctx, store.CreateAgentParams{
		OwnerUserID: a.OwnerID, AgentRef: a.Ref, Version: a.Version,
		Definition: defBytes, DisplayMeta: []byte("{}"),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return agent.Agent{}, agent.ErrDuplicateVersion
		}
		return agent.Agent{}, err
	}
	return toDomainAgent(row)
}

// DeleteByRef maps any failure to ErrVersionLocked: the only thing that
// rejects this delete is migration 0006's immutable trigger, fired when a
// version is snapshot-locked by a subscriber.
func (r *AgentRepository) DeleteByRef(ctx context.Context, ownerID int64, ref string) error {
	if err := r.q.DeleteAgentsByRef(ctx, store.DeleteAgentsByRefParams{OwnerUserID: ownerID, AgentRef: ref}); err != nil {
		return agent.ErrVersionLocked
	}
	return nil
}

func (r *AgentRepository) CountActiveSubscribedVersions(ctx context.Context, ownerID int64, ref string) (int64, error) {
	return r.q.CountActiveSubscribedListingsForAgentRef(ctx, store.CountActiveSubscribedListingsForAgentRefParams{
		OwnerUserID: ownerID, AgentRef: ref,
	})
}

func (r *AgentRepository) FindReferencingBundles(ctx context.Context, ownerID int64, ref string) ([]agent.BundleRef, error) {
	rows, err := r.q.FindBundlesReferencingAgentRef(ctx, store.FindBundlesReferencingAgentRefParams{
		OwnerUserID: ownerID, Column2: ref,
	})
	if err != nil {
		return nil, err
	}
	out := make([]agent.BundleRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, agent.BundleRef{Ref: row.BundleRef, Version: row.Version})
	}
	return out, nil
}
