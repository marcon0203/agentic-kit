package extism

import (
	"context"

	"github.com/marcon0203/agentic-kit/internal/orchestrator/adk"
)

// ForADK adapts a Runtime to adk.PluginRuntime. The two packages
// deliberately don't share an Options type — adk stays free of a WASM
// runtime dependency (spec-10's "所有 ADK 调用收敛在 internal/orchestrator/adk
// 包内" cuts the other way for sandboxing details, same reasoning as
// PluginRuntime's own doc comment) — so this is just the field-for-field
// translation between the two.
func ForADK(rt *Runtime) adk.PluginRuntime { return adkRuntime{rt: rt} }

type adkRuntime struct{ rt *Runtime }

func (a adkRuntime) Call(ctx context.Context, wasmKey string, wasmBytes []byte, opts adk.PluginRuntimeOptions, funcName string, input []byte) ([]byte, error) {
	return a.rt.Call(ctx, wasmKey, wasmBytes, Options{
		AllowedHosts: opts.AllowedHosts, TimeoutMS: opts.TimeoutMS,
		MaxMemoryPages: opts.MaxMemoryPages, Config: opts.Config,
	}, funcName, input)
}
