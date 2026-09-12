package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func disableLifecycleTestIndexes(t *testing.T) {
	t.Helper()
	t.Setenv(scipsearch.EnvEnableScipSearch, "false")
	t.Setenv(stacklit.EnvEnableStacklit, "false")
	t.Setenv(functionalclusters.EnvEnableFunctionalClusters, "false")
}

func TestLifecycleSubmissionIndexingDoesNotHoldTaskLock(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, inputSHA, agentID, bb := setupSuccessfulSubmitScenario(t)
	authority := models.AgentAuthority{ID: agentID, Generation: "concurrent-submission-session"}
	if err := bb.Modify(func(state *models.State) error {
		agent := testhelpers.RegisteredTestAgent("coder")
		agent.Generation = authority.Generation
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	request := LifecycleRequestOptions{RequestID: "concurrent-submission", ExpectedTransition: models.TaskTransitionID(state.FindTask(taskID))}
	indexing, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	var first *SubmitForReviewResult
	var firstErr error
	t.Cleanup(replaceSubmitReviewScipRefreshForTest(func(scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		close(indexing)
		<-resume
		return scipsearch.RefreshResult{}, nil
	}))
	t.Cleanup(func() {
		release.Do(func() { close(resume) })
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("submission did not finish")
		}
	})
	go func() {
		defer close(finished)
		first, firstErr = SubmitForReviewWithAuthorityAndOptions(root, taskID, inputSHA, authority, request)
	}()
	select {
	case <-indexing:
	case <-time.After(10 * time.Second):
		t.Fatal("submission did not reach indexing")
	}
	lock := filelock.New(claimTaskWorktreeLockPath(paths.New(root).StatePath(), taskID)).WithTimeout(500 * time.Millisecond)
	if err := lock.WithLockOperation("test-index-lock-release", func() error { return nil }); err != nil {
		t.Fatalf("index refresh held task lock: %v", err)
	}
	before, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	_, err = SubmitForReviewWithAuthorityAndOptions(root, taskID, inputSHA, authority, request)
	var pending *LifecycleError
	if !errors.As(err, &pending) || pending.Outcome.Outcome != models.LifecycleStateChanged || pending.Outcome.SafeAction != "requery" || pending.Outcome.Effects != "unknown" {
		t.Fatalf("concurrent pending submission = %v; want STATE_CHANGED/requery/unknown", err)
	}
	after, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("pending duplicate wrote state")
	}
	release.Do(func() { close(resume) })
	<-finished
	if firstErr != nil {
		t.Fatalf("original submission: %v", firstErr)
	}
	replay, err := SubmitForReviewWithAuthorityAndOptions(root, taskID, inputSHA, authority, request)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.ReviewCommit != first.ReviewCommit {
		t.Fatalf("completed replay = %+v, %v", replay, err)
	}
}

