package builtinplugins

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

type fakeSeedService struct {
	got plugin.SeedBuiltinCommand
	err error
}

func (f *fakeSeedService) SeedBuiltin(_ context.Context, cmd plugin.SeedBuiltinCommand) (plugin.Plugin, error) {
	f.got = cmd
	if f.err != nil {
		return plugin.Plugin{}, f.err
	}
	return plugin.Plugin{PluginID: cmd.PluginID, Version: cmd.Version, Manifest: cmd.Manifest}, nil
}

func TestSeedAll_LoadsEmbeddedSQLConnector(t *testing.T) {
	svc := &fakeSeedService{}
	if err := SeedAll(context.Background(), svc); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	if svc.got.PluginID != "acme.sql-connector" {
		t.Errorf("PluginID = %q, want \"acme.sql-connector\"", svc.got.PluginID)
	}
	if svc.got.Version == "" {
		t.Error("expected a non-empty version parsed out of the embedded manifest")
	}
	wasmBytes, ok := svc.got.Files["plugin.wasm"]
	if !ok || len(wasmBytes) == 0 {
		t.Error("expected the embedded plugin.wasm to be present under the \"plugin.wasm\" key")
	}
	if manifestID, _ := svc.got.Manifest["id"].(string); manifestID != svc.got.PluginID {
		t.Errorf("manifest id %q does not match PluginID %q", manifestID, svc.got.PluginID)
	}
}

func TestSeedAll_PropagatesServiceError(t *testing.T) {
	svc := &fakeSeedService{err: errBoom}
	if err := SeedAll(context.Background(), svc); err == nil {
		t.Fatal("expected SeedAll to propagate the service's error")
	}
}

var errBoom = errors.New("boom")
