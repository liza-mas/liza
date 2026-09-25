package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// resetAcceptanceTaskForClaim puts the scenario task back where a doer claim
// finds it, mirroring resetForClaim in the lifecycle tests.
func resetAcceptanceTaskForClaim(t *testing.T, bb *db.Blackboard, taskID, agentID string, mutate func(state *models.State, task *models.Task)) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		clearAttemptState(task, attemptStateInitialReset)
		task.Status = models.TaskStatusReady
		task.AssignedTo, task.LeaseExpires = nil, nil
		state.Agents[agentID] = testhelpers.RegisteredTestAgent(models.RoleCoder)
		if mutate != nil {
			mutate(state, task)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// refuseAcceptanceClaim claims the task and returns the claim-stage refusal.
func refuseAcceptanceClaim(t *testing.T, root, taskID, agentID string) *AcceptanceEvidenceError {
	t.Helper()
	_, err := ClaimTask(root, taskID, agentID)
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Claim == nil || refusal.Claim.Digest == "" {
		t.Fatalf("ClaimTask() = %v, want a claim-stage acceptance refusal", err)
	}
	return refusal
}

func acceptanceCoderAuthority(agentID string) models.AgentAuthority {
	return models.AgentAuthority{ID: agentID, Generation: testhelpers.TestAgentGeneration}
}

func mismatchValidation(_ *models.State, task *models.Task) {
	task.Validation = []string{"sh boundary_test.sh", "ruff check ."}
}

func dropParentAllocation(state *models.State, _ *models.Task) {
	state.FindTask("acceptance-parent").Output = nil
}

// Each refusal site is classified where it refuses; a failed Git read is never
// reported as a content or allocation fault.
func TestAcceptanceRefusalClassifiedAtOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(state *models.State, task *models.Task)
		want   AcceptanceFault
	}{
		{"validation differs", mismatchValidation, AcceptanceFaultContent},
		{"allocation heading missing", func(_ *models.State, task *models.Task) { task.PlanRef = "specs/acceptance-plan.md#task-1-slug" }, AcceptanceFaultContent},
		{"no allocating parent", dropParentAllocation, AcceptanceFaultAllocation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
			resetAcceptanceTaskForClaim(t, bb, taskID, agentID, tc.mutate)
			if got := refuseAcceptanceClaim(t, root, taskID, agentID).Class; got != tc.want {
				t.Fatalf("class = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("integration unreadable", func(t *testing.T) {
		root, taskID, _, _, bb := completeAcceptanceScenario(t)
		state := readAcceptanceState(t, bb)
		_, err := loadAcceptanceInput(root, state, state.FindTask(taskID), strings.Repeat("0", 40))
		var refusal *AcceptanceEvidenceError
		if !errors.As(err, &refusal) || refusal.Reason != "cannot inspect integration source" || refusal.Class != "" {
			t.Fatalf("unreadable integration = %#v, want the unclassified read failure", err)
		}
	})
}

// The observation changes with every state input acceptance reads, and only
// with those.
func TestAcceptanceObservationCoversEveryInput(t *testing.T) {
	_, taskID, _, _, bb := completeAcceptanceScenario(t)
	base := readAcceptanceState(t, bb)
	baseDigest := AcceptanceObservation(base, base.FindTask(taskID))
	if baseDigest == "" || AcceptanceObservation(readAcceptanceState(t, bb), readAcceptanceState(t, bb).FindTask(taskID)) != baseDigest {
		t.Fatal("observation is empty or unstable across reads of unchanged state")
	}
	for _, tc := range []struct {
		name    string
		mutate  func(state *models.State)
		changes bool
	}{
		{"task validation", func(s *models.State) { s.FindTask(taskID).Validation = []string{"other"} }, true},
		{"task parent list", func(s *models.State) { s.FindTask(taskID).ParentTask = nil }, true},
		{"parent approvals", func(s *models.State) {
			p := s.FindTask("acceptance-parent")
			p.Approvals = append(p.Approvals, models.Approval{Agent: "code-plan-reviewer-2"})
		}, true},
		{"parent output", func(s *models.State) { s.FindTask("acceptance-parent").Output = nil }, true},
		{"parent missing", func(s *models.State) {
			kept := s.Tasks[:0]
			for _, task := range s.Tasks {
				if task.ID != "acceptance-parent" {
					kept = append(kept, task)
				}
			}
			s.Tasks = kept
		}, true},
		{"reaffirmation for the parent", func(s *models.State) {
			s.ProofReaffirmations = append(s.ProofReaffirmations, models.ProofReaffirmation{ParentTask: "acceptance-parent", ReferenceID: "identity"})
		}, true},
		{"reaffirmation for another parent", func(s *models.State) {
			s.ProofReaffirmations = append(s.ProofReaffirmations, models.ProofReaffirmation{ParentTask: "unrelated-plan", ReferenceID: "identity"})
		}, false},
		{"unrelated task", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "unrelated", Status: models.TaskStatusReady, Created: time.Now().UTC()})
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := readAcceptanceState(t, bb)
			tc.mutate(state)
			changed := AcceptanceObservation(state, state.FindTask(taskID)) != baseDigest
			if changed != tc.changes {
				t.Fatalf("digest changed = %v, want %v", changed, tc.changes)
			}
		})
	}
}

