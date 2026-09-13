package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func completeAcceptanceScenario(t *testing.T) (string, string, string, string, *db.Blackboard) {
	t.Helper()
	root, taskID, _, agentID, bb := setupAcceptanceScenario(t)
	wt := git.New(root).GetWorktreePath(taskID)
	// Admission verifies the complete declared mapping; whether the named replay
	// assertion adequately exercises concurrency remains the reviewer's decision.
	manifest := `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"boundary_test.sh","assertion":"identity assertion","command_index":0},{"obligation_id":"AC-replay","file":"boundary_test.sh","assertion":"replay assertion","command_index":0}]}` + "\n"
	if err := os.WriteFile(filepath.Join(wt, "acceptance/task-1.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "acceptance/task-1.json")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: complete acceptance mapping")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	return root, taskID, commit, agentID, bb
}

func readAcceptanceState(t *testing.T, bb *db.Blackboard) *models.State {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func registerAcceptanceReviewer(t *testing.T, bb *db.Blackboard, id string) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		state.Agents[id] = testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func requireAcceptanceError(t *testing.T, err error, taskID string) {
	t.Helper()
	var evidenceErr *AcceptanceEvidenceError
	if !errors.As(err, &evidenceErr) || !strings.Contains(err.Error(), taskID) || !strings.Contains(err.Error(), "update-review-commit") {
		t.Fatalf("want typed acceptance error with task ID and repair instruction, got %v", err)
	}
}

func TestAcceptanceLifecycleClaimAdoptsSourceAndReclaimCannotDowngrade(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetForClaim := func(state *models.State) error {
		task := state.FindTask(taskID)
		clearAttemptState(task, attemptStateInitialReset)
		task.Status = models.TaskStatusReady
		task.AssignedTo, task.LeaseExpires = nil, nil
		state.Agents[agentID] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		return nil
	}
	if err := bb.Modify(resetForClaim); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTask(root, taskID, agentID); err != nil {
		t.Fatalf("normal claim did not adopt reviewed acceptance source: %v", err)
	}
	adopted := readAcceptanceState(t, bb).FindTask(taskID)
	if adopted.AcceptanceSource == nil || adopted.AcceptanceSource.Ref != adopted.PlanRef || adopted.AcceptanceSource.ParentTask != "acceptance-parent" || adopted.Status != models.TaskStatusImplementing {
		t.Fatalf("claim omitted reviewed source provenance: %#v", adopted)
	}
	planPath := filepath.Join(root, "specs/acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte(strings.Replace(string(plan), "### Acceptance Contract", "### Removed acceptance marker", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: remove adopted acceptance marker")
	if err := bb.Modify(resetForClaim); err != nil {
		t.Fatal(err)
	}
	before := readAcceptanceState(t, bb)
	_, err = ClaimTask(root, taskID, agentID)
	requireAcceptanceError(t, err, taskID)
	after := readAcceptanceState(t, bb)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) || !reflect.DeepEqual(adopted.AcceptanceSource, after.FindTask(taskID).AcceptanceSource) {
		t.Fatal("failed reclaim downgraded source adoption or mutated claim state")
	}
}

func TestAcceptanceLifecycleSubmissionReceipt(t *testing.T) {
	for _, advanceIntegration := range []bool{false, true} {
		name := "current integration"
		if advanceIntegration {
			name = "post rebase"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			if advanceIntegration {
				if err := os.WriteFile(filepath.Join(root, "independent.txt"), []byte("independent change\n"), 0644); err != nil {
					t.Fatal(err)
				}
				testhelpers.MustGit(t, root, "add", "independent.txt")
				testhelpers.MustGit(t, root, "commit", "-m", "test: advance integration before submission")
			}
			if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
				t.Fatal(err)
			}
			state := readAcceptanceState(t, bb)
			task := state.FindTask(taskID)
			receipt := task.AcceptanceReceipt
			head := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD")
			if receipt == nil || receipt.ReviewCommit != head || task.ReviewCommit == nil || *task.ReviewCommit != head {
				t.Fatalf("receipt does not bind actual review HEAD %s: %#v", head, receipt)
			}
			if advanceIntegration && head == commit {
				t.Fatal("fixture did not exercise a rewritten post-rebase commit")
			}
			if task.Status != models.TaskStatusReadyForReview || len(receipt.Mappings) != 2 || len(receipt.Commands) != 1 {
				t.Fatalf("missing admitted mappings/execution: %#v", task)
			}
			if receipt.Commands[0].ExitCode != 0 || receipt.Commands[0].Output != "PASS identity assertion\n" {
				t.Fatalf("receipt omitted actual execution output: %#v", receipt.Commands)
			}
			blob := testhelpers.MustGit(t, root, "rev-parse", head+":acceptance/task-1.json")
			if receipt.ManifestBlob != blob || !reflect.DeepEqual(task.AcceptanceSource, &receipt.Source) {
				t.Fatal("receipt does not bind committed manifest and adopted source")
			}
			if err := validateAcceptanceForAssignment(root, state, task); err != nil {
				t.Fatalf("admitted receipt is not reviewable: %v", err)
			}
		})
	}
}

