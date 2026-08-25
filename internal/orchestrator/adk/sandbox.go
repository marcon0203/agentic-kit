package adk

import (
	"context"
	"fmt"
	"sync"
	"time"

	daytona "github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	daytonaoptions "github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	daytonatypes "github.com/daytonaio/daytona/libs/sdk-go/pkg/types"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	// sandboxAutoStopMinutes/sandboxAutoDeleteMinutes bound how long an idle
	// Daytona sandbox this package created survives if the process that
	// created it never gets to clean it up itself (a crashed run, a killed
	// process) — Daytona's own server-side auto-stop/auto-delete chain is
	// the backstop, not something wired into run lifecycle. A sandbox
	// auto-stops after sandboxAutoStopMinutes of inactivity, then
	// auto-deletes sandboxAutoDeleteMinutes after that.
	sandboxAutoStopMinutes   = 15
	sandboxAutoDeleteMinutes = 15
	sandboxCreateTimeout     = 60 * time.Second
)

// BuildSandboxTools constructs the two ADK tools a "sandbox" component
// resource exposes — run_code (an interpreter session for Python/
// JavaScript/TypeScript) and execute_command (a raw shell command) — the
// same two primitives Daytona's own ADK plugin exposes for Python ADK.
// This package's Go equivalent is hand-built directly on Daytona's Go SDK
// (google.golang.org/adk has no first-party Daytona integration), using
// the same "wrap an external service as a functiontool" pattern as
// buildEndpointTool/BuildMCPToolset.
//
// Both tools share one lazily-created Daytona sandbox per compiled agent
// (see sandboxSession below): the first call pays sandbox startup cost,
// every later call in the same run reuses it instead of spinning up a new
// sandbox per tool call.
func BuildSandboxTools(spec ToolSpec) ([]tool.Tool, error) {
	apiURL, _ := spec.Config["api_url"].(string)
	apiKey, _ := spec.Config["api_key"].(string)
	organizationID, _ := spec.Config["organization_id"].(string)
	if apiURL == "" {
		return nil, fmt.Errorf("resource %q: sandbox component has no api_url configured", spec.Ref)
	}

	sess := &sandboxSession{ref: spec.Ref, apiURL: apiURL, apiKey: apiKey, organizationID: organizationID}

	runCode, err := functiontool.New(functiontool.Config{
		Name:        spec.Ref + "_run_code",
		Description: fmt.Sprintf("Runs Python, JavaScript, or TypeScript code in the %q sandbox and returns its output.", spec.Ref),
	}, sess.runCode)
	if err != nil {
		return nil, fmt.Errorf("resource %q: build run_code tool: %w", spec.Ref, err)
	}

	executeCommand, err := functiontool.New(functiontool.Config{
		Name:        spec.Ref + "_execute_command",
		Description: fmt.Sprintf("Runs a shell command in the %q sandbox and returns its exit code and output.", spec.Ref),
	}, sess.executeCommand)
	if err != nil {
		return nil, fmt.Errorf("resource %q: build execute_command tool: %w", spec.Ref, err)
	}

	return []tool.Tool{runCode, executeCommand}, nil
}

// sandboxSession lazily creates one Daytona sandbox on first use and reuses
// it for every later tool call from the same compiled agent. A fresh
// sandbox per call would make every single tool call pay Daytona's
// multi-second startup latency for no reason within one run.
type sandboxSession struct {
	ref            string
	apiURL         string
	apiKey         string
	organizationID string

	mu      sync.Mutex
	sandbox *daytona.Sandbox
}

func (s *sandboxSession) get(ctx context.Context) (*daytona.Sandbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sandbox != nil {
		return s.sandbox, nil
	}

	client, err := daytona.NewClientWithConfig(&daytonatypes.DaytonaConfig{
		APIKey: s.apiKey, APIUrl: s.apiURL, OrganizationID: s.organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox %q: connect to Daytona: %w", s.ref, err)
	}

	autoStop, autoDelete := sandboxAutoStopMinutes, sandboxAutoDeleteMinutes
	createCtx, cancel := context.WithTimeout(ctx, sandboxCreateTimeout)
	defer cancel()
	// Passing types.SnapshotParams (rather than a bare SandboxBaseParams)
	// is what makes Create honor these fields at all — its params any
	// argument only reads AutoStopInterval/AutoDeleteInterval/etc. off a
	// SnapshotParams or ImageParams, anything else falls through to
	// all-defaults. An empty Snapshot means "use the server's default
	// base image", which is what we want here — this isn't a
	// snapshot-pinned sandbox.
	sandbox, err := client.Create(createCtx, daytonatypes.SnapshotParams{
		SandboxBaseParams: daytonatypes.SandboxBaseParams{
			Language:           daytonatypes.CodeLanguagePython,
			AutoStopInterval:   &autoStop,
			AutoDeleteInterval: &autoDelete,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox %q: create Daytona sandbox: %w", s.ref, err)
	}

	s.sandbox = sandbox
	return sandbox, nil
}

type sandboxRunCodeArgs struct {
	Code     string `json:"code" jsonschema_description:"The Python, JavaScript, or TypeScript source code to run."`
	Language string `json:"language,omitempty" jsonschema_description:"One of python, javascript, typescript. Defaults to python."`
}

type sandboxExecArgs struct {
	Command string `json:"command" jsonschema_description:"The shell command to run in the sandbox."`
	Cwd     string `json:"cwd,omitempty" jsonschema_description:"Working directory for the command. Defaults to the sandbox's home directory."`
}

type sandboxExecResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func (s *sandboxSession) runCode(ctx agent.ToolContext, args sandboxRunCodeArgs) (sandboxExecResult, error) {
	sandbox, err := s.get(ctx)
	if err != nil {
		return sandboxExecResult{}, err
	}

	var opts []func(*daytonaoptions.CodeRun)
	if args.Language != "" {
		opts = append(opts, daytonaoptions.WithCodeRunLanguage(daytonatypes.CodeLanguage(args.Language)))
	}
	resp, err := sandbox.Process.CodeRun(ctx, args.Code, opts...)
	if err != nil {
		return sandboxExecResult{}, fmt.Errorf("sandbox %q: run_code: %w", s.ref, err)
	}
	return sandboxExecResult{ExitCode: resp.ExitCode, Output: resp.Result}, nil
}

func (s *sandboxSession) executeCommand(ctx agent.ToolContext, args sandboxExecArgs) (sandboxExecResult, error) {
	sandbox, err := s.get(ctx)
	if err != nil {
		return sandboxExecResult{}, err
	}

	var opts []func(*daytonaoptions.ExecuteCommand)
	if args.Cwd != "" {
		opts = append(opts, daytonaoptions.WithCwd(args.Cwd))
	}
	resp, err := sandbox.Process.ExecuteCommand(ctx, args.Command, opts...)
	if err != nil {
		return sandboxExecResult{}, fmt.Errorf("sandbox %q: execute_command: %w", s.ref, err)
	}
	return sandboxExecResult{ExitCode: resp.ExitCode, Output: resp.Result}, nil
}
