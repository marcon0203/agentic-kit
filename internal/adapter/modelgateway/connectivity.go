// Package modelgateway adapts internal/modelgateway's real provider
// endpoints to the ports the 模型中心 context declares.
package modelgateway

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/domain/modelcenter"
	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

// ConnectivityChecker implements modelcenter.ConnectivityChecker by
// authenticating against the provider's real API.
type ConnectivityChecker struct {
	// newValidator is injectable so tests can point at an httptest server
	// instead of the vendor.
	newValidator func(provider, baseURL string) modelgateway.Validator
}

func NewConnectivityChecker() *ConnectivityChecker {
	return &ConnectivityChecker{newValidator: modelgateway.NewValidator}
}

func (c *ConnectivityChecker) Check(ctx context.Context, provider, apiKey, baseURL string) error {
	validator := c.newValidator(provider, baseURL)
	if validator == nil {
		return modelcenter.ErrUnknownProvider
	}
	return validator.Validate(ctx, apiKey)
}
