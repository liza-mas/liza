package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

type LLMAgentEventKind string

const (
	LLMAgentEventStarted     LLMAgentEventKind = "started"
	LLMAgentEventOutputChunk LLMAgentEventKind = "output_chunk"
	LLMAgentEventMessage     LLMAgentEventKind = "agent_message_chunk"
	LLMAgentEventThought     LLMAgentEventKind = "agent_thought_chunk"
	LLMAgentEventTrajectory  LLMAgentEventKind = "trajectory"
	LLMAgentEventToolCall    LLMAgentEventKind = "tool_call"
	LLMAgentEventToolUpdate  LLMAgentEventKind = "tool_call_update"
	LLMAgentEventUsage       LLMAgentEventKind = "usage"
	LLMAgentEventCompleted   LLMAgentEventKind = "completed"
)

// LLMAgentEvent is the provider-neutral observability surface for an agent run.
//
// CLIAgent emits process lifecycle and output chunks. A future ACPAgent can emit
// richer ACP trajectory, delta, and tool-call events through the same sink.
type LLMAgentEvent struct {
	Time        time.Time
	Kind        LLMAgentEventKind
	BackendName string
	AgentID     string
	TaskID      string
	SessionID   string
	Message     string
	Payload     map[string]any
}

type LLMAgentEventSink interface {
	RecordLLMAgentEvent(ctx context.Context, event LLMAgentEvent)
}

type LLMAgentEventFunc func(ctx context.Context, event LLMAgentEvent)

func (f LLMAgentEventFunc) RecordLLMAgentEvent(ctx context.Context, event LLMAgentEvent) {
	f(ctx, event)
}

// LLMAgentRunRequest contains parameters for running one LLM agent turn.
type LLMAgentRunRequest struct {
	BackendName    string
	AgentID        string
	TaskID         string
	SessionID      string
	ResumeSession  string
	WarmSession    bool
	ProfileName    string
	ProfileVars    map[string]string
	Prompt         string
	PromptFile     string
	ProjectRoot    string
	AdditionalDirs []string
	RuntimeConfig  models.Config
	EventSink      LLMAgentEventSink
}

// LLMAgentRunResult contains the result of an LLM agent execution.
type LLMAgentRunResult struct {
	ExitCode int
	Output   string
	Usage    LLMAgentUsage
	// WarmUsage indicates whether this run reused warm session state.
	WarmUsage bool
	// SessionID is the active session for this run.
	SessionID string
}

// LLMAgentUsage tracks provider token consumption for a single run.
type LLMAgentUsage struct {
	InputTokens       int
	OutputTokens      int
	CachedReadTokens  int
	CachedWriteTokens int
}

// LLMAgentInteractiveRequest contains parameters for interactive mode.
type LLMAgentInteractiveRequest struct {
	BackendName    string
	AgentID        string
	SessionID      string
	ProfileName    string
	ProfileVars    map[string]string
	ProjectRoot    string
	AdditionalDirs []string
	RuntimeConfig  models.Config
	EventSink      LLMAgentEventSink
}

// LLMAgent is the semantic boundary for something that can run an LLM agent turn.
//
// The OSS implementation is CLIAgent. Omni can provide an ACPAgent in its fork
// without changing supervisor execution semantics.
type LLMAgent interface {
	Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error)
	RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (exitCode int, err error)
}

// CLIExecutionResult is the legacy result struct name.
//
// Deprecated: use LLMAgentRunResult.
type CLIExecutionResult = LLMAgentRunResult

// CLIExecutor is the legacy CLI executor interface.
//
// Deprecated: implement LLMAgent directly.
type CLIExecutor interface {
	Execute(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string, runtimeConfig models.Config) (CLIExecutionResult, error)
	ExecuteInteractive(ctx context.Context, cliName string, agentID string, projectRoot string, additionalDirs []string) (exitCode int, err error)
}

type legacyCLIExecutorAdapter struct {
	executor CLIExecutor
}

func (a legacyCLIExecutorAdapter) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	return a.executor.Execute(ctx, req.BackendName, req.AgentID, req.Prompt, req.ProjectRoot, req.AdditionalDirs, req.RuntimeConfig)
}

func (a legacyCLIExecutorAdapter) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	return a.executor.ExecuteInteractive(ctx, req.BackendName, req.AgentID, req.ProjectRoot, req.AdditionalDirs)
}

func resolveLLMAgent(config SupervisorConfig) (LLMAgent, error) {
	if config.LLMAgent != nil {
		return config.LLMAgent, nil
	}
	if config.Executor != nil {
		return legacyCLIExecutorAdapter{executor: config.Executor}, nil
	}
	return nil, fmt.Errorf("no LLM agent configured")
}

func emitLLMAgentEvent(ctx context.Context, sink LLMAgentEventSink, event LLMAgentEvent) {
	if sink == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	sink.RecordLLMAgentEvent(ctx, event)
}
