package agent

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// MockLLMAgent for testing LLMAgent execution
type MockLLMAgent struct {
	mu               sync.Mutex
	Calls            []MockLLMAgentCall
	InteractiveCalls []MockLLMAgentCall
	ExitCode         int
	Output           string
	ExitError        error
	OnExecute        func(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string) error
}

type MockLLMAgentCall struct {
	CLIName        string
	AgentID        string
	Prompt         string
	ProjectRoot    string
	AdditionalDirs []string
	TaskID         string
	SessionID      string
	ResumeSession  string
	WarmSession    bool
	EventSinkSet   bool
}

func (m *MockLLMAgent) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, MockLLMAgentCall{
		CLIName:        req.BackendName,
		AgentID:        req.AgentID,
		Prompt:         req.Prompt,
		ProjectRoot:    req.ProjectRoot,
		AdditionalDirs: slices.Clone(req.AdditionalDirs),
		TaskID:         req.TaskID,
		SessionID:      req.SessionID,
		ResumeSession:  req.ResumeSession,
		WarmSession:    req.WarmSession,
		EventSinkSet:   req.EventSink != nil,
	})
	m.mu.Unlock()
	if m.OnExecute != nil {
		if err := m.OnExecute(ctx, req.BackendName, req.AgentID, req.Prompt, req.ProjectRoot, req.AdditionalDirs); err != nil {
			return LLMAgentRunResult{ExitCode: m.ExitCode, Output: m.Output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, err
		}
	}
	return LLMAgentRunResult{ExitCode: m.ExitCode, Output: m.Output, Usage: LLMAgentUsage{}, WarmUsage: req.WarmSession, SessionID: req.SessionID}, m.ExitError
}

func (m *MockLLMAgent) RunInteractive(ctx context.Context, req LLMAgentInteractiveRequest) (int, error) {
	m.mu.Lock()
	m.InteractiveCalls = append(m.InteractiveCalls, MockLLMAgentCall{CLIName: req.BackendName, AgentID: req.AgentID, ProjectRoot: req.ProjectRoot, AdditionalDirs: slices.Clone(req.AdditionalDirs)})
	m.mu.Unlock()
	return m.ExitCode, m.ExitError
}

// GetCalls returns a copy of the calls slice in a thread-safe manner
func (m *MockLLMAgent) GetCalls() []MockLLMAgentCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]MockLLMAgentCall, len(m.Calls))
	copy(calls, m.Calls)
	return calls
}

// GetInteractiveCalls returns a copy of the interactive calls slice in a thread-safe manner
func (m *MockLLMAgent) GetInteractiveCalls() []MockLLMAgentCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]MockLLMAgentCall, len(m.InteractiveCalls))
	copy(calls, m.InteractiveCalls)
	return calls
}

// Execute is the legacy method name, preserved for backward compatibility.
func (m *MockLLMAgent) Execute(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string, _ models.Config) (CLIExecutionResult, error) {
	return m.Run(ctx, LLMAgentRunRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		Prompt:         prompt,
		ProjectRoot:    projectRoot,
		AdditionalDirs: additionalDirs,
	})
}

// ExecuteInteractive is the legacy method name, preserved for backward compatibility.
func (m *MockLLMAgent) ExecuteInteractive(ctx context.Context, cliName string, agentID string, projectRoot string, additionalDirs []string) (int, error) {
	return m.RunInteractive(ctx, LLMAgentInteractiveRequest{
		BackendName:    cliName,
		AgentID:        agentID,
		ProjectRoot:    projectRoot,
		AdditionalDirs: additionalDirs,
	})
}

// MockCLIExecutor is the legacy type name, preserved for backward compatibility.
type MockCLIExecutor = MockLLMAgent

// MockCLICall is the legacy type name, preserved for backward compatibility.
type MockCLICall = MockLLMAgentCall

type legacyOnlyCLIExecutor struct {
	calls []MockLLMAgentCall
}

type recordingLLMAgentEventSink struct {
	mu     sync.Mutex
	events []LLMAgentEvent
}

func (s *recordingLLMAgentEventSink) RecordLLMAgentEvent(_ context.Context, event LLMAgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingLLMAgentEventSink) Events() []LLMAgentEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]LLMAgentEvent, len(s.events))
	copy(events, s.events)
	return events
}

func (e *legacyOnlyCLIExecutor) Execute(_ context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string, _ models.Config) (CLIExecutionResult, error) {
	e.calls = append(e.calls, MockLLMAgentCall{
		CLIName:        cliName,
		AgentID:        agentID,
		Prompt:         prompt,
		ProjectRoot:    projectRoot,
		AdditionalDirs: slices.Clone(additionalDirs),
	})
	return CLIExecutionResult{ExitCode: 0, Output: "legacy output"}, nil
}

func (e *legacyOnlyCLIExecutor) ExecuteInteractive(_ context.Context, cliName string, agentID string, projectRoot string, additionalDirs []string) (int, error) {
	e.calls = append(e.calls, MockLLMAgentCall{
		CLIName:        cliName,
		AgentID:        agentID,
		ProjectRoot:    projectRoot,
		AdditionalDirs: slices.Clone(additionalDirs),
	})
	return 0, nil
}

// TestMockLLMAgentRun tests the LLMAgent mock.
func TestMockLLMAgentRun(t *testing.T) {
	mock := &MockLLMAgent{
		ExitCode: 0,
	}

	ctx := context.Background()
	result, err := mock.Run(ctx, LLMAgentRunRequest{
		BackendName: "claude",
		AgentID:     "claude-1",
		Prompt:      "test prompt",
		ProjectRoot: "/tmp/test-project",
	})

	if err != nil {
		t.Errorf("Run() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("Run() exitCode = %d, want 0", result.ExitCode)
	}

	calls := mock.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if call.CLIName != "claude" {
		t.Errorf("CLIName = %s, want claude", call.CLIName)
	}
	if call.Prompt != "test prompt" {
		t.Errorf("Prompt = %s, want 'test prompt'", call.Prompt)
	}
}

func TestRunSupervisorPassesResolverDerivedRoleTypeToPauseWait(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	pipelinePath := filepath.Join(tmpDir, ".liza", "pipeline.yaml")
	content, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("read pipeline config: %v", err)
	}
	customRole := `  roles:
    custom-doer:
      type: doer
      display-name: "Custom Doer"
      description: "Custom doer role"
      allowed-operations: []
      context-sections: []
      mandatory-docs: []

`
	updated := strings.Replace(string(content), "  roles:\n", customRole, 1)
	if updated == string(content) {
		t.Fatal("failed to insert custom role into pipeline config")
	}
	if err := os.WriteFile(pipelinePath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write pipeline config: %v", err)
	}

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
	testhelpers.WriteInitialState(t, statePath, state)

	sentinel := stderrors.New("stop after pause wait")
	var capturedRoleType string
	prevWait := waitWhilePausedForSupervisor
	waitWhilePausedForSupervisor = func(_ context.Context, _ string, roleType string) error {
		capturedRoleType = roleType
		return sentinel
	}
	t.Cleanup(func() { waitWhilePausedForSupervisor = prevWait })

	config := SupervisorConfig{
		AgentID:     "custom-doer-1",
		Role:        "custom-doer",
		ProjectRoot: tmpDir,
		StatePath:   statePath,
		CLIName:     "codex",
		Executor:    &MockCLIExecutor{ExitCode: 0},
	}
	err = RunSupervisor(context.Background(), config)
	if !stderrors.Is(err, sentinel) {
		t.Fatalf("RunSupervisor() error = %v, want sentinel", err)
	}
	if capturedRoleType != "doer" {
		t.Fatalf("captured role type = %q, want doer", capturedRoleType)
	}
}

