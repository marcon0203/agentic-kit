package extism

import (
	"context"
	"encoding/json"
	"fmt"

	extism "github.com/extism/go-sdk"
)

// callerIdentityKey is unexported so only withCallerIdentity/callerIdentity
// in this package can set or read it — nothing a plugin sends can forge
// this value, because it never travels through the plugin's own JSON
// request/response, only through Go's own context on the host side of the
// call (see Runtime.Call).
type callerIdentityKey struct{}

type callerIdentity struct {
	PluginID string
	OwnerID  int64
}

func withCallerIdentity(ctx context.Context, pluginID string, ownerID int64) context.Context {
	return context.WithValue(ctx, callerIdentityKey{}, callerIdentity{PluginID: pluginID, OwnerID: ownerID})
}

func callerIdentityFrom(ctx context.Context) (callerIdentity, bool) {
	id, ok := ctx.Value(callerIdentityKey{}).(callerIdentity)
	return id, ok
}

// HostServices backs the sql/kv host functions every compiled plugin gets
// access to (spec-20 §4.3). Implementations live outside this package
// (internal/adapter/orchestrator) — this package only knows how to get a
// JSON request out of a plugin's WASM memory and a JSON response back in,
// never what a connection_ref or a kv namespace actually means.
//
// SQLExecute's allow_write enforcement is the one rule spec-20 §4.3 is
// explicit must live in the host function, never in plugin code: an
// implementation MUST reject a write on a connection that wasn't opened
// with allow_write — a malicious or buggy plugin cannot talk its way
// around this by any input it sends, because it never has an alternative
// path to the database at all.
type HostServices interface {
	SQLQuery(ctx context.Context, connRef, query string, args []any) ([]map[string]any, error)
	SQLExecute(ctx context.Context, connRef, query string, args []any) (affectedRows int64, err error)
	SQLSchema(ctx context.Context, connRef string) (tables []string, err error)
	KVGet(ctx context.Context, namespace, key string) (value string, ok bool, err error)
	KVSet(ctx context.Context, namespace, key, value string) error
}

// hostFunctions builds the fixed set of Extism host functions every
// compiled plugin is given (spec-20 §4.3's table: sql.query/execute/schema,
// kv.get/set — log is handled separately since Extism already provides a
// built-in log/var mechanism the SDK's CurrentPlugin.Log wraps). services
// may be nil: every function then returns a clear "not configured" error
// response rather than panicking — a deployment without connectors
// configured still boots and still runs plain tools[] plugins.
func hostFunctions(services HostServices) []extism.HostFunction {
	return []extism.HostFunction{
		jsonHostFunction("sql_query", func(ctx context.Context, req sqlQueryRequest) (sqlQueryResponse, error) {
			if services == nil {
				return sqlQueryResponse{}, errNotConfigured
			}
			rows, err := services.SQLQuery(ctx, req.ConnRef, req.SQL, req.Args)
			if err != nil {
				return sqlQueryResponse{}, err
			}
			return sqlQueryResponse{Rows: rows}, nil
		}),
		jsonHostFunction("sql_execute", func(ctx context.Context, req sqlExecuteRequest) (sqlExecuteResponse, error) {
			if services == nil {
				return sqlExecuteResponse{}, errNotConfigured
			}
			n, err := services.SQLExecute(ctx, req.ConnRef, req.SQL, req.Args)
			if err != nil {
				return sqlExecuteResponse{}, err
			}
			return sqlExecuteResponse{AffectedRows: n}, nil
		}),
		jsonHostFunction("sql_schema", func(ctx context.Context, req sqlSchemaRequest) (sqlSchemaResponse, error) {
			if services == nil {
				return sqlSchemaResponse{}, errNotConfigured
			}
			tables, err := services.SQLSchema(ctx, req.ConnRef)
			if err != nil {
				return sqlSchemaResponse{}, err
			}
			return sqlSchemaResponse{Tables: tables}, nil
		}),
		jsonHostFunction("kv_get", func(ctx context.Context, req kvGetRequest) (kvGetResponse, error) {
			if services == nil {
				return kvGetResponse{}, errNotConfigured
			}
			namespace, err := callerNamespace(ctx)
			if err != nil {
				return kvGetResponse{}, err
			}
			value, ok, err := services.KVGet(ctx, namespace, req.Key)
			if err != nil {
				return kvGetResponse{}, err
			}
			return kvGetResponse{Value: value, Found: ok}, nil
		}),
		jsonHostFunction("kv_set", func(ctx context.Context, req kvSetRequest) (kvSetResponse, error) {
			if services == nil {
				return kvSetResponse{}, errNotConfigured
			}
			namespace, err := callerNamespace(ctx)
			if err != nil {
				return kvSetResponse{}, err
			}
			if err := services.KVSet(ctx, namespace, req.Key, req.Value); err != nil {
				return kvSetResponse{}, err
			}
			return kvSetResponse{}, nil
		}),
	}
}

