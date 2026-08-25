package resource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

type fakeOpenAPIParser struct {
	result resource.OpenAPIParseResult
	err    error
}

func (f *fakeOpenAPIParser) Parse(_ context.Context, _, _ string) (resource.OpenAPIParseResult, error) {
	return f.result, f.err
}

func TestImportOpenAPIPreview_RequiresURLOrContent(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{}).WithOpenAPIImport(&fakeOpenAPIParser{})

	_, err := svc.ImportOpenAPIPreview(context.Background(), "", "")
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestImportOpenAPIPreview_WithoutParserConfigured_ReturnsClearError(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{}) // no .WithOpenAPIImport

	_, err := svc.ImportOpenAPIPreview(context.Background(), "https://example.com/spec.json", "")
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestImportOpenAPIPreview_ReturnsParsedOperations(t *testing.T) {
	parser := &fakeOpenAPIParser{result: resource.OpenAPIParseResult{
		BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{
			{OperationID: "get_user", Method: "GET", Path: "/users/{id}", Summary: "Get a user"},
		},
	}}
	svc := newSvc(newFakeRepo(), stubProbe{}).WithOpenAPIImport(parser)

	result, err := svc.ImportOpenAPIPreview(context.Background(), "https://example.com/spec.json", "")
	if err != nil {
		t.Fatalf("ImportOpenAPIPreview: %v", err)
	}
	if len(result.Operations) != 1 || result.Operations[0].OperationID != "get_user" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestImportOpenAPIPreview_ParseFailureWrappedAsValidation(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{}).WithOpenAPIImport(&fakeOpenAPIParser{err: errors.New("bad spec")})

	_, err := svc.ImportOpenAPIPreview(context.Background(), "https://example.com/spec.json", "")
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestCreateComponentsBatch_CreatesOneToolPerOperation(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})

	created, err := svc.CreateComponentsBatch(context.Background(), 1, resource.BatchCreateComponentsCommand{
		BaseRef: "petstore", BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{
			{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}", Summary: "Get a pet"},
			{OperationID: "list_pets", Method: "GET", Path: "/pets", Summary: "List pets"},
		},
	})
	if err != nil {
		t.Fatalf("CreateComponentsBatch: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(created))
	}
	if created[0].Ref != "petstore__get_pet" || created[0].Kind != resource.KindTool {
		t.Fatalf("unexpected first resource: %+v", created[0])
	}
	if created[0].Config["tool_type"] != "openapi" || created[0].Config["method"] != "GET" {
		t.Fatalf("unexpected config: %+v", created[0].Config)
	}
	group0, _ := created[0].Config["import_group"].(string)
	group1, _ := created[1].Config["import_group"].(string)
	if group0 == "" || group0 != group1 {
		t.Fatalf("expected both resources to share one import_group, got %q and %q", group0, group1)
	}
}

func TestCreateComponentsBatch_CategoryAppliesToWholeBatch(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})

	created, err := svc.CreateComponentsBatch(context.Background(), 1, resource.BatchCreateComponentsCommand{
		BaseRef: "petstore", BaseURL: "https://api.example.com", Category: "business",
		Operations: []resource.OpenAPIOperation{
			{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}"},
			{OperationID: "list_pets", Method: "GET", Path: "/pets"},
		},
	})
	if err != nil {
		t.Fatalf("CreateComponentsBatch: %v", err)
	}
	for _, r := range created {
		if r.Config["category"] != "business" {
			t.Fatalf("expected every imported operation to carry the batch's category, got %+v", r.Config)
		}
	}
}

// No category given means the key is absent, not present-and-empty — the
// 组件广场's "未分类" filter matches on absence.
func TestCreateComponentsBatch_WithoutCategoryOmitsTheKey(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	created, err := svc.CreateComponentsBatch(context.Background(), 1, resource.BatchCreateComponentsCommand{
		BaseRef: "petstore", BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}"}},
	})
	if err != nil {
		t.Fatalf("CreateComponentsBatch: %v", err)
	}
	if _, ok := created[0].Config["category"]; ok {
		t.Fatalf("expected no category key at all, got %+v", created[0].Config)
	}
}

func TestCreateComponentsBatch_DuplicateOperationIDRejected(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.CreateComponentsBatch(context.Background(), 1, resource.BatchCreateComponentsCommand{
		BaseRef: "petstore", BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{
			{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}"},
			{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}"},
		},
	})
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}

func TestCreateComponentsBatch_ConflictingRefRejectsWholeBatch(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, stubProbe{})
	cmd := resource.BatchCreateComponentsCommand{
		BaseRef: "petstore", BaseURL: "https://api.example.com",
		Operations: []resource.OpenAPIOperation{{OperationID: "get_pet", Method: "GET", Path: "/pets/{id}"}},
	}
	if _, err := svc.CreateComponentsBatch(context.Background(), 1, cmd); err != nil {
		t.Fatalf("first batch: %v", err)
	}

	cmd.Operations = append(cmd.Operations, resource.OpenAPIOperation{OperationID: "list_pets", Method: "GET", Path: "/pets"})
	_, err := svc.CreateComponentsBatch(context.Background(), 1, cmd)
	assertErr(t, err, domain.KindConflict, domain.CodeResourceRefDuplicate)

	// The whole batch must have been rejected — list_pets must not have
	// been partially created alongside the conflicting get_pet.
	items, _ := repo.ListPage(context.Background(), resource.KindTool, 1, 0, 10)
	if len(items) != 1 {
		t.Fatalf("expected the conflicting batch to leave state unchanged, found %d tool resources", len(items))
	}
}

func TestCreateComponentsBatch_RequiresBaseRefAndOperations(t *testing.T) {
	svc := newSvc(newFakeRepo(), stubProbe{})

	_, err := svc.CreateComponentsBatch(context.Background(), 1, resource.BatchCreateComponentsCommand{})
	assertErr(t, err, domain.KindInvalid, domain.CodeValidationFailed)
}
