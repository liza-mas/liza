package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/spf13/cobra"
)

func TestAwaitCommands_TimeoutHelp(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "verdict", cmd: awaitVerdictCmd},
		{name: "resubmission", cmd: awaitResubmissionCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := tt.cmd.Flags().Lookup("timeout-seconds")
			if flag == nil {
				t.Fatal("--timeout-seconds flag is missing")
			}
			if flag.DefValue != "1800" {
				t.Fatalf("--timeout-seconds default = %q, want 1800", flag.DefValue)
			}
			for _, phrase := range []string{"total wait budget", "at most 100 seconds"} {
				if !strings.Contains(flag.Usage, phrase) {
					t.Errorf("--timeout-seconds help = %q, want phrase %q", flag.Usage, phrase)
				}
			}
			if !strings.Contains(tt.cmd.Long, "POLL:") || !strings.Contains(tt.cmd.Long, "TIMEOUT:") {
				t.Errorf("long help must list POLL and TIMEOUT separately:\n%s", tt.cmd.Long)
			}
		})
	}
}

func TestAwaitVerdictCLI_BudgetAndOutput(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		result          *commands.AwaitVerdictResult
		wantBudget      time.Duration
		wantHumanOutput string
		wantJSONVerdict string
		wantJSONTimeout float64
	}{
		{
			name: "json poll uses default remaining budget",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1", "--json"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{Verdict: ops.VerdictPoll, TaskStatus: models.TaskStatusReadyForReview},
				TimeoutSeconds:     1700,
			},
			wantBudget:      1800 * time.Second,
			wantJSONVerdict: ops.VerdictPoll,
			wantJSONTimeout: 1700,
		},
		{
			name: "json timeout uses caller remaining budget",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1", "--timeout-seconds", "80", "--json"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{Verdict: ops.VerdictTimeout, TaskStatus: models.TaskStatusReadyForReview},
				TimeoutSeconds:     0,
			},
			wantBudget:      80 * time.Second,
			wantJSONVerdict: ops.VerdictTimeout,
			wantJSONTimeout: 0,
		},
		{
			name: "human poll uses default remaining budget",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{Verdict: ops.VerdictPoll, TaskStatus: models.TaskStatusReadyForReview},
				TimeoutSeconds:     1700,
			},
			wantBudget:      1800 * time.Second,
			wantHumanOutput: "Verdict: POLL\nStatus: CODE_TO_REVIEW\nTimeout seconds: 1700\n",
		},
		{
			name: "human timeout uses caller remaining budget",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1", "--timeout-seconds", "80"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{Verdict: ops.VerdictTimeout, TaskStatus: models.TaskStatusReadyForReview},
				TimeoutSeconds:     0,
			},
			wantBudget:      80 * time.Second,
			wantHumanOutput: "Verdict: TIMEOUT\nStatus: CODE_TO_REVIEW\n",
		},
		{
			name: "json immediate verdict omits wait-only budget",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1", "--json"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{Verdict: ops.VerdictApproved, TaskStatus: models.TaskStatusApproved},
			},
			wantBudget:      1800 * time.Second,
			wantJSONVerdict: ops.VerdictApproved,
		},
		{
			name: "human immediate verdict retains existing fields",
			args: []string{"await-verdict", "task-await", "--agent-id", "coder-1"},
			result: &commands.AwaitVerdictResult{
				AwaitVerdictResult: &ops.AwaitVerdictResult{
					Verdict:         ops.VerdictRejected,
					TaskStatus:      models.TaskStatusRejected,
					Reason:          "changes requested",
					ReviewerAgent:   "reviewer-1",
					ReviewCommit:    "review-commit",
					CurrentAssignee: "coder-1",
					SafeAction:      ops.SafeActionRevise,
					Guidance:        "Revise and resubmit.",
				},
			},
			wantBudget:      1800 * time.Second,
			wantHumanOutput: "Verdict: REJECTED\nStatus: CODE_REJECTED\nReason: changes requested\nReviewer: reviewer-1\nReview commit: review-commit\nCurrent assignee: coder-1\nSafe action: revise\n\nRevise and resubmit.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := setupAwaitCLIProject(t)
			resetFlagIfPresent(awaitVerdictCmd, "timeout-seconds")
			t.Cleanup(func() { resetFlagIfPresent(awaitVerdictCmd, "timeout-seconds") })

			originalAwait := awaitVerdict
			t.Cleanup(func() { awaitVerdict = originalAwait })
			var gotBudget time.Duration
			awaitVerdict = func(projectRoot, taskID string, authority models.AgentAuthority, remaining time.Duration) (*commands.AwaitVerdictResult, error) {
				gotBudget = remaining
				return tt.result, nil
			}

			stdout, err := executeRootCommandCapture(t, projectRoot, tt.args...)
			if err != nil {
				t.Fatalf("await-verdict failed: %v\n%s", err, stdout)
			}
			if gotBudget != tt.wantBudget {
				t.Errorf("adapter remaining budget = %s, want %s", gotBudget, tt.wantBudget)
			}
			if tt.wantHumanOutput != "" {
				if stdout != tt.wantHumanOutput {
					t.Errorf("human output = %q, want %q", stdout, tt.wantHumanOutput)
				}
				return
			}
			assertAwaitJSONResult(t, stdout, tt.wantJSONVerdict, tt.wantJSONTimeout)
		})
	}
}

