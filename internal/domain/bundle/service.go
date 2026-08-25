package bundle

import (
	"context"
	"errors"
	"fmt"

	"github.com/marcon0203/agentic-kit/internal/bundlegraph"
	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Port sentinels — adapters translate storage signals into these.
var (
	ErrDuplicateVersion = errors.New("bundle: ref/version already exists")
	ErrVersionLocked    = errors.New("bundle: version is locked by a subscription")
)

// Repository is this context's persistence port.
type Repository interface {
	ListLatestByOwner(ctx context.Context, ownerID int64, q domain.PageQuery) ([]Bundle, error)
	Create(ctx context.Context, b Bundle) (Bundle, error)
	DeleteByRef(ctx context.Context, ownerID int64, ref string) error
	CountActiveSubscribedVersions(ctx context.Context, ownerID int64, ref string) (int64, error)
}

// AgentHandoffs resolves an Agent's advisory handoff declaration. A ref that
// doesn't resolve yields a zero Handoff rather than an error: whether the
// Agent exists is checked elsewhere, and it is not this check's job to fail
// the save over it.
type AgentHandoffs interface {
	Lookup(ctx context.Context, ownerID int64, agentRef string) (Handoff, error)
}

// DefinitionValidator checks a definition against the Bundle DSL schema.
type DefinitionValidator interface {
	Validate(def map[string]any) ([]domain.FieldError, error)
}

// Service is the Bundle application service.
type Service struct {
	repo      Repository
	handoffs  AgentHandoffs
	validator DefinitionValidator
}

func NewService(repo Repository, handoffs AgentHandoffs, validator DefinitionValidator) *Service {
	return &Service{repo: repo, handoffs: handoffs, validator: validator}
}

// List returns one entry per bundle_ref (its latest version), paginated.
func (s *Service) List(ctx context.Context, ownerID int64, q domain.PageQuery) (domain.Page[Bundle], error) {
	q = q.Normalize()
	rows, err := s.repo.ListLatestByOwner(ctx, ownerID, domain.PageQuery{Limit: q.Limit + 1, After: q.After})
	if err != nil {
		return domain.Page[Bundle]{}, domain.Internal(err)
	}
	return domain.NewPage(rows, q.Limit, func(b Bundle) string { return b.Ref }), nil
}

// Create validates and saves a Bundle definition.
//
// Three gates in order, each failing differently:
//  1. schema (40001-equivalent for Bundles: 40002) — blocking, 400
//  2. graph statics (40003) — blocking, 422: a node that can never fire
//     means the Bundle simply cannot run
//  3. reachability + handoff drift — never blocking, returned as warnings on
//     the created Bundle (spec-07: "允许暂时脱节但要可见")
//
// Gate 2 and the reachability half of gate 3 are specific to RunTypeGraph's
// arbitrary edge topology — a flow has no branch that can fail to fire (it
// is exactly one path through agents[] in order) and a single Bundle has no
// edges at all, so both skip straight to the handoff check and the save.
func (s *Service) Create(ctx context.Context, ownerID int64, def Definition) (CreateResult, error) {
	if def == nil {
		return CreateResult{}, domain.Invalid(domain.CodeValidationFailed, "definition is required")
	}

	schemaErrs, err := s.validator.Validate(def)
	if err != nil {
		return CreateResult{}, domain.Internal(err)
	}
	if len(schemaErrs) > 0 {
		return CreateResult{}, domain.Invalid(domain.CodeBundleSchemaInvalid, "Bundle 定义不符合 Schema").WithDetails(schemaErrs...)
	}

	var warnings []Warning
	switch def.Type() {
	case RunTypeFlow, RunTypeSingle:
		handoffWarnings, err := s.checkHandoffDrift(ctx, ownerID, def, consecutiveEdges(def.Agents()))
		if err != nil {
			return CreateResult{}, domain.Internal(err)
		}
		warnings = handoffWarnings
	default:
		graph, err := bundlegraph.ParseGraph(def)
		if err != nil {
			return CreateResult{}, domain.Invalid(domain.CodeBundleSchemaInvalid, "malformed orchestration block")
		}

		result := bundlegraph.Validate(graph)
		if !result.Valid() {
			return CreateResult{}, domain.Unprocessable(domain.CodeBundleGraphInvalid, "Bundle 编排图存在无法触发的节点").
				WithDetails(issuesToFieldErrors(result.Errors)...)
		}

		warnings = issuesToWarnings(result.Warnings)
		handoffWarnings, err := s.checkHandoffDrift(ctx, ownerID, def, graph.Edges)
		if err != nil {
			return CreateResult{}, domain.Internal(err)
		}
		warnings = append(warnings, handoffWarnings...)
	}

	created, err := s.repo.Create(ctx, Bundle{
		OwnerID: ownerID, Ref: def.Ref(), Version: def.Version(), Definition: def,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateVersion) {
			return CreateResult{}, domain.Conflict(domain.CodeResourceRefDuplicate, "this bundle_ref/version already exists")
		}
		return CreateResult{}, domain.Internal(err)
	}
	return CreateResult{Bundle: created, Warnings: warnings}, nil
}

// consecutiveEdges gives a RunTypeFlow/RunTypeSingle Bundle's implicit
// linear order (agents[i] -> agents[i+1], as declared) the same shape
// checkHandoffDrift already knows how to read off a graph's real edges —
// a flow's execution order needs no separate representation, since it's
// exactly the array order agents[] already carries.
func consecutiveEdges(bindings []AgentBinding) []bundlegraph.Edge {
	if len(bindings) < 2 {
		return nil
	}
	edges := make([]bundlegraph.Edge, 0, len(bindings)-1)
	for i := 0; i < len(bindings)-1; i++ {
		edges = append(edges, bundlegraph.Edge{Index: i, From: bindings[i].Node, To: []string{bindings[i+1].Node}})
	}
	return edges
}

// Delete removes every version of a ref. A subscribed-and-active version can
// never be deleted — snapshot isolation means the subscriber's version must
// keep working; the author can only stop distribution.
func (s *Service) Delete(ctx context.Context, ownerID int64, ref string) error {
	subscribed, err := s.repo.CountActiveSubscribedVersions(ctx, ownerID, ref)
	if err != nil {
		return domain.Internal(err)
	}
	if subscribed > 0 {
		return domain.Conflict(domain.CodeSubscribedVersionLocked, "该 Bundle 的某个版本已被订阅，无法删除，只能停止分发")
	}

	if err := s.repo.DeleteByRef(ctx, ownerID, ref); err != nil {
		if errors.Is(err, ErrVersionLocked) {
			return domain.Conflict(domain.CodeSubscribedVersionLocked, "该版本已被订阅，不可删除（快照隔离）")
		}
		return domain.Internal(err)
	}
	return nil
}

// checkHandoffDrift implements spec-07 point 5: an Agent's handoff
// declarations and the Bundle's actual edges can disagree, because the two
// DSLs may be maintained by different people. Reporting the drift is useful;
// blocking on it would force one team to wait for the other.
func (s *Service) checkHandoffDrift(ctx context.Context, ownerID int64, def Definition, edges []bundlegraph.Edge) ([]Warning, error) {
	nodeToRef := map[string]string{}
	for _, binding := range def.Agents() {
		nodeToRef[binding.Node] = binding.Ref
	}

	// One lookup per distinct agent ref, not per node: the same Agent can be
	// bound to several nodes.
	handoffs := map[string]Handoff{}
	for _, ref := range nodeToRef {
		if ref == "" {
			continue
		}
		if _, done := handoffs[ref]; done {
			continue
		}
		h, err := s.handoffs.Lookup(ctx, ownerID, ref)
		if err != nil {
			return nil, err
		}
		handoffs[ref] = h
	}

	var warnings []Warning
	for _, edge := range edges {
		fromRef := nodeToRef[edge.From]
		for _, to := range edge.To {
			if to == bundlegraph.EndNode {
				continue
			}
			toRef := nodeToRef[to]
			field := fmt.Sprintf("orchestration.edges[%d]", edge.Index)

			// An empty declaration means "unspecified", not "accepts
			// nothing" — only a non-empty list that omits the peer is drift.
			if h, ok := handoffs[toRef]; ok && len(h.AcceptsInputFrom) > 0 && !h.accepts(fromRef) {
				warnings = append(warnings, Warning{
					Field:  field,
					Reason: fmt.Sprintf("Agent %q 的 handoff.accepts_input_from 未声明接受来自 %q 的输入，与实际编排边不一致", toRef, fromRef),
				})
			}
			if h, ok := handoffs[fromRef]; ok && len(h.ProducesOutputTo) > 0 && !h.producesTo(toRef) {
				warnings = append(warnings, Warning{
					Field:  field,
					Reason: fmt.Sprintf("Agent %q 的 handoff.produces_output_to 未声明输出给 %q，与实际编排边不一致", fromRef, toRef),
				})
			}
		}
	}
	return warnings, nil
}

func issuesToFieldErrors(issues []bundlegraph.Issue) []domain.FieldError {
	out := make([]domain.FieldError, len(issues))
	for i, iss := range issues {
		out[i] = domain.FieldError{Field: iss.Field, Reason: iss.Message}
	}
	return out
}

func issuesToWarnings(issues []bundlegraph.Issue) []Warning {
	out := make([]Warning, len(issues))
	for i, iss := range issues {
		out[i] = Warning{Field: iss.Field, Reason: iss.Message}
	}
	return out
}
