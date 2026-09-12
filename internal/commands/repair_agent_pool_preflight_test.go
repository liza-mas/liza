package commands

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRepairAgentPoolValidationPreflightCapacity(t *testing.T) {
	for _, role := range []string{models.RoleCoder, models.RoleCodeReviewer} {
		t.Run(role, func(t *testing.T) {
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			status := models.TaskStatusReady
			if role == models.RoleCodeReviewer {
				status = models.TaskStatusReadyForReview
			}
			task := testhelpers.BuildTaskByStatus("task-1", status, now)
			task.Validation = []string{"canonical check"}
			task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "canonical check", Env: []string{"REQUIRED"}}}
			state.Tasks = []models.Task{task}
			agentID := role + "-1"
			agent := testhelpers.RegisteredTestAgent(role)
			agent.Generation = "generation-current"
			state.Agents = map[string]models.Agent{agentID: agent}
			commit := "worktree-head"
			if task.ReviewCommit != nil {
				commit = *task.ReviewCommit
			}
			state.ValidationReadiness = map[string]map[string]models.ValidationReadiness{agentID: {task.ID: {TaskID: task.ID, Generation: agent.Generation, Commit: commit, Digest: models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites), CheckedAt: now, Result: "failed", Code: "missing_env"}}}
			if task.ReviewCommit != nil {
				record := state.ValidationReadiness[agentID][task.ID]
				record.ReviewCommit = *task.ReviewCommit
				state.ValidationReadiness[agentID][task.ID] = record
			}
			root := writeRepairAgentPoolState(t, state)
			var calls []spawnedAgentCall
			withFakeRepairSpawner(t, &calls, nil)
			result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, CLI: "claude"})
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 0 || len(result.Missing) != 0 {
				t.Fatalf("failed context triggered equivalent replacement: %#v %#v", calls, result.Missing)
			}
			if len(result.Validation) != 1 || result.Validation[0].Status != "failed" || result.Validation[0].Code != "missing_env" {
				t.Fatalf("missing task-specific diagnostic: %#v", result.Validation)
			}
			pr, err := ops.LoadResolverForModels(root)
			if err != nil {
				t.Fatal(err)
			}
			delete(state.ValidationReadiness, agentID)
			entries := findValidationAgentCapacity(state, pr, nil)
			if len(entries) != 1 || entries[0].Status != "unverified" {
				t.Fatalf("unknown session reported capable: %#v", entries)
			}
		})
	}
}
