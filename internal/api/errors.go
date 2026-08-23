package api

import "github.com/marcon0203/agentic-kit/internal/domain"

// The business error table now lives in internal/domain (codes.go) — it is
// business vocabulary, not a transport detail. These aliases keep the
// transport layer's existing call sites readable while modules are migrated
// onto domain services one at a time; they are the same constants, not a
// second table.
const (
	ErrValidationFailed = domain.CodeValidationFailed
	ErrRateLimited      = domain.CodeRateLimited
	ErrRequestTooLarge  = domain.CodeRequestTooLarge
	ErrInternal         = domain.CodeInternal

	ErrTokenInvalid           = domain.CodeTokenInvalid
	ErrTokenExpired           = domain.CodeTokenExpired
	ErrForbidden              = domain.CodeForbidden
	ErrAPIKeyRevoked          = domain.CodeAPIKeyRevoked
	ErrEmailAlreadyRegistered = domain.CodeEmailAlreadyRegistered
	ErrInvalidCredentials     = domain.CodeInvalidCredentials

	ErrResourceNotFound     = domain.CodeResourceNotFound
	ErrResourceDisabled     = domain.CodeResourceDisabled
	ErrResourceRefDuplicate = domain.CodeResourceRefDuplicate
	ErrMCPHealthCheckFailed = domain.CodeMCPHealthCheckFailed

	ErrAgentSchemaInvalid   = domain.CodeAgentSchemaInvalid
	ErrBundleSchemaInvalid  = domain.CodeBundleSchemaInvalid
	ErrBundleGraphInvalid   = domain.CodeBundleGraphInvalid
	ErrAgentVersionNotFound = domain.CodeAgentVersionNotFound

	ErrRunNotFound           = domain.CodeRunNotFound
	ErrRunAlreadyFinished    = domain.CodeRunAlreadyFinished
	ErrGateAlreadyResolved   = domain.CodeGateAlreadyResolved
	ErrGateApproverForbidden = domain.CodeGateApproverForbidden
	ErrRunTimeout            = domain.CodeRunTimeout

	ErrProviderNotConfigured  = domain.CodeProviderNotConfigured
	ErrProviderCredsInvalid   = domain.CodeProviderCredsInvalid
	ErrProviderAllUnavailable = domain.CodeProviderAllUnavailable
	ErrTokenQuotaExceeded     = domain.CodeTokenQuotaExceeded

	ErrPublishUnpublishedDeps    = domain.CodePublishUnpublishedDeps
	ErrListingNotFound           = domain.CodeListingNotFound
	ErrNotSubscribed             = domain.CodeNotSubscribed
	ErrAlreadySubscribed         = domain.CodeAlreadySubscribed
	ErrSubscribedVersionLocked   = domain.CodeSubscribedVersionLocked
	ErrBlackboxDefinitionHidden  = domain.CodeBlackboxDefinitionHidden
	ErrDependencyStillReferenced = domain.CodeDependencyStillReferenced
	ErrCircularDependency        = domain.CodeCircularDependency

	ErrReportNotFound        = domain.CodeReportNotFound
	ErrReportAlreadyResolved = domain.CodeReportAlreadyResolved
)