// A content refusal blocks with a valid BLOCKED record the orchestrator acts on,
// and a second escalation of the same refusal is a no-op.
func TestBlockAcceptanceRefusedTask_ContentRefusalBlocksOnce(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, mismatchValidation)
	refusal := refuseAcceptanceClaim(t, root, taskID, agentID)

	blocked, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal)
	if err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
	}
	state := readAcceptanceState(t, bb)
	task := state.FindTask(taskID)
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.HasPrefix(*task.BlockedReason, "acceptance_evidence_invalid: acceptance.source (specs/acceptance-plan.md#Task 1): validation must equal") {
		t.Fatalf("blocked task = %s %v", task.Status, task.BlockedReason)
	}
	if len(task.BlockedQuestions) != 1 || !strings.Contains(task.BlockedQuestions[0], "replace-task") || !strings.Contains(task.BlockedQuestions[0], "unblock-task "+taskID) {
		t.Fatalf("blocked_questions = %v", task.BlockedQuestions)
	}
	// Whole-state validation of the BLOCKED record runs in the agent-package
	// test, whose fixture is a complete state; this scenario task is not.
	if err := statevalidate.ValidateTaskLifecycle(task); err != nil || task.RepairRequest != nil {
		t.Fatalf("blocked lifecycle = %v, repair_request = %v; want a valid lifecycle and no partial repair request", err, task.RepairRequest)
	}
	if CountActionableBlockedTasks(state) != 1 {
		t.Fatal("blocked task does not wake the orchestrator")
	}

	before := readAcceptanceState(t, bb)
	if again, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal); err != nil || again {
		t.Fatalf("second escalation = %v, %v; want a no-op", again, err)
	}
	requireAcceptanceStateUnchanged(t, bb, before)
}