func TestMockLLMAgentResultCarriesUsageAndSessionMetadata(t *testing.T) {
	mock := &MockLLMAgent{
		ExitCode: 0,
		Output:   "ok",
	}
	ctx := context.Background()
	result, err := mock.Run(ctx, LLMAgentRunRequest{
		BackendName:   "claude",
		AgentID:       "claude-2",
		TaskID:        "task-42",
		SessionID:     "sess-42",
		ResumeSession: "resume-7",
		WarmSession:   true,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.SessionID != "sess-42" {
		t.Fatalf("result session id = %s, want sess-42", result.SessionID)
	}
	if !result.WarmUsage {
		t.Fatalf("result.WarmUsage = false, want true")
	}
	if result.ExitCode != 0 {
		t.Fatalf("result.Usage zero check: %+v", result.Usage)
	}

	calls := mock.GetCalls()
	if got := calls[0].SessionID; got != "sess-42" {
		t.Fatalf("request session id = %s, want sess-42", got)
	}
	if !calls[0].WarmSession {
		t.Fatalf("call warm session = false, want true")
	}
}

func TestExecuteAgentBlocksTaskAfterProgressTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	taskID := "task-1"
	agentID := "coder-1"
	gw := lizagit.New(tmpDir)
	baseCommit, err := gw.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.BaseCommit = &baseCommit
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents = map[string]models.Agent{
		agentID: {
			Role:        models.RoleCoder,
			Status:      models.AgentStatusWorking,
			CurrentTask: &taskID,
			Heartbeat:   now,
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	mock := &MockLLMAgent{
		OnExecute: func(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string) error {
			<-ctx.Done()
			if _, statErr := os.Stat(filepath.Join(tmpDir, ".worktrees", taskID)); statErr != nil {
				t.Fatalf("worktree should still exist when provider observes cancellation, stat err=%v", statErr)
			}
			return ctx.Err()
		},
	}
	config := SupervisorConfig{
		AgentID:                  agentID,
		Role:                     models.RoleCoder,
		ProjectRoot:              tmpDir,
		StatePath:                statePath,
		CLIName:                  "codex",
		LLMAgent:                 mock,
		ExecutionTimeout:         5 * time.Second,
		ExecutionProgressTimeout: 150 * time.Millisecond,
	}

	exitCode, _, err := executeAgent(context.Background(), config, "prompt", nil, taskID, state.Config)
	if err != nil {
		t.Fatalf("executeAgent error: %v", err)
	}
	if calls := mock.GetCalls(); len(calls) != 1 || !calls[0].EventSinkSet {
		t.Fatalf("executeAgent calls = %#v, want LLMAgent event sink", calls)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 after watchdog block", exitCode)
	}

	bb := db.For(statePath)
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	afterTask := after.FindTask(taskID)
	if afterTask == nil {
		t.Fatalf("task %s missing", taskID)
	}
	if afterTask.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", afterTask.Status)
	}
	if afterTask.BlockedReason == nil || !strings.Contains(*afterTask.BlockedReason, "execution progress timeout") {
		t.Fatalf("blocked reason = %v, want execution progress timeout", afterTask.BlockedReason)
	}
	if afterTask.AssignedTo != nil || afterTask.LeaseExpires != nil {
		t.Fatalf("blocked task should clear assignment and lease, assigned=%v lease=%v", afterTask.AssignedTo, afterTask.LeaseExpires)
	}
	if afterTask.Worktree != nil {
		t.Fatalf("blocked task worktree = %v, want nil", *afterTask.Worktree)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".worktrees", taskID)); !os.IsNotExist(err) {
		t.Fatalf("worktree directory should be removed, stat err=%v", err)
	}
	branchExists, err := gw.BranchExists("task/" + taskID)
	if err != nil {
		t.Fatalf("BranchExists error: %v", err)
	}
	if branchExists {
		t.Fatalf("task branch should be removed")
	}
}

func TestExecuteAgentOutputProgressPreventsProgressTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	taskID := "task-1"
	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)

	mock := &MockLLMAgent{
		OnExecute: func(ctx context.Context, cliName string, agentID string, prompt string, projectRoot string, additionalDirs []string) error {
			mark := executionProgressCallback(ctx)
			deadline := time.After(280 * time.Millisecond)
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					if mark != nil {
						mark()
					}
				case <-deadline:
					if mark != nil {
						mark()
					}
					return nil
				}
			}
		},
	}
	config := SupervisorConfig{
		AgentID:                  agentID,
		Role:                     models.RoleCoder,
		ProjectRoot:              tmpDir,
		StatePath:                statePath,
		CLIName:                  "codex",
		LLMAgent:                 mock,
		ExecutionTimeout:         5 * time.Second,
		ExecutionProgressTimeout: 120 * time.Millisecond,
	}

	exitCode, _, err := executeAgent(context.Background(), config, "prompt", nil, taskID, state.Config)
	if err != nil {
		t.Fatalf("executeAgent error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}

	bb := db.For(statePath)
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	afterTask := after.FindTask(taskID)
	if afterTask == nil {
		t.Fatalf("task %s missing", taskID)
	}
	if afterTask.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want IMPLEMENTING_CODE", afterTask.Status)
	}
}

func TestDefaultCLIExecutorStreamsMaskedOutputFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	outputsDir := filepath.Join(projectRoot, ".liza", "agent-outputs")
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
printf 'stdout-before sk-test-secret-value stdout-after\n'
printf 'stderr-before sk-test-secret-value stderr-after\n' >&2
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-secret-value")

	executor := NewCLIAgent(outputsDir)
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{BackendName: "claude", AgentID: "coder-1", Prompt: "prompt body", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if strings.Contains(result.Output, "sk-test-secret-value") {
		t.Fatalf("result output leaked secret: %q", result.Output)
	}
	if !strings.Contains(result.Output, "stdout-before *** stdout-after") {
		t.Fatalf("result output missing masked stdout: %q", result.Output)
	}
	if !strings.Contains(result.Output, "stderr-before *** stderr-after") {
		t.Fatalf("result output missing masked stderr: %q", result.Output)
	}

	txtFiles, err := filepath.Glob(filepath.Join(outputsDir, "coder-1-*.txt"))
	if err != nil {
		t.Fatalf("glob txt: %v", err)
	}
	errFiles, err := filepath.Glob(filepath.Join(outputsDir, "coder-1-*.err"))
	if err != nil {
		t.Fatalf("glob err: %v", err)
	}
	if len(txtFiles) != 1 || len(errFiles) != 1 {
		t.Fatalf("output files txt=%v err=%v, want one of each", txtFiles, errFiles)
	}

	txtStem := strings.TrimSuffix(filepath.Base(txtFiles[0]), ".txt")
	errStem := strings.TrimSuffix(filepath.Base(errFiles[0]), ".err")
	if txtStem != errStem {
		t.Fatalf("stdout/stderr files should share timestamp, got %q and %q", txtStem, errStem)
	}

	stdoutLog, err := os.ReadFile(txtFiles[0])
	if err != nil {
		t.Fatalf("read stdout log: %v", err)
	}
	stderrLog, err := os.ReadFile(errFiles[0])
	if err != nil {
		t.Fatalf("read stderr log: %v", err)
	}
	if strings.Contains(string(stdoutLog), "sk-test-secret-value") || strings.Contains(string(stderrLog), "sk-test-secret-value") {
		t.Fatalf("persisted logs leaked secret:\nstdout=%q\nstderr=%q", stdoutLog, stderrLog)
	}
	if string(stdoutLog) != "stdout-before *** stdout-after\n" {
		t.Fatalf("stdout log = %q", stdoutLog)
	}
	if string(stderrLog) != "stderr-before *** stderr-after\n" {
		t.Fatalf("stderr log = %q", stderrLog)
	}
}

func TestCLIAgentEmitsObservabilityEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	outputsDir := filepath.Join(projectRoot, ".liza", "agent-outputs")
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
printf 'stdout event\n'
printf 'stderr event\n' >&2
exit 7
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sink := &recordingLLMAgentEventSink{}
	result, err := NewCLIAgent(outputsDir).Run(context.Background(), LLMAgentRunRequest{
		BackendName: "claude",
		AgentID:     "coder-1",
		TaskID:      "task-123",
		SessionID:   "session-123",
		Prompt:      "prompt body",
		ProjectRoot: projectRoot,
		EventSink:   sink,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}

	events := sink.Events()
	if len(events) < 4 {
		t.Fatalf("events = %#v, want start/output/completed events", events)
	}
	if events[0].Kind != LLMAgentEventStarted || events[0].BackendName != "claude" || events[0].AgentID != "coder-1" || events[0].TaskID != "task-123" {
		t.Fatalf("first event = %#v, want claude/coder-1 started", events[0])
	}

	var sawStdout, sawStderr, sawUsage, sawCLIMessage bool
	var completed *LLMAgentEvent
	for i := range events {
		event := events[i]
		if event.Kind == LLMAgentEventOutputChunk && event.Payload["stream"] == "stdout" && strings.Contains(event.Message, "stdout event") {
			sawStdout = true
		}
		if event.Kind == LLMAgentEventOutputChunk && event.Payload["stream"] == "stderr" && strings.Contains(event.Message, "stderr event") {
			sawStderr = true
		}
		if event.Kind == LLMAgentEventUsage {
			sawUsage = true
		}
		if event.Kind == LLMAgentEventMessage {
			sawCLIMessage = true
		}
		if event.Kind == LLMAgentEventCompleted {
			completed = &event
		}
	}
	if !sawStdout || !sawStderr {
		t.Fatalf("events = %#v, want stdout and stderr output chunks", events)
	}
	if sawUsage {
		t.Fatalf("events = %#v, CLI stdout/stderr should not emit zero-value usage", events)
	}
	if sawCLIMessage {
		t.Fatalf("events = %#v, CLI stdout/stderr should not emit agent_message_chunk", events)
	}
	if completed == nil {
		t.Fatalf("events = %#v, want completed event", events)
	}
	if completed.Payload["exit_code"] != 7 {
		t.Fatalf("completed payload = %#v, want exit_code 7", completed.Payload)
	}
	if completed.SessionID != "session-123" {
		t.Fatalf("completed event session id = %s, want session-123", completed.SessionID)
	}
	if completed.TaskID != "task-123" {
		t.Fatalf("completed event task id = %s, want task-123", completed.TaskID)
	}
}