func TestAcceptanceLifecycleReviewerRefusesInvalidReceipt(t *testing.T) {
	for _, missing := range []bool{true, false} {
		name := "stale"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
				t.Fatal(err)
			}
			registerAcceptanceReviewer(t, bb, "code-reviewer-1")
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask(taskID)
				if missing {
					task.AcceptanceReceipt = nil
				} else {
					task.AcceptanceReceipt.ReviewCommit = strings.Repeat("0", 40)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readAcceptanceState(t, bb)
			_, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", TaskID: taskID})
			requireAcceptanceError(t, err, taskID)
			after := readAcceptanceState(t, bb)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("receipt refusal mutated task or reviewer state")
			}
		})
	}
}

func TestAcceptanceLifecycleSameHEADRepair(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatal(err)
	}
	registerAcceptanceReviewer(t, bb, "code-reviewer-1")
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", TaskID: taskID}); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask(taskID).AcceptanceReceipt = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := UpdateReviewCommit(root, taskID, "human")
	if err != nil {
		t.Fatalf("same-HEAD repair failed: %v", err)
	}
	state := readAcceptanceState(t, bb)
	task := state.FindTask(taskID)
	if result.NewReviewCommit != commit || !result.ReviewerReleased || task.AcceptanceReceipt == nil || task.AcceptanceReceipt.Commands[0].Output != "PASS identity assertion\n" {
		t.Fatalf("repair did not rerun evidence and release reviewer: %#v", result)
	}
	if task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil || state.Agents["code-reviewer-1"].CurrentTask != nil {
		t.Fatal("repaired task did not return to unclaimed submission")
	}
}

func TestAcceptanceLifecycleFailedSameHEADRepairIsAtomic(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	wt := git.New(root).GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wt, "boundary_test.sh"), []byte("set -eu\nif [ \"${ACCEPTANCE_FIXTURE_FAIL:-}\" = yes ]; then printf 'FAIL identity assertion\\n' >&2; exit 7; fi\nprintf 'PASS identity assertion\\n'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "boundary_test.sh")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: external acceptance prerequisite")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatal(err)
	}
	registerAcceptanceReviewer(t, bb, "code-reviewer-1")
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", TaskID: taskID}); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask(taskID).AcceptanceReceipt = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := readAcceptanceState(t, bb)
	t.Setenv("ACCEPTANCE_FIXTURE_FAIL", "yes")
	_, err := UpdateReviewCommit(root, taskID, "human")
	requireAcceptanceError(t, err, taskID)
	if !strings.Contains(err.Error(), "exit 7") || !strings.Contains(err.Error(), "FAIL identity assertion") {
		t.Fatalf("repair did not execute failing canonical command: %v", err)
	}
	after := readAcceptanceState(t, bb)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
		t.Fatal("failed receipt repair changed review boundary or released reviewer")
	}
}

func TestAcceptanceLifecycleReturningReviewerRefusalIsAtomic(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatal(err)
	}
	reviewer := "code-reviewer-1"
	registerAcceptanceReviewer(t, bb, reviewer)
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.AcceptanceReceipt = nil
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &reviewer})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := readAcceptanceState(t, bb)
	_, err := AwaitResubmission(context.Background(), root, taskID, reviewer, time.Second)
	requireAcceptanceError(t, err, taskID)
	after := readAcceptanceState(t, bb)
	if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
		t.Fatal("returning reviewer receipt refusal changed task or agent state")
	}
}

