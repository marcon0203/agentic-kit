package adk

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestADKImportsConfinedToThisPackage enforces spec-10's explicit
// acceptance item: "所有 ADK 依赖仅出现在 internal/orchestrator/adk 包内
// （用 lint 或依赖检查工具约束）". ADK Go 2.0 is still pre-1.0-stable per
// spec-10's own risk note, so every import site outside this package is a
// future breaking-change liability the isolation boundary is meant to
// prevent.
func TestADKImportsConfinedToThisPackage(t *testing.T) {
	repoRoot := findRepoRoot(t)
	thisPkgDir := filepath.Join(repoRoot, "internal", "orchestrator", "adk")

	var offenders []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasPrefix(path, thisPkgDir+string(filepath.Separator)) {
			return nil // this package (and its tests) is exactly where ADK belongs
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // not our concern here; go build/vet already catch parse errors
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == "google.golang.org/adk" || strings.HasPrefix(importPath, "google.golang.org/adk/") {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("google.golang.org/adk imported outside internal/orchestrator/adk: %v", offenders)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}
