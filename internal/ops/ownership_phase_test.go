package ops

import (
	"bytes"
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

// Phase-bounded proofs of the ownership release and recovery concerns. Tests
// that set a package-level test hook are not parallel.

// setupReviewingTaskWithWorktree returns a project whose task-review is under
// review by code-reviewer-1 with a real worktree on branch task/task-review.
func setupReviewingTaskWithWorktree(t *testing.T, mutate func(*models.State)) (root, stateFile string, gitWrapper *git.Git) {
	t.Helper()
	root = t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ = testhelpers.SetupLizaDir(t, root)
	gitWrapper = git.New(root)
	baseCommit, err := gitWrapper.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	reviewCommit, err := gitWrapper.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatalf("GetWorktreeHEAD() error: %v", err)
	}
	taskID, reviewerID, worktreeRef := "task-review", "code-reviewer-1", ".worktrees/task-review"
	state := testhelpers.CreateValidState()
	state.Agents[reviewerID] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing, CurrentTask: &taskID}
	state.Tasks = []models.Task{{
		ID:           taskID,
		Description:  "review candidate",
		Status:       models.TaskStatusReviewing,
		RolePair:     "coding-pair",
		Priority:     1,
		ReviewingBy:  &reviewerID,
		ReviewCommit: &reviewCommit,
		BaseCommit:   &baseCommit,
		Worktree:     &worktreeRef,
		SpecRef:      "spec.md",
		DoneWhen:     "tests pass",
		Scope:        "small",
	}}
	if mutate != nil {
		mutate(state)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile, gitWrapper
}

func requireWorktreeAndBranch(t *testing.T, root string, gitWrapper *git.Git, taskID string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".worktrees", taskID)); err != nil {
		t.Fatalf("worktree for %s missing: %v", taskID, err)
	}
	exists, err := gitWrapper.BranchExists("task/" + taskID)
	if err != nil || !exists {
		t.Fatalf("branch for %s exists = %v, err = %v", taskID, exists, err)
	}
}

func TestRecoverTaskFresh_PreflightFailureReturnsBeforeCleanup(t *testing.T) {
	root, stateFile, gitWrapper := setupReviewingTaskWithWorktree(t, func(state *models.State) {
		state.Config.IntegrationBranch = "missing-integration"
	})
	cleanupCalls := 0
	testRecoverTaskHooks = &recoverTaskTestHooks{cleanupGitArtifacts: func() error { cleanupCalls++; return nil }}
	t.Cleanup(func() { testRecoverTaskHooks = nil })

	_, err := RecoverTaskWithOptions(root, "task-review", "preflight", RecoverTaskOptions{Fresh: true})
	testhelpers.RequireErrorContains(t, err, "fresh reset preflight failed for integration branch missing-integration")
	if cleanupCalls != 0 {
		t.Fatalf("cleanup ran %d times after a failed preflight", cleanupCalls)
	}
	requireWorktreeAndBranch(t, root, gitWrapper, "task-review")
	state, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task := state.FindTask("task-review")
	if task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || task.Worktree == nil {
		t.Fatalf("task changed after a failed preflight: %+v", task)
	}
	if _, ok := state.Agents["code-reviewer-1"]; !ok {
		t.Fatal("snapshot agent released after a failed preflight")
	}
}

func TestRecoverTaskFresh_CompletionErrorAfterEffectsAbortsCallback(t *testing.T) {
	root, stateFile, gitWrapper := setupReviewingTaskWithWorktree(t, nil)
	cleanupCalls := 0
	testRecoverTaskHooks = &recoverTaskTestHooks{
		cleanupGitArtifacts: func() error { cleanupCalls++; return nil },
		// A malformed receipt makes the completion's lifecycle validation fail
		// after the Git effects ran and the task was mutated.
		beforeFreshComplete: func(task *models.Task) {
			task.Lifecycle = &models.TaskLifecycle{Receipts: []models.LifecycleReceipt{{}}}
		},
	}
	t.Cleanup(func() { testRecoverTaskHooks = nil })

	_, err := RecoverTaskWithOptions(root, "task-review", "completion", RecoverTaskOptions{Fresh: true})
	testhelpers.RequireErrorContains(t, err, "failed to record fresh recovery")
	if cleanupCalls != 1 {
		t.Fatalf("cleanup ran %d times, want the Git effects to have run once", cleanupCalls)
	}
	requireWorktreeAndBranch(t, root, gitWrapper, "task-review")
	state, readErr := db.New(stateFile).Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task := state.FindTask("task-review")
	if task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || len(state.HumanNotes) != 0 {
		t.Fatalf("aborted callback published state: %+v", task)
	}
	if _, ok := state.Agents["code-reviewer-1"]; !ok {
		t.Fatal("aborted callback released the snapshot agent")
	}
}

