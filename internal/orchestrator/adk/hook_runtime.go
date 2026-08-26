package adk

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// HookTimeout is the hard per-call ceiling for a plugin hook invocation
// (spec-20 §4.4) — tighter than a tool's own 30s ceiling (extism.
// DefaultTimeoutMS), because a hook runs on every tool call and every
// response, not just when the model chooses to call a tool.
const HookTimeout = 5 * time.Second

// invokeHook calls one plugin hook's wasm function with input marshaled to
// JSON, enforcing HookTimeout. Any failure — timeout, a wasm trap, a
// non-JSON response — is treated as "this hook didn't run" rather than a
// run failure (spec-20 §4.4's "失败降级"): the caller proceeds as if the
// hook had never been declared, and the tool call or response continues
// unmodified.
func invokeHook(ctx context.Context, reg HookRegistration, runtime PluginRuntime, input any) (map[string]any, bool) {
	if runtime == nil || len(reg.WasmBytes) == 0 || reg.FuncName == "" {
		return nil, false
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, HookTimeout)
	defer cancel()
	opts := PluginRuntimeOptions{PluginID: reg.PluginID}
	out, err := runtime.Call(ctx, reg.PluginID+"@"+reg.Version, reg.WasmBytes, opts, reg.FuncName, body)
	if err != nil {
		return nil, false
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, false
	}
	return result, true
}

// applyHookCallbacks wires compileHooks' per-point registrations into
// ADK's llmagent lifecycle callbacks (spec-20 §4.4): before/after a tool
// call, before/after the model produces a response. A hook that didn't
// declare the write permission its point requires (HookRegistration.
// CanWrite) still runs — for whatever side effect it has via its own
// connectors/kv access — but its return value is never applied.
func applyHookCallbacks(cfg *llmagent.Config, hooks []HookRegistration, runtime PluginRuntime) {
	byPoint := make(map[string]HookRegistration, len(hooks))
	for _, h := range hooks {
		byPoint[h.Point] = h
	}

	if reg, ok := byPoint["before_tool_call"]; ok {
		cfg.BeforeToolCallbacks = append(cfg.BeforeToolCallbacks, func(ctx agent.ToolContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			result, ran := invokeHook(ctx, reg, runtime, map[string]any{"tool": t.Name(), "args": args})
			if !ran || !reg.CanWrite() {
				return nil, nil
			}
			if newArgs, ok := result["args"].(map[string]any); ok {
				for k, v := range newArgs {
					args[k] = v
				}
			}
			return nil, nil
		})
	}

	if reg, ok := byPoint["after_tool_call"]; ok {
		cfg.AfterToolCallbacks = append(cfg.AfterToolCallbacks, func(ctx agent.ToolContext, t tool.Tool, args, result map[string]any, callErr error) (map[string]any, error) {
			out, ran := invokeHook(ctx, reg, runtime, map[string]any{"tool": t.Name(), "args": args, "result": result})
			if !ran || !reg.CanWrite() {
				return nil, callErr
			}
			if newResult, ok := out["result"].(map[string]any); ok {
				return newResult, nil
			}
			return nil, callErr
		})
	}

	if reg, ok := byPoint["on_error"]; ok {
		cfg.OnToolErrorCallbacks = append(cfg.OnToolErrorCallbacks, func(ctx agent.ToolContext, t tool.Tool, args map[string]any, callErr error) (map[string]any, error) {
			out, ran := invokeHook(ctx, reg, runtime, map[string]any{"tool": t.Name(), "args": args, "error": callErr.Error()})
			if !ran || !reg.CanWrite() {
				return nil, callErr
			}
			if newResult, ok := out["result"].(map[string]any); ok {
				return newResult, nil
			}
			return nil, callErr
		})
	}

	if reg, ok := byPoint["before_response"]; ok {
		cfg.BeforeModelCallbacks = append(cfg.BeforeModelCallbacks, func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			_, _ = invokeHook(ctx, reg, runtime, map[string]any{"model": req.Model})
			// before_response has nothing meaningful to rewrite without a
			// candidate response yet to hand back in its place — it runs
			// for its side effects (kv/connector access, observation),
			// never to short-circuit the actual model call.
			return nil, nil
		})
	}

	if reg, ok := byPoint["after_response"]; ok {
		cfg.AfterModelCallbacks = append(cfg.AfterModelCallbacks, func(ctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
			if resp == nil {
				return nil, respErr
			}
			text := contentText(resp.Content)
			out, ran := invokeHook(ctx, reg, runtime, map[string]any{"text": text})
			if !ran || !reg.CanWrite() {
				return nil, respErr
			}
			newText, ok := out["text"].(string)
			if !ok || newText == "" {
				return nil, respErr
			}
			rewritten := *resp
			rewritten.Content = genai.NewContentFromText(newText, genai.RoleModel)
			return &rewritten, respErr
		})
	}
}
