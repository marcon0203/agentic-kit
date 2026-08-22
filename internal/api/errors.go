package api

// Business error codes: five digits, AABBB — first two digits identify the
// module, last three are a sequence number within that module. Codes are
// append-only once published: a retired code stays reserved and is never
// reused, and a code's meaning never changes (message copy can improve
// freely). See docs/架构设计文档_AI-Agent平台_V1.md 6.3 for the authoritative
// table this mirrors.
const (
	// 10xxx — 通用/网关
	ErrValidationFailed = 10001
	ErrRateLimited      = 10002
	ErrRequestTooLarge  = 10003
	ErrInternal         = 10099

	// 20xxx — IAM
	ErrTokenInvalid  = 20001
	ErrTokenExpired  = 20002
	ErrForbidden     = 20003
	ErrAPIKeyRevoked = 20004

	// 30xxx — 资源中心
	ErrResourceNotFound     = 30001
	ErrResourceDisabled     = 30002
	ErrResourceRefDuplicate = 30003
	ErrMCPHealthCheckFailed = 30004

	// 40xxx — 应用中心（Agent/Bundle）
	ErrAgentSchemaInvalid   = 40001
	ErrBundleSchemaInvalid  = 40002
	ErrBundleGraphInvalid   = 40003
	ErrAgentVersionNotFound = 40004

	// 50xxx — 编排运行时
	ErrRunNotFound           = 50001
	ErrRunAlreadyFinished    = 50002
	ErrGateAlreadyResolved   = 50003
	ErrGateApproverForbidden = 50004
	ErrRunTimeout            = 50005

	// 60xxx — 模型中心
	ErrProviderNotConfigured  = 60001
	ErrProviderCredsInvalid   = 60002
	ErrProviderAllUnavailable = 60003
	ErrTokenQuotaExceeded     = 60004

	// 70xxx — 广场与订阅
	ErrPublishUnpublishedDeps    = 70001
	ErrListingNotFound           = 70002
	ErrNotSubscribed             = 70003
	ErrAlreadySubscribed         = 70004
	ErrSubscribedVersionLocked   = 70005
	ErrBlackboxDefinitionHidden  = 70006
	ErrDependencyStillReferenced = 70007
	ErrCircularDependency        = 70008
)
