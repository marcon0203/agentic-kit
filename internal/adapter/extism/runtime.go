// Package extism wraps the Extism/wazero WASM runtime (spec-20 §四): pure
// Go, no CGO, in-process — a plugin call is a function call inside the
// same pod, not a separate container or sidecar, which is what keeps the
// whole plugin system K8s-neutral (spec-20 §九).
//
// This package owns everything sandbox-shaped: compiling a plugin's .wasm
// once and reusing it across calls, per-call instantiation and teardown
// ("谁创建谁回收" — an instance never outlives the one Call that created
// it), timeout and memory ceilings, and AllowedHosts domain whitelisting
// for the plugin's own outbound HTTP (Extism's built-in http_request host
// function enforces this itself; this package only has to pass the list
// through). internal/orchestrator/adk knows none of this — it sees the
// PluginRuntime port and nothing else.
package extism

import (
	"context"
	"fmt"
	"strings"
	"sync"

	extism "github.com/extism/go-sdk"
)

// DefaultTimeoutMS and DefaultMaxMemoryPages are the sandbox's hard
// ceilings (spec-20 §4.1) — a plugin manifest cannot ask for more, only
// less. Timeout matches the existing toolCallTimeout every other tool kind
// already uses; the memory ceiling has no empirical basis yet (spec-20's
// own 【待确认】) and may need revisiting once real plugins exist to profile.
const (
	DefaultTimeoutMS      uint64 = 30_000
	DefaultMaxMemoryPages uint32 = 2048 // 64 KiB/page × 2048 = 128 MiB
)

// Options configures one plugin's sandbox. AllowedHosts is the manifest's
// requires.network list verbatim — nil/empty means the plugin gets no
// outbound HTTP at all, which is Extism's own default behavior for an
// empty allow-list.
type Options struct {
	AllowedHosts   []string
	TimeoutMS      uint64
	MaxMemoryPages uint32
	// Config surfaces plugin-visible key/value config (Extism's config.get
	// host function) — e.g. an installation's granted permission list.
	// Extism's manifest only carries string values; anything richer stays
	// out of the sandbox's reach entirely, by construction.
	Config map[string]string
	// PluginID/OwnerID scope the kv.get/set host function's namespace
	// (spec-20 §4.3) — carried on the call's context, never trusted from
	// the plugin's own request payload (see host_functions.go).
	PluginID string
	OwnerID  int64
}

// normalize applies the hard ceilings: a zero value gets the default, and
// per spec-20 §4.1 ("不接受插件清单里调大") anything above the ceiling is
// clamped down to it rather than honored — a plugin's own manifest can ask
// for a tighter sandbox, never a looser one.
func (o Options) normalize() Options {
	if o.TimeoutMS == 0 || o.TimeoutMS > DefaultTimeoutMS {
		o.TimeoutMS = DefaultTimeoutMS
	}
	if o.MaxMemoryPages == 0 || o.MaxMemoryPages > DefaultMaxMemoryPages {
		o.MaxMemoryPages = DefaultMaxMemoryPages
	}
	return o
}

// Runtime compiles and calls plugin WASM modules. One Runtime is meant to
// be shared process-wide: compilation is the expensive part (parsing and
// validating the module), so it is cached by wasmKey and reused across
// every call and every run — only instantiation, which is cheap, happens
// per call.
type Runtime struct {
	mu       sync.Mutex
	cache    map[string]*extism.CompiledPlugin
	services HostServices
}

// NewRuntime wires services (spec-20 §4.3's sql/kv host functions) into
// every plugin this Runtime ever compiles. services may be nil — a
// deployment with no connector backend configured still runs plain
// tools[] plugins; a plugin that does call sql.query/execute/schema or
// kv.get/set just gets a clear "not configured" error back instead of
// those functions being unavailable at the wasm-import level (which would
// fail every plugin, not just ones that use them).
func NewRuntime(services HostServices) *Runtime {
	return &Runtime{cache: map[string]*extism.CompiledPlugin{}, services: services}
}

