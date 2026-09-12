package ops

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleVerdictRolloverAuthorityLossPreservesCompletion(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	taskID, coderID := "task-1", "coder-1"
	authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: "original-review-registration"}
	reviewCommit := strings.Repeat("a", 40)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())
	task.AssignedTo = &coderID
	task.ReviewCommit = &reviewCommit
	task.Worktree = nil
	task.Attempt, task.Iteration = 1, 3
	task.ReviewCyclesCurrent, task.ReviewCyclesTotal = 1, 1
	state := testhelpers.CreateValidState()
	state.Config.MaxReviewCycles = 2
	state.Tasks = []models.Task{task}
	state.Agents[coderID] = models.Agent{Role: "coder", Status: models.AgentStatusWaiting, CurrentTask: &taskID}
	state.Agents[authority.ID] = models.Agent{
		Role: models.RoleCodeReviewer, Status: models.AgentStatusReviewing,
		Generation: authority.Generation, CurrentTask: &taskID,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	current, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	request := LifecycleRequestOptions{RequestID: "rejection-before-registration-change", ExpectedTransition: models.TaskTransitionID(current.FindTask(taskID))}
	var completedTransition string
	var afterReplacement []byte
	previousHooks := testTransitionHooks
	testTransitionHooks = &transitionTestHooks{afterPhase1: func() {
		// Rejection and rollover phase one have committed; phase three must now
		// lose authority while retaining the already-completed verdict identity.
		if err := bb.Modify(func(current *models.State) error {
			task := current.FindTask(taskID)
			if task.Lifecycle == nil {
				return errors.New("rejection has no lifecycle receipt")
			}
			for _, receipt := range task.Lifecycle.Receipts {
				if receipt.Operation == "submit-verdict" && receipt.RequestID == request.RequestID {
					completedTransition = receipt.TransitionID
				}
			}
			if completedTransition == "" {
				return errors.New("rejection receipt is missing")
			}
			agent := current.Agents[authority.ID]
			agent.Generation = "replacement-review-registration"
			current.Agents[authority.ID] = agent
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var err error
		afterReplacement, err = os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
	}}
	t.Cleanup(func() { testTransitionHooks = previousHooks })

	result, err := SubmitVerdictWithAuthorityAndOptions(root, taskID, "REJECTED", "Approach is wrong", authority, "", reviewCommit, request)
	if result != nil {
		t.Fatalf("partially completed invocation returned success result: %+v", result)
	}
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("error = %v, want preserved AgentAuthorityError", err)
	}
	var failure *LifecycleError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want structured lifecycle failure", err)
	}
	outcome := failure.Outcome
	if outcome.Outcome != models.LifecycleStaleCaller || outcome.SafeAction != "stop" || outcome.Effects != "committed" {
		t.Fatalf("post-commit authority loss = %+v, want STALE_CALLER/stop/committed", outcome)
	}
	if completedTransition == "" || outcome.CompletedTransitionID != completedTransition || outcome.RequestID != request.RequestID {
		t.Fatalf("completion identity lost: %+v, receipt=%q", outcome, completedTransition)
	}
	if outcome.TaskStatus != "UNKNOWN" || outcome.TransitionID != "" || outcome.CurrentAssignee != "" || outcome.CurrentReviewer != "" {
		t.Fatalf("stale reviewer received current task authority: %+v", outcome)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if afterReplacement == nil || !bytes.Equal(afterReplacement, after) {
		t.Fatal("stale finalization changed replacement state or appended quarantine/anomaly data")
	}
	current, readErr = bb.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	unfinished := current.FindTask(taskID)
	if unfinished.Attempt != 2 || unfinished.AssignedTo == nil || *unfinished.AssignedTo != transitioning {
		t.Fatalf("fixture did not stop after committed rollover phase one: %+v", unfinished)
	}
}
