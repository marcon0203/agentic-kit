package extism

import (
	"context"
	"errors"
	"testing"
)

func TestCallerIdentity_RoundTrip(t *testing.T) {
	ctx := withCallerIdentity(context.Background(), "acme.charts", 42)
	id, ok := callerIdentityFrom(ctx)
	if !ok {
		t.Fatal("expected an identity to be present")
	}
	if id.PluginID != "acme.charts" || id.OwnerID != 42 {
		t.Errorf("id = %+v, want {acme.charts 42}", id)
	}
}

func TestCallerIdentityFrom_AbsentByDefault(t *testing.T) {
	if _, ok := callerIdentityFrom(context.Background()); ok {
		t.Error("expected no identity on a plain context")
	}
}

func TestCallerNamespace_DerivedFromIdentityNotRequest(t *testing.T) {
	ctx := withCallerIdentity(context.Background(), "acme.charts", 7)
	ns, err := callerNamespace(ctx)
	if err != nil {
		t.Fatalf("callerNamespace: %v", err)
	}
	if ns != "acme.charts:7" {
		t.Errorf("namespace = %q, want \"acme.charts:7\"", ns)
	}
}

func TestCallerNamespace_ErrorsWithoutIdentity(t *testing.T) {
	if _, err := callerNamespace(context.Background()); err == nil {
		t.Fatal("expected an error when no caller identity is on the context")
	}
}

func TestCallerNamespace_ErrorsOnEmptyPluginID(t *testing.T) {
	ctx := withCallerIdentity(context.Background(), "", 7)
	if _, err := callerNamespace(ctx); err == nil {
		t.Fatal("expected an error when PluginID is empty")
	}
}

func TestEncodeOK_MergesRespFieldsWithOKTrue(t *testing.T) {
	out := encodeOK(kvGetResponse{Value: "v", Found: true})
	if out["ok"] != true {
		t.Errorf("ok = %v, want true", out["ok"])
	}
	if out["value"] != "v" || out["found"] != true {
		t.Errorf("out = %+v", out)
	}
}

func TestEncodeErr_CarriesOKFalseAndMessage(t *testing.T) {
	out := encodeErr[kvGetResponse](errors.New("boom"))
	if out["ok"] != false {
		t.Errorf("ok = %v, want false", out["ok"])
	}
	if out["error"] != "boom" {
		t.Errorf("error = %v, want \"boom\"", out["error"])
	}
}

func TestStructToMap_RoundTripsJSONTags(t *testing.T) {
	m := structToMap(sqlQueryResponse{Rows: []map[string]any{{"id": float64(1)}}})
	rows, ok := m["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("m = %+v", m)
	}
}

func TestJSONError_ErrorString(t *testing.T) {
	err := jsonError("something failed")
	if err.Error() != "something failed" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestHostFunctions_BuildsFixedSet(t *testing.T) {
	fns := hostFunctions(nil)
	names := map[string]bool{}
	for _, fn := range fns {
		names[fn.Name] = true
	}
	for _, want := range []string{"sql_query", "sql_execute", "sql_schema", "kv_get", "kv_set"} {
		if !names[want] {
			t.Errorf("missing host function %q", want)
		}
	}
}
