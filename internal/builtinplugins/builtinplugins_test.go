package builtinplugins

import (
	"context"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/plugin"
)

type fakeSeedService struct {
	seeded []plugin.SeedBuiltinCommand
	err    error
}

func (f *fakeSeedService) SeedBuiltin(_ context.Context, cmd plugin.SeedBuiltinCommand) (plugin.Plugin, error) {
	f.seeded = append(f.seeded, cmd)
	if f.err != nil {
		return plugin.Plugin{}, f.err
	}
	return plugin.Plugin{PluginID: cmd.PluginID, Version: cmd.Version, Manifest: cmd.Manifest}, nil
}

func TestSeedAll_LoadsAllThreeBuiltins(t *testing.T) {
	svc := &fakeSeedService{}
	if err := SeedAll(context.Background(), svc); err != nil {
		t.Fatalf("SeedAll: %v", err)
	}
	if len(svc.seeded) != 3 {
		t.Fatalf("expected 3 built-ins seeded, got %d", len(svc.seeded))
	}

	byID := map[string]plugin.SeedBuiltinCommand{}
	for _, cmd := range svc.seeded {
		byID[cmd.PluginID] = cmd
	}

	pg, ok := byID["agentic-kit.postgres-connector"]
	if !ok {
		t.Fatal("expected agentic-kit.postgres-connector to be seeded")
	}
	if len(pg.Files["plugin.wasm"]) == 0 {
		t.Error("expected the postgres connector's plugin.wasm to be non-empty")
	}

	mysql, ok := byID["agentic-kit.mysql-connector"]
	if !ok {
		t.Fatal("expected agentic-kit.mysql-connector to be seeded")
	}
	if len(mysql.Files["plugin.wasm"]) == 0 {
		t.Error("expected the mysql connector's plugin.wasm to be non-empty")
	}
	// Both connectors are dialect-agnostic at the wasm layer — the
	// dialect lives on the connector resource a user binds, not on the
	// plugin — so they're expected to share the exact same compiled
	// module rather than each shipping their own.
	if string(pg.Files["plugin.wasm"]) != string(mysql.Files["plugin.wasm"]) {
		t.Error("expected the postgres and mysql connectors to share the same wasm module")
	}

	chart, ok := byID["agentic-kit.chart-renderer"]
	if !ok {
		t.Fatal("expected agentic-kit.chart-renderer to be seeded")
	}
	if len(chart.Files["ui/chart.html"]) == 0 {
		t.Error("expected the chart renderer's ui/chart.html to be non-empty")
	}
	if len(chart.Files["plugin.wasm"]) != 0 {
		t.Error("expected the chart renderer (frontend-only) to ship no wasm module")
	}

	for id, cmd := range byID {
		if manifestID, _ := cmd.Manifest["id"].(string); manifestID != id {
			t.Errorf("manifest id %q does not match PluginID %q", manifestID, id)
		}
		if cmd.Version == "" {
			t.Errorf("%s: expected a non-empty version", id)
		}
	}
}

func TestSeedAll_PropagatesServiceError(t *testing.T) {
	svc := &fakeSeedService{err: errBoom}
	if err := SeedAll(context.Background(), svc); err == nil {
		t.Fatal("expected SeedAll to propagate the service's error")
	}
}

var errBoom = errors.New("boom")