func TestAwaitResubmissionCLI_BudgetAndOutput(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		result          *commands.AwaitResubmissionResult
		wantBudget      time.Duration
		wantHumanOutput string
		wantJSONVerdict string
		wantJSONTimeout float64
	}{
		{
			name: "json poll uses default remaining budget",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1", "--json"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionPoll, TaskStatus: models.TaskStatusRejected},
				TimeoutSeconds:          1700,
			},
			wantBudget:      1800 * time.Second,
			wantJSONVerdict: ops.ResubmissionPoll,
			wantJSONTimeout: 1700,
		},
		{
			name: "json timeout uses caller remaining budget",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1", "--timeout-seconds", "80", "--json"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionTimeout, TaskStatus: models.TaskStatusRejected},
				TimeoutSeconds:          0,
			},
			wantBudget:      80 * time.Second,
			wantJSONVerdict: ops.ResubmissionTimeout,
			wantJSONTimeout: 0,
		},
		{
			name: "human poll uses default remaining budget",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionPoll, TaskStatus: models.TaskStatusRejected},
				TimeoutSeconds:          1700,
			},
			wantBudget:      1800 * time.Second,
			wantHumanOutput: "Verdict: POLL\nStatus: CODE_REJECTED\nTimeout seconds: 1700\n",
		},
		{
			name: "human timeout uses caller remaining budget",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1", "--timeout-seconds", "80"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionTimeout, TaskStatus: models.TaskStatusRejected},
				TimeoutSeconds:          0,
			},
			wantBudget:      80 * time.Second,
			wantHumanOutput: "Verdict: TIMEOUT\nStatus: CODE_REJECTED\n",
		},
		{
			name: "json immediate resubmission omits wait-only budget",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1", "--json"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{Verdict: ops.ResubmissionResubmitted, TaskStatus: models.TaskStatusReadyForReview},
			},
			wantBudget:      1800 * time.Second,
			wantJSONVerdict: ops.ResubmissionResubmitted,
		},
		{
			name: "human immediate resubmission retains existing fields",
			args: []string{"await-resubmission", "task-await", "--agent-id", "code-reviewer-1"},
			result: &commands.AwaitResubmissionResult{
				AwaitResubmissionResult: &ops.AwaitResubmissionResult{
					Verdict:      ops.ResubmissionResubmitted,
					TaskStatus:   models.TaskStatusReadyForReview,
					BaseCommit:   "base-commit",
					ReviewCommit: "review-commit",
					ReviewCycle:  2,
				},
			},
			wantBudget:      1800 * time.Second,
			wantHumanOutput: "Verdict: RESUBMITTED\nStatus: CODE_TO_REVIEW\nBase commit: base-commit\nReview commit: review-commit\nReview cycle: 2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := setupAwaitCLIProject(t)
			resetFlagIfPresent(awaitResubmissionCmd, "timeout-seconds")
			t.Cleanup(func() { resetFlagIfPresent(awaitResubmissionCmd, "timeout-seconds") })

			originalAwait := awaitResubmission
			t.Cleanup(func() { awaitResubmission = originalAwait })
			var gotBudget time.Duration
			awaitResubmission = func(projectRoot, taskID string, authority models.AgentAuthority, remaining time.Duration) (*commands.AwaitResubmissionResult, error) {
				gotBudget = remaining
				return tt.result, nil
			}

			stdout, err := executeRootCommandCapture(t, projectRoot, tt.args...)
			if err != nil {
				t.Fatalf("await-resubmission failed: %v\n%s", err, stdout)
			}
			if gotBudget != tt.wantBudget {
				t.Errorf("adapter remaining budget = %s, want %s", gotBudget, tt.wantBudget)
			}
			if tt.wantHumanOutput != "" {
				if stdout != tt.wantHumanOutput {
					t.Errorf("human output = %q, want %q", stdout, tt.wantHumanOutput)
				}
				return
			}
			assertAwaitJSONResult(t, stdout, tt.wantJSONVerdict, tt.wantJSONTimeout)
		})
	}
}

