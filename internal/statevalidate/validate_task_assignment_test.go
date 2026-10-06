package statevalidate

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateTaskInvariants_NonTerminalAssignments(t *testing.T) {
	t.Parallel()
	resolver := loadTestResolver(t)
	cfg := loadTestConfig(t)
	now := time.Now().UTC()
	for _, status := range []models.TaskStatus{
		models.TaskStatusImplementing, models.TaskStatusRejected, models.TaskStatusReadyForReview,
		models.TaskStatusReviewing, models.TaskStatusApproved, models.TaskStatusBlocked,
	} {
		t.Run(string(status), func(t *testing.T) {
			state := testhelpers.CreateValidState()
			first := testhelpers.BuildTaskByStatus("A", status, now)
			first.AssignedTo = testhelpers.StringPtr("coder-1")
			second := testhelpers.BuildTaskByStatus("B", models.TaskStatusRejected, now)
			state.Tasks = []models.Task{first, second}
			err := taskInvariantsErr(state, "", true, resolver, cfg)
			const want = "agent coder-1 assigned to multiple active tasks simultaneously: [A B]"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("validation = %v, want %q (including when the agent row is absent)", err, want)
			}
		})
	}
}

func TestValidateState_DuplicateDormantAssignments(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	state := candidateFixture(
		testhelpers.BuildTaskByStatus("A", models.TaskStatusRejected, now),
		testhelpers.BuildTaskByStatus("B", models.TaskStatusRejected, now),
	)
	err := ValidateState(state, pipelineRoot(t), true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "agent coder-1 assigned to multiple active tasks simultaneously: [A B]") {
		t.Fatalf("strict state validation missed dormant assignments: %v", err)
	}
}

func TestValidateTaskInvariants_PipelineCleanAssignment(t *testing.T) {
	t.Parallel()
	cfg := loadTestConfig(t)
	pair := cfg.Pipeline.RolePairs["coding-pair"]
	pair.States.Clean = "CUSTOM_CLEAN"
	cfg.Pipeline.RolePairs["coding-pair"] = pair
	resolver := pipeline.NewResolver(cfg)
	now := time.Now().UTC()
	state := candidateFixture(testhelpers.BuildTaskByStatus("B", models.TaskStatusRejected, now))
	clean := testhelpers.BuildTaskByStatus("A", models.TaskStatusReady, now)
	clean.Status = "CUSTOM_CLEAN"
	clean.AssignedTo = testhelpers.StringPtr("coder-1")
	state.Tasks = append(state.Tasks, clean)
	if err := taskInvariantsErr(state, "", true, resolver, cfg); err != nil && strings.Contains(err.Error(), "assigned to multiple") {
		t.Fatalf("pipeline clean assignment counted: %v", err)
	}
}

func TestValidateTaskInvariants_AssignmentExclusions(t *testing.T) {
	t.Parallel()
	resolver := loadTestResolver(t)
	cfg := loadTestConfig(t)
	now := time.Now().UTC()
	for _, status := range []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusAbandoned, models.TaskStatusSuperseded} {
		t.Run(string(status), func(t *testing.T) {
			state := testhelpers.CreateValidState()
			terminal := testhelpers.BuildTaskByStatus("A", status, now)
			terminal.AssignedTo = testhelpers.StringPtr("coder-1")
			state.Tasks = []models.Task{terminal, testhelpers.BuildTaskByStatus("B", models.TaskStatusRejected, now)}
			if err := taskInvariantsErr(state, "", true, resolver, cfg); err != nil && strings.Contains(err.Error(), "assigned to multiple") {
				t.Fatalf("historical assignment counted: %v", err)
			}
		})
	}
	for _, owner := range []string{"", "$transitioning"} {
		t.Run(owner, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			for _, id := range []string{"A", "B"} {
				task := testhelpers.BuildTaskByStatus(id, models.TaskStatusImplementing, now)
				task.AssignedTo = &owner
				state.Tasks = append(state.Tasks, task)
			}
			if err := taskInvariantsErr(state, "", true, resolver, cfg); err != nil && strings.Contains(err.Error(), "assigned to multiple") {
				t.Fatalf("non-agent assignment counted: %v", err)
			}
		})
	}
}
