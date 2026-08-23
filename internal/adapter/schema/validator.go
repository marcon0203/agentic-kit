// Package schema adapts internal/dslschema's JSON Schema validators to the
// DefinitionValidator ports the domain contexts declare, translating
// dslschema's field errors into domain.FieldError.
package schema

import (
	"github.com/marcon0203/agentic-kit/internal/domain"
	"github.com/marcon0203/agentic-kit/internal/dslschema"
)

// Validator wraps a *dslschema.Validator.
type Validator struct {
	v *dslschema.Validator
}

func NewValidator(v *dslschema.Validator) *Validator { return &Validator{v: v} }

func (a *Validator) Validate(def map[string]any) ([]domain.FieldError, error) {
	errs, err := a.v.Validate(def)
	if err != nil {
		return nil, err
	}
	if len(errs) == 0 {
		return nil, nil
	}
	out := make([]domain.FieldError, len(errs))
	for i, e := range errs {
		out[i] = domain.FieldError{Field: e.Field, Reason: e.Message}
	}
	return out, nil
}