func assertAwaitJSONResult(t *testing.T, stdout, wantVerdict string, wantTimeout float64) {
	t.Helper()
	env := parseEnvelope(t, stdout)
	result, ok := env["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %#v, want object", env["result"])
	}
	if result["verdict"] != wantVerdict {
		t.Errorf("verdict = %#v, want %q", result["verdict"], wantVerdict)
	}
	gotTimeout, hasTimeout := result["timeout_seconds"]
	if wantVerdict == ops.VerdictPoll || wantVerdict == ops.ResubmissionPoll {
		if !hasTimeout || gotTimeout != wantTimeout {
			t.Errorf("timeout_seconds = %#v, want %v", gotTimeout, wantTimeout)
		}
	} else if hasTimeout {
		t.Errorf("non-POLL result contains timeout_seconds = %#v", gotTimeout)
	}
}

func setupAwaitCLIProject(t *testing.T) string {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)
	projectRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp project root: %v", err)
	}
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	return projectRoot
}

func TestSubmitForReviewCLI_CommitRefHandling(t *testing.T) {
	tests := []struct {
		name string
		args func(taskID, agentID string) []string
	}{
		{
			name: "accepts HEAD ref",
			args: func(taskID, agentID string) []string {
				return []string{"submit-for-review", taskID, "HEAD", "--agent-id", agentID, "--json"}
			},
		},
		{
			name: "defaults omitted ref to HEAD",
			args: func(taskID, agentID string) []string {
				return []string{"submit-for-review", taskID, "--agent-id", agentID, "--json"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot, statePath, taskID, agentID := setupSubmitForReviewCLIProject(t)

			if err := executeRootCommand(t, projectRoot, tt.args(taskID, agentID)...); err != nil {
				t.Fatalf("submit-for-review failed: %v", err)
			}

			state, err := db.For(statePath).Read()
			if err != nil {
				t.Fatalf("read state: %v", err)
			}
			task := state.FindTask(taskID)
			if task == nil {
				t.Fatalf("task %s not found", taskID)
			}
			if task.ReviewCommit == nil || *task.ReviewCommit == "" {
				t.Fatalf("ReviewCommit = %v, want non-empty", task.ReviewCommit)
			}
		})
	}
}

func TestSubmitForReviewCLI_JSONIncludesScipWarnings(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("LIZA_ENABLE_SCIP_SEARCH", "true")
	t.Setenv("LIZA_ENABLE_STACKLIT", "false")
	projectRoot, statePath, taskID, agentID := setupSubmitForReviewCLIProject(t)
	installFailingSubmitReviewCLIIndexer(t)

	if err := db.For(statePath).Modify(func(state *models.State) error {
		state.Config.ScipSearch = []string{"go"}
		return nil
	}); err != nil {
		t.Fatalf("set scip config: %v", err)
	}

	stdout, err := executeRootCommandCapture(t, projectRoot, "submit-for-review", taskID, "HEAD", "--agent-id", agentID, "--json")
	if err != nil {
		t.Fatalf("submit-for-review --json failed: %v\n%s", err, stdout)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("unmarshal json: %v\n%s", err, stdout)
	}
	warnings, ok := env["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want one scip warning", env["warnings"])
	}
	if !strings.Contains(warnings[0].(string), "scip-search go:") || !strings.Contains(warnings[0].(string), "fake scip-go failed") {
		t.Fatalf("warning = %q, want scip-search go failure", warnings[0])
	}

	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task status = %s, want READY_FOR_REVIEW", task.Status)
	}
}

