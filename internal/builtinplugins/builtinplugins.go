// Package builtinplugins embeds the platform's own plugin packages
// (spec-20 §5.1's "publisher_id NULL = 平台内置") directly into the server
// binary, and seeds them into the plugins table on startup — a built-in
// plugin's supply chain is "it shipped in this binary," not a publisher's
// signature, so there is nothing for a user to upload or sign.
package builtinplugins

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

//go:embed postgresconnector/plugin.json postgresconnector/plugin.wasm
var postgresConnectorFS embed.FS

//go:embed mysqlconnector/plugin.json mysqlconnector/plugin.wasm
var mysqlConnectorFS embed.FS

//go:embed chartrenderer/plugin.json chartrenderer/plugin.wasm chartrenderer/ui/chart.html
var chartRendererFS embed.FS

// seedService is the one plugin.Service method SeedAll needs — narrowed to
// a port so this package doesn't have to import the concrete Service type
// just to be testable.
type seedService interface {
	SeedBuiltin(ctx context.Context, cmd plugin.SeedBuiltinCommand) (plugin.Plugin, error)
}

// builtin is one embedded plugin's file set: its plugin.json plus every
// other file the manifest's OSS prefix needs, each already keyed the way
// they'll be stored (a wasm module always under "plugin.wasm", a
// renderer's static assets under their own manifest-relative path).
type builtin struct {
	fs    embed.FS
	files map[string]string // OSS-relative path -> path inside fs
}

// SeedAll seeds every built-in plugin this binary ships with: the
// PostgreSQL and MySQL connectors (both share one dialect-agnostic wasm
// module — dialect lives on the connector resource a user binds, not on
// the plugin — spec-20 §4.3) and the chart renderer (a render_chart tool
// call whose result an explicit tools[].ui entry renders — spec-20 §4.2
// method A; its wasm export does no real computation, it only exists so
// the call is a real, sequenced tool call rather than a fenced code block
// pattern-matched out of the model's own free text after the fact). Safe
// to call on every server startup — plugin.Service.SeedBuiltin is itself a
// no-op for a version that already exists. The first error is returned
// rather than continuing past it, so a caller sees which built-in failed
// rather than a swallowed partial seed.
func SeedAll(ctx context.Context, svc seedService) error {
	builtins := []builtin{
		{fs: postgresConnectorFS, files: map[string]string{"plugin.wasm": "postgresconnector/plugin.wasm"}},
		{fs: mysqlConnectorFS, files: map[string]string{"plugin.wasm": "mysqlconnector/plugin.wasm"}},
		{fs: chartRendererFS, files: map[string]string{
			"plugin.wasm":   "chartrenderer/plugin.wasm",
			"ui/chart.html": "chartrenderer/ui/chart.html",
		}},
	}
	manifestPaths := []string{"postgresconnector/plugin.json", "mysqlconnector/plugin.json", "chartrenderer/plugin.json"}

	for i, b := range builtins {
		cmd, err := loadEmbedded(b.fs, manifestPaths[i], b.files)
		if err != nil {
			return fmt.Errorf("builtinplugins: %w", err)
		}
		if _, err := svc.SeedBuiltin(ctx, cmd); err != nil {
			return fmt.Errorf("builtinplugins: seed %q: %w", cmd.PluginID, err)
		}
	}
	return nil
}

// loadEmbedded reads one embedded plugin's plugin.json plus its other
// files into a SeedBuiltinCommand — the same {manifest, Files} shape
// UploadCommand uses, so validateAndStore treats a built-in exactly like
// an upload. files maps the OSS-relative key each file is stored (and
// referenced from the manifest) under, to its path inside fs.
func loadEmbedded(fs embed.FS, manifestPath string, files map[string]string) (plugin.SeedBuiltinCommand, error) {
	manifestRaw, err := fs.ReadFile(manifestPath)
	if err != nil {
		return plugin.SeedBuiltinCommand{}, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return plugin.SeedBuiltinCommand{}, fmt.Errorf("parse %s: %w", manifestPath, err)
	}

	out := make(map[string][]byte, len(files))
	for ossKey, fsPath := range files {
		content, err := fs.ReadFile(fsPath)
		if err != nil {
			return plugin.SeedBuiltinCommand{}, fmt.Errorf("read %s: %w", fsPath, err)
		}
		out[ossKey] = content
	}

	pluginID, _ := manifest["id"].(string)
	version, _ := manifest["version"].(string)
	return plugin.SeedBuiltinCommand{PluginID: pluginID, Version: version, Manifest: manifest, Files: out}, nil
}