func TestAcceptanceLifecycleWaitingReviewerRefusalRestoresOwnership(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		name := "watcher"
		if fallback {
			name = "polling"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
				t.Fatal(err)
			}
			reviewer := "code-reviewer-1"
			registerAcceptanceReviewer(t, bb, reviewer)
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask(taskID)
				task.Status = models.TaskStatusRejected
				task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &reviewer})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readAcceptanceState(t, bb)
			previousWatcher := newAwaitResubmissionWatcher
			t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })
			enteredWait := false
			newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
				waiting := readAcceptanceState(t, bb)
				task := waiting.FindTask(taskID)
				agent := waiting.Agents[reviewer]
				if task.ReviewingBy == nil || *task.ReviewingBy != reviewer || task.ReviewLeaseExpires == nil ||
					agent.Status != models.AgentStatusWaiting || agent.CurrentTask == nil || *agent.CurrentTask != taskID {
					t.Fatal("test did not reach the wait path with ownership acquired")
				}
				enteredWait = true
				// Simulate resubmission whose receipt is invalidated before reclaim.
				if err := bb.Modify(func(state *models.State) error {
					task := state.FindTask(taskID)
					task.Status = models.TaskStatusReadyForReview
					task.AcceptanceReceipt = nil
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if fallback {
					return nil, errors.New("test polling fallback")
				}
				return bb.WatchForChanges()
			}
			_, err := AwaitResubmissionWithOptions(context.Background(), root, taskID, reviewer, 5*time.Second,
				AwaitResubmissionOptions{AbortPollInterval: time.Millisecond, FallbackPollInterval: time.Millisecond})
			requireAcceptanceError(t, err, taskID)
			if !enteredWait {
				t.Fatal("test bypassed the waiting path")
			}
			// Only the simulated doer's status and receipt changes should survive.
			before.FindTask(taskID).Status = models.TaskStatusReadyForReview
			before.FindTask(taskID).AcceptanceReceipt = nil
			after := readAcceptanceState(t, bb)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("evidence refusal did not restore pre-wait ownership and agent state")
			}
		})
	}
}

func TestAcceptanceLifecycleCleanupPreservesAdoption(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatal(err)
	}
	original := readAcceptanceState(t, bb).FindTask(taskID)
	for _, profile := range []attemptStateCleanupProfile{attemptStateReviewRejection, attemptStateClaimReleaseReset, attemptStateInitialReset, attemptStateRetire, attemptStateIntegrationFixClaim} {
		task := *original
		clearAttemptState(&task, profile)
		if task.AcceptanceReceipt != nil || task.ReviewCommit != nil || !reflect.DeepEqual(task.AcceptanceSource, original.AcceptanceSource) {
			t.Fatalf("cleanup profile %d cleared adoption or retained stale evidence", profile)
		}
	}
}

func TestAcceptanceLifecycleReviewerCandidateSelection(t *testing.T) {
	for _, allInvalid := range []bool{false, true} {
		name := "healthy candidate survives invalid higher priority"
		if allInvalid {
			name = "all invalid candidates reported"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
				t.Fatal(err)
			}
			g := git.New(root)
			invalidID := "invalid-acceptance-candidate"
			if _, err := g.CreateWorktree(invalidID, commit); err != nil {
				t.Fatal(err)
			}
			registerAcceptanceReviewer(t, bb, "code-reviewer-1")
			registerAcceptanceReviewer(t, bb, "code-reviewer-2")
			if err := bb.Modify(func(state *models.State) error {
				original := state.FindTask(taskID)
				original.Priority = 2
				invalid := *original
				invalid.ID, invalid.Priority = invalidID, 1
				worktree := g.GetWorktreeRelPath(invalidID)
				invalid.Worktree = &worktree
				invalid.AcceptanceReceipt = nil
				if allInvalid {
					original.AcceptanceReceipt = nil
				}
				state.Tasks = append(state.Tasks, invalid)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readAcceptanceState(t, bb)
			result, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1"})
			if allInvalid {
				requireAcceptanceError(t, err, invalidID)
				if !strings.Contains(err.Error(), taskID) {
					t.Fatalf("all-invalid error omitted second blocked task: %v", err)
				}
				after := readAcceptanceState(t, bb)
				if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
					t.Fatal("all-invalid candidate scan mutated tasks or agents")
				}
				return
			}
			if err != nil || result.TaskID != taskID {
				t.Fatalf("invalid receipt prevented healthy claim: %#v, %v", result, err)
			}
			after := readAcceptanceState(t, bb)
			if !reflect.DeepEqual(before.FindTask(invalidID), after.FindTask(invalidID)) {
				t.Fatal("healthy claim mutated skipped invalid candidate")
			}
			_, err = ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-2"})
			requireAcceptanceError(t, err, invalidID)
		})
	}
}
