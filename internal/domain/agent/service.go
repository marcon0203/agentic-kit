package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Service is the Agent context's application service: every business rule
// about creating, listing and deleting Agents lives here, expressed against
// ports. It returns *domain.Error, so a caller never has to decide what a
// failure "means" — only how to render it.
type Service struct {
	repo      Repository
	catalog   ResourceCatalog
	validator DefinitionValidator
}

func NewService(repo Repository, catalog ResourceCatalog, validator DefinitionValidator) *Service {
	return &Service{repo: repo, catalog: catalog, validator: validator}
}

// List returns one entry per agent_ref (its latest version), paginated.
func (s *Service) List(ctx context.Context, ownerID int64, q domain.PageQuery) (domain.Page[Agent], error) {
	q = q.Normalize()

	// Over-fetch by one: presence of the extra row is what proves another
	// page exists, without a second COUNT round-trip.
	rows, err := s.repo.ListLatestByOwner(ctx, ownerID, domain.PageQuery{Limit: q.Limit + 1, After: q.After})
	if err != nil {
		return domain.Page[Agent]{}, domain.Internal(err)
	}
	return domain.NewPage(rows, q.Limit, func(a Agent) string { return a.Ref }), nil
}

// ListVersions returns every version of one ref, newest first.
func (s *Service) ListVersions(ctx context.Context, ownerID int64, ref string) ([]Agent, error) {
	rows, err := s.repo.ListVersions(ctx, ownerID, ref)
	if err != nil {
		return nil, domain.Internal(err)
	}
	if len(rows) == 0 {
		return nil, domain.NotFound(domain.CodeResourceNotFound, "agent not found")
	}
	return rows, nil
}

// Create validates a definition and persists it as a new Agent version.
//
// Two gates, in order, because they fail for different reasons and the
// second is meaningless if the first fails: the definition must satisfy the
// Agent DSL schema (40001), and every capability it references must exist
// and be enabled (30002, spec-06).
func (s *Service) Create(ctx context.Context, ownerID int64, def Definition) (Agent, error) {
	if def == nil {
		return Agent{}, domain.Invalid(domain.CodeValidationFailed, "definition is required")
	}

	schemaErrs, err := s.validator.Validate(def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(schemaErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 定义不符合 Schema").WithDetails(schemaErrs...)
	}

	refErrs, err := s.checkCapabilities(ctx, ownerID, def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(refErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeResourceDisabled, "Agent 引用了不存在或已禁用的资源").WithDetails(refErrs...)
	}

	created, err := s.repo.Create(ctx, Agent{
		OwnerID:    ownerID,
		Ref:        def.Ref(),
		Version:    def.Version(),
		Definition: def,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateVersion) {
			return Agent{}, domain.Conflict(domain.CodeResourceRefDuplicate, "this agent_ref/version already exists")
		}
		return Agent{}, domain.Internal(err)
	}
	return created, nil
}

// Update edits an Agent by creating a new version from its latest existing
// version. The path ref must match definition.agent; the version is bumped
// automatically so callers don't have to know the existing version number.
func (s *Service) Update(ctx context.Context, ownerID int64, ref string, def Definition) (Agent, error) {
	if def == nil {
		return Agent{}, domain.Invalid(domain.CodeValidationFailed, "definition is required")
	}
	if def.Ref() != ref {
		return Agent{}, domain.Invalid(domain.CodeValidationFailed, "definition.agent must match path ref")
	}

	schemaErrs, err := s.validator.Validate(def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(schemaErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeAgentSchemaInvalid, "Agent 定义不符合 Schema").WithDetails(schemaErrs...)
	}

	refErrs, err := s.checkCapabilities(ctx, ownerID, def)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(refErrs) > 0 {
		return Agent{}, domain.Invalid(domain.CodeResourceDisabled, "Agent 引用了不存在或已禁用的资源").WithDetails(refErrs...)
	}

	versions, err := s.repo.ListVersions(ctx, ownerID, ref)
	if err != nil {
		return Agent{}, domain.Internal(err)
	}
	if len(versions) == 0 {
		return Agent{}, domain.NotFound(domain.CodeResourceNotFound, "agent not found")
	}
	latest := versions[0]

	def["version"] = bumpVersion(latest.Version)

	created, err := s.repo.Create(ctx, Agent{
		OwnerID:    ownerID,
		Ref:        def.Ref(),
		Version:    def.Version(),
		Definition: def,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateVersion) {
			return Agent{}, domain.Conflict(domain.CodeResourceRefDuplicate, "this agent_ref/version already exists")
		}
		return Agent{}, domain.Internal(err)
	}
	return created, nil
}

// bumpVersion increments the last numeric segment of a dotted version string.
// Non-numeric or malformed versions are returned unchanged.
func bumpVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return v
	}
	last := parts[len(parts)-1]
	n, err := strconv.Atoi(last)
	if err != nil {
		return v
	}
	parts[len(parts)-1] = strconv.Itoa(n + 1)
	return strings.Join(parts, ".")
}

