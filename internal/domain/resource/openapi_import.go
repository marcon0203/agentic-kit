package resource

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// OpenAPIOperation is one operation out of a parsed OpenAPI spec — what the
// preview step shows and what the batch-create step turns into a Resource.
// OperationID is already ref-safe (the parser sanitizes it), not necessarily
// the spec's own `operationId` verbatim.
type OpenAPIOperation struct {
	OperationID string
	Method      string
	Path        string
	Summary     string
}

// OpenAPIParseResult is what parsing one spec yields: every operation it
// declares, plus the base URL requests actually go to (the spec's
// `servers[0].url`, or empty if the spec has none — the caller must then
// supply one).
type OpenAPIParseResult struct {
	BaseURL    string
	Operations []OpenAPIOperation
}

// OpenAPIParser parses an OpenAPI spec (fetched from specURL, or given
// directly as specContent — exactly one of the two is set) into its
// operations, without touching the database. Implementations live outside
// this package (internal/adapter/openapi).
type OpenAPIParser interface {
	Parse(ctx context.Context, specURL, specContent string) (OpenAPIParseResult, error)
}

// WithOpenAPIImport enables ImportOpenAPIPreview/CreateComponentsBatch — a
// separate opt-in step for the same reason WithSkillUploads is: the OpenAPI
// import surface is independent of the rest of resource.Service's more
// commonly-configured dependencies.
func (s *Service) WithOpenAPIImport(parser OpenAPIParser) *Service {
	s.openAPIParser = parser
	return s
}

// ImportOpenAPIPreview parses a spec and returns its operations — nothing is
// persisted. This is the "预览" step (spec-05a §4): a person picks which
// operations to actually turn into 组件 in a second, separate call.
func (s *Service) ImportOpenAPIPreview(ctx context.Context, specURL, specContent string) (OpenAPIParseResult, error) {
	if s.openAPIParser == nil {
		return OpenAPIParseResult{}, domain.Invalid(domain.CodeValidationFailed, "OpenAPI import is not configured on this deployment")
	}
	if specURL == "" && specContent == "" {
		return OpenAPIParseResult{}, domain.Invalid(domain.CodeValidationFailed, "invalid OpenAPI import request").
			WithDetails(domain.FieldError{Field: "spec_url", Reason: "either spec_url or spec_content is required"})
	}
	result, err := s.openAPIParser.Parse(ctx, specURL, specContent)
	if err != nil {
		return OpenAPIParseResult{}, domain.Invalid(domain.CodeValidationFailed, fmt.Sprintf("failed to parse OpenAPI spec: %v", err))
	}
	return result, nil
}

// BatchCreateComponentsCommand is the "勾选 → 批量创建" step: BaseURL
// overrides whatever the spec's own servers[0].url was (a person may need to
// point at a different environment than the spec advertises), and Operations
// is the subset the preview's caller chose to actually register.
type BatchCreateComponentsCommand struct {
	BaseRef    string
	BaseURL    string
	Operations []OpenAPIOperation
}

// CreateComponentsBatch turns each selected operation into its own "tool"
// resource — `{base_ref}__{operation_id}`, `config.tool_type = "openapi"` —
// in one all-or-nothing transaction (Repository.CreateBatch), so a duplicate
// ref partway through a batch doesn't leave half the import registered.
func (s *Service) CreateComponentsBatch(ctx context.Context, ownerID int64, cmd BatchCreateComponentsCommand) ([]Resource, error) {
	var errs []domain.FieldError
	if !refPattern.MatchString(cmd.BaseRef) {
		errs = append(errs, domain.FieldError{Field: "base_ref", Reason: "must match ^[a-z][a-z0-9_-]*$"})
	}
	if cmd.BaseURL == "" {
		errs = append(errs, domain.FieldError{Field: "base_url", Reason: "required"})
	}
	if len(cmd.Operations) == 0 {
		errs = append(errs, domain.FieldError{Field: "operations", Reason: "at least one operation must be selected"})
	}
	if len(errs) > 0 {
		return nil, domain.Invalid(domain.CodeValidationFailed, "invalid batch import request").WithDetails(errs...)
	}

	importGroup, err := randomImportGroup()
	if err != nil {
		return nil, domain.Internal(err)
	}

	resources := make([]Resource, 0, len(cmd.Operations))
	seenRefs := make(map[string]bool, len(cmd.Operations))
	for _, op := range cmd.Operations {
		ref := cmd.BaseRef + "__" + op.OperationID
		if !refPattern.MatchString(ref) {
			return nil, domain.Invalid(domain.CodeValidationFailed, "invalid batch import request").
				WithDetails(domain.FieldError{Field: "operations", Reason: fmt.Sprintf("operation_id %q produces an invalid ref", op.OperationID)})
		}
		if seenRefs[ref] {
			return nil, domain.Invalid(domain.CodeValidationFailed, "invalid batch import request").
				WithDetails(domain.FieldError{Field: "operations", Reason: fmt.Sprintf("duplicate operation_id %q", op.OperationID)})
		}
		seenRefs[ref] = true

		resources = append(resources, Resource{
			OwnerID: ownerID, Kind: KindTool, Ref: ref, Version: "1.0",
			DisplayName: op.Summary,
			Config: Config{
				// "tool"/"openapi" mirror internal/orchestrator/adk's
				// ComponentTypeTool/ToolTypeOpenAPI constants — duplicated
				// as literals rather than imported, since this domain
				// package doesn't depend on the ADK orchestration layer.
				"component_type": "tool",
				"tool_type":      "openapi",
				"method":         op.Method,
				"path":           op.Path,
				"base_url":       cmd.BaseURL,
				"import_group":   importGroup,
			},
			Status: StatusEnabled,
		})
	}

	created, err := s.repo.CreateBatch(ctx, resources)
	if err != nil {
		if err == ErrDuplicate {
			return nil, domain.Conflict(domain.CodeResourceRefDuplicate, "a resource with one of these refs already exists")
		}
		return nil, domain.Internal(err)
	}
	for i := range created {
		created[i].Config = created[i].Config.Redact()
	}
	return created, nil
}

func randomImportGroup() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
