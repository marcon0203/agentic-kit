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
	"sync"

	extism "github.com/extism/go-sdk"
)

// DefaultTimeoutMS and DefaultMaxMemoryPages are the sandbox ceilings a
// plugin call gets when its manifest doesn't ask for something tighter —
// generous enough for a real tool call, small enough that one runaway
// plugin can't take a pod's other in-flight requests down with it
// (spec-20 §九's "watch item", not a hard blocker).
const (
	DefaultTimeoutMS      uint64 = 10_000
	DefaultMaxMemoryPages uint32 = 256 // 64 KiB/page × 256 = 16 MiB
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
}

func (o Options) normalize() Options {
	if o.TimeoutMS == 0 {
		o.TimeoutMS = DefaultTimeoutMS
	}
	if o.MaxMemoryPages == 0 {
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
	mu    sync.Mutex
	cache map[string]*extism.CompiledPlugin
}

func NewRuntime() *Runtime {
	return &Runtime{cache: map[string]*extism.CompiledPlugin{}}
}

// compile returns the cached CompiledPlugin for wasmKey, compiling it on
// first use. wasmKey is expected to be "{plugin_id}@{version}" — the
// resolved version is part of the key because two installations pinned to
// different versions of the same plugin_id must never share one compiled
// module.
func (r *Runtime) compile(ctx context.Context, wasmKey string, wasmBytes []byte, opts Options) (*extism.CompiledPlugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cp, ok := r.cache[wasmKey]; ok {
		return cp, nil
	}

	opts = opts.normalize()
	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmData{Data: wasmBytes}},
		AllowedHosts: opts.AllowedHosts,
		Timeout:      opts.TimeoutMS,
		Memory:       &extism.ManifestMemory{MaxPages: opts.MaxMemoryPages},
		Config:       opts.Config,
	}
	cp, err := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("extism: compile plugin %q: %w", wasmKey, err)
	}
	r.cache[wasmKey] = cp
	return cp, nil
}

// Call runs one plugin function to completion: compile-once (cached),
// instantiate, call, close the instance — every call gets its own fresh
// instance so one plugin's state from a previous call never leaks into
// the next.
func (r *Runtime) Call(ctx context.Context, wasmKey string, wasmBytes []byte, opts Options, funcName string, input []byte) ([]byte, error) {
	cp, err := r.compile(ctx, wasmKey, wasmBytes, opts)
	if err != nil {
		return nil, err
	}

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