// A refusal is escalated only while it is current: every supported repair,
// a moved integration ref, a new owner or a stale registration leaves the task
// as it is.
func TestBlockAcceptanceRefusedTask_StaleRefusalLeavesTaskUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name      string
		refuse    func(state *models.State, task *models.Task)
		repair    func(t *testing.T, root, taskID, agentID string, bb *db.Blackboard)
		authority func(agentID string) models.AgentAuthority
		wantErr   bool
	}{
		{name: "task corrected", refuse: mismatchValidation, repair: func(t *testing.T, _, taskID, _ string, bb *db.Blackboard) {
			modifyAcceptanceState(t, bb, func(s *models.State) { s.FindTask(taskID).Validation = []string{"sh boundary_test.sh"} })
		}},
		{name: "parent authorization repaired", refuse: dropParentAllocation, repair: func(t *testing.T, _, taskID, _ string, bb *db.Blackboard) {
			modifyAcceptanceState(t, bb, func(s *models.State) {
				task := s.FindTask(taskID)
				s.FindTask("acceptance-parent").Output = []models.OutputEntry{{PlanRef: task.PlanRef, SpecRef: task.SpecRef, Validation: task.Validation}}
			})
		}},
		{name: "integration moved", refuse: mismatchValidation, repair: func(t *testing.T, root, _, _ string, _ *db.Blackboard) {
			// The fixture root has integration checked out, so a commit advances it.
			if err := os.WriteFile(filepath.Join(root, "later.txt"), []byte("later\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "later.txt")
			testhelpers.MustGit(t, root, "commit", "-m", "test: advance integration")
		}},
		{name: "claimed by another doer", refuse: mismatchValidation, repair: func(t *testing.T, _, taskID, _ string, bb *db.Blackboard) {
			modifyAcceptanceState(t, bb, func(s *models.State) {
				task := s.FindTask(taskID)
				task.Status = models.TaskStatusImplementing
				task.AssignedTo = testhelpers.StringPtr("coder-2")
			})
		}},
		{name: "stale registration", refuse: mismatchValidation, repair: func(*testing.T, string, string, string, *db.Blackboard) {},
			authority: func(agentID string) models.AgentAuthority {
				return models.AgentAuthority{ID: agentID, Generation: "superseded-generation"}
			}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
			resetAcceptanceTaskForClaim(t, bb, taskID, agentID, tc.refuse)
			refusal := refuseAcceptanceClaim(t, root, taskID, agentID)
			if tc.refuse == nil || refusal.Class == "" {
				t.Fatalf("fixture refusal class = %q", refusal.Class)
			}
			tc.repair(t, root, taskID, agentID, bb)
			before := readAcceptanceState(t, bb)
			authority := acceptanceCoderAuthority(agentID)
			if tc.authority != nil {
				authority = tc.authority(agentID)
			}

			blocked, err := BlockAcceptanceRefusedTask(root, authority, refusal)

			if tc.wantErr != (err != nil) || (err != nil && !IsAgentAuthorityError(err)) || blocked {
				t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want no block (authority error: %v)", blocked, err, tc.wantErr)
			}
			requireAcceptanceStateUnchanged(t, bb, before)
		})
	}
}

// Re-affirming a drifted approved proof is a state-only repair: it moves no
// task field, so only the observation's reaffirmations can fence it.
func TestBlockAcceptanceRefusedTask_ReaffirmationAfterRefusalLeavesTaskUnchanged(t *testing.T) {
	root, taskID, agentID, bb := proofDriftScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, nil)
	refusal := refuseAcceptanceClaim(t, root, taskID, agentID)
	if refusal.Class != AcceptanceFaultAllocation || !strings.Contains(refusal.Reason, "reaffirm-proof") {
		t.Fatalf("fixture refusal = %q (%s), want the drifted-proof allocation refusal", refusal.Reason, refusal.Class)
	}
	recordTestReaffirmation(t, root, bb, taskID, "identity")
	before := readAcceptanceState(t, bb)

	blocked, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal)

	if err != nil || blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want no block after the re-affirmation", blocked, err)
	}
	requireAcceptanceStateUnchanged(t, bb, before)
}

// A cooperating integration move that arrives after the equality check waits
// for the block, so the task is never blocked against a ref that has moved.
// The mover takes the completion linearization, as wt-merge's forward and
// rollback paths do.
func TestBlockAcceptanceRefusedTask_IntegrationMoveAfterEqualityWaitsForBlock(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, mismatchValidation)
	refusal := refuseAcceptanceClaim(t, root, taskID, agentID)
	observed := refusal.Claim.IntegrationCommit
	// A later integration commit that is not yet on the ref: the mover applies it.
	later := testhelpers.MustGit(t, root, "commit-tree", observed+"^{tree}", "-p", observed, "-m", "test: later integration")

	const moverOperation = "test acceptance block cooperating integration move"
	moverReachedLock := make(chan struct{})
	moverDone := make(chan error, 1)
	beforeEffectiveIntegrationCompletionLinearizationTestHook = func(operation string) {
		if operation == moverOperation {
			close(moverReachedLock)
		}
	}
	t.Cleanup(func() {
		beforeEffectiveIntegrationCompletionLinearizationTestHook = nil
		testAcceptanceClaimBlockHooks = nil
	})
	testAcceptanceClaimBlockHooks = &acceptanceClaimBlockTestHooks{
		afterIntegrationEqualityCheck: func() {
			go func() {
				moverDone <- withEffectiveIntegrationCompletionLinearization(root, moverOperation, func() error {
					state, err := bb.Read()
					if err != nil {
						return err
					}
					if task := state.FindTask(taskID); task == nil || task.Status != models.TaskStatusBlocked {
						return fmt.Errorf("integration mover ran before the block committed: %+v", task)
					}
					return withIntegrationMutationLock(root, "test acceptance block ref move", func() error {
						return git.New(root).UpdateRef("refs/heads/integration", later, observed)
					})
				})
			}()
			<-moverReachedLock
			select {
			case err := <-moverDone:
				t.Fatalf("cooperating integration move completed inside the equality/block boundary: %v", err)
			default:
			}
		},
	}

	blocked, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal)
	if err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
	}
	if err := <-moverDone; err != nil {
		t.Fatalf("cooperating integration move: %v", err)
	}
	if got := testhelpers.MustGit(t, root, "rev-parse", "integration"); got != later {
		t.Fatalf("integration = %s, want the post-block move %s", got, later)
	}
}