func TestRecoverTask_AbsentTaskRefusesRequestBinding(t *testing.T) {
	root, _ := setupEmptyState(t)
	testhelpers.SetupTestGitRepo(t, root)
	gitWrapper := git.New(root)
	if _, err := gitWrapper.CreateWorktree("task-9", "integration"); err != nil {
		t.Fatal(err)
	}
	opts := RecoverTaskOptions{Force: true, RequestOptions: LifecycleRequestOptions{RequestID: "absent", ExpectedTransition: strings.Repeat("a", 64)}}
	_, err := RecoverTaskWithOptions(root, "task-9", "absent", opts)
	testhelpers.RequireErrorContains(t, err, "cannot bind a recovery request to an absent task")
	requireWorktreeAndBranch(t, root, gitWrapper, "task-9")
}

// A preserve refusal that attached nothing was a read-only inspection, so the
// refused request retires its own preparation (D60); after an attach attempt
// the preparation fence stays.
func TestRecoverTask_PreserveRefusalPreparationFence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		removeWt     bool
		wantFenceSet bool
	}{
		{name: "no attach retires the preparation", removeWt: false, wantFenceSet: false},
		{name: "attach attempt keeps the fence", removeWt: true, wantFenceSet: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, stateFile, gitWrapper := setupReviewingTaskWithWorktree(t, func(state *models.State) {
				// A review commit the worktree HEAD never matches: the preserve
				// check refuses after the health and status checks.
				other := strings.Repeat("b", 40)
				state.Tasks[0].ReviewCommit = &other
			})
			if tc.removeWt {
				testhelpers.MustGit(t, root, "worktree", "remove", "--force", ".worktrees/task-review")
			}
			_, err := RecoverTaskWithOptions(root, "task-review", "preserve", RecoverTaskOptions{})
			testhelpers.RequireErrorContains(t, err, "refuses to review a different candidate")
			state, readErr := db.New(stateFile).Read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			task := state.FindTask("task-review")
			hasFence := task.Lifecycle != nil && task.Lifecycle.Preparation != nil
			if hasFence != tc.wantFenceSet {
				t.Fatalf("preparation fence present = %v, want %v", hasFence, tc.wantFenceSet)
			}
			if exists, _ := gitWrapper.BranchExists("task/task-review"); !exists {
				t.Fatal("preserve refusal removed the task branch")
			}
		})
	}
}

func TestRecoverAgent_ClaimSetChangedBeforeRecovery(t *testing.T) {
	root, statePath := setupImplementingTask(t, 999999)
	testhelpers.SetupTestGitRepo(t, root)
	before := ownershipStateBytes(t, statePath)
	// The caller holds no lock for task-1: its claim set differs from the lock set.
	_, err := recoverAgentLifecycle(root, "coder-1", false, "crashed", LifecycleRequestOptions{}, nil, new(bool))
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("claim-set mismatch changed state")
	}
}

func replaceCoderGeneration(t *testing.T, statePath string) {
	t.Helper()
	if err := db.For(statePath).Modify(func(state *models.State) error {
		agent := state.Agents["coder-1"]
		agent.Generation = "replacement-generation"
		state.Agents["coder-1"] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverAgent_RegistrationReplacedBeforePreparation(t *testing.T) {
	root, statePath := setupImplementingTask(t, 999999)
	testhelpers.SetupTestGitRepo(t, root)
	testRecoverAgentHooks = &recoverAgentTestHooks{beforePrepare: func() { replaceCoderGeneration(t, statePath) }}
	t.Cleanup(func() { testRecoverAgentHooks = nil })

	_, err := RecoverAgent(root, "coder-1", false, "crashed")
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "none")
	state, readErr := db.New(statePath).Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task := state.FindTask("task-1")
	if task.AssignedTo == nil || *task.AssignedTo != "coder-1" || (task.Lifecycle != nil && task.Lifecycle.Preparation != nil) {
		t.Fatalf("refused recovery changed the claim or recorded a preparation: %+v", task)
	}
}

func TestRecoverAgent_RegistrationReplacedBetweenGitRemovalAndCompletion(t *testing.T) {
	root, statePath := setupImplementingTask(t, 999999)
	testhelpers.SetupTestGitRepo(t, root)
	testRecoverAgentHooks = &recoverAgentTestHooks{afterGitRemoval: func() { replaceCoderGeneration(t, statePath) }}
	t.Cleanup(func() { testRecoverAgentHooks = nil })

	_, err := RecoverAgent(root, "coder-1", false, "crashed")
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "unknown")
	state, readErr := db.New(statePath).Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	task := state.FindTask("task-1")
	if task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
		t.Fatal("stale completion released the claim")
	}
	if _, ok := state.Agents["coder-1"]; !ok {
		t.Fatal("stale completion deleted the replacement registration")
	}
}

