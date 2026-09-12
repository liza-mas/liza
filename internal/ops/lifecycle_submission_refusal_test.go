package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleSubmissionRefusalAllowsSameSessionCorrection(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, _, actor, bb := completeAcceptanceScenario(t)
	authority := models.AgentAuthority{ID: actor, Generation: "same-submission-session"}
	if err := bb.Modify(func(state *models.State) error {
		agent := testhelpers.RegisteredTestAgent("coder")
		agent.Generation = authority.Generation
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		state.Agents[actor] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wt := git.New(root).GetWorktreePath(taskID)
	// Keep the approved command, manifest and assertion unchanged. Its existing
	// identity assertion must reject the bad data before that data is repaired.
	if err := os.WriteFile(filepath.Join(wt, "identity.txt"), []byte("invalid\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "identity.txt")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: introduce invalid identity")
	badSHA := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	before := readAcceptanceState(t, bb)
	original := LifecycleRequestOptions{RequestID: "refused-submission", ExpectedTransition: models.TaskTransitionID(before.FindTask(taskID))}
	result, err := SubmitForReviewWithAuthorityAndOptions(root, taskID, badSHA, authority, original)
	var evidenceErr *AcceptanceEvidenceError
	if result != nil || !errors.As(err, &evidenceErr) || evidenceErr.TaskID != taskID || evidenceErr.Field != "acceptance.execution" {
		t.Fatalf("bad identity submission = %+v, %v; want acceptance.execution refusal", result, err)
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged ||
		lifecycleErr.Outcome.SafeAction != "requery" || lifecycleErr.Outcome.Effects != "unknown" {
		t.Fatalf("refusal must report uncertain execution effects: %v", err)
	}
	refused := readAcceptanceState(t, bb)
	refusedTask := refused.FindTask(taskID)
	if refusedTask.Lifecycle == nil || refusedTask.Lifecycle.Preparation != nil ||
		refusedTask.Lifecycle.CompletionSequence != 0 || len(refusedTask.Lifecycle.Receipts) != 0 {
		t.Fatalf("refusal retained preparation or recorded completion: %+v", refusedTask.Lifecycle)
	}
	currentToken := models.TaskTransitionID(refusedTask)
	if currentToken == original.ExpectedTransition {
		t.Fatal("refusal did not retire the original request boundary")
	}
	statePath := paths.New(root).StatePath()
	beforeRetry, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	head := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	result, err = SubmitForReviewWithAuthorityAndOptions(root, taskID, badSHA, authority, original)
	if result != nil || !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged ||
		lifecycleErr.Outcome.SafeAction != "requery" || lifecycleErr.Outcome.Effects != "none" {
		t.Fatalf("original refused request = %+v, %v; want side-effect-free STATE_CHANGED/requery", result, err)
	}
	afterRetry, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeRetry, afterRetry) || testhelpers.MustGit(t, wt, "rev-parse", "HEAD") != head {
		t.Fatal("original request replay changed state or HEAD")
	}

	if err := os.WriteFile(filepath.Join(wt, "identity.txt"), []byte("human\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "identity.txt")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: repair identity data")
	correctedSHA := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	fresh := LifecycleRequestOptions{RequestID: "corrected-submission", ExpectedTransition: currentToken}
	result, err = SubmitForReviewWithAuthorityAndOptions(root, taskID, correctedSHA, authority, fresh)
	if err != nil || result == nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("corrected submission in the same session = %+v, %v", result, err)
	}
	completed := readAcceptanceState(t, bb)
	task := completed.FindTask(taskID)
	if task.Lifecycle == nil || task.Lifecycle.Preparation != nil || task.Lifecycle.CompletionSequence != 1 ||
		len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Receipts[0].RequestID != fresh.RequestID {
		t.Fatalf("correction did not record exactly its own completion: %+v", task.Lifecycle)
	}
	if completed.Agents[actor].Generation != authority.Generation {
		t.Fatal("correction unexpectedly required a registration change")
	}
	submissions := 0
	for _, event := range task.History {
		if event.Event == models.TaskEventSubmittedForReview {
			submissions++
			if event.SubmissionInputCommit != correctedSHA {
				t.Fatalf("refused candidate entered submission history: %+v", event)
			}
		}
	}
	if submissions != 1 {
		t.Fatalf("submitted history count = %d, want one corrected submission", submissions)
	}
}
