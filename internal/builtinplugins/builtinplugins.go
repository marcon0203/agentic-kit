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

//go:embed sqlconnector/plugin.json sqlconnector/plugin.wasm
var sqlConnectorFS embed.FS

// seedService is the one plugin.Service method SeedAll needs — narrowed to
// a port so this package doesn't have to import the concrete Service type
// just to be testable.
type seedService interface {
	SeedBuiltin(ctx context.Context, cmd plugin.SeedBuiltinCommand) (plugin.Plugin, error)
}

// SeedAll seeds every built-in plugin this binary ships with. Safe to call
// on every server startup — plugin.Service.SeedBuiltin is itself a no-op
// for a version that already exists. svc.SeedBuiltin returning an error
// here (most likely "OSS not configured on this deployment") is reported
// to the caller rather than swallowed, since a caller may want to log it
// as a warning rather than fail startup over it.
func SeedAll(ctx context.Context, svc seedService) error {
	cmd, err := loadEmbedded(sqlConnectorFS, "sqlconnector")
	if err != nil {
		return fmt.Errorf("builtinplugins: %w", err)
	}
	if _, err := svc.SeedBuiltin(ctx, cmd); err != nil {
		return fmt.Errorf("builtinplugins: seed %q: %w", cmd.PluginID, err)
	}
	return nil
}

// loadEmbedded reads one embedded plugin's plugin.json + plugin.wasm into
// a SeedBuiltinCommand — the same {manifest, Files} shape UploadCommand
// uses, so validateAndStore treats a built-in exactly like an upload.
func loadEmbedded(fs embed.FS, dir string) (plugin.SeedBuiltinCommand, error) {
	manifestRaw, err := fs.ReadFile(dir + "/plugin.json")
	if err != nil {
		return plugin.SeedBuiltinCommand{}, fmt.Errorf("read %s/plugin.json: %w", dir, err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return plugin.SeedBuiltinCommand{}, fmt.Errorf("parse %s/plugin.json: %w", dir, err)
	}
	wasmBytes, err := fs.ReadFile(dir + "/plugin.wasm")
	if err != nil {
		return plugin.SeedBuiltinCommand{}, fmt.Errorf("read %s/plugin.wasm: %w", dir, err)
	}

	pluginID, _ := manifest["id"].(string)
	version, _ := manifest["version"].(string)
	return plugin.SeedBuiltinCommand{
		PluginID: pluginID, Version: version, Manifest: manifest,
		Files: map[string][]byte{"plugin.wasm": wasmBytes},
	}, nil
}