func TestRecoverAgent_FailedReleasePreservesAgentWithWarning(t *testing.T) {
	root, statePath := setupImplementingTask(t, 999999)
	testhelpers.SetupTestGitRepo(t, root)
	// Without a pipeline config no claim can be released.
	if err := os.Remove(filepath.Join(filepath.Dir(statePath), "pipeline.yaml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	result, err := RecoverAgent(root, "coder-1", false, "crashed")
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentDeleted || result.ClaimReleased {
		t.Fatalf("failed release deleted the agent or reported a release: %+v", result)
	}
	var preserved bool
	for _, warning := range result.Warnings {
		preserved = preserved || strings.Contains(warning, "agent coder-1 preserved because one or more claims could not be released")
	}
	if !preserved {
		t.Fatalf("missing preserved-agent warning: %v", result.Warnings)
	}
	state, _ := db.New(statePath).Read()
	if _, ok := state.Agents["coder-1"]; !ok {
		t.Fatal("agent was deleted")
	}
}

func TestHandoff_ReplayAndRefusalPublishNothing(t *testing.T) {
	root, statePath := setupHandoffScenario(t)
	bb := db.For(statePath)
	opts := ownershipRequestOptions(t, bb, "handoff-1")
	input := &HandoffInput{Request: opts, ProjectRoot: root, TaskID: "task-1", Summary: "s", NextAction: "n", AgentID: "coder-1"}
	first, err := Handoff(input)
	if err != nil {
		t.Fatal(err)
	}
	after := ownershipStateBytes(t, statePath)
	replay, err := Handoff(input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.CompletedTransitionID != first.CompletedTransitionID {
		t.Fatalf("invalid replay: %+v", replay)
	}
	if !bytes.Equal(after, ownershipStateBytes(t, statePath)) {
		t.Fatal("replay published state")
	}
	// A second, unrequested handoff is refused because one is already pending.
	_, err = Handoff(&HandoffInput{ProjectRoot: root, TaskID: "task-1", Summary: "s", NextAction: "n", AgentID: "coder-1"})
	testhelpers.RequireErrorContains(t, err, "handoff is already pending")
	if !bytes.Equal(after, ownershipStateBytes(t, statePath)) {
		t.Fatal("refusal published state")
	}
}

func TestTransitionToNewAttempt_SentinelReplacedSkipsCleanup(t *testing.T) {
	tmpDir, statePath := setupTransitionTest(t)
	testhelpers.CreateTestWorktree(t, tmpDir, "task-1")
	bb := db.New(statePath)
	if err := bb.Modify(func(state *models.State) error {
		wt := ".worktrees/task-1"
		state.FindTask("task-1").Worktree = &wt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	testTransitionHooks = &transitionTestHooks{afterPhase1: func() {
		if err := bb.Modify(func(state *models.State) error {
			intruder := "coder-99"
			state.FindTask("task-1").AssignedTo = &intruder
			return nil
		}); err != nil {
			t.Errorf("simulate concurrent modification: %v", err)
		}
	}}
	t.Cleanup(func() { testTransitionHooks = nil })

	_, err := TransitionToNewAttempt(tmpDir, "task-1", "cap reached")
	testhelpers.RequireErrorContains(t, err, "sentinel replaced")
	if _, statErr := os.Stat(filepath.Join(tmpDir, ".worktrees", "task-1")); statErr != nil {
		t.Fatalf("refused finalisation still cleaned the worktree: %v", statErr)
	}
	state, _ := bb.Read()
	task := state.FindTask("task-1")
	if task.Worktree == nil || task.Attempt != 2 {
		t.Fatalf("refused finalisation lost the recovery evidence: %+v", task)
	}
}

// A combined release is reviewer-then-doer: both ownership effects commit
// together, the history records the reviewer event first, and the retained
// receipt projects both releases.
func TestReleaseClaim_BothClaimsReviewerThenDoerEffectsAndProjection(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, time.Now().UTC())}
	for id, role := range map[string]string{"coder-1": "coder", "code-reviewer-1": "code-reviewer"} {
		state.Agents[id] = models.Agent{Role: role, Status: models.AgentStatusWorking, CurrentTask: testhelpers.StringPtr("task-1")}
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := ReleaseClaim(tmpDir, "task-1", "both", true, "full reset", "human")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReleasedReviewer || !result.ReleasedDoer {
		t.Fatalf("result = %+v", result)
	}
	after, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	task := after.FindTask("task-1")
	if task.AssignedTo != nil || task.LeaseExpires != nil || task.ReviewingBy != nil || task.ReviewLeaseExpires != nil {
		t.Fatalf("ownership not fully released: %+v", task)
	}
	for _, id := range []string{"coder-1", "code-reviewer-1"} {
		if agent := after.Agents[id]; agent.CurrentTask != nil {
			t.Fatalf("agent %s still holds %s", id, *agent.CurrentTask)
		}
	}
	reviewerAt, doerAt := -1, -1
	for index, entry := range task.History {
		switch entry.Event {
		case models.TaskEventReviewClaimReleased:
			reviewerAt = index
		case models.TaskEventDoerClaimReleased:
			doerAt = index
		}
	}
	if reviewerAt < 0 || doerAt < 0 || reviewerAt > doerAt {
		t.Fatalf("history order: reviewer release at %d, doer release at %d", reviewerAt, doerAt)
	}
	receipts := task.Lifecycle.Receipts
	if len(receipts) != 1 || receipts[0].Operation != "release-claim" || !receipts[0].Projection.ReleasedDoer || !receipts[0].Projection.ReleasedReviewer {
		t.Fatalf("receipt projection = %+v", receipts)
	}
}
