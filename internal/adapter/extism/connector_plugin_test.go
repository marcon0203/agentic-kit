package extism_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/adapter/extism"
)

// testdata/sql_connector.wasm is examples/plugins/sql-connector's compiled
// output (Rust + Extism PDK, spec-20 §4.3's sample connector plugin) — a
// real WASM module exercising the actual sql.query/execute/schema host
// functions end to end, not a mock of them. fakeConnectorServices below
// stands in for the real connectorRegistry (internal/adapter/orchestrator)
// so this test can run without a live database, while still proving the
// plugin sends the right JSON over the real Extism/wazero boundary and
// that allow_write's rejection genuinely comes from the host side.
type fakeConnectorServices struct {
	gotConnRef string
	gotSQL     string
	execCalled bool
	execErr    error
}

func (f *fakeConnectorServices) SQLQuery(_ context.Context, connRef, query string, _ []any) ([]map[string]any, error) {
	f.gotConnRef, f.gotSQL = connRef, query
	return []map[string]any{{"id": float64(1), "name": "widgets"}}, nil
}

func (f *fakeConnectorServices) SQLExecute(_ context.Context, connRef, query string, _ []any) (int64, error) {
	f.execCalled = true
	f.gotConnRef, f.gotSQL = connRef, query
	if f.execErr != nil {
		return 0, f.execErr
	}
	return 3, nil
}

func (f *fakeConnectorServices) SQLSchema(_ context.Context, connRef string) ([]string, error) {
	f.gotConnRef = connRef
	return []string{"widgets", "orders"}, nil
}

func (f *fakeConnectorServices) KVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (f *fakeConnectorServices) KVSet(context.Context, string, string, string) error { return nil }

var _ extism.HostServices = (*fakeConnectorServices)(nil)

func TestSQLConnectorPlugin_ListTables(t *testing.T) {
	services := &fakeConnectorServices{}
	rt := extism.NewRuntime(services)
	defer func() { _ = rt.Close(context.Background()) }()

	opts := extism.Options{Config: map[string]string{"connection_ref": "conn-abc"}}
	out, err := rt.Call(context.Background(), "acme.sql-connector@0.1.0", loadWasm(t, "sql_connector.wasm"), opts, "list_tables", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var result struct {
		Rows []string `json:"rows"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output %q: %v", out, err)
	}
	if len(result.Rows) != 2 || result.Rows[0] != "widgets" || result.Rows[1] != "orders" {
		t.Errorf("rows = %+v, want [widgets orders]", result.Rows)
	}
	if services.gotConnRef != "conn-abc" {
		t.Errorf("connRef seen by host = %q, want %q — plugin must read it from config, not invent one", services.gotConnRef, "conn-abc")
	}
}

func TestSQLConnectorPlugin_RunQuery(t *testing.T) {
	services := &fakeConnectorServices{}
	rt := extism.NewRuntime(services)
	defer func() { _ = rt.Close(context.Background()) }()

	opts := extism.Options{Config: map[string]string{"connection_ref": "conn-xyz"}}
	input, _ := json.Marshal(map[string]string{"query": "SELECT * FROM widgets"})
	out, err := rt.Call(context.Background(), "acme.sql-connector@0.1.0", loadWasm(t, "sql_connector.wasm"), opts, "run_query", input)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output %q: %v", out, err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "widgets" {
		t.Errorf("rows = %+v", result.Rows)
	}
	if services.gotSQL != "SELECT * FROM widgets" {
		t.Errorf("sql seen by host = %q", services.gotSQL)
	}
	if services.gotConnRef != "conn-xyz" {
		t.Errorf("connRef seen by host = %q, want %q", services.gotConnRef, "conn-xyz")
	}
}

// TestSQLConnectorPlugin_RunWrite_HostRejectsWithoutAllowWrite proves the
// rejection genuinely happens on the host side of the wasm boundary: the
// plugin has no allow_write concept of its own and always calls
// sql_execute, so a rejection here can only be connectorRegistry's own
// enforcement (the real implementation returns errWriteNotAllowed for a
// connection bound without allow_write) — this fake stands in for exactly
// that behavior.
func TestSQLConnectorPlugin_RunWrite_HostRejectsWithoutAllowWrite(t *testing.T) {
	services := &fakeConnectorServices{execErr: errWriteNotAllowedForTest}
	rt := extism.NewRuntime(services)
	defer func() { _ = rt.Close(context.Background()) }()

	opts := extism.Options{Config: map[string]string{"connection_ref": "conn-readonly"}}
	input, _ := json.Marshal(map[string]string{"query": "DELETE FROM widgets"})
	_, err := rt.Call(context.Background(), "acme.sql-connector@0.1.0", loadWasm(t, "sql_connector.wasm"), opts, "run_write", input)
	if err == nil {
		t.Fatal("expected the plugin call to fail when the host rejects the write")
	}
	if !services.execCalled {
		t.Fatal("expected the plugin to still call sql_execute — it has no way to know allow_write itself")
	}
}

func TestSQLConnectorPlugin_RunWrite_Succeeds(t *testing.T) {
	services := &fakeConnectorServices{}
	rt := extism.NewRuntime(services)
	defer func() { _ = rt.Close(context.Background()) }()

	opts := extism.Options{Config: map[string]string{"connection_ref": "conn-rw"}}
	input, _ := json.Marshal(map[string]string{"query": "UPDATE widgets SET name = 'x'"})
	out, err := rt.Call(context.Background(), "acme.sql-connector@0.1.0", loadWasm(t, "sql_connector.wasm"), opts, "run_write", input)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var result struct {
		AffectedRows float64 `json:"affected_rows"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output %q: %v", out, err)
	}
	if result.AffectedRows != 3 {
		t.Errorf("affected_rows = %v, want 3", result.AffectedRows)
	}
}

var errWriteNotAllowedForTest = errors.New("connector: this connection was not granted allow_write")