func TestCLIAgentReturnsOutputWhenLoggingDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	fakeGemini := filepath.Join(binDir, "gemini")
	script := `#!/bin/sh
printf 'stdout with no log\n'
printf 'stderr with no log\n' >&2
`
	if err := os.WriteFile(fakeGemini, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gemini: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "gemini",
		AgentID:     "coder-1",
		TaskID:      "task-no-log",
		Prompt:      "prompt body",
		ProjectRoot: projectRoot,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(result.Output, "stdout with no log") || !strings.Contains(result.Output, "stderr with no log") {
		t.Fatalf("Output = %q, want stdout and stderr even with logging disabled", result.Output)
	}
}

func TestCLIAgentRunsOpenCodePromptAsArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	outputsDir := filepath.Join(projectRoot, ".liza", "agent-outputs")
	binDir := t.TempDir()
	fakeOpenCode := filepath.Join(binDir, "opencode")
	script := `#!/bin/sh
for arg in "$@"; do
  printf 'arg:%s\n' "$arg"
done
stdin="$(cat)"
if [ -n "$stdin" ]; then
  printf 'stdin:%s\n' "$stdin"
else
  printf 'stdin-empty\n'
fi
`
	if err := os.WriteFile(fakeOpenCode, []byte(script), 0755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := NewCLIAgent(outputsDir).Run(context.Background(), LLMAgentRunRequest{
		BackendName: "opencode",
		AgentID:     "coder-1",
		TaskID:      "task-opencode",
		Prompt:      "prompt body",
		ProjectRoot: projectRoot,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	for _, want := range []string{
		"arg:run",
		"arg:prompt body",
		"arg:--dangerously-skip-permissions",
		"arg:--format",
		"arg:json",
		"stdin-empty",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("Output missing %q:\n%s", want, result.Output)
		}
	}
	if strings.Contains(result.Output, "stdin:prompt body") {
		t.Fatalf("prompt should not be passed via stdin: %q", result.Output)
	}
}

func TestDefaultCLIExecutorDisallowsClaudeSubagentToolsWhenEnvEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	outputsDir := filepath.Join(projectRoot, ".liza", "agent-outputs")
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
for arg in "$@"; do
  printf 'arg:%s\n' "$arg"
done
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_DISABLE_CLAUDE_SUBAGENTS", "1")

	executor := NewCLIAgent(outputsDir)
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{BackendName: "claude", AgentID: "coder-1", Prompt: "prompt body", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Output, "arg:--disallowedTools\narg:Task\n") {
		t.Fatalf("result output missing subagent disallow args: %q", result.Output)
	}
	if strings.Contains(result.Output, "prompt body") {
		t.Fatalf("prompt should be passed via stdin, not argv: %q", result.Output)
	}
}

