package adk

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// Event is the platform-facing shape AgentRunner.Run streams through emit
// — the same POC-validated contract spec-10 §4 asks to keep as the
// adapter boundary, so orchestration logic (and its unit tests) can run
// against a Mock implementation without a real ADK/model round-trip.
type Event = PlatformEvent

// AgentInput starts one run: which user/session it belongs to (ADK's
// session.Service keys on both) and the initial message.
type AgentInput struct {
	UserID    string
	SessionID string
	Message   string
}

// AgentOutput is what a completed run produced, once its root agent has no
// more events to emit.
type AgentOutput struct {
	FinalText string
}

// AgentRunner is the adapter boundary spec-10 §4 requires: "保留 POC 验证
// 过的 AgentRunner 接口作为适配层，ADK 实现放在其后...这样单测可以用 Mock
// 实现跑通编排逻辑，不必真的调模型". ADKRunner is the only implementation
// that actually imports ADK; everything above this interface (run-engine
// handlers, task 11) can be tested against a hand-written mock instead.
type AgentRunner interface {
	Run(ctx context.Context, in AgentInput, emit func(Event)) (AgentOutput, error)
}

// ADKRunner drives a compiled root agent (CompileAgent/CompileBundle's
// output) through a real ADK runner.Runner, translating every session.Event
// it produces via TranslateEvent before handing it to emit.
type ADKRunner struct {
	AppName        string
	Agent          agent.Agent
	SessionService session.Service
	// MemoryService backs the load_memory/preload_memory builtin tools
	// (capabilities.builtin_tools). Nil defaults to an in-process
	// InMemoryService — same durability tradeoff as the default
	// SessionService below; a real persisted implementation is a
	// "memory" resource concern (internal/domain/knowledgebase's sibling),
	// injected here the same way a real SessionService eventually would be.
	MemoryService memory.Service
}

// NewADKRunner builds an ADKRunner backed by ADK's in-memory session
// service. Durable, restart-survivable session persistence (spec-10's
// acceptance item "重启进程后运行仍能从暂停处恢复") is a spec-11 concern —
// it requires a custom session.Service backed by this platform's own
// tables, built alongside the run engine that owns bundle_runs.
func NewADKRunner(appName string, root agent.Agent) (*ADKRunner, error) {
	return &ADKRunner{AppName: appName, Agent: root, SessionService: session.InMemoryService()}, nil
}

func (r *ADKRunner) Run(ctx context.Context, in AgentInput, emit func(Event)) (AgentOutput, error) {
	memSvc := r.MemoryService
	if memSvc == nil {
		memSvc = memory.InMemoryService()
	}
	rn, err := runner.New(runner.Config{
		AppName: r.AppName, Agent: r.Agent, SessionService: r.SessionService,
		MemoryService: memSvc, AutoCreateSession: true,
	})
	if err != nil {
		return AgentOutput{}, fmt.Errorf("adk: build runner: %w", err)
	}

	msg := genai.NewContentFromText(in.Message, genai.RoleUser)
	var out AgentOutput
	for ev, err := range rn.Run(ctx, in.UserID, in.SessionID, msg, agent.RunConfig{}) {
		if err != nil {
			return out, err
		}
		for _, pe := range TranslateEvent(ev.Author, ev) {
			if emit != nil {
				emit(pe)
			}
			if pe.Type == EventNodeFinished {
				if text, ok := pe.Payload["text"].(string); ok {
					out.FinalText = text
				}
			}
		}
	}

	// The runner doesn't do this automatically (per ADK's own examples) —
	// without it, load_memory would only ever search an empty store no
	// matter how many turns had already happened.
	if resp, err := r.SessionService.Get(ctx, &session.GetRequest{AppName: r.AppName, UserID: in.UserID, SessionID: in.SessionID}); err == nil && resp.Session != nil {
		_ = memSvc.AddSessionToMemory(ctx, resp.Session)
	}
	return out, nil
}
