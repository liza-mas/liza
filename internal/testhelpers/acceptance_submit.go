package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// AcceptanceSubmitScenario is an executing coder task whose committed
// candidate satisfies a strict acceptance contract, so submit-for-review runs
// the canonical command and publishes on success.
type AcceptanceSubmitScenario struct {
	Root      string
	StatePath string
	TaskID    string
	AgentID   string
	Commit    string
	Authority models.AgentAuthority
	BB        *db.Blackboard
}

// SetupAcceptanceSubmitScenario commits the reviewed source and allocation on
// integration, then a worktree candidate whose only canonical command is
// `sh boundary_test.sh` with the given script body and batch timeout.
func SetupAcceptanceSubmitScenario(t *testing.T, script string, timeoutSeconds int) AcceptanceSubmitScenario {
	t.Helper()
	root := t.TempDir()
	SetupTestGitRepo(t, root)
	statePath, _ := SetupLizaDir(t, root)
	MustGit(t, root, "checkout", "integration")
	write := func(dir, name, contents string) {
		t.Helper()
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(root, "specs/acceptance-goal.md", "# Boundary\n\n## Identity\nReject malformed identity.\n")
	MustGit(t, root, "add", "specs/acceptance-goal.md")
	MustGit(t, root, "commit", "-m", "test: record boundary requirement")
	sourceCommit := MustGit(t, root, "rev-parse", "HEAD")
	plan := fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":%d,\"approved_proofs\":[]}\n```\n", sourceCommit, timeoutSeconds)
	write(root, "specs/acceptance-plan.md", plan)
	MustGit(t, root, "add", "specs/acceptance-plan.md")
	MustGit(t, root, "commit", "-m", "test: independently reviewed acceptance allocation")
	parentCommit := MustGit(t, root, "rev-parse", "HEAD")

	taskID := "task-acceptance-submit"
	worktree := filepath.Join(".worktrees", taskID)
	wt := filepath.Join(root, worktree)
	MustGit(t, root, "worktree", "add", "-b", "task/"+taskID, wt, "integration")
	write(wt, "feature.go", "package main\n")
	write(wt, "feature_test.go", "package main\n")
	write(wt, "boundary_test.sh", script)
	write(wt, "acceptance/task-1.json", `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"boundary_test.sh","assertion":"identity assertion","command_index":0}]}`+"\n")
	MustGit(t, wt, "add", "feature.go", "feature_test.go", "boundary_test.sh", "acceptance/task-1.json")
	MustGit(t, wt, "commit", "-m", "test: candidate with complete acceptance mapping")
	commit := MustGit(t, wt, "rev-parse", "HEAD")

	now := time.Now().UTC()
	agentID := "coder-1"
	leaseExpires := now.Add(30 * time.Minute)
	parentID, planner, approver := "acceptance-parent", "code-planner-1", "code-plan-reviewer-1"
	planRef := "specs/acceptance-plan.md#Task 1"
	specRef := "specs/acceptance-goal.md"
	validation := []string{"sh boundary_test.sh"}
	agent := RegisteredTestAgent(models.RoleCoder)
	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &taskID
	state := &models.State{
		Sprint: models.Sprint{ID: "sprint-1", Number: 1, Timeline: models.SprintTimeline{Started: now}},
		Config: models.Config{IntegrationBranch: "integration", LeaseDuration: 1800},
		Tasks: []models.Task{
			{
				ID:           taskID,
				Description:  "Task with a strict acceptance contract",
				Status:       models.TaskStatusImplementing,
				RolePair:     "coding-pair",
				AssignedTo:   &agentID,
				LeaseExpires: &leaseExpires,
				Worktree:     &worktree,
				BaseCommit:   &parentCommit,
				PlanRef:      planRef,
				SpecRef:      specRef,
				Validation:   validation,
				ParentTask:   &parentID,
				Iteration:    1,
				Created:      now,
				History: []models.TaskHistoryEntry{{
					Time:  now,
					Event: models.TaskEventPreExecutionCheckpoint,
					Agent: &agentID,
					Extra: map[string]any{
						"intent":          "exercise canonical acceptance execution",
						"validation_plan": "submit-for-review runs the acceptance contract",
						"files_to_modify": []string{"feature.go"},
					},
				}},
			},
			{
				ID: parentID, Type: models.TaskTypePlanning, RolePair: "code-planning-pair", Status: models.TaskStatusMerged,
				AssignedTo: &planner, ApprovedBy: &approver,
				Approvals:  []models.Approval{{Agent: approver, Timestamp: now}},
				BaseCommit: &sourceCommit, ReviewCommit: &parentCommit, MergeCommit: &parentCommit,
				Output:  []models.OutputEntry{{PlanRef: planRef, SpecRef: specRef, Validation: validation}},
				Created: now,
			},
		},
		Agents: map[string]models.Agent{agentID: agent},
	}
	bb := WriteInitialState(t, statePath, state)
	return AcceptanceSubmitScenario{
		Root:      root,
		StatePath: statePath,
		TaskID:    taskID,
		AgentID:   agentID,
		Commit:    commit,
		Authority: models.AgentAuthority{ID: agentID, Generation: TestAgentGeneration},
		BB:        bb,
	}
}
