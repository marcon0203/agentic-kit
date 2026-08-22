package api

// Business error codes: five digits, AABBB — first two digits identify the
// module, last three are a sequence number within that module.
// See docs/架构设计文档_AI-Agent平台_V1.md 第六章 for the authoritative list;
// this file is extended as each module's spec is implemented.
const (
	// 10xxx — Auth
	ErrInvalidCredentials = 10001
	ErrTokenExpired       = 10002

	// 30xxx — Agent Center
	ErrAgentSchemaInvalid  = 30001
	ErrResourceUnavailable = 30002

	// 40xxx — Bundle Center
	ErrBundleSchemaInvalid = 40001
	ErrBundleFieldInvalid  = 40002
	ErrBundleGraphInvalid  = 40003

	// 50xxx — Run Engine
	ErrGateApproverForbidden = 50004

	// 70xxx — Marketplace
	ErrDependencyStillReferenced = 70007
	ErrCircularDependency        = 70008
)
