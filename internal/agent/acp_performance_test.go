package agent

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	acpplugin "github.com/liza-mas/liza/plugin/acp"
)

type lizaACPBenchmarkAgent struct {
	mu         sync.Mutex
	mode       string
	acp        *acpplugin.RunManager
	runs       []acpplugin.RunMetric
	lastPrompt string
}

func newLizaACPBenchmarkAgent(mode string) *lizaACPBenchmarkAgent {
	return &lizaACPBenchmarkAgent{
		mode: mode,
		acp:  acpplugin.NewRunManager(),
	}
}

func (a *lizaACPBenchmarkAgent) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	taskID := req.TaskID
	if taskID == "" {
		taskID = req.SessionID
	}
	if taskID == "" {
		taskID = "unknown-task"
	}

	sessionKey := taskID
	if a.mode == "acp" {
		sessionKey = req.ProjectRoot + ":" + req.AgentID
	}
	run := a.acp.StartWithSessionKey(taskID, sessionKey)

	fullPromptTokens := estimateTokens(req.Prompt)
	inputTokens := fullPromptTokens
	if a.mode == "acp" && run.Warm {
		inputTokens = estimateACPTaskDeltaTokens(req.ProjectRoot, taskID)
		if inputTokens < 1 {
			inputTokens = 1
		}
	}

	a.mu.Lock()
	a.lastPrompt = req.Prompt
	a.mu.Unlock()

	_ = a.acp.HandleEvent(acpplugin.Event{
		TaskID: taskID,
		Kind:   acpplugin.EventUsage,
		Payload: map[string]any{
			"usage": map[string]any{
				"input_tokens":        inputTokens,
				"output_tokens":       24,
				"cached_read_tokens":  maxInt(fullPromptTokens-inputTokens, 0),
				"cached_write_tokens": inputTokens,
			},
		},
	})
	_ = a.acp.HandleEvent(acpplugin.Event{
		TaskID:  taskID,
		Kind:    acpplugin.EventMessage,
		Message: fmt.Sprintf("%s completed %s", a.mode, taskID),
	})

	sleepForSimulatedProviderCost(inputTokens)

	if err := markBenchmarkTaskReadyForReview(req.ProjectRoot, taskID, req.AgentID); err != nil {
		metric, _ := a.acp.Finish(taskID, 1)
		a.record(metric)
		return LLMAgentRunResult{ExitCode: 1, Output: err.Error(), Usage: toLLMUsage(metric.Usage), WarmUsage: metric.Warm, SessionID: metric.SessionID}, err
	}

	metric, err := a.acp.Finish(taskID, 0)
	if err != nil {
		return LLMAgentRunResult{ExitCode: 1, Output: err.Error()}, err
	}
	a.record(metric)
	return LLMAgentRunResult{
		ExitCode:  0,
		Output:    metric.Output,
		Usage:     toLLMUsage(metric.Usage),
		WarmUsage: metric.Warm,
		SessionID: metric.SessionID,
	}, nil
}

func (a *lizaACPBenchmarkAgent) RunInteractive(context.Context, LLMAgentInteractiveRequest) (int, error) {
	return 1, fmt.Errorf("interactive mode is not supported by benchmark agent")
}

func (a *lizaACPBenchmarkAgent) record(metric acpplugin.RunMetric) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runs = append(a.runs, metric)
}

func (a *lizaACPBenchmarkAgent) Runs() []acpplugin.RunMetric {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]acpplugin.RunMetric, len(a.runs))
	copy(out, a.runs)
	return out
}

func TestLizaSimplifiedProjectACPPerformanceComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("slow test: skipped in -short mode")
	}
	baseline := runSimplifiedLizaBenchmark(t, "cli")
	acp := runSimplifiedLizaBenchmark(t, "acp")

	// Wall-clock duration is diagnostic only because scheduler noise must not decide the comparison.
	baselineDuration := totalDuration(baseline)
	acpDuration := totalDuration(acp)
	baselineTokens := totalInputTokens(baseline)
	acpTokens := totalInputTokens(acp)
	baselineModeledCost := totalModeledProviderCost(baseline)
	acpModeledCost := totalModeledProviderCost(acp)

	if len(baseline) != 2 || len(acp) != 2 {
		t.Fatalf("runs: baseline=%d acp=%d, want 2 each", len(baseline), len(acp))
	}
	if !acp[1].Warm {
		t.Fatalf("second ACP run should be warm: %#v", acp[1])
	}
	if acpTokens >= baselineTokens {
		t.Fatalf("ACP tokens = %d, baseline tokens = %d; expected ACP to use fewer tokens", acpTokens, baselineTokens)
	}
	if err := validateLowerModeledProviderCost(baseline, acp); err != nil {
		t.Fatal(err)
	}

	tokenSavings := percentImprovement(float64(baselineTokens), float64(acpTokens))
	modeledCostSavings := percentImprovement(float64(baselineModeledCost), float64(acpModeledCost))
	t.Logf("baseline: runs=%d observed_duration=%s modeled_provider_cost=%s input_tokens=%d warm_runs=%d", len(baseline), baselineDuration, baselineModeledCost, baselineTokens, warmRunCount(baseline))
	t.Logf("acp: runs=%d observed_duration=%s modeled_provider_cost=%s input_tokens=%d warm_runs=%d", len(acp), acpDuration, acpModeledCost, acpTokens, warmRunCount(acp))
	t.Logf("difference: modeled_provider_cost_savings=%.2f%% input_token_savings=%.2f%%", modeledCostSavings, tokenSavings)
	warmRun := findWarmRun(t, acp)
	baselineWarmTaskTokens := findRunTokens(t, baseline, warmRun.TaskID)
	t.Logf("warm task delta: task=%s baseline_tokens=%d acp_tokens=%d savings=%.2f%%",
		warmRun.TaskID,
		baselineWarmTaskTokens,
		warmRun.Usage.InputTokens,
		percentImprovement(float64(baselineWarmTaskTokens), float64(warmRun.Usage.InputTokens)))
}

