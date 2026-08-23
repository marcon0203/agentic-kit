package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marcon0203/agentic-kit/internal/store"
)

// ResourceRefChecker validates that an Agent DSL's capabilities.tools[] /
// capabilities.skills[] entries point at resources that (a) exist and
// (b) are enabled, per spec-06's acceptance checklist ("引用不存在或已禁用的
// 资源时返回 30002"). CheckCapabilities' returned FieldErrors distinguish
// not-found from disabled in their message even though the handler maps
// both to the same 30002 response.
type ResourceRefChecker struct {
	q store.Querier
}

func NewResourceRefChecker(q store.Querier) *ResourceRefChecker {
	return &ResourceRefChecker{q: q}
}

// refCheckResult is "found and its status", or "not found at all".
type refStatus struct {
	found   bool
	enabled bool
}

// toolRefStatus looks up ref among tools/mcp_servers/knowledge_bases — the
// three kinds an Agent DSL's capabilities.tools[] entry can name (see
// resource_kind.go's FindAgentsReferencing* comments for why knowledge
// bases and MCP servers surface there instead of a separate array).
func (c *ResourceRefChecker) toolRefStatus(ctx context.Context, ownerID int64, ref string) (refStatus, error) {
	if status, found, err := getLatestStatus(func() (int16, error) {
		return c.q.GetToolLatestStatusByRef(ctx, store.GetToolLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
	}); err != nil {
		return refStatus{}, err
	} else if found {
		return refStatus{found: true, enabled: status == 1}, nil
	}

	if status, found, err := getLatestStatus(func() (int16, error) {
		return c.q.GetMCPServerLatestStatusByRef(ctx, store.GetMCPServerLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
	}); err != nil {
		return refStatus{}, err
	} else if found {
		return refStatus{found: true, enabled: status == 1}, nil
	}

	if status, found, err := getLatestStatus(func() (int16, error) {
		return c.q.GetKnowledgeBaseLatestStatusByRef(ctx, store.GetKnowledgeBaseLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
	}); err != nil {
		return refStatus{}, err
	} else if found {
		return refStatus{found: true, enabled: status == 1}, nil
	}

	return refStatus{found: false}, nil
}

func (c *ResourceRefChecker) skillRefStatus(ctx context.Context, ownerID int64, ref string) (refStatus, error) {
	status, found, err := getLatestStatus(func() (int16, error) {
		return c.q.GetSkillLatestStatusByRef(ctx, store.GetSkillLatestStatusByRefParams{OwnerUserID: ownerID, Ref: ref})
	})
	if err != nil {
		return refStatus{}, err
	}
	return refStatus{found: found, enabled: status == 1}, nil
}

func getLatestStatus(query func() (int16, error)) (int16, bool, error) {
	status, err := query()
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return status, true, nil
}

// CheckCapabilities validates every entry in tools/skills against the
// owner's resources, returning one FieldError per bad reference — not
// found gets ErrResourceNotFound's wording, disabled gets
// ErrResourceDisabled's, so both land in the same `details` array a
// caller can render directly.
func (c *ResourceRefChecker) CheckCapabilities(ctx context.Context, ownerID int64, tools, skills []string) ([]FieldError, error) {
	var errs []FieldError

	for i, ref := range tools {
		status, err := c.toolRefStatus(ctx, ownerID, ref)
		if err != nil {
			return nil, err
		}
		if !status.found {
			errs = append(errs, FieldError{Field: fmt.Sprintf("capabilities.tools[%d]", i), Message: fmt.Sprintf("resource %q does not exist", ref)})
		} else if !status.enabled {
			errs = append(errs, FieldError{Field: fmt.Sprintf("capabilities.tools[%d]", i), Message: fmt.Sprintf("resource %q is disabled", ref)})
		}
	}

	for i, ref := range skills {
		status, err := c.skillRefStatus(ctx, ownerID, ref)
		if err != nil {
			return nil, err
		}
		if !status.found {
			errs = append(errs, FieldError{Field: fmt.Sprintf("capabilities.skills[%d]", i), Message: fmt.Sprintf("skill %q does not exist", ref)})
		} else if !status.enabled {
			errs = append(errs, FieldError{Field: fmt.Sprintf("capabilities.skills[%d]", i), Message: fmt.Sprintf("skill %q is disabled", ref)})
		}
	}

	return errs, nil
}