// compile returns the CompiledPlugin for wasmKey, either the shared cached
// one or a fresh, uncached one — see the ephemeral return value's doc.
// wasmKey is expected to be "{plugin_id}@{version}" — the resolved version
// is part of the key because two installations pinned to different
// versions of the same plugin_id must never share one compiled module.
func (r *Runtime) compile(ctx context.Context, wasmKey string, wasmBytes []byte, opts Options) (cp *extism.CompiledPlugin, ephemeral bool, err error) {
	// opts.Config carries per-call data baked into Extism's Manifest.Config
	// at CompiledPlugin construction time — most importantly a connectors
	// binding's connection_ref (spec-20 §4.3), which bindConnector mints
	// fresh on every single run and never reuses. This SDK version has no
	// supported way to override a CompiledPlugin's Config per Instance()
	// call, so a plugin call carrying Config can never be served from (or
	// added to) the shared, wasmKey-only cache: doing so would silently
	// hand a later run whatever connection_ref (or other Config) the
	// *first* caller happened to compile with — a connection the run that
	// opened it has since released, surfacing as "unknown or expired
	// connection_ref". Compiling fresh here costs the wasm parse/validate
	// step on every such call instead of amortizing it, but a connector
	// call is an interactive tool invocation, not a hot loop, so that
	// trade is the right one against silently serving stale credentials.
	if len(opts.Config) > 0 {
		cp, err = r.compileUncached(ctx, wasmKey, wasmBytes, opts)
		return cp, true, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if cp, ok := r.cache[wasmKey]; ok {
		return cp, false, nil
	}
	cp, err = r.compileUncached(ctx, wasmKey, wasmBytes, opts)
	if err != nil {
		return nil, false, err
	}
	r.cache[wasmKey] = cp
	return cp, false, nil
}

func (r *Runtime) compileUncached(ctx context.Context, wasmKey string, wasmBytes []byte, opts Options) (*extism.CompiledPlugin, error) {
	opts = opts.normalize()
	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmData{Data: wasmBytes}},
		AllowedHosts: opts.AllowedHosts,
		Timeout:      opts.TimeoutMS,
		Memory:       &extism.ManifestMemory{MaxPages: opts.MaxMemoryPages},
		Config:       opts.Config,
	}
	cp, err := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, hostFunctions(r.services))
	if err != nil {
		return nil, fmt.Errorf("extism: compile plugin %q: %w", wasmKey, err)
	}
	return cp, nil
}

// Call runs one plugin function to completion: compile-once (cached, unless
// opts.Config makes this call's CompiledPlugin ephemeral — see compile's
// doc), instantiate, call, close the instance — every call gets its own
// fresh instance so one plugin's state from a previous call never leaks
// into the next. An ephemeral CompiledPlugin is closed here too, once this
// call is done with it — it was never added to r.cache for Runtime.Close
// to find later.
func (r *Runtime) Call(ctx context.Context, wasmKey string, wasmBytes []byte, opts Options, funcName string, input []byte) ([]byte, error) {
	cp, ephemeral, err := r.compile(ctx, wasmKey, wasmBytes, opts)
	if err != nil {
		return nil, err
	}
	if ephemeral {
		defer func() { _ = cp.Close(ctx) }()
	}
	ctx = withCallerIdentity(ctx, opts.PluginID, opts.OwnerID)

	instance, err := cp.Instance(ctx, extism.PluginInstanceConfig{})
	if err != nil {
		return nil, fmt.Errorf("extism: instantiate plugin %q: %w", wasmKey, err)
	}
	defer func() { _ = instance.Close(ctx) }()

	if !instance.FunctionExists(funcName) {
		return nil, fmt.Errorf("extism: plugin %q has no function %q", wasmKey, funcName)
	}
	exitCode, output, err := instance.CallWithContext(ctx, funcName, input)
	if err != nil {
		return nil, fmt.Errorf("extism: call %q %q: %w", wasmKey, funcName, err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("extism: %q %q exited %d: %s", wasmKey, funcName, exitCode, instance.GetError())
	}
	return output, nil
}

// ValidateEntries compiles wasmBytes — populating the same compile cache
// Call uses under wasmKey, so a validation run at upload time also primes
// the first real call rather than compiling twice — and confirms every
// name in funcNames is actually exported. This is the upload-time
// automated gate (spec-20 §5.3): a manifest can *claim* an entry point
// exists; only resolving it against the compiled module proves it.
func (r *Runtime) ValidateEntries(ctx context.Context, wasmKey string, wasmBytes []byte, funcNames []string) error {
	cp, _, err := r.compile(ctx, wasmKey, wasmBytes, Options{})
	if err != nil {
		return err
	}

	instance, err := cp.Instance(ctx, extism.PluginInstanceConfig{})
	if err != nil {
		return fmt.Errorf("extism: instantiate plugin %q for validation: %w", wasmKey, err)
	}
	defer func() { _ = instance.Close(ctx) }()

	var missing []string
	for _, fn := range funcNames {
		if !instance.FunctionExists(fn) {
			missing = append(missing, fn)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("extism: plugin %q missing exported function(s): %s", wasmKey, strings.Join(missing, ", "))
	}
	return nil
}

// Close releases every cached compiled module — call this on server
// shutdown, not between runs (the whole point of the cache is that it
// outlives any one run).
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for key, cp := range r.cache {
		if err := cp.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.cache, key)
	}
	return firstErr
}
