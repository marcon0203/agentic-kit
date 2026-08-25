// Package dslschema validates Agent/Bundle DSL documents against the JSON
// Schema files in schemas/ — the same files the frontend validates against
// with ajv (schemas/README.md), so a definition can never pass one side and
// fail the other.
package dslschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/marcon0203/agentic-kit/schemas"
)

// Validator wraps one compiled JSON Schema.
type Validator struct {
	schema *jsonschema.Schema
}

// NewAgentValidator compiles schemas/agent.schema.json.
func NewAgentValidator() (*Validator, error) {
	return newValidator("agent.schema.json", schemas.AgentSchemaJSON)
}

// NewBundleValidator compiles schemas/bundle.schema.json.
func NewBundleValidator() (*Validator, error) {
	return newValidator("bundle.schema.json", schemas.BundleSchemaJSON)
}

// NewPluginValidator compiles schemas/plugin.schema.json (spec-20).
func NewPluginValidator() (*Validator, error) {
	return newValidator("plugin.schema.json", schemas.PluginSchemaJSON)
}

func newValidator(name string, raw []byte) (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(name, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("dslschema: add resource %s: %w", name, err)
	}
	schema, err := compiler.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("dslschema: compile %s: %w", name, err)
	}
	return &Validator{schema: schema}, nil
}

// FieldError mirrors api.FieldError's shape without importing the api
// package (which would create an import cycle: api -> dslschema -> api).
type FieldError struct {
	Field   string
	Message string
}

// Validate checks definition (already unmarshaled into a generic
// map[string]any, as json.Decode produces) against the schema. On failure
// it returns one FieldError per leaf validation error, with Field set to a
// dot/bracket path like "capabilities.tools[2]" — ready to hand to the
// API's `details` array.
func (v *Validator) Validate(definition any) ([]FieldError, error) {
	err := v.schema.Validate(definition)
	if err == nil {
		return nil, nil
	}

	validationErr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return nil, fmt.Errorf("dslschema: unexpected validation error type: %w", err)
	}

	var out []FieldError
	collectLeafErrors(validationErr, &out)
	return out, nil
}

// ValidateJSON is a convenience wrapper for raw JSON bytes.
func (v *Validator) ValidateJSON(raw []byte) ([]FieldError, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []FieldError{{Field: "", Message: "malformed JSON"}}, nil
	}
	return v.Validate(doc)
}

func collectLeafErrors(e *jsonschema.ValidationError, out *[]FieldError) {
	if len(e.Causes) == 0 {
		*out = append(*out, FieldError{
			Field:   instanceLocationToField(e.InstanceLocation),
			Message: e.Message,
		})
		return
	}
	for _, cause := range e.Causes {
		collectLeafErrors(cause, out)
	}
}

// instanceLocationToField turns jsonschema's InstanceLocation
// ("/capabilities/tools/2") into the dotted/bracketed path the frontend
// forms expect ("capabilities.tools[2]").
func instanceLocationToField(loc string) string {
	var b strings.Builder
	for _, seg := range strings.Split(strings.TrimPrefix(loc, "/"), "/") {
		if seg == "" {
			continue
		}
		if isArrayIndex(seg) {
			b.WriteString("[")
			b.WriteString(seg)
			b.WriteString("]")
			continue
		}
		if b.Len() > 0 {
			b.WriteString(".")
		}
		b.WriteString(seg)
	}
	return b.String()
}

func isArrayIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
