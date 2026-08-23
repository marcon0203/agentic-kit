// Package depclosure implements the recursive dependency-closure validation
// spec-08-marketplace calls "本任务最容易写错的地方": before a resource can
// be published, every resource it transitively depends on
// (Bundle -> Agent -> Skill/MCP) must already be published, checked by
// exact version where the DSL pins one. The traversal is an explicit stack
// (not recursion — dependency depth has no hard limit) and always finishes:
// a repeated node on the current path is a cycle, reported once and not
// re-expanded, rather than looped until timeout.
package depclosure

import (
	"fmt"
	"strings"
)

// NodeKey identifies one resource version in the dependency graph.
// Version is empty for Skill/MCP dependency edges, which the Agent DSL
// doesn't pin to a specific version (only Bundle.agents[].version does).
type NodeKey struct {
	Kind    string
	Ref     string
	Version string
}

func (k NodeKey) String() string {
	if k.Version == "" {
		return k.Kind + "/" + k.Ref
	}
	return fmt.Sprintf("%s/%s@%s", k.Kind, k.Ref, k.Version)
}

// DependencyRef is one edge out of a node: Field is the DSL path it came
// from, used to build the `details[].field` the caller returns.
type DependencyRef struct {
	Node  NodeKey
	Field string
}

// Resolver supplies graph data on demand, one node at a time, so the
// traversal itself has no database dependency and can be unit tested with
// an in-memory fake (see closure_test.go) independent of Postgres.
type Resolver interface {
	// Exists reports whether node is a real private resource at all.
	Exists(node NodeKey) (bool, error)
	// IsPublished reports whether node already has a satisfying listing.
	// Never called for the root (it's the thing being published right now).
	IsPublished(node NodeKey) (bool, error)
	// Dependencies returns node's direct dependencies. Only called for
	// expandable kinds (Bundle, Agent) — leaf kinds (Skill, MCP) return nil.
	Dependencies(node NodeKey) ([]DependencyRef, error)
}

// Issue is one problem found during the walk, ready to become a FieldError.
type Issue struct {
	Field  string
	Reason string
}

// ErrCycle marks an Issue produced by cycle detection, so a caller can map
// it to 70008 instead of 70001 (spec-08: "循环依赖返回 70008").
const ErrCycle = "cycle"

// TaggedIssue carries Issue plus a Kind discriminator for error-code mapping.
type TaggedIssue struct {
	Issue
	Kind string // "" for a normal unpublished-dependency issue, ErrCycle for a cycle
}

// Validate walks the transitive dependency closure of root and returns
// every problem found — every unpublished dependency and every cycle, not
// just the first (spec-08: "一次返回全部问题"). An empty result means root
// is safe to publish.
func Validate(root NodeKey, resolver Resolver) ([]TaggedIssue, error) {
	type frame struct {
		node  NodeKey
		path  []NodeKey // path from root to (not including) node
		field string
	}

	var issues []TaggedIssue
	stack := []frame{{node: root, path: nil, field: rootFieldLabel(root)}}

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if pathContains(f.path, f.node) {
			issues = append(issues, TaggedIssue{
				Kind: ErrCycle,
				Issue: Issue{
					Field:  f.field,
					Reason: fmt.Sprintf("循环依赖：%s", formatCyclePath(f.path, f.node)),
				},
			})
			continue
		}

		isRoot := f.node == root && len(f.path) == 0

		if !isRoot {
			exists, err := resolver.Exists(f.node)
			if err != nil {
				return nil, err
			}
			if !exists {
				issues = append(issues, TaggedIssue{Issue: Issue{
					Field:  f.field,
					Reason: fmt.Sprintf("依赖路径 %s 不存在", formatPath(f.path, f.node)),
				}})
				continue // can't have dependencies of a resource that doesn't exist
			}

			published, err := resolver.IsPublished(f.node)
			if err != nil {
				return nil, err
			}
			if !published {
				issues = append(issues, TaggedIssue{Issue: Issue{
					Field:  f.field,
					Reason: fmt.Sprintf("依赖路径 %s 未发布", formatPath(f.path, f.node)),
				}})
				// Still expand below: a broken branch's own deeper issues
				// should surface in the same pass, not require a second
				// round-trip after this one is fixed.
			}
		}

		deps, err := resolver.Dependencies(f.node)
		if err != nil {
			return nil, err
		}
		childPath := append(append([]NodeKey{}, f.path...), f.node)
		for _, d := range deps {
			stack = append(stack, frame{node: d.Node, path: childPath, field: d.Field})
		}
	}

	return issues, nil
}

func rootFieldLabel(root NodeKey) string {
	return root.Kind
}

func pathContains(path []NodeKey, node NodeKey) bool {
	for _, p := range path {
		if p == node {
			return true
		}
	}
	return false
}

func formatPath(path []NodeKey, node NodeKey) string {
	parts := make([]string, 0, len(path)+1)
	for _, p := range path {
		parts = append(parts, p.String())
	}
	parts = append(parts, node.String())
	return strings.Join(parts, " → ")
}

func formatCyclePath(path []NodeKey, repeated NodeKey) string {
	return formatPath(path, repeated) + " → " + repeated.String()
}