// A rejected task reclaimed after its adopted declaration disappeared blocks
// without downgrading the adopted source.
func TestBlockAcceptanceRefusedTask_RejectedReclaimKeepsAdoptedSource(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, nil)
	if _, err := ClaimTask(root, taskID, agentID); err != nil {
		t.Fatalf("adopting claim: %v", err)
	}
	adopted := readAcceptanceState(t, bb).FindTask(taskID).AcceptanceSource
	if adopted == nil {
		t.Fatal("fixture claim did not adopt the source")
	}
	planPath := filepath.Join(root, "specs/acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte(strings.Replace(string(plan), "### Acceptance Contract", "### Removed acceptance marker", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: remove adopted acceptance marker")
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, func(_ *models.State, task *models.Task) {
		task.Status = models.TaskStatusRejected
		task.RejectionReason = testhelpers.StringPtr("changes requested")
		lease := time.Now().UTC().Add(time.Hour)
		task.AssignedTo, task.LeaseExpires = &agentID, &lease
	})
	refusal := refuseAcceptanceClaim(t, root, taskID, agentID)

	blocked, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal)

	if err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
	}
	task := readAcceptanceState(t, bb).FindTask(taskID)
	if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil || task.AcceptanceSource == nil || *task.AcceptanceSource != *adopted {
		t.Fatalf("blocked reclaim = %s assigned=%v source=%v, want BLOCKED, unassigned, adopted source kept", task.Status, task.AssignedTo, task.AcceptanceSource)
	}
}

// After the orchestrator corrects the allocation, unblock-task restores the
// task and a doer claims it normally.
func TestBlockAcceptanceRefusedTask_UnblockAfterRepairRestoresClaim(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, mismatchValidation)
	refusal := refuseAcceptanceClaim(t, root, taskID, agentID)
	if blocked, err := BlockAcceptanceRefusedTask(root, acceptanceCoderAuthority(agentID), refusal); err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v", blocked, err)
	}
	modifyAcceptanceState(t, bb, func(s *models.State) {
		s.FindTask(taskID).Validation = []string{"sh boundary_test.sh"}
		s.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent(models.RoleOrchestrator)
	})

	if _, err := UnblockTaskWithOptions(root, taskID, "allocation corrected", "orchestrator-1", UnblockTaskOptions{}); err != nil {
		t.Fatalf("UnblockTaskWithOptions() error: %v", err)
	}
	if _, err := ClaimTask(root, taskID, agentID); err != nil {
		t.Fatalf("claim after repair: %v", err)
	}
	if task := readAcceptanceState(t, bb).FindTask(taskID); task.Status != models.TaskStatusImplementing || task.AcceptanceSource == nil {
		t.Fatalf("restored task = %s source=%v, want IMPLEMENTING with the adopted source", task.Status, task.AcceptanceSource)
	}
}

func modifyAcceptanceState(t *testing.T, bb *db.Blackboard, mutate func(state *models.State)) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		mutate(state)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