func setupSubmitForReviewCLIProject(t *testing.T) (projectRoot, statePath, taskID, agentID string) {
	t.Helper()
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)

	projectRoot = t.TempDir()
	projectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatalf("failed to resolve temp project root: %v", err)
	}
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	g := git.New(projectRoot)
	taskID = "task-submit-cli"
	baseCommit, err := g.CreateWorktree(taskID, "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)

	if err := os.WriteFile(filepath.Join(wtPath, "feature.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "feature_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "feature.go", "feature_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add feature with tests")

	agentID = "coder-1"
	worktree := g.GetWorktreeRelPath(taskID)
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	currentTask := taskID
	initialState := &models.State{
		Config: models.Config{
			IntegrationBranch: "integration",
			LeaseDuration:     1800,
		},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Task for submit-for-review CLI",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &baseCommit,
				Iteration:    1,
				Created:      time.Now().UTC(),
				History: []models.TaskHistoryEntry{
					{
						Time:  time.Now().UTC(),
						Event: models.TaskEventPreExecutionCheckpoint,
						Agent: &agentID,
						Extra: map[string]any{
							"intent":          "test CLI submit ref handling",
							"validation_plan": "submit using HEAD without shell expansion",
							"files_to_modify": []string{"feature.go"},
						},
					},
				},
			},
		},
		Agents: map[string]models.Agent{
			agentID: {
				Role:        "coder",
				Status:      models.AgentStatusWorking,
				CurrentTask: &currentTask,
				Generation:  testhelpers.TestAgentGeneration,
			},
		},
	}
	testhelpers.WriteInitialState(t, statePath, initialState)

	return projectRoot, statePath, taskID, agentID
}

func installFailingSubmitReviewCLIIndexer(t *testing.T) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho fake scip-go failed >&2\nexit 3\n"
	testhelpers.WriteShellStub(t, filepath.Join(binDir, "scip-go"), script)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestAwaitTimeoutSecondsRejectsOverCeiling covers the CLI boundary: an operator
// asking for more than the maximum is told, not silently given less.
func TestAwaitTimeoutSecondsRejectsOverCeiling(t *testing.T) {
	overCeiling := strconv.Itoa(int(ops.DefaultAwaitBudget.Seconds()) + 1)

	t.Run("await-verdict", func(t *testing.T) {
		projectRoot := setupAwaitCLIProject(t)
		resetFlagIfPresent(awaitVerdictCmd, "timeout-seconds")
		t.Cleanup(func() { resetFlagIfPresent(awaitVerdictCmd, "timeout-seconds") })

		original := awaitVerdict
		t.Cleanup(func() { awaitVerdict = original })
		called := false
		awaitVerdict = func(string, string, models.AgentAuthority, time.Duration) (*commands.AwaitVerdictResult, error) {
			called = true
			return nil, nil
		}

		_, err := executeRootCommandCapture(t, projectRoot,
			"await-verdict", "task-await", "--agent-id", "coder-1",
			"--timeout-seconds", overCeiling)
		assertOverCeilingRejected(t, err, called)
	})

	t.Run("await-resubmission", func(t *testing.T) {
		projectRoot := setupAwaitCLIProject(t)
		resetFlagIfPresent(awaitResubmissionCmd, "timeout-seconds")
		t.Cleanup(func() { resetFlagIfPresent(awaitResubmissionCmd, "timeout-seconds") })

		original := awaitResubmission
		t.Cleanup(func() { awaitResubmission = original })
		called := false
		awaitResubmission = func(string, string, models.AgentAuthority, time.Duration) (*commands.AwaitResubmissionResult, error) {
			called = true
			return nil, nil
		}

		_, err := executeRootCommandCapture(t, projectRoot,
			"await-resubmission", "task-await", "--agent-id", "code-reviewer-1",
			"--timeout-seconds", overCeiling)
		assertOverCeilingRejected(t, err, called)
	})
}

func assertOverCeilingRejected(t *testing.T, err error, awaitCalled bool) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error for a budget above the ceiling")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want it to name the maximum", err)
	}
	if awaitCalled {
		t.Error("await ran despite an out-of-range budget")
	}
}