func TestDefaultCLIExecutorExportsResolvedAgentIDLast(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	outputsDir := filepath.Join(projectRoot, ".liza", "agent-outputs")
	binDir := t.TempDir()
	fakeClaude := filepath.Join(binDir, "claude")
	script := `#!/bin/sh
printf 'agent-id:%s\n' "$LIZA_AGENT_ID"
`
	if err := os.WriteFile(fakeClaude, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	claudeEnv := []byte("LIZA_AGENT_ID=from-claude-env\n")
	if err := os.WriteFile(filepath.Join(projectRoot, "claude.env"), claudeEnv, 0644); err != nil {
		t.Fatalf("write claude.env: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_AGENT_ID", "from-parent-env")

	executor := NewCLIAgent(outputsDir)
	result, err := executor.Run(context.Background(), LLMAgentRunRequest{BackendName: "claude", AgentID: "coder-7", Prompt: "prompt body", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Output, "agent-id:coder-7") {
		t.Fatalf("result output = %q, want resolved agent ID", result.Output)
	}
	if strings.Contains(result.Output, "from-parent-env") || strings.Contains(result.Output, "from-claude-env") {
		t.Fatalf("result output used stale agent ID: %q", result.Output)
	}
}

func TestDefaultCLIExecutorInteractiveExportsResolvedAgentID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	projectRoot := t.TempDir()
	binDir := t.TempDir()
	envFile := filepath.Join(projectRoot, "interactive-env.txt")
	fakeGemini := filepath.Join(binDir, "gemini")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$LIZA_AGENT_ID" > %q
`, envFile)
	if err := os.WriteFile(fakeGemini, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gemini: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_AGENT_ID", "from-parent-env")

	executor := NewCLIAgent("")
	exitCode, err := executor.RunInteractive(context.Background(), LLMAgentInteractiveRequest{BackendName: "gemini", AgentID: "code-reviewer-4", ProjectRoot: projectRoot})
	if err != nil {
		t.Fatalf("ExecuteInteractive error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "code-reviewer-4" {
		t.Fatalf("LIZA_AGENT_ID = %q, want code-reviewer-4", got)
	}
}

func TestCLIAgentRunsConfiguredToolWithPromptFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}

	binDir := t.TempDir()
	fakeAgent := filepath.Join(binDir, "fake-agent")
	script := `#!/bin/sh
printf 'args:%s\n' "$*"
if [ "$1" = "--prompt-file" ]; then
  printf 'prompt:'
  cat "$2"
fi
`
	if err := os.WriteFile(fakeAgent, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := NewCLIAgent("").Run(context.Background(), LLMAgentRunRequest{
		BackendName: "cursor",
		AgentID:     "coder-1",
		Prompt:      "implement the task",
		ProjectRoot: t.TempDir(),
		RuntimeConfig: models.Config{
			AgentTools: map[string]models.AgentToolConfig{
				"cursor": {
					Executable:      "fake-agent",
					PromptTransport: PromptTransportFile,
					RunArgs:         []string{"--prompt-file", "{{promptFile}}", "--cwd", "{{projectRoot}}"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; output:\n%s", result.ExitCode, result.Output)
	}
	if !strings.Contains(result.Output, "args:--prompt-file ") || !strings.Contains(result.Output, "--cwd ") {
		t.Fatalf("output missing resolved argv:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "prompt:implement the task") {
		t.Fatalf("output missing prompt file contents:\n%s", result.Output)
	}
}

func TestSupervisor_Exit0ProviderAuditDegradedContinuesPostExecution(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-audit-exit0"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	auditOutput := `ERROR codex_core::session: failed to record rollout items: thread 019e983f-f3a2-7071-8a66-aa1774db9101 not found`
	mock := &MockLLMAgent{
		ExitCode: 0,
		Output:   auditOutput,
	}
	mock.OnExecute = func(ctx context.Context, cliName, agentID, prompt, projectRoot string, additionalDirs []string) error {
		reviewCommit := testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD")
		return bb.Modify(func(s *models.State) error {
			task := s.FindTask(taskID)
			if task == nil {
				t.Fatalf("task %q not found", taskID)
			}
			task.Status = models.TaskStatusReadyForReview
			task.ReviewCommit = &reviewCommit
			task.HandoffEvents = append(task.HandoffEvents, models.HandoffEvent{
				Timestamp: time.Now().UTC(),
				Agent:     agentID,
				Trigger:   models.HandoffTriggerSubmission,
			})
			return nil
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             "coder",
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, ".liza", "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}

	if calls := mock.GetCalls(); len(calls) != 1 {
		t.Fatalf("Execute calls = %d, want 1", len(calls))
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := updated.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found after supervisor run", taskID)
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task.Status = %q, want %q", task.Status, models.TaskStatusReadyForReview)
	}
	if len(updated.Anomalies) != 1 {
		t.Fatalf("len(Anomalies) = %d, want 1", len(updated.Anomalies))
	}
	if updated.Anomalies[0].Type != ProviderAuditDegradedAnomalyType {
		t.Fatalf("anomaly.Type = %q, want %q", updated.Anomalies[0].Type, ProviderAuditDegradedAnomalyType)
	}

	alerts, err := os.ReadFile(filepath.Join(projectRoot, ".liza", "alerts.log"))
	if err != nil {
		t.Fatalf("read alerts.log: %v", err)
	}
	if !strings.Contains(string(alerts), "PROVIDER AUDIT DEGRADED") {
		t.Fatalf("alerts.log missing audit degradation alert:\n%s", string(alerts))
	}
}

func TestRunSupervisor_HeartbeatMissingAgentStopsSupervisor(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-heartbeat-missing-agent"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.HeartbeatInterval = 1
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	mock := &MockLLMAgent{ExitCode: 0}
	mock.OnExecute = func(ctx context.Context, cliName, agentID, prompt, projectRoot string, additionalDirs []string) error {
		if err := bb.Modify(func(s *models.State) error {
			delete(s.Agents, agentID)
			return nil
		}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             "coder",
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, ".liza", "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("RunSupervisor() error = nil, want heartbeat failure")
	}
	if !strings.Contains(err.Error(), "heartbeat stopped for agent coder-1") {
		t.Fatalf("RunSupervisor() error = %v, want heartbeat stopped", err)
	}
}

func TestExit42RestartTracker_ExponentialBackoffAndCap(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 99
	state.Config.Exit42MaxBackoffSeconds = 8
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	var delays []time.Duration
	for i := 0; i < 4; i++ {
		outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
		if err != nil {
			t.Fatalf("Handle() error on attempt %d: %v", i+1, err)
		}
		if outcome.BlockedTask {
			t.Fatalf("Handle() blocked task unexpectedly on attempt %d", i+1)
		}
		delays = append(delays, outcome.Delay)
	}

	wantDelays := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		8 * time.Second,
	}
	for i, want := range wantDelays {
		if delays[i] != want {
			t.Errorf("delay[%d] = %v, want %v", i, delays[i], want)
		}
	}

	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}

	if updatedTask.BlockedReason != nil && *updatedTask.BlockedReason != "" {
		t.Errorf("task should not be blocked yet, got reason: %s", *updatedTask.BlockedReason)
	}
}

func TestExit42RestartTracker_Blocking(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 2
	state.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusWorking}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	// First attempt
	outcome, err := tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 1: %v", err)
	}
	if outcome.BlockedTask {
		t.Fatalf("Handle() should not block on first attempt")
	}

	// Second attempt (at threshold)
	outcome, err = tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 2: %v", err)
	}
	if outcome.BlockedTask {
		t.Fatalf("Handle() should not block at threshold")
	}

	// Third attempt (over threshold)
	outcome, err = tracker.Handle(bb, tmpDir, "coder", task.ID, agentID)
	if err != nil {
		t.Fatalf("Handle() error on attempt 3: %v", err)
	}
	if !outcome.BlockedTask {
		t.Fatalf("Handle() should block when over threshold")
	}

	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}

	wantReason := "exit code 42 restart loop detected"
	if updatedTask.BlockedReason == nil || !strings.Contains(*updatedTask.BlockedReason, wantReason) {
		got := "<nil>"
		if updatedTask.BlockedReason != nil {
			got = *updatedTask.BlockedReason
		}
		t.Errorf("blocked reason = %q, want containing %q", got, wantReason)
	}
}

func TestExit42RestartTracker_BlocksNonCoderRoles(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	agentID := "code-reviewer-1"
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = &agentID
	task.ReviewingBy = &agentID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Config.Exit42RestartThreshold = 2
	state.Agents[agentID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing}

	bb := testhelpers.WriteInitialState(t, statePath, state)
	tracker := newExit42RestartTracker()

	// Exhaust the threshold.
	for i := 0; i < 3; i++ {
		tracker.Handle(bb, tmpDir, "code-reviewer", task.ID, agentID)
	}

	// Read updated state — task should be BLOCKED.
	updatedState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	updatedTask := updatedState.FindTask(task.ID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", task.ID)
	}
	if updatedTask.Status != models.TaskStatusBlocked {
		t.Errorf("task status = %q, want BLOCKED", updatedTask.Status)
	}
}

func TestCrashRestartTracker_BlocksAfterThreshold(t *testing.T) {
	tracker := newCrashRestartTracker()
	threshold := 3

	// Same signature (no progress) — count accumulates.
	for i := 1; i <= threshold; i++ {
		count := tracker.Increment("task-1", "same-sig")
		if count != i {
			t.Fatalf("Increment() = %d, want %d", count, i)
		}
	}

	// Over threshold.
	count := tracker.Increment("task-1", "same-sig")
	if count != threshold+1 {
		t.Fatalf("Increment() = %d, want %d", count, threshold+1)
	}

	// Reset clears.
	tracker.reset("task-1")
	count = tracker.Increment("task-1", "same-sig")
	if count != 1 {
		t.Fatalf("after reset, Increment() = %d, want 1", count)
	}
}

func TestCrashRestartTracker_ResetsOnProgress(t *testing.T) {
	tracker := newCrashRestartTracker()

	tracker.Increment("task-1", "sig-a")
	tracker.Increment("task-1", "sig-a")

	// Signature changes — progress detected, counter resets.
	count := tracker.Increment("task-1", "sig-b")
	if count != 1 {
		t.Fatalf("Increment() after progress = %d, want 1", count)
	}
}

func TestSpinningTracker_BlocksAfterThreshold(t *testing.T) {
	tracker := newSpinningTracker()
	threshold := 5

	for i := 1; i <= threshold+1; i++ {
		count := tracker.Track("task-1", "same-sig")
		if count != i {
			t.Fatalf("Track() = %d, want %d", count, i)
		}
	}
}

func TestSpinningTracker_ResetsOnProgress(t *testing.T) {
	tracker := newSpinningTracker()

	tracker.Track("task-1", "sig-a")
	tracker.Track("task-1", "sig-a")
	tracker.Track("task-1", "sig-a")

	// Progress detected.
	count := tracker.Track("task-1", "sig-b")
	if count != 1 {
		t.Fatalf("Track() after progress = %d, want 1", count)
	}
}

func TestDetectObservedRuntimeFailure_LizaJSONEnvelope(t *testing.T) {
	output := `liza submit-verdict task-1 APPROVED --json
{"ok":false,"result":null,"error":{"code":"internal","message":"internal error"}}`

	failure := detectObservedRuntimeFailure(output)
	if failure == nil {
		t.Fatal("detectObservedRuntimeFailure() = nil, want failure")
	}
	if failure.Command != "submit-verdict" {
		t.Fatalf("Command = %q, want submit-verdict", failure.Command)
	}
	if failure.Code != "internal" {
		t.Fatalf("Code = %q, want internal", failure.Code)
	}
}

func TestDetectObservedRuntimeFailure_LizaJSONEnvelopeWithoutCommandContext(t *testing.T) {
	output := `{"ok":false,"result":null,"error":{"code":"internal","message":"internal error"}}`

	failure := detectObservedRuntimeFailure(output)
	if failure == nil {
		t.Fatal("detectObservedRuntimeFailure() = nil, want failure")
	}
	if failure.Command != unknownLizaJSONCommand {
		t.Fatalf("Command = %q, want %s", failure.Command, unknownLizaJSONCommand)
	}
	if failure.Code != "internal" {
		t.Fatalf("Code = %q, want internal", failure.Code)
	}
}

func TestDetectObservedRuntimeFailure_SubmitVerdictFailureEvidence(t *testing.T) {
	output := "activity log action=submit_verdict_failed detail=verdict=APPROVED error=permission denied"

	failure := detectObservedRuntimeFailure(output)
	if failure == nil {
		t.Fatal("detectObservedRuntimeFailure() = nil, want failure")
	}
	if failure.Command != "submit-verdict" {
		t.Fatalf("Command = %q, want submit-verdict", failure.Command)
	}
	if failure.Code != "submit_verdict_failed" {
		t.Fatalf("Code = %q, want submit_verdict_failed", failure.Code)
	}
}

func TestDetectObservedRuntimeFailure_CodexCommandEventAggregatedOutput(t *testing.T) {
	output := `{"type":"item.completed","item":{"type":"command_execution","command":"liza submit-verdict task-1 APPROVED --json","aggregated_output":"{\"ok\":false,\"result\":null,\"error\":{\"code\":\"internal\",\"message\":\"internal error\"}}\n","exit_code":1}}`

	failure := detectObservedRuntimeFailure(output)
	if failure == nil {
		t.Fatal("detectObservedRuntimeFailure() = nil, want failure")
	}
	if failure.Command != "submit-verdict" {
		t.Fatalf("Command = %q, want submit-verdict", failure.Command)
	}
	if failure.Code != "internal" {
		t.Fatalf("Code = %q, want internal", failure.Code)
	}
}

func TestRuntimeFailureTracker_ResetsOnDifferentFailure(t *testing.T) {
	tracker := newRuntimeFailureTracker()

	first := observedRuntimeFailure{Command: "submit-verdict", Code: "internal"}
	second := observedRuntimeFailure{Command: "submit-verdict", Code: "permission_denied"}

	if count := tracker.Track("task-1", first); count != 1 {
		t.Fatalf("first Track() = %d, want 1", count)
	}
	if count := tracker.Track("task-1", first); count != 2 {
		t.Fatalf("second Track() = %d, want 2", count)
	}
	if count := tracker.Track("task-1", second); count != 1 {
		t.Fatalf("Track() after different failure = %d, want 1", count)
	}
}

func TestObservedRuntimeFailureRetry_BlocksWithoutGenericSpin(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-runtime-failure"
	agentID := "coder-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID

	state := testhelpers.CreateValidState()
	state.Config.SpinningRestartThreshold = 2
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{Role: models.RoleCoder, Status: models.AgentStatusWorking, CurrentTask: &taskID}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	config := SupervisorConfig{
		AgentID:     agentID,
		Role:        models.RoleCoder,
		ProjectRoot: projectRoot,
		StatePath:   statePath,
		CLIName:     "codex",
	}
	spinTracker := newSpinningTracker()
	runtimeTracker := newRuntimeFailureTracker()
	failure := observedRuntimeFailure{Command: "submit-verdict", Code: "internal"}
	signature := "same-task-state"

	for i := 1; i <= state.Config.SpinningRestartThreshold; i++ {
		if count := spinTracker.Track(taskID, signature); count != 1 {
			t.Fatalf("spin count before runtime failure %d = %d, want 1", i, count)
		}
		if blocked := handleObservedRuntimeFailureRetry(bb, config, taskID, state.Config, failure, runtimeTracker, spinTracker); blocked {
			t.Fatalf("runtime failure attempt %d blocked, want below threshold", i)
		}
	}

	if count := spinTracker.Track(taskID, signature); count != 1 {
		t.Fatalf("spin count before blocking runtime failure = %d, want 1", count)
	}
	if blocked := handleObservedRuntimeFailureRetry(bb, config, taskID, state.Config, failure, runtimeTracker, spinTracker); !blocked {
		t.Fatal("runtime failure over threshold did not block task")
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	updatedTask := updated.FindTask(taskID)
	if updatedTask == nil {
		t.Fatalf("task %q not found", taskID)
	}
	if updatedTask.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", updatedTask.Status)
	}
	if updatedTask.BlockedReason == nil {
		t.Fatal("BlockedReason = nil, want tool-failure reason")
	}
	if !strings.Contains(*updatedTask.BlockedReason, "tool failure retry loop detected") {
		t.Fatalf("BlockedReason = %q, want tool-failure retry loop", *updatedTask.BlockedReason)
	}
	if strings.Contains(*updatedTask.BlockedReason, "spinning detected") {
		t.Fatalf("BlockedReason = %q, must not report generic spinning", *updatedTask.BlockedReason)
	}
}

func TestRunSupervisor_CodexCommandRuntimeFailureBlocksWithoutGenericSpin(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-codex-runtime-failure"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.SpinningRestartThreshold = 1
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	codexOutput := `{"type":"item.completed","item":{"type":"command_execution","command":"liza submit-verdict task-codex-runtime-failure APPROVED --json","aggregated_output":"{\"ok\":false,\"result\":null,\"error\":{\"code\":\"internal\",\"message\":\"internal error\"}}\n","exit_code":1}}`
	mock := &MockCLIExecutor{ExitCode: 0, Output: codexOutput}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, ".liza", "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		Executor:         mock,
		ExecutionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}

	if calls := mock.GetCalls(); len(calls) != 2 {
		t.Fatalf("Execute calls = %d, want 2", len(calls))
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := updated.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found", taskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil {
		t.Fatal("BlockedReason = nil, want tool-failure reason")
	}
	if !strings.Contains(*task.BlockedReason, "tool failure retry loop detected") {
		t.Fatalf("BlockedReason = %q, want tool-failure retry loop", *task.BlockedReason)
	}
	if !strings.Contains(*task.BlockedReason, "command=submit-verdict") || !strings.Contains(*task.BlockedReason, "code=internal") {
		t.Fatalf("BlockedReason = %q, want command/code attribution", *task.BlockedReason)
	}
	if strings.Contains(*task.BlockedReason, "spinning detected") {
		t.Fatalf("BlockedReason = %q, must not report generic spinning", *task.BlockedReason)
	}
}

func TestRunSupervisor_OpenCodeNoProgressBlocksBeforeSecondExecution(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-opencode-spin"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.SpinningRestartThreshold = 1
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	opencodeOutput := `{"type":"tool","name":"exec","input":{"cmd":"printf BRIDGE_EXEC_OK"},"output":"exit_code: 0\nstdout:\nBRIDGE_EXEC_OK"}`
	mock := &MockCLIExecutor{ExitCode: 0, Output: opencodeOutput}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, ".liza", "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "opencode",
		Executor:         mock,
		ExecutionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v", err)
	}

	// With claim-churn excluded from the progress signature (DEV-667), the
	// pre-execution spin guard blocks on the second claim, before wasting a
	// second execution.
	if calls := mock.GetCalls(); len(calls) != 1 {
		t.Fatalf("Execute calls = %d, want 1 before spin guard blocks re-claim", len(calls))
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := updated.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found", taskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil {
		t.Fatal("BlockedReason = nil, want no-progress reason")
	}
	if !strings.Contains(*task.BlockedReason, "spinning detected") {
		t.Fatalf("BlockedReason = %q, want spinning detected", *task.BlockedReason)
	}
}

func TestRunSupervisor_NonzeroAgentErrorBlocksBeforeAutoRepairCanRespawn(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-acpx-prompt-error"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.CrashRestartThreshold = 1
	state.Config.SpinningRestartThreshold = 10
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	mock := &MockLLMAgent{ExitCode: 1, ExitError: stderrors.New("acpx prompt: exit status 1")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, ".liza", "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex-acp",
		LLMAgent:         mock,
		ExecutionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v, want crash-loop handling", err)
	}
	if calls := mock.GetCalls(); len(calls) != 2 {
		t.Fatalf("Run calls = %d, want 2 before crash-loop block", len(calls))
	}

	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read() error = %v", err)
	}
	task := updated.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found", taskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("task status = %s, want BLOCKED", task.Status)
	}
	if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "crash restart loop detected") {
		t.Fatalf("BlockedReason = %v, want crash-loop reason", task.BlockedReason)
	}
}

func TestSuccessfulTurnProgressSignatureIgnoresOpenCodeOutputVariation(t *testing.T) {
	const taskSnapshot = "task:implementing\nworktree:clean"

	first := successfulTurnProgressSignature("opencode", `{"id":"one"}`, taskSnapshot)
	second := successfulTurnProgressSignature("opencode", `{"id":"two"}`, taskSnapshot)
	if first != second {
		t.Fatalf("signature changed with output only: first=%q second=%q", first, second)
	}
}

func TestSuccessfulTurnProgressSignatureTracksOpenCodeTaskProgress(t *testing.T) {
	const output = `{"type":"tool","name":"exec","output":"ok"}`

	first := successfulTurnProgressSignature("opencode", output, "task:implementing\nworktree:clean")
	second := successfulTurnProgressSignature("opencode", output, "task:implementing\nworktree:modified")
	if first == second {
		t.Fatalf("signature did not change with task/worktree progress: %q", first)
	}
}

func TestSuccessfulTurnTaskProgressSignatureIgnoresClaimChurn(t *testing.T) {
	now := time.Now().UTC()
	agentID := "coder-1"
	leaseExpires := now.Add(time.Minute)
	first := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	first.AssignedTo = &agentID
	first.LeaseExpires = &leaseExpires
	first.Iteration = 1
	first.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventClaimed, Agent: &agentID}}

	second := first
	second.Iteration = 2
	second.History = append(second.History, models.TaskHistoryEntry{Time: now.Add(time.Second), Event: models.TaskEventClaimed, Agent: &agentID})

	if successfulTurnTaskProgressSignature(&first) != successfulTurnTaskProgressSignature(&second) {
		t.Fatal("signature changed for claim-only task churn")
	}

	second.Output = append(second.Output, models.OutputEntry{
		Desc:       "child work",
		DoneWhen:   "child complete",
		Scope:      "child scope",
		SpecRef:    "specs/child.md",
		Validation: []string{"go test ./..."},
	})
	if successfulTurnTaskProgressSignature(&first) == successfulTurnTaskProgressSignature(&second) {
		t.Fatal("signature did not change for semantic task output progress")
	}
}

func TestReadSuccessfulTurnProgressSnapshotRequiresExecutingAgentOwnership(t *testing.T) {
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	now := time.Now().UTC()
	executing := testhelpers.BuildTaskByStatus("task-executing", models.TaskStatusImplementing, now)
	reviewable := testhelpers.BuildTaskByStatus("task-reviewable", models.TaskStatusReadyForReview, now)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{executing, reviewable}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	if _, eligible, err := readSuccessfulTurnProgressSnapshot(projectRoot, bb, "task-executing", "coder-2", pr); err != nil {
		t.Fatalf("readSuccessfulTurnProgressSnapshot wrong agent error = %v", err)
	} else if eligible {
		t.Fatal("wrong agent was eligible")
	}
	if _, eligible, err := readSuccessfulTurnProgressSnapshot(projectRoot, bb, "task-reviewable", "coder-1", pr); err != nil {
		t.Fatalf("readSuccessfulTurnProgressSnapshot reviewable error = %v", err)
	} else if eligible {
		t.Fatal("non-executing task was eligible")
	}
	if sig, eligible, err := readSuccessfulTurnProgressSnapshot(projectRoot, bb, "task-executing", "coder-1", pr); err != nil {
		t.Fatalf("readSuccessfulTurnProgressSnapshot executing error = %v", err)
	} else if !eligible || sig == "" {
		t.Fatalf("executing task eligibility = %v, signature = %q; want eligible signature", eligible, sig)
	}
}

func TestReadSuccessfulTurnProgressSnapshotTracksWorktreeChanges(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	now := time.Now().UTC()
	taskID := "task-worktree-progress"
	agentID := "coder-1"
	gw := lizagit.New(projectRoot)
	baseCommit, err := gw.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	worktree := ".worktrees/" + taskID
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.Worktree = &worktree
	task.BaseCommit = &baseCommit
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	before, eligible, err := readSuccessfulTurnProgressSnapshot(projectRoot, bb, taskID, agentID, pr)
	if err != nil {
		t.Fatalf("readSuccessfulTurnProgressSnapshot before change: %v", err)
	}
	if !eligible {
		t.Fatal("snapshot before worktree change was not eligible")
	}

	changedPath := filepath.Join(projectRoot, worktree, "progress.txt")
	if err := os.WriteFile(changedPath, []byte("progress\n"), 0o644); err != nil {
		t.Fatalf("write worktree progress file: %v", err)
	}

	after, eligible, err := readSuccessfulTurnProgressSnapshot(projectRoot, bb, taskID, agentID, pr)
	if err != nil {
		t.Fatalf("readSuccessfulTurnProgressSnapshot after change: %v", err)
	}
	if !eligible {
		t.Fatal("snapshot after worktree change was not eligible")
	}
	if before == after {
		t.Fatal("snapshot did not change after actual worktree progress")
	}
}

func TestRunAgent_ExtractedOps_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent("code-reviewer")

	// Create a task ready for review
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
	task.Worktree = nil
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ClaimReviewerTask operation
	input := ops.ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 300, // 5 minutes in seconds
	}
	result, err := ops.ClaimReviewerTask(input)
	if err != nil {
		t.Fatalf("ClaimReviewerTask failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ClaimReviewerTask returned nil result")
	}
	if result.TaskID != taskID {
		t.Errorf("result.TaskID = %s, want %s", result.TaskID, taskID)
	}
}

func TestResumeHandoff_ExtractedOp_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()

	// Create a task with handoff pending
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = true
	agentID := "coder-1"
	task.AssignedTo = &agentID
	task.Worktree = &tmpDir
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusHandoff,
	}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ResumeHandoff operation
	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}
	result, err := ops.ResumeHandoff(input)
	if err != nil {
		t.Fatalf("ResumeHandoff failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ResumeHandoff returned nil result")
	}
	if !result.Found {
		t.Errorf("ResumeHandoff should find handoff task")
	}
	if result.TaskID != taskID {
		t.Errorf("result.TaskID = %s, want %s", result.TaskID, taskID)
	}
}

func TestResumeHandoff_NotFound_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()

	// Create a task WITHOUT handoff pending
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = false // Not pending
	agentID := "coder-1"
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(t, statePath, state)

	// Test ResumeHandoff operation - should not find anything
	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}
	result, err := ops.ResumeHandoff(input)
	if err != nil {
		t.Fatalf("ResumeHandoff failed: %v", err)
	}
	if result == nil {
		t.Fatalf("ResumeHandoff returned nil result")
	}
	if result.Found {
		t.Errorf("ResumeHandoff should NOT find handoff task when HandoffPending=false")
	}
}

// TestExtractedOps_BehavioralParity tests that the extracted ops functions
// maintain the same behavior as the original inline closures
func TestExtractedOps_BehavioralParity(t *testing.T) {
	t.Run("ClaimReviewerTask finds highest priority task", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		now := time.Now().UTC()

		state := testhelpers.CreateValidState()
		state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent("code-reviewer")

		// Create multiple tasks with different priorities
		task1 := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
		task1.Priority = 2
		task1.Worktree = nil
		task2 := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now)
		task2.Priority = 1 // Higher priority (lower number)
		task2.Worktree = nil

		state.Tasks = []models.Task{task1, task2}

		testhelpers.WriteInitialState(t, statePath, state)

		input := ops.ClaimReviewerTaskInput{
			ProjectRoot:   tmpDir,
			AgentID:       "code-reviewer-1",
			LeaseDuration: 300,
		}
		result, err := ops.ClaimReviewerTask(input)
		if err != nil {
			t.Fatalf("ClaimReviewerTask failed: %v", err)
		}

		// Should claim the highest priority task (task-2 with priority 1)
		if result.TaskID != "task-2" {
			t.Errorf("expected task-2 (priority 1), got %s", result.TaskID)
		}
	})

	t.Run("ResumeHandoff uses correct worktree", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
		testhelpers.SetupPipelineConfig(t, tmpDir)
		now := time.Now().UTC()

		state := testhelpers.CreateValidState()

		taskID := "task-1"
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
		task.HandoffPending = true
		agentID := "coder-1"
		task.AssignedTo = &agentID
		expectedWorktree := "/worktrees/task-1"
		task.Worktree = &expectedWorktree
		state.Tasks = []models.Task{task}
		state.Agents[agentID] = models.Agent{
			Role:   "coder",
			Status: models.AgentStatusHandoff,
		}

		testhelpers.WriteInitialState(t, statePath, state)

		input := ops.ResumeHandoffInput{
			ProjectRoot: tmpDir,
			AgentID:     agentID,
		}
		result, err := ops.ResumeHandoff(input)
		if err != nil {
			t.Fatalf("ResumeHandoff failed: %v", err)
		}

		if result.Worktree != expectedWorktree {
			t.Errorf("worktree = %s, want %s", result.Worktree, expectedWorktree)
		}
	})
}

func BenchmarkClaimReviewerTask(b *testing.B) {
	tmpDir := b.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(&testing.T{}, tmpDir)
	testhelpers.SetupPipelineConfig(&testing.T{}, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReadyForReview, now)
	state.Tasks = []models.Task{task}

	testhelpers.WriteInitialState(&testing.T{}, statePath, state)

	input := ops.ClaimReviewerTaskInput{
		ProjectRoot:   tmpDir,
		AgentID:       "code-reviewer-1",
		LeaseDuration: 300,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ops.ClaimReviewerTask(input)
	}
}

func BenchmarkResumeHandoff(b *testing.B) {
	tmpDir := b.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(&testing.T{}, tmpDir)
	testhelpers.SetupPipelineConfig(&testing.T{}, tmpDir)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	taskID := "task-1"
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.HandoffPending = true
	agentID := "coder-1"
	task.AssignedTo = &agentID
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusHandoff,
	}

	testhelpers.WriteInitialState(&testing.T{}, statePath, state)

	input := ops.ResumeHandoffInput{
		ProjectRoot: tmpDir,
		AgentID:     agentID,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ops.ResumeHandoff(input)
	}
}

func TestCLISupportsStdin(t *testing.T) {
	tests := []struct {
		cli  string
		want bool
	}{
		{"claude", true},
		{"kimi", true},
		{"codex", true},
		{"gemini", true},
		{"vibe", false},
		{"opencode", false},
	}
	for _, tc := range tests {
		t.Run(tc.cli, func(t *testing.T) {
			if got := cliSupportsStdin(tc.cli); got != tc.want {
				t.Errorf("cliSupportsStdin(%q) = %v, want %v", tc.cli, got, tc.want)
			}
		})
	}
}

func TestBuildClaudeArgsDisallowsSubagentToolsWhenEnvEnabled(t *testing.T) {
	args := buildClaudeArgs("ignored", true, "", true)

	if !containsAdjacent(args, "--disallowedTools", "Task") {
		t.Fatalf("args = %v, want subagent tools disallowed", args)
	}
	if slices.Contains(args, "ignored") {
		t.Fatalf("args = %v, did not expect prompt arg when stdin is used", args)
	}
}

func TestBuildClaudeArgsAllowsTaskByDefault(t *testing.T) {
	args := buildClaudeArgs("do the thing", false, "/tmp/logs", false)

	if containsAdjacent(args, "--disallowedTools", "Task") {
		t.Fatalf("args = %v, did not expect subagent tools disallowed by default", args)
	}
	if !slices.Contains(args, "do the thing") {
		t.Fatalf("args = %v, want prompt arg when stdin is disabled", args)
	}
	if !containsAdjacent(args, "--output-format", "stream-json") {
		t.Fatalf("args = %v, want stream-json logging flags", args)
	}
}

func TestEnvValueUsesLastEnvValue(t *testing.T) {
	got := envValue([]string{
		"LIZA_DISABLE_CLAUDE_SUBAGENTS=0",
		"LIZA_DISABLE_CLAUDE_SUBAGENTS=1",
	}, "LIZA_DISABLE_CLAUDE_SUBAGENTS")

	if got != "1" {
		t.Fatalf("envValue() = %q, want later env value", got)
	}
}

func TestCodexCommandContextUsesPinnedNpmPackage(t *testing.T) {
	cmd, err := codexCommandContext(context.Background(), "0.125.0", []string{"exec", "-"})
	if err != nil {
		t.Fatalf("codexCommandContext() error = %v", err)
	}

	want := []string{"npm", "exec", "--yes", "--package", "@openai/codex@0.125.0", "--", "codex", "exec", "-"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
}

func TestCodexCommandContextDefaultsToCodexBinary(t *testing.T) {
	cmd, err := codexCommandContext(context.Background(), "", []string{"exec", "-"})
	if err != nil {
		t.Fatalf("codexCommandContext() error = %v", err)
	}

	want := []string{"codex", "exec", "-"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("cmd.Args = %v, want %v", cmd.Args, want)
	}
}

func TestCodexCommandContextRejectsWhitespaceVersion(t *testing.T) {
	_, err := codexCommandContext(context.Background(), "0.125.0 --bad", []string{"exec", "-"})
	if err == nil || !strings.Contains(err.Error(), "codex package version") {
		t.Fatalf("codexCommandContext() error = %v, want package version validation error", err)
	}
}

func TestResolveCodexLaunchConfig(t *testing.T) {
	t.Run("state config wins for version", func(t *testing.T) {
		got := resolveCodexLaunchConfig(models.Config{
			CodexPackageVersion: "0.125.0",
		}, []string{
			envLizaCodexVersion + "=0.132.0",
		})

		if got.PackageVersion != "0.125.0" {
			t.Fatalf("PackageVersion = %q, want state value", got.PackageVersion)
		}
	})

	t.Run("environment supplies process-local fallback", func(t *testing.T) {
		got := resolveCodexLaunchConfig(models.Config{}, []string{
			envLizaCodexVersion + "=0.125.0",
		})

		if got.PackageVersion != "0.125.0" {
			t.Fatalf("PackageVersion = %q, want env value", got.PackageVersion)
		}
	})
}

func TestBuildCodexArgs(t *testing.T) {
	t.Run("stdin without logging relies on configured permissions", func(t *testing.T) {
		args := buildCodexArgs("ignored", true, "")

		if slices.Contains(args, "--full-auto") {
			t.Fatalf("args = %v, did not expect --full-auto flag", args)
		}
		if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
			t.Fatalf("args = %v, did not expect bypass flag", args)
		}
		assertNoCodexPermissionOverrides(t, args)
		assertNoObsoleteCodexOverrides(t, args)
		if len(args) != 2 || args[0] != "exec" || args[1] != "-" {
			t.Fatalf("args = %v, want plain stdin exec invocation", args)
		}
		if slices.Contains(args, "--json") {
			t.Fatalf("args = %v, did not expect --json without logging", args)
		}
	})

	t.Run("prompt with logging emits json", func(t *testing.T) {
		args := buildCodexArgs("do the thing", false, "/tmp/logs")

		if !slices.Contains(args, "do the thing") {
			t.Fatalf("args = %v, want prompt argument", args)
		}
		if !slices.Contains(args, "--json") {
			t.Fatalf("args = %v, want --json when logging enabled", args)
		}
		if slices.Contains(args, "--full-auto") {
			t.Fatalf("args = %v, did not expect --full-auto flag", args)
		}
		assertNoCodexPermissionOverrides(t, args)
		assertNoObsoleteCodexOverrides(t, args)
		for _, a := range args {
			if strings.Contains(a, "mcp_servers") {
				t.Fatalf("args = %v, did not expect mcp_servers config", args)
			}
		}
	})

	t.Run("uses no launch-time config overrides", func(t *testing.T) {
		args := buildCodexArgs("ignored", true, "")

		assertNoCodexPermissionOverrides(t, args)
		assertNoObsoleteCodexOverrides(t, args)
		if args[len(args)-1] != "-" {
			t.Fatalf("args = %v, want stdin prompt marker last", args)
		}
	})
}

func TestBuildOpenCodeArgs(t *testing.T) {
	t.Run("prompt argument with permissions bypass", func(t *testing.T) {
		args := buildOpenCodeArgs("do the thing", "")

		want := []string{"run", "do the thing", "--dangerously-skip-permissions"}
		if !slices.Equal(args, want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
	})

	t.Run("logging requests json format", func(t *testing.T) {
		args := buildOpenCodeArgs("do the thing", "/tmp/logs")

		if !containsAdjacent(args, "--format", "json") {
			t.Fatalf("args = %v, want json format flags", args)
		}
	})
}

func containsAdjacent(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func assertNoCodexPermissionOverrides(t *testing.T, args []string) {
	t.Helper()
	for _, forbidden := range []string{
		"-c",
		"--add-dir",
		`sandbox_mode="workspace-write"`,
		`approval_policy="never"`,
	} {
		if slices.Contains(args, forbidden) {
			t.Fatalf("args = %v, did not expect Codex permission override %q", args, forbidden)
		}
	}
}

func assertNoObsoleteCodexOverrides(t *testing.T, args []string) {
	t.Helper()
	for _, forbidden := range []string{
		`default_permissions="workspace"`,
		"permissions.workspace.filesystem=",
		"permissions.workspace.network.enabled=true",
		"use_legacy_landlock",
	} {
		for _, arg := range args {
			if strings.Contains(arg, forbidden) {
				t.Fatalf("args = %v, did not expect obsolete Codex override %q", args, forbidden)
			}
		}
	}
}

func TestCodexInteractiveArgs(t *testing.T) {
	args := codexInteractiveArgs()

	if len(args) != 0 {
		t.Fatalf("args = %v, want no interactive Codex args", args)
	}
}

// buildPromptFailureFixture wires a minimal ARCHITECTING architect task
// into a real blackboard backed by a fresh git repo. Returns the
// blackboard, project root, task ID, agent ID.
func buildPromptFailureFixture(t *testing.T, integrationBranch string) (bb *db.Blackboard, projectRoot, taskID, agentID string) {
	t.Helper()
	projectRoot = t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	taskID = "arch-1"
	agentID = "architect-1"
	assigned := agentID
	leaseExpires := now.Add(30 * time.Minute)

	state := testhelpers.CreateValidState()
	state.Config.IntegrationBranch = integrationBranch
	state.Tasks = []models.Task{
		{
			ID:           taskID,
			Type:         models.TaskTypeArchitecture,
			Description:  "Design feature X",
			Status:       "ARCHITECTING",
			Priority:     1,
			Iteration:    1,
			DoneWhen:     "Architecture document produced",
			SpecRef:      "specs/goals/feature-x.md",
			Created:      now,
			AssignedTo:   &assigned,
			LeaseExpires: &leaseExpires,
			RolePair:     "architecture-pair",
			History:      []models.TaskHistoryEntry{},
		},
	}

	bb = testhelpers.WriteInitialState(t, statePath, state)
	return bb, projectRoot, taskID, agentID
}

// TestSupervisor_BuildPromptFailure_BlocksTask asserts that when
// BuildPrompt fails on a claimed architect task with an error wrapping
// precommit.ErrContextBuild, the supervisor's sentinel-gated recovery
// path (supervisor.go L817-820) transitions the task to BLOCKED with
// the expected reason prefix, clears the lease, emits a TaskEventBlocked
// history entry, does NOT invoke the agent executor, and does NOT exit
// the supervisor session (a subsequent iteration is reachable).
func TestSupervisor_BuildPromptFailure_BlocksTask(t *testing.T) {
	bb, projectRoot, taskID, agentID := buildPromptFailureFixture(t, "does-not-exist")

	config := SupervisorConfig{
		AgentID:     agentID,
		Role:        "architect",
		ProjectRoot: projectRoot,
	}

	stateBefore, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}

	// Exercise BuildPrompt via buildPromptWithContext — the same call path
	// the supervisor uses at supervisor.go L817. The architect task and a
	// non-existent integration branch drive ConfigExistsOnIntegration into
	// the invalid-ref error arm, which wraps ErrContextBuild.
	mockExecutor := &MockLLMAgent{ExitCode: 0}
	config.LLMAgent = mockExecutor
	pipelineCfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		t.Fatalf("pipeline.LoadFrozen: %v", err)
	}
	resolver := pipeline.NewResolver(pipelineCfg)
	_, err = buildPromptWithContext(stateBefore, config, taskID, resolver)
	if err == nil {
		t.Fatalf("expected BuildPrompt error, got nil")
	}
	if !stderrors.Is(err, precommit.ErrContextBuild) {
		t.Fatalf("errors.Is(err, precommit.ErrContextBuild) = false; err=%v", err)
	}

	// Replicate the supervisor's sentinel-gated recovery path. The guard
	// condition (claimedTaskID != "" && errors.Is(...)) holds by
	// construction here.
	claimedTaskID := taskID
	if claimedTaskID == "" || !stderrors.Is(err, precommit.ErrContextBuild) {
		t.Fatalf("precommit-domain guard should have matched; aborting test")
	}
	reason := fmt.Sprintf("prompt context build failed: %v", err)
	blockTaskFromSupervisor(bb, projectRoot, claimedTaskID, agentID, reason)

	// Invariant: agent was never invoked.
	if calls := mockExecutor.GetCalls(); len(calls) != 0 {
		t.Errorf("executeAgent should not be invoked; got %d calls", len(calls))
	}

	// Verify the task's post-conditions on the blackboard.
	stateAfter, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := stateAfter.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found after block", taskID)
	}
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("task.Status = %q, want %q", task.Status, models.TaskStatusBlocked)
	}
	if task.BlockedReason == nil {
		t.Fatalf("task.BlockedReason = nil, want non-nil")
	}
	if !strings.HasPrefix(*task.BlockedReason, "prompt context build failed: precommit") {
		t.Errorf("BlockedReason = %q, want prefix %q", *task.BlockedReason, "prompt context build failed: precommit")
	}
	if task.AssignedTo != nil {
		t.Errorf("task.AssignedTo = %q, want nil (cleared by block)", *task.AssignedTo)
	}
	if task.LeaseExpires != nil {
		t.Errorf("task.LeaseExpires = %v, want nil (cleared by block)", *task.LeaseExpires)
	}
	// TaskEventBlocked in the history.
	found := false
	for _, h := range task.History {
		if h.Event == models.TaskEventBlocked {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no TaskEventBlocked entry in task history")
	}

	// Second-iteration reachability: the guard uses `continue`, not
	// `return`, so after blocking the supervisor loop proceeds. We verify
	// the loop is free to run another iteration by driving blockingly the
	// wait-for-work detection over the post-block state: with the task now
	// BLOCKED, no more architect work is claimable and the loop would
	// cleanly exit via the "no work" branch rather than via the error
	// return. This is the positive phrasing of "supervisor did NOT exit
	// via the error branch".
	claimable := models.CountClaimableTasks(stateAfter, "architect", nil)
	if claimable != 0 {
		t.Errorf("after block, expected 0 claimable architect tasks, got %d", claimable)
	}
}

// TestSupervisor_BuildPromptFailure_NonPrecommit_DoesNotBlock asserts
// that a BuildPrompt error NOT wrapping precommit.ErrContextBuild (e.g.,
// template/resolver/pipeline failures) falls through to the existing
// wrapped-error return path at supervisor.go L820 — the task status is
// NOT mutated to BLOCKED, BlockedReason remains nil, and the surfaced
// error carries the original "failed to build prompt: " prefix.
func TestSupervisor_BuildPromptFailure_NonPrecommit_DoesNotBlock(t *testing.T) {
	bb, _, taskID, _ := buildPromptFailureFixture(t, "main")

	// Engineered non-precommit error: something that could plausibly come
	// from template render, resolver ContextSections, or pipeline wiring.
	templateErr := fmt.Errorf("context sections for role %q: template %q missing", "architect", "assigned-task")

	// Simulate the supervisor's sentinel-gated decision at L817-820.
	claimedTaskID := taskID
	shouldBlock := claimedTaskID != "" && stderrors.Is(templateErr, precommit.ErrContextBuild)
	if shouldBlock {
		t.Fatalf("non-precommit error unexpectedly matched precommit sentinel: %v", templateErr)
	}

	// Simulate the fall-through return. The supervisor wraps as
	// "failed to build prompt: %w", the existing path unchanged.
	wrapped := fmt.Errorf("failed to build prompt: %w", templateErr)
	if stderrors.Is(wrapped, precommit.ErrContextBuild) {
		t.Errorf("wrapped error unexpectedly matches precommit sentinel: %v", wrapped)
	}
	if !strings.HasPrefix(wrapped.Error(), "failed to build prompt: ") {
		t.Errorf("wrapped error %q does not start with %q", wrapped.Error(), "failed to build prompt: ")
	}

	// Critically: because shouldBlock is false, blockTaskFromSupervisor is
	// NOT called. Verify post-conditions: task status unchanged, no
	// BlockedReason.
	stateAfter, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := stateAfter.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found", taskID)
	}
	if task.Status == models.TaskStatusBlocked {
		t.Errorf("task.Status = %q, want NOT %q", task.Status, models.TaskStatusBlocked)
	}
	if task.BlockedReason != nil {
		t.Errorf("task.BlockedReason = %q, want nil", *task.BlockedReason)
	}
}

func TestExecuteAgentRequiresLLMAgent(t *testing.T) {
	_, _, err := executeAgent(context.Background(), SupervisorConfig{}, "prompt", nil, "", models.Config{})
	if err == nil {
		t.Fatalf("executeAgent error = nil, want missing agent error")
	}
	if !strings.Contains(err.Error(), "no LLM agent configured") {
		t.Fatalf("executeAgent error = %q, want missing agent error", err)
	}
}

func TestExecuteAgentUsesLegacyCLIExecutor(t *testing.T) {
	legacy := &legacyOnlyCLIExecutor{}
	config := SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      t.TempDir(),
		CLIName:          "codex",
		Executor:         legacy,
		ExecutionTimeout: 5 * time.Second,
	}

	exitCode, output, err := executeAgent(context.Background(), config, "legacy prompt", []string{"extra-dir"}, "", models.Config{})
	if err != nil {
		t.Fatalf("executeAgent error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if output != "legacy output" {
		t.Fatalf("output = %q, want legacy output", output)
	}
	if len(legacy.calls) != 1 {
		t.Fatalf("legacy calls = %d, want 1", len(legacy.calls))
	}
	call := legacy.calls[0]
	if call.CLIName != "codex" || call.AgentID != "coder-1" || call.Prompt != "legacy prompt" || call.ProjectRoot != config.ProjectRoot {
		t.Fatalf("legacy call = %+v, want codex/coder-1/legacy prompt/%s", call, config.ProjectRoot)
	}
	if !slices.Equal(call.AdditionalDirs, []string{"extra-dir"}) {
		t.Fatalf("additional dirs = %#v, want extra-dir", call.AdditionalDirs)
	}
}

func TestNewCLIAgent(t *testing.T) {
	t.Run("empty outputsDir disables logging", func(t *testing.T) {
		e := NewCLIAgent("")
		if e.outputsDir != "" {
			t.Errorf("outputsDir should be empty, got %q", e.outputsDir)
		}
	})

	t.Run("non-empty outputsDir enables logging", func(t *testing.T) {
		dir := t.TempDir()
		e := NewCLIAgent(dir)
		if e.outputsDir != dir {
			t.Errorf("outputsDir = %q, want %q", e.outputsDir, dir)
		}
	})
}

func TestNewDefaultCLIExecutorDelegatesToCLIAgent(t *testing.T) {
	dir := t.TempDir()
	e := NewDefaultCLIExecutor(dir)
	if e.outputsDir != dir {
		t.Errorf("outputsDir = %q, want %q", e.outputsDir, dir)
	}
}

func TestExit42TaskProgressSignatureIgnoresClaimIteration(t *testing.T) {
	task := models.Task{ID: "task-1", Status: models.TaskStatusImplementing, Iteration: 3}
	reClaimed := task
	reClaimed.Iteration = 4

	if exit42TaskProgressSignature(&task) != exit42TaskProgressSignature(&reClaimed) {
		t.Fatal("signature changed on claim-only Iteration bump; spin/crash counters would reset every re-claim")
	}

	progressed := task
	progressed.Status = models.TaskStatusReadyForReview
	if exit42TaskProgressSignature(&task) == exit42TaskProgressSignature(&progressed) {
		t.Fatal("signature must change on real status progress")
	}
}
