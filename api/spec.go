// Package apispec embeds this platform's own OpenAPI contract so the
// running binary can serve it back — the same file api/openapi.yaml is,
// checked into source control, lint-checked in CI (make openapi-lint), and
// the one openapi-typescript reads to produce web/src/lib/api/schema.d.ts.
// A third party integrating a published app (系统配置 → API Key 管理's
// whole reason to exist) needs the actual contract, not a description of
// it — this is that contract, verbatim, at runtime.
package apispec

import _ "embed"

//go:embed openapi.yaml
var YAML []byte
