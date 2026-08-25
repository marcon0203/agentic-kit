// Package schemas embeds the DSL JSON Schema files so the backend doesn't
// depend on a working-directory-relative path to find them at runtime.
// schemas/agent.schema.json and schemas/bundle.schema.json remain the
// single source of truth (see schemas/README.md) — this file only makes
// their bytes available to Go code without copying them.
package schemas

import _ "embed"

//go:embed agent.schema.json
var AgentSchemaJSON []byte

//go:embed bundle.schema.json
var BundleSchemaJSON []byte

//go:embed plugin.schema.json
var PluginSchemaJSON []byte
