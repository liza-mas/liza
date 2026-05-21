package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

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
	t.Setenv("LIZA_ENABLE_SCIP_SEARCH", "true")
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
	if err := os.WriteFile(filepath.Join(binDir, "scip-go"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
