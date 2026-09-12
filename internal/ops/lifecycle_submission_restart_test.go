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
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleSubmissionAbandonedPreparationReleaseReclaim(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, originalSHA, originalAgent, bb := setupSuccessfulSubmitScenario(t)
	originalAuthority := models.AgentAuthority{ID: originalAgent, Generation: "original-registration"}
	replacementAuthority := models.AgentAuthority{ID: "coder-2", Generation: "replacement-registration"}
	if err := bb.Modify(func(state *models.State) error {
		original := testhelpers.RegisteredTestAgent("coder")
		original.Generation = originalAuthority.Generation
		original.Status = models.AgentStatusWorking
		original.CurrentTask = &taskID
		state.Agents[originalAgent] = original
		replacement := testhelpers.RegisteredTestAgent("coder")
		replacement.Generation = replacementAuthority.Generation
		state.Agents[replacementAuthority.ID] = replacement
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Force preparation to perform a real rebase before the simulated crash.
	if err := os.WriteFile(filepath.Join(root, "integration-progress.txt"), []byte("integration advanced\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "integration-progress.txt")
	testhelpers.MustGit(t, root, "commit", "-m", "Advance integration before submission")
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	originalRequest := LifecycleRequestOptions{RequestID: "abandoned-submission", ExpectedTransition: models.TaskTransitionID(state.FindTask(taskID))}
	var abandoned *preparedSubmission
	invocation := &submissionInvocation{}
	if err := withOwnershipTaskLock(root, taskID, "test-abandoned-submission", func() error {
		var err error
		abandoned, err = prepareSubmitForReview(root, taskID, originalSHA, originalAgent, &originalAuthority, originalRequest, invocation)
		return err
	}); err != nil {
		t.Fatalf("prepare original submission: %v", err)
	}
	if abandoned == nil || abandoned.complete == nil || abandoned.replay != nil || !invocation.effects {
		t.Fatal("fixture did not prepare an unfinished submission with Git effects")
	}
	gw := git.New(root)
	rebasedSHA, err := gw.GetWorktreeHEAD(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if rebasedSHA == originalSHA {
		t.Fatal("preparation did not rebase the original commit")
	}
	state, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	preparedTask := state.FindTask(taskID)
	if preparedTask.Lifecycle == nil || preparedTask.Lifecycle.Preparation == nil || preparedTask.Lifecycle.Preparation.RequestID != originalRequest.RequestID {
		t.Fatal("unfinished submission has no durable preparation")
	}
	for _, event := range preparedTask.History {
		if event.Event == models.TaskEventSubmittedForReview {
			t.Fatal("preparation prematurely recorded submission")
		}
	}

	// The original process disappears with every lock released. Recover through
	// normal ownership APIs; do not erase the marker or change task status directly.
	released, err := ReleaseClaim(root, taskID, roles.ClaimDoer, true, "original process exited during indexing", "human")
	if err != nil || released == nil || !released.ReleasedDoer {
		t.Fatalf("release abandoned claim: %+v, %v", released, err)
	}
	state, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	releasedTask := state.FindTask(taskID)
	if releasedTask.AssignedTo != nil || releasedTask.Lifecycle == nil || releasedTask.Lifecycle.Preparation != nil {
		t.Fatal("normal release did not retire the abandoned ownership preparation")
	}
	claimed, err := ClaimTaskWithAuthority(root, taskID, replacementAuthority)
	if err != nil || claimed == nil || claimed.Outcome != models.LifecycleCompleted {
		t.Fatalf("claim after abandoned submission: %+v, %v", claimed, err)
	}
	if err := WriteCheckpointWithAuthority(root, &WriteCheckpointInput{
		TaskID: taskID, AgentID: replacementAuthority.ID,
		Intent: "complete recovered task", ValidationPlan: "verify source and tests in new submission",
		FilesToModify: []string{"recovered.go", "recovered_test.go"},
	}, replacementAuthority); err != nil {
		t.Fatal(err)
	}
	worktree := gw.GetWorktreePath(taskID)
	for _, filename := range []string{"recovered.go", "recovered_test.go"} {
		if err := os.WriteFile(filepath.Join(worktree, filename), []byte("package main\n// Recovered implementation and test.\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testhelpers.MustGit(t, worktree, "add", "recovered.go", "recovered_test.go")
	testhelpers.MustGit(t, worktree, "commit", "-m", "Complete recovered task with tests")
	freshSHA := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	state, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	freshRequest := LifecycleRequestOptions{RequestID: "replacement-submission", ExpectedTransition: models.TaskTransitionID(state.FindTask(taskID))}
	fresh, err := SubmitForReviewWithAuthorityAndOptions(root, taskID, freshSHA, replacementAuthority, freshRequest)
	if err != nil || fresh == nil || fresh.Outcome != models.LifecycleCompleted {
		t.Fatalf("fresh submission after recovery: %+v, %v", fresh, err)
	}
	before, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	lateErr := withOwnershipTaskLock(root, taskID, "test-abandoned-finalization", func() error {
		_, err := abandoned.complete()
		return err
	})
	var rejected *LifecycleError
	if !errors.As(lateErr, &rejected) || rejected.Outcome.Outcome != models.LifecycleStateChanged || rejected.Outcome.SafeAction != "requery" {
		t.Fatalf("abandoned finalization = %v, want STATE_CHANGED/requery", lateErr)
	}
	_, retryErr := SubmitForReviewWithAuthorityAndOptions(root, taskID, originalSHA, originalAuthority, originalRequest)
	if !errors.As(retryErr, &rejected) || rejected.Outcome.Outcome != models.LifecycleStateChanged || rejected.Outcome.SafeAction != "requery" {
		t.Fatalf("old request retry = %v, want STATE_CHANGED/requery", retryErr)
	}
	after, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("old finalization or retry changed the replacement submission")
	}
	state, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	completedTask := state.FindTask(taskID)
	if completedTask.Lifecycle.Preparation != nil || completedTask.ReviewCommit == nil || *completedTask.ReviewCommit != fresh.ReviewCommit {
		t.Fatal("fresh submission did not retain its completed review boundary")
	}
	submissions := 0
	for _, event := range completedTask.History {
		if event.Event == models.TaskEventSubmittedForReview {
			submissions++
			if event.Agent == nil || *event.Agent != replacementAuthority.ID || event.SubmissionInputCommit != freshSHA {
				t.Fatalf("submission history attributed to abandoned request: %+v", event)
			}
		}
	}
	if submissions != 1 {
		t.Fatalf("submitted history count = %d, want one replacement submission", submissions)
	}
}