type sqlQueryRequest struct {
	ConnRef string `json:"conn_ref"`
	SQL     string `json:"sql"`
	Args    []any  `json:"args"`
}
type sqlQueryResponse struct {
	Rows []map[string]any `json:"rows"`
}

type sqlExecuteRequest struct {
	ConnRef string `json:"conn_ref"`
	SQL     string `json:"sql"`
	Args    []any  `json:"args"`
}
type sqlExecuteResponse struct {
	AffectedRows int64 `json:"affected_rows"`
}

type sqlSchemaRequest struct {
	ConnRef string `json:"conn_ref"`
}
type sqlSchemaResponse struct {
	Tables []string `json:"tables"`
}

type kvGetRequest struct {
	Key string `json:"key"`
}
type kvGetResponse struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type kvSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type kvSetResponse struct{}

// callerNamespace derives the kv namespace from the call's own identity
// (set by Runtime.Call), never from anything the plugin's request claims —
// see callerIdentityKey's doc comment for why that split matters.
func callerNamespace(ctx context.Context) (string, error) {
	id, ok := callerIdentityFrom(ctx)
	if !ok || id.PluginID == "" {
		return "", jsonError("internal: no caller identity for this call")
	}
	return fmt.Sprintf("%s:%d", id.PluginID, id.OwnerID), nil
}

var errNotConfigured = jsonError("connector host services are not configured on this deployment")

type jsonError string

func (e jsonError) Error() string { return string(e) }

// jsonHostFunction wraps a typed (request -> response, error) callback as
// a raw Extism HostFunction: read the guest's I64 memory offset, decode
// its JSON request, run fn, encode the JSON envelope, write it back to
// guest memory, return the new offset. This is the one piece of this
// package that talks to raw wasm memory — everything above it is
// ordinary, unit-testable Go.
func jsonHostFunction[Req, Resp any](name string, fn func(ctx context.Context, req Req) (Resp, error)) extism.HostFunction {
	return extism.NewHostFunctionWithStack(name,
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
			reqBytes, err := p.ReadBytes(stack[0])
			if err != nil {
				stack[0] = writeEnvelope(p, encodeErr[Resp](err))
				return
			}
			var req Req
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				stack[0] = writeEnvelope(p, encodeErr[Resp](err))
				return
			}
			resp, err := fn(ctx, req)
			if err != nil {
				stack[0] = writeEnvelope(p, encodeErr[Resp](err))
				return
			}
			stack[0] = writeEnvelope(p, encodeOK(resp))
		},
		[]extism.ValueType{extism.ValueTypeI64}, []extism.ValueType{extism.ValueTypeI64},
	)
}

// envelope is the flattened wire shape jsonHostFunction actually
// marshals — {"ok":bool,"error"?:string} plus Resp's own fields inlined
// via Go's anonymous-struct-embedding-through-json trick isn't available
// generically, so this builds the merged map by hand instead.
func encodeOK[Resp any](resp Resp) map[string]any {
	out := structToMap(resp)
	out["ok"] = true
	return out
}

func encodeErr[Resp any](err error) map[string]any {
	return map[string]any{"ok": false, "error": err.Error()}
}

func structToMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func writeEnvelope(p *extism.CurrentPlugin, envelope map[string]any) uint64 {
	b, err := json.Marshal(envelope)
	if err != nil {
		b = []byte(`{"ok":false,"error":"internal: failed to encode response"}`)
	}
	offset, err := p.WriteBytes(b)
	if err != nil {
		return 0
	}
	return offset
}
