package domain

// Business error codes: five digits, AABBB — first two digits identify the
// module, last three are a sequence number within that module. Codes are
// append-only once published: a retired code stays reserved and is never
// reused, and a code's meaning never changes (message copy can improve
// freely). See docs/架构设计文档_AI-Agent平台_V1.md 6.3 for the authoritative
// table this mirrors.
//
// These live in the shared kernel rather than in internal/api because they
// are business vocabulary: a service decides "this is 70005 subscribed-
// version-locked", and the transport only decides which HTTP status carries
// that fact.
const (
	// 10xxx — 通用/网关
	CodeValidationFailed = 10001
	CodeRateLimited      = 10002
	CodeRequestTooLarge  = 10003
	CodeInternal         = 10099

	// 20xxx — IAM
	CodeTokenInvalid           = 20001
	CodeTokenExpired           = 20002
	CodeForbidden              = 20003
	CodeAPIKeyRevoked          = 20004
	CodeEmailAlreadyRegistered = 20005
	CodeInvalidCredentials     = 20006

	// 30xxx — 资源中心
	CodeResourceNotFound     = 30001
	CodeResourceDisabled     = 30002
	CodeResourceRefDuplicate = 30003
	CodeMCPHealthCheckFailed = 30004

	// 40xxx — 应用中心（Agent/Bundle）
	CodeAgentSchemaInvalid   = 40001
	CodeBundleSchemaInvalid  = 40002
	CodeBundleGraphInvalid   = 40003
	CodeAgentVersionNotFound = 40004

	// 50xxx — 编排运行时
	CodeRunNotFound           = 50001
	CodeRunAlreadyFinished    = 50002
	CodeGateAlreadyResolved   = 50003
	CodeGateApproverForbidden = 50004
	CodeRunTimeout            = 50005

	// 60xxx — 模型中心
	CodeProviderNotConfigured  = 60001
	CodeProviderCredsInvalid   = 60002
	CodeProviderAllUnavailable = 60003
	CodeTokenQuotaExceeded     = 60004

	// 70xxx — 广场与订阅
	CodePublishUnpublishedDeps    = 70001
	CodeListingNotFound           = 70002
	CodeNotSubscribed             = 70003
	CodeAlreadySubscribed         = 70004
	CodeSubscribedVersionLocked   = 70005
	CodeBlackboxDefinitionHidden  = 70006
	CodeDependencyStillReferenced = 70007
	CodeCircularDependency        = 70008

	// 80xxx — 运营中心
	CodeReportNotFound        = 80001
	CodeReportAlreadyResolved = 80002
)
