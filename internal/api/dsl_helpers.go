package api

import "github.com/marcon0203/agentic-kit/internal/dslschema"

// Helpers for the modules still awaiting migration onto a domain service
// (bundle, marketplace). Each disappears when its last caller moves: the
// Agent context, for example, now reads its DSL through
// agent.Definition's accessors instead of deepGet, and reports schema
// failures as domain.FieldError instead of converting here.

func toAPIFieldErrors(errs []dslschema.FieldError) []FieldError {
	out := make([]FieldError, len(errs))
	for i, e := range errs {
		out[i] = FieldError{Field: e.Field, Reason: e.Message}
	}
	return out
}

func deepGet(m map[string]any, path ...string) any {
	var cur any = m
	for _, key := range path {
		asMap, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = asMap[key]
	}
	return cur
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