// Delete removes every version of a ref.
//
// Two occupancy rules, checked before the delete because each needs to
// explain itself differently: a subscribed-and-active version can never be
// deleted (70005 — snapshot isolation means a subscriber's version must
// keep working), and a version still referenced by one of the owner's
// Bundles is refused with the referencing Bundles listed (40004). The DB's
// immutable trigger is the last line of defence if either check races a
// concurrent subscribe.
func (s *Service) Delete(ctx context.Context, ownerID int64, ref string) error {
	subscribed, err := s.repo.CountActiveSubscribedVersions(ctx, ownerID, ref)
	if err != nil {
		return domain.Internal(err)
	}
	if subscribed > 0 {
		return domain.Conflict(domain.CodeSubscribedVersionLocked, "该 Agent 的某个版本已被订阅，无法删除，只能停止分发")
	}

	bundles, err := s.repo.FindReferencingBundles(ctx, ownerID, ref)
	if err != nil {
		return domain.Internal(err)
	}
	if len(bundles) > 0 {
		details := make([]domain.FieldError, 0, len(bundles))
		for _, b := range bundles {
			details = append(details, domain.FieldError{
				Field:  "referenced_by",
				Reason: fmt.Sprintf("Bundle %s v%s 正在引用", b.Ref, b.Version),
			})
		}
		return domain.Conflict(domain.CodeAgentVersionNotFound, "该 Agent 正被其他 Bundle 引用，无法删除").WithDetails(details...)
	}

	if err := s.repo.DeleteByRef(ctx, ownerID, ref); err != nil {
		if errors.Is(err, ErrVersionLocked) {
			return domain.Conflict(domain.CodeSubscribedVersionLocked, "该版本已被订阅，不可删除（快照隔离）")
		}
		return domain.Internal(err)
	}
	return nil
}

// checkCapabilities resolves every tools[]/skills[] entry, returning one
// field error per bad reference. Not-found and disabled are reported with
// different wording even though both map to the same business code — the
// fix differs, so the message has to.
func (s *Service) checkCapabilities(ctx context.Context, ownerID int64, def Definition) ([]domain.FieldError, error) {
	var errs []domain.FieldError

	check := func(kind, noun string, refs []string, lookup func(context.Context, int64, string) (RefStatus, error)) error {
		for i, ref := range refs {
			status, err := lookup(ctx, ownerID, ref)
			if err != nil {
				return err
			}
			field := fmt.Sprintf("capabilities.%s[%d]", kind, i)
			switch {
			case !status.Found:
				errs = append(errs, domain.FieldError{Field: field, Reason: fmt.Sprintf("%s %q does not exist", noun, ref)})
			case !status.Enabled:
				errs = append(errs, domain.FieldError{Field: field, Reason: fmt.Sprintf("%s %q is disabled", noun, ref)})
			}
		}
		return nil
	}

	if err := check("tools", "resource", def.Tools(), s.catalog.ToolStatus); err != nil {
		return nil, err
	}
	if err := check("skills", "skill", def.Skills(), s.catalog.SkillStatus); err != nil {
		return nil, err
	}
	return errs, nil
}
