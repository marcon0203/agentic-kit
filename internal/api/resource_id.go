package api

import (
	"fmt"
	"strconv"
	"strings"
)

// External resource IDs are "{kind}_{row id}" (e.g. "tool_1", "mcp_42") —
// with four independent tables (spec-05-resource-center.md "分表设计"),
// each with its own identity sequence, a bare numeric ID can't say which
// table it came from. The prefix is what lets /resources/{id} route to the
// right one without a lookup across all four tables first.
func encodeResourceID(kind ResourceKind, id int64) string {
	return fmt.Sprintf("%s_%d", kind, id)
}

func decodeResourceID(external string) (ResourceKind, int64, error) {
	idx := strings.LastIndex(external, "_")
	if idx < 0 {
		return "", 0, fmt.Errorf("malformed resource id %q", external)
	}
	kind := ResourceKind(external[:idx])
	id, err := strconv.ParseInt(external[idx+1:], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("malformed resource id %q: %w", external, err)
	}
	if !isValidResourceKind(kind) {
		return "", 0, fmt.Errorf("unknown resource kind in id %q", external)
	}
	return kind, id, nil
}

func isValidResourceKind(kind ResourceKind) bool {
	for _, k := range AllResourceKinds {
		if k == kind {
			return true
		}
	}
	return false
}
