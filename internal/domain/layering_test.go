package domain_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenInDomain lists the import prefixes that would mean infrastructure
// has leaked back into a domain package. The whole point of the port/adapter
// split is that business rules are expressed without them; without a guard
// this erodes one convenient import at a time.
var forbiddenInDomain = []string{
	"net/http",              // transport
	"github.com/go-chi/chi", // transport
	"github.com/jackc/pgx",  // driver (covers pgconn/pgtype paths)
	"github.com/marcon0203/agentic-kit/internal/store",   // sqlc-generated persistence
	"github.com/marcon0203/agentic-kit/internal/api",     // transport
	"github.com/marcon0203/agentic-kit/internal/adapter", // adapters depend on domain, never the reverse
}

// TestDomainDoesNotImportInfrastructure enforces the dependency rule stated
// in internal/domain's package doc: dependencies point inward.
//
// Test files are checked too. A domain test that reaches for pgx or a store
// type is a sign the fake should have been written against the port instead
// — which is the property that keeps these tests fast and Postgres-free.
func TestDomainDoesNotImportInfrastructure(t *testing.T) {
	domainRoot := domainDir(t)

	type violation struct{ file, imp string }
	var found []violation

	err := filepath.WalkDir(domainRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return nil // go build/vet already report parse errors
		}
		rel, _ := filepath.Rel(domainRoot, path)
		for _, spec := range f.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			for _, bad := range forbiddenInDomain {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					found = append(found, violation{file: rel, imp: imp})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk domain: %v", err)
	}

	for _, v := range found {
		t.Errorf("internal/domain/%s imports %q — move it behind a port implemented in internal/adapter", v.file, v.imp)
	}
}

func domainDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd() // the package under test: internal/domain
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return dir
}