func TestValidateLowerModeledProviderCostRejectsEqualOrHigherACP(t *testing.T) {
	const baselineInputTokens = 100
	baseline := []acpplugin.RunMetric{{Usage: acpplugin.Usage{InputTokens: baselineInputTokens}}}
	tests := []struct {
		name        string
		inputTokens int
	}{
		{name: "equal", inputTokens: baselineInputTokens},
		{name: "higher", inputTokens: baselineInputTokens + 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			acp := []acpplugin.RunMetric{{Usage: acpplugin.Usage{InputTokens: test.inputTokens}}}
			err := validateLowerModeledProviderCost(baseline, acp)
			if err == nil {
				t.Fatalf("ACP input tokens = %d, baseline input tokens = %d; expected modeled provider cost comparison to fail", test.inputTokens, baselineInputTokens)
			}
			if !strings.Contains(err.Error(), "expected ACP modeled provider cost to be lower") {
				t.Fatalf("error = %q, want modeled provider cost comparison", err)
			}
		})
	}
}

func runSimplifiedLizaBenchmark(t *testing.T, mode string) []acpplugin.RunMetric {
	t.Helper()

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateCommittedSpecFileOnIntegration(t, projectRoot, "vision.md", "# Benchmark Vision\n\nImplement two tiny tasks.\n")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.AgentProgressTimeout = 30
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-alpha", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("task-beta", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	benchAgent := newLizaACPBenchmarkAgent(mode)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:                  "coder-1",
		Role:                     "coder",
		ProjectRoot:              projectRoot,
		StatePath:                statePath,
		LogPath:                  filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml"),
		SpecsDir:                 filepath.Join(projectRoot, "specs"),
		CLIName:                  mode,
		LLMAgent:                 benchAgent,
		ExecutionTimeout:         10 * time.Second,
		ExecutionProgressTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunSupervisor(%s): %v", mode, err)
	}

	return benchAgent.Runs()
}

func markBenchmarkTaskReadyForReview(projectRoot, taskID, agentID string) error {
	head, err := exec.Command("git", "-C", projectRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	reviewCommit := strings.TrimSpace(string(head))
	bb := db.For(paths.New(projectRoot).StatePath())

	return bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task %q not found", taskID)
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

func estimateTokens(prompt string) int {
	if prompt == "" {
		return 0
	}
	return maxInt(len(prompt)/4, 1)
}

func estimateACPTaskDeltaTokens(projectRoot, taskID string) int {
	bb := db.For(paths.New(projectRoot).StatePath())
	state, err := bb.Read()
	if err != nil {
		return 1
	}
	task := state.FindTask(taskID)
	if task == nil {
		return 1
	}
	payload := strings.Join([]string{
		task.ID,
		string(task.Type),
		task.Description,
		string(task.Status),
		task.SpecRef,
		task.DoneWhen,
		task.Scope,
		task.RolePair,
	}, "\n")
	return estimateTokens(payload)
}

func sleepForSimulatedProviderCost(inputTokens int) {
	time.Sleep(modeledProviderCost(inputTokens))
}

func modeledProviderCost(inputTokens int) time.Duration {
	return 15*time.Millisecond + time.Duration(inputTokens)*50*time.Microsecond
}

func toLLMUsage(usage acpplugin.Usage) LLMAgentUsage {
	return LLMAgentUsage{
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CachedReadTokens:  usage.CachedReadTokens,
		CachedWriteTokens: usage.CachedWriteTokens,
	}
}

func totalDuration(runs []acpplugin.RunMetric) time.Duration {
	var total time.Duration
	for _, run := range runs {
		total += run.Duration
	}
	return total
}

func totalInputTokens(runs []acpplugin.RunMetric) int {
	var total int
	for _, run := range runs {
		total += run.Usage.InputTokens
	}
	return total
}

func totalModeledProviderCost(runs []acpplugin.RunMetric) time.Duration {
	var total time.Duration
	for _, run := range runs {
		total += modeledProviderCost(run.Usage.InputTokens)
	}
	return total
}

func validateLowerModeledProviderCost(baseline, acp []acpplugin.RunMetric) error {
	baselineCost := totalModeledProviderCost(baseline)
	acpCost := totalModeledProviderCost(acp)
	if acpCost >= baselineCost {
		return fmt.Errorf("ACP modeled provider cost = %s, baseline modeled provider cost = %s; expected ACP modeled provider cost to be lower", acpCost, baselineCost)
	}
	return nil
}

func warmRunCount(runs []acpplugin.RunMetric) int {
	var total int
	for _, run := range runs {
		if run.Warm {
			total++
		}
	}
	return total
}

func findRunTokens(t *testing.T, runs []acpplugin.RunMetric, taskID string) int {
	t.Helper()
	for _, run := range runs {
		if run.TaskID == taskID {
			return run.Usage.InputTokens
		}
	}
	t.Fatalf("run %q not found", taskID)
	return 0
}

func findWarmRun(t *testing.T, runs []acpplugin.RunMetric) acpplugin.RunMetric {
	t.Helper()
	for _, run := range runs {
		if run.Warm {
			return run
		}
	}
	t.Fatalf("warm run not found")
	return acpplugin.RunMetric{}
}

func percentImprovement(before, after float64) float64 {
	if before <= 0 {
		return 0
	}
	return 100 - (after / before * 100)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