func TestLifecycleSubmissionGenerationTurnoverDuringIndexing(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, inputSHA, actor, bb := setupSuccessfulSubmitScenario(t)
	old := models.AgentAuthority{ID: actor, Generation: "submission-before-restart"}
	current := models.AgentAuthority{ID: actor, Generation: "submission-after-restart"}
	setGeneration := func(authority models.AgentAuthority) {
		t.Helper()
		if err := bb.Modify(func(state *models.State) error {
			agent := state.Agents[actor]
			agent.Generation = authority.Generation
			state.Agents[actor] = agent
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	setGeneration(old)
	// Force the abandoned operation to change HEAD, so its original input
	// cannot silently become the new generation's candidate.
	if err := os.WriteFile(filepath.Join(root, "restart-integration.txt"), []byte("integration advanced\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "restart-integration.txt")
	testhelpers.MustGit(t, root, "commit", "-m", "Advance before interrupted submission")
	indexing, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var refreshes atomic.Int32
	var release sync.Once
	var oldErr error
	t.Cleanup(replaceSubmitReviewScipRefreshForTest(func(scipsearch.RefreshOptions) (scipsearch.RefreshResult, error) {
		if refreshes.Add(1) == 1 {
			close(indexing)
			<-resume
		}
		return scipsearch.RefreshResult{}, nil
	}))
	t.Cleanup(func() {
		release.Do(func() { close(resume) })
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("old submission did not finish")
		}
	})
	go func() {
		defer close(finished)
		_, oldErr = SubmitForReviewWithAuthority(root, taskID, inputSHA, old)
	}()
	select {
	case <-indexing:
	case <-time.After(10 * time.Second):
		t.Fatal("old submission did not reach unlocked indexing")
	}
	setGeneration(current)
	before := ownershipStateBytes(t, paths.New(root).StatePath())
	_, err := SubmitForReviewWithAuthority(root, taskID, inputSHA, current)
	requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "none")
	if !bytes.Equal(before, ownershipStateBytes(t, paths.New(root).StatePath())) {
		t.Fatal("stale input changed the abandoned reservation")
	}
	sha := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD")
	if sha == inputSHA {
		t.Fatal("fixture did not rebase the original input")
	}
	fresh, err := SubmitForReviewWithAuthority(root, taskID, sha, current)
	if err != nil || fresh.Outcome != models.LifecycleCompleted {
		t.Fatalf("new generation failed to progress: %+v, %v", fresh, err)
	}
	completed := ownershipStateBytes(t, paths.New(root).StatePath())
	release.Do(func() { close(resume) })
	<-finished
	requireLifecycleError(t, oldErr, models.LifecycleStaleCaller, "stop", "unknown")
	_, err = SubmitForReviewWithAuthority(root, taskID, inputSHA, old)
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "none")
	if !bytes.Equal(completed, ownershipStateBytes(t, paths.New(root).StatePath())) {
		t.Fatal("old process changed the new generation's completed state")
	}
	task, err := bb.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range task.History {
		if entry.Event == models.TaskEventSubmittedForReview {
			count++
		}
	}
	if count != 1 || task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || refreshes.Load() != 2 {
		t.Fatal("turnover duplicated submission work or retained an abandoned completion")
	}
}

func TestLifecycleSubmissionReplayAfterWorktreeRemoval(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, inputSHA, agentID, bb := setupSuccessfulSubmitScenario(t)
	if _, err := SubmitForReview(root, taskID, inputSHA, agentID); err != nil {
		t.Fatal(err)
	}
	if err := git.New(root).RemoveWorktreeDir(taskID); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.Status, task.Worktree, task.AssignedTo = models.TaskStatusMerged, nil, nil
		models.AdvanceLifecycle(task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := SubmitForReview(root, taskID, inputSHA, agentID)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "stop" {
		t.Fatalf("historical replay = %+v, %v", replay, err)
	}
	_, err = SubmitForReview(root, taskID, "HEAD", agentID)
	var late *LifecycleError
	if !errors.As(err, &late) || late.Outcome.Outcome != models.LifecycleAlreadyTransitioned || late.Outcome.SafeAction != "stop" {
		t.Fatalf("late HEAD = %v", err)
	}
	after, err := os.Stat(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("historical calls rewrote state")
	}
}

func TestLifecycleSubmissionReplayUsesOriginalRebaseInput(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, inputSHA, agentID, bb := setupSuccessfulSubmitScenario(t)
	if err := os.WriteFile(filepath.Join(root, "integration-only.txt"), []byte("advance integration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "integration-only.txt")
	testhelpers.MustGit(t, root, "commit", "-m", "Advance integration")
	first, err := SubmitForReview(root, taskID, inputSHA, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReviewCommit == inputSHA {
		t.Fatal("fixture did not rewrite the submitted SHA")
	}
	replay, err := SubmitForReview(root, taskID, inputSHA, agentID)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.ReviewCommit != first.ReviewCommit {
		t.Fatalf("original SHA replay = %+v, %v", replay, err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range state.FindTask(taskID).History {
		if event.Event == models.TaskEventSubmittedForReview {
			if event.SubmissionInputCommit != inputSHA || event.SubmissionAttempt != 1 {
				t.Fatalf("missing input provenance: %+v", event)
			}
			return
		}
	}
	t.Fatal("missing submission history")
}

func TestLifecycleSubmissionExactSHARetry(t *testing.T) {
	disableLifecycleTestIndexes(t)
	root, taskID, inputSHA, agentID, bb := setupSuccessfulSubmitScenario(t)
	first, err := SubmitForReview(root, taskID, inputSHA, agentID)
	if err != nil {
		t.Fatalf("first submission: %v", err)
	}
	before, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}

	duplicate, err := SubmitForReview(root, taskID, inputSHA, agentID)
	if err != nil {
		t.Fatalf("identical immutable submission must replay its completion: %v", err)
	}
	if duplicate.ReviewCommit != first.ReviewCommit {
		t.Fatalf("replay commit = %q, original = %q", duplicate.ReviewCommit, first.ReviewCommit)
	}
	encoded, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result["outcome"] != "ALREADY_COMPLETED" {
		t.Errorf("outcome = %v, want ALREADY_COMPLETED", result["outcome"])
	}
	if result["transition_id"] == nil || result["transition_id"] == "" {
		t.Error("replay has no transition identity")
	}
	after, err := os.ReadFile(paths.New(root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("duplicate submission changed durable state")
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range state.FindTask(taskID).History {
		if event.Event == models.TaskEventSubmittedForReview {
			count++
		}
	}
	if count != 1 {
		t.Errorf("submitted events = %d, want 1", count)
	}
}

func TestLifecycleSubmissionLateHEADStops(t *testing.T) {
	for _, status := range []models.TaskStatus{
		models.TaskStatusReadyForReview,
		models.TaskStatusMerged,
		models.TaskStatusAbandoned,
		models.TaskStatusSuperseded,
	} {
		t.Run(string(status), func(t *testing.T) {
			disableLifecycleTestIndexes(t)
			root, taskID, inputSHA, agentID, bb := setupSuccessfulSubmitScenario(t)
			if _, err := SubmitForReview(root, taskID, inputSHA, agentID); err != nil {
				t.Fatalf("first submission: %v", err)
			}
			// Simulate an already-observed later lifecycle state. The late call
			// must classify it without needing to inspect mutable HEAD again.
			if err := bb.Modify(func(state *models.State) error {
				state.FindTask(taskID).Status = status
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			_, err = SubmitForReview(root, taskID, "HEAD", agentID)
			if err == nil {
				t.Fatal("mutable HEAD cannot prove identical intent after state advancement")
			}
			var detailed interface{ SafeDetails() map[string]any }
			if !errors.As(err, &detailed) {
				t.Fatalf("late call lacks structured recovery details: %v", err)
			}
			details := detailed.SafeDetails()
			for key, want := range map[string]string{
				"outcome": "ALREADY_TRANSITIONED", "safe_action": "stop", "task_status": string(status),
			} {
				if details[key] != want {
					t.Errorf("%s = %v, want %s", key, details[key], want)
				}
			}
			if details["transition_id"] == nil || details["transition_id"] == "" {
				t.Error("late call has no current transition identity")
			}
			after, readErr := os.ReadFile(paths.New(root).StatePath())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(before, after) {
				t.Error("late submission changed durable state")
			}
		})
	}
}
