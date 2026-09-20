package ops

import (
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

func requireAcceptanceSubmitRejected(t *testing.T, root, taskID, commit, agentID string, bb *db.Blackboard, field string) {
	t.Helper()
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, err = SubmitForReview(root, taskID, commit, agentID)
	var evidenceErr *AcceptanceEvidenceError
	if !errors.As(err, &evidenceErr) || evidenceErr.TaskID != taskID || !strings.HasPrefix(evidenceErr.Field, field) {
		t.Fatalf("SubmitForReview error = %T %v, want acceptance precondition for task %s field %s", err, err, taskID, field)
	}
	requireAcceptanceStateUnchanged(t, bb, before)
}

func requireAcceptanceStateUnchanged(t *testing.T, bb *db.Blackboard, before *models.State) {
	t.Helper()
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected admission changed blackboard state, including task, agent, or review metadata")
	}
}

func requireAcceptancePreparationRetired(t *testing.T, bb *db.Blackboard, before *models.State, taskID string) {
	t.Helper()
	previous := before.FindTask(taskID).Lifecycle
	if previous == nil || previous.Preparation == nil {
		t.Fatal("competing snapshot did not capture the pending submission")
	}
	after := readAcceptanceState(t, bb)
	current := after.FindTask(taskID).Lifecycle
	if current == nil || current.Preparation != nil || current.Revision <= previous.Revision {
		t.Fatalf("refusal did not retire its preparation: %+v", current)
	}
	if current.CompletionSequence != previous.CompletionSequence || len(current.Receipts) != 0 {
		t.Fatalf("source refusal recorded a completion: %+v", current)
	}
	// Normalize only the two retirement fields; preserve every concurrent
	// allocation change and every other lifecycle/domain field for comparison.
	current.Revision = previous.Revision
	current.Preparation = previous.Preparation
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source refusal changed the competing snapshot beyond preparation retirement")
	}
}

func TestAcceptanceProvenance_PlanningScopePreservesPriorAdoption(t *testing.T) {
	for _, adopted := range []bool{false, true} {
		name := "planning task without adoption"
		if adopted {
			name = "planning task with prior coding adoption"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, _, _, bb := completeAcceptanceScenario(t)
			state := readAcceptanceState(t, bb)
			task := state.FindTask(taskID)
			integrationCommit := testhelpers.MustGit(t, root, "rev-parse", "integration")
			original, err := loadAcceptanceInput(root, state, task, integrationCommit)
			if err != nil || original == nil {
				t.Fatalf("coding fixture failed strict acceptance premise: %v", err)
			}
			task.Type = models.TaskTypePlanning
			if adopted {
				task.AcceptanceSource = &original.source
			}
			input, err := loadAcceptanceInput(root, state, task, integrationCommit)
			if err != nil {
				t.Fatal(err)
			}
			if !adopted {
				if input != nil {
					t.Fatal("planning task adopted coding acceptance solely because its carrier contains Acceptance Contract")
				}
				return
			}
			if !reflect.DeepEqual(input, original) {
				t.Fatal("changing task type discarded or changed the previously adopted strict input")
			}
			// A changed type cannot bypass the downgrade guard when the carrier
			// selection subsequently loses its acceptance declaration.
			task.PlanRef = ""
			_, err = loadAcceptanceInput(root, state, task, integrationCommit)
			var evidenceErr *AcceptanceEvidenceError
			if !errors.As(err, &evidenceErr) || evidenceErr.Field != "acceptance.source" {
				t.Fatalf("planning task bypassed adopted-source downgrade guard: %v", err)
			}
		})
	}
}

func TestAcceptanceProvenance_RequiresIndependentPlanningAllocation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(parent, child *models.Task)
	}{
		{"unapproved parent", func(parent, _ *models.Task) {
			parent.ApprovedBy, parent.Approvals = nil, nil
		}},
		{"self approved parent", func(parent, _ *models.Task) {
			parent.ApprovedBy = parent.AssignedTo
			parent.Approvals = []models.Approval{{Agent: *parent.AssignedTo}}
		}},
		{"wrong allocation reference", func(parent, _ *models.Task) {
			parent.Output[0].PlanRef = "specs/other-plan.md#Task 1"
		}},
		{"wrong allocation commands", func(parent, _ *models.Task) {
			parent.Output[0].Validation = []string{"sh unrelated_test.sh"}
		}},
		{"coding parent", func(parent, _ *models.Task) {
			parent.Type, parent.RolePair = models.TaskTypeCoding, "coding-pair"
		}},
		{"unmerged parent", func(parent, _ *models.Task) {
			parent.Status = models.TaskStatusApproved
		}},
		{"carrier outside reviewed change", func(parent, _ *models.Task) {
			parent.BaseCommit = parent.ReviewCommit
		}},
		{"symbolic review commit", func(parent, _ *models.Task) {
			ref := "integration"
			parent.ReviewCommit = &ref
		}},
		{"merge predates reviewed commit", func(parent, _ *models.Task) {
			parent.MergeCommit = parent.BaseCommit
		}},
		{"non direct parent", func(_, child *models.Task) {
			child.ParentTask, child.ParentTasks = nil, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			if err := bb.Modify(func(state *models.State) error {
				tc.mutate(state.FindTask("acceptance-parent"), state.FindTask(taskID))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")
		})
	}
}

func TestAcceptanceProvenance_ParentSubmissionSurvivesOwnershipRelease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*models.Task)
		allow  bool
	}{
		{name: "released author", allow: true},
		{name: "empty live author", allow: true, mutate: func(parent *models.Task) {
			parent.AssignedTo = testhelpers.StringPtr("")
		}},
		{name: "missing submission", mutate: func(parent *models.Task) {
			parent.History = nil
		}},
		{name: "empty author without submission", mutate: func(parent *models.Task) {
			parent.AssignedTo = testhelpers.StringPtr("")
			parent.History = nil
		}},
		{name: "different submitted revision", mutate: func(parent *models.Task) {
			parent.History[0].Commit = parent.BaseCommit
		}},
		{name: "claim is not submission", mutate: func(parent *models.Task) {
			parent.History[0].Event = models.TaskEventClaimed
		}},
		{name: "missing historical author", mutate: func(parent *models.Task) {
			parent.History[0].Agent = nil
		}},
		{name: "self approval", mutate: func(parent *models.Task) {
			parent.ApprovedBy = parent.History[0].Agent
			parent.Approvals = []models.Approval{{Agent: *parent.History[0].Agent}}
		}},
		{name: "conflicting submission authors", mutate: func(parent *models.Task) {
			other := parent.History[0]
			other.Agent = testhelpers.StringPtr("code-planner-2")
			parent.History = append(parent.History, other)
		}},
		{name: "conflicting live author", mutate: func(parent *models.Task) {
			parent.AssignedTo = testhelpers.StringPtr("code-planner-2")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
			if err := bb.Modify(func(state *models.State) error {
				parent := state.FindTask("acceptance-parent")
				parent.History = []models.TaskHistoryEntry{{
					Event: models.TaskEventSubmittedForReview,
					Agent: parent.AssignedTo, Commit: parent.ReviewCommit,
				}}
				parent.AssignedTo = nil
				if tc.mutate != nil {
					tc.mutate(parent)
				}
				task := state.FindTask(taskID)
				clearAttemptState(task, attemptStateInitialReset)
				task.Status = models.TaskStatusReady
				task.AssignedTo, task.LeaseExpires = nil, nil
				state.Agents[agentID] = testhelpers.RegisteredTestAgent(models.RoleCoder)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readAcceptanceState(t, bb)
			_, err := ClaimTask(root, taskID, agentID)
			if !tc.allow {
				requireAcceptanceError(t, err, taskID)
				requireAcceptanceStateUnchanged(t, bb, before)
				return
			}
			if err != nil {
				t.Fatalf("reviewed parent lost authority after ownership release: %v", err)
			}
			task := readAcceptanceState(t, bb).FindTask(taskID)
			if task.Status != models.TaskStatusImplementing || task.AcceptanceSource == nil || task.AcceptanceSource.ParentTask != "acceptance-parent" || task.AcceptanceSource.ParentReviewCommit != *before.FindTask("acceptance-parent").ReviewCommit {
				t.Fatal("claim did not retain the independently reviewed allocation")
			}
		})
	}
}

func TestAcceptanceProvenance_RejectsDisconnectedParentBase(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	parent := readAcceptanceState(t, bb).FindTask("acceptance-parent")
	// Reuse the actual base tree, including pinned requirements, but create an
	// independent history. Blob differences alone do not establish ancestry.
	tree := testhelpers.MustGit(t, root, "rev-parse", *parent.BaseCommit+"^{tree}")
	disconnectedBase := testhelpers.MustGit(t, root, "commit-tree", tree, "-m", "test: disconnected parent history")
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask("acceptance-parent").BaseCommit = &disconnectedBase
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")
}

func TestAcceptanceProvenance_CarrierMustRemainReviewed(t *testing.T) {
	for _, location := range []string{"candidate", "integration", "adopted marker removed"} {
		t.Run(location, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			dir := root
			if location == "candidate" {
				dir = git.New(root).GetWorktreePath(taskID)
			}
			planPath := filepath.Join(dir, "specs", "acceptance-plan.md")
			plan, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			updated := string(plan) + "\n## Unreviewed notes\nUnreviewed source change.\n"
			if location == "adopted marker removed" {
				integrationCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
				blob := testhelpers.MustGit(t, root, "rev-parse", "HEAD:specs/acceptance-plan.md")
				if err := bb.Modify(func(state *models.State) error {
					task := state.FindTask(taskID)
					task.AcceptanceSource = &models.AcceptanceSource{
						Ref: task.PlanRef, Commit: integrationCommit, Blob: blob,
						ParentTask: "acceptance-parent", ParentReviewCommit: integrationCommit,
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				updated = strings.Replace(string(plan), "### Acceptance Contract", "### Legacy Acceptance", 1)
			}
			if err := os.WriteFile(planPath, []byte(updated), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, dir, "add", "specs/acceptance-plan.md")
			testhelpers.MustGit(t, dir, "commit", "-m", "test: change source outside independent review")
			if location == "candidate" {
				commit = testhelpers.MustGit(t, dir, "rev-parse", "HEAD")
			}
			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")
		})
	}
}

func TestAcceptanceProvenance_ArtifactsMustBeRegularCommittedFiles(t *testing.T) {
	for _, artifact := range []string{"manifest", "assertion file"} {
		for _, change := range []string{"missing", "symlink"} {
			t.Run(artifact+" "+change, func(t *testing.T) {
				root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
				wt := git.New(root).GetWorktreePath(taskID)
				name, field := "acceptance/task-1.json", "acceptance.manifest"
				if artifact == "assertion file" {
					name, field = "boundary_test.sh", "acceptance.mappings[AC-identity].file"
				}
				filename := filepath.Join(wt, name)
				if err := os.Remove(filename); err != nil {
					t.Fatal(err)
				}
				if change == "symlink" {
					if err := os.Symlink("../identity.txt", filename); err != nil {
						t.Skipf("symlink unavailable on this host: %v", err)
					}
				}
				testhelpers.MustGit(t, wt, "add", name)
				testhelpers.MustGit(t, wt, "commit", "-m", "test: invalid acceptance artifact")
				commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
				requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, field)
			})
		}
	}
}

func TestAcceptanceProvenance_RejectsDirtyCandidate(t *testing.T) {
	for _, dirty := range []string{"untracked", "unstaged", "staged"} {
		t.Run(dirty, func(t *testing.T) {
			root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
			wt := git.New(root).GetWorktreePath(taskID)
			name := "identity.txt"
			if dirty == "untracked" {
				name = "uncommitted-input.txt"
			}
			if err := os.WriteFile(filepath.Join(wt, name), []byte("uncommitted\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if dirty == "staged" {
				testhelpers.MustGit(t, wt, "add", name)
			}
			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.worktree")
		})
	}
}

func TestAcceptanceProvenance_ExecutionCannotChangeCandidate(t *testing.T) {
	for _, tc := range []struct{ name, script, field string }{
		{"failed command", "printf 'assertion failed\\n'\nexit 7\n", "acceptance.execution"},
		{"creates untracked file", "printf 'generated\\n' > unexpected.txt\n", "acceptance.worktree"},
		{"changes tracked file", "printf 'changed\\n' > identity.txt\n", "acceptance.worktree"},
		{"moves HEAD", "git commit --allow-empty -m 'test: command changed HEAD'\n", "acceptance.review_commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
			wt := git.New(root).GetWorktreePath(taskID)
			if err := os.WriteFile(filepath.Join(wt, "boundary_test.sh"), []byte("set -eu\n"+tc.script), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, wt, "add", "boundary_test.sh")
			testhelpers.MustGit(t, wt, "commit", "-m", "test: validation failure or candidate mutation")
			commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
			before := readAcceptanceState(t, bb)
			_, err := SubmitForReview(root, taskID, commit, agentID)
			var evidenceErr *AcceptanceEvidenceError
			if !errors.As(err, &evidenceErr) || evidenceErr.TaskID != taskID || evidenceErr.Field != tc.field {
				t.Fatalf("submission error = %v, want acceptance refusal at %s", err, tc.field)
			}
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged ||
				lifecycleErr.Outcome.SafeAction != "requery" || lifecycleErr.Outcome.Effects != "unknown" {
				t.Fatalf("post-execution refusal must retain uncertainty: %v", err)
			}
			after := readAcceptanceState(t, bb)
			task := after.FindTask(taskID)
			if task.Lifecycle == nil || task.Lifecycle.Preparation != nil {
				t.Fatal("returned refusal retained its preparation")
			}
			if models.TaskTransitionID(task) == models.TaskTransitionID(before.FindTask(taskID)) {
				t.Fatal("retirement did not invalidate the refused request's boundary")
			}
			// Only retirement revision may change. Review admission, receipts,
			// task history, ownership and every other blackboard field stay intact.
			if task.Lifecycle.Revision == 0 || task.Lifecycle.CompletionSequence != 0 || len(task.Lifecycle.Receipts) != 0 {
				t.Fatalf("rejected admission recorded a lifecycle completion: %+v", task.Lifecycle)
			}
			task.Lifecycle = before.FindTask(taskID).Lifecycle
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected admission changed state beyond its retirement revision")
			}
		})
	}
}

func TestAcceptanceProvenance_ConcurrentAllocationChange(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	var competingState *models.State
	previousHook := submitReviewBeforeModifyTestHook
	submitReviewBeforeModifyTestHook = func() {
		if err := bb.Modify(func(state *models.State) error {
			state.FindTask("acceptance-parent").Output[0].Validation = []string{"sh replacement_test.sh"}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var err error
		competingState, err = bb.Read()
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })
	_, err := SubmitForReview(root, taskID, commit, agentID)
	if competingState == nil {
		t.Fatalf("submission did not reach final transaction barrier: %v", err)
	}
	var evidenceErr *AcceptanceEvidenceError
	if !errors.As(err, &evidenceErr) || evidenceErr.Field != "acceptance.source" {
		t.Fatalf("concurrent allocation error = %T %v", err, err)
	}
	requireAcceptancePreparationRetired(t, bb, competingState, taskID)
}

func TestAcceptanceProvenance_ConcurrentMatchingSpecRefChange(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	var competingState *models.State
	previousHook := submitReviewBeforeModifyTestHook
	submitReviewBeforeModifyTestHook = func() {
		if err := bb.Modify(func(state *models.State) error {
			// Both references remain valid and mutually consistent. The change
			// must still invalidate execution under the earlier allocation.
			const replacement = "specs/acceptance-goal.md#Replay"
			state.FindTask(taskID).SpecRef = replacement
			state.FindTask("acceptance-parent").Output[0].SpecRef = replacement
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		competingState = readAcceptanceState(t, bb)
	}
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })
	_, err := SubmitForReview(root, taskID, commit, agentID)
	if competingState == nil {
		t.Fatalf("submission did not reach final transaction barrier: %v", err)
	}
	var evidenceErr *AcceptanceEvidenceError
	if !errors.As(err, &evidenceErr) || evidenceErr.Field != "acceptance.source" {
		task := readAcceptanceState(t, bb).FindTask(taskID)
		t.Fatalf("concurrent matching spec_ref change error = %T %v; status = %s, receipt recorded = %t; want stale allocation rejection", err, err, task.Status, task.AcceptanceReceipt != nil)
	}
	requireAcceptancePreparationRetired(t, bb, competingState, taskID)
}

func TestAcceptanceProvenance_ConcurrentGenerationChange(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationA)
	stale := models.AgentAuthority{ID: agentID, Generation: lifecycleGenerationA}
	var competingState *models.State
	previousHook := submitReviewBeforeModifyTestHook
	submitReviewBeforeModifyTestHook = func() {
		setLifecycleAgentGeneration(t, bb, agentID, lifecycleGenerationB)
		var err error
		competingState, err = bb.Read()
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })
	_, err := SubmitForReviewWithAuthority(root, taskID, commit, stale)
	if competingState == nil {
		t.Fatalf("submission did not reach final generation barrier: %v", err)
	}
	assertLifecycleAuthorityError(t, err, agentID)
	requireAcceptanceStateUnchanged(t, bb, competingState)
}

func TestAcceptanceProvenance_NonExecutableProof(t *testing.T) {
	for _, proof := range []string{"approved", "unapproved", "missing pinned heading"} {
		t.Run(proof, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
			wt := git.New(root).GetWorktreePath(taskID)
			if proof != "unapproved" {
				planPath := filepath.Join(root, "specs", "acceptance-plan.md")
				plan, err := os.ReadFile(planPath)
				if err != nil {
					t.Fatal(err)
				}
				updated := strings.Replace(string(plan), `"approved_proofs":[]`, `"approved_proofs":[{"obligation_id":"AC-replay","reference_id":"replay","rationale":"independently approved analysis"}]`, 1)
				if proof == "missing pinned heading" {
					updated = strings.Replace(updated, "acceptance-goal.md#Replay", "acceptance-goal.md#Missing", 1)
				}
				base := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
				if err := os.WriteFile(planPath, []byte(updated), 0644); err != nil {
					t.Fatal(err)
				}
				testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
				testhelpers.MustGit(t, root, "commit", "-m", "test: independently approve proof exception")
				reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
				if err := bb.Modify(func(state *models.State) error {
					parent := state.FindTask("acceptance-parent")
					parent.BaseCommit, parent.ReviewCommit, parent.MergeCommit = &base, &reviewCommit, &reviewCommit
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				testhelpers.MustGit(t, wt, "rebase", "integration")
			}
			manifest := `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"boundary_test.sh","assertion":"identity assertion","command_index":0},{"obligation_id":"AC-replay","approved_reference_id":"replay"}]}`
			if err := os.WriteFile(filepath.Join(wt, "acceptance", "task-1.json"), []byte(manifest+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, wt, "add", "acceptance/task-1.json")
			testhelpers.MustGit(t, wt, "commit", "-m", "test: map replay to non-executable proof")
			commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
			if proof == "approved" {
				if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
					t.Fatalf("independently approved resolved proof rejected: %v", err)
				}
				state, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				receipt := state.FindTask(taskID).AcceptanceReceipt
				if receipt == nil || len(receipt.Commands) != 1 || receipt.Commands[0].ExitCode != 0 || len(receipt.Mappings) != 2 || receipt.Mappings[1].ApprovedReferenceID != "replay" || receipt.ReviewCommit != commit {
					t.Fatalf("receipt lost resolved proof or executable evidence: %#v", receipt)
				}
				return
			}
			field := "acceptance.mappings"
			if proof == "missing pinned heading" {
				field = "acceptance.source"
			}
			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, field)
		})
	}
}

// The D8 repin loop: re-pinning a reference in the carrier's Source References
// changes the file but not the allocation the reviewer approved. Judging the
// whole blob stranded every child of the plan until it was re-reviewed, and
// each repin created the next instance.
func TestAcceptanceProvenance_UnrelatedSectionEditKeepsAllocation(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	// Re-pin a reference. This sits in `## Source References`, entirely before
	// `## Task 1`, so the reviewed allocation span is untouched.
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	repinned := strings.Replace(string(plan),
		`- "identity": "specs/acceptance-goal.md#Identity"`,
		`- "identity": "specs/acceptance-goal.md#Identity" @ "`+sourceCommit+`"`, 1)
	if repinned == string(plan) {
		t.Fatal("fixture did not contain the reference to re-pin")
	}
	if err := os.WriteFile(planPath, []byte(repinned), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "docs(plans): re-pin a stale shared-contract reference")

	// Submission carries the candidate onto current integration, as the real
	// flow does; the separate candidate-vs-integration guard is not what this
	// test exercises.
	wt := git.New(root).GetWorktreePath(taskID)
	testhelpers.MustGit(t, wt, "rebase", "integration")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")

	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("SubmitForReview after an unrelated-section edit: %v", err)
	}
	state := readAcceptanceState(t, bb)
	if src := state.FindTask(taskID).AcceptanceSource; src == nil || src.ParentTask != "acceptance-parent" {
		t.Fatalf("AcceptanceSource = %+v, want the allocation preserved across the repin", src)
	}
}

// The allocation section itself is still immutable after review.
func TestAcceptanceProvenance_ReviewedSectionEditRefusesAllocation(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)
	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(plan), `"timeout_seconds":10`, `"timeout_seconds":600`, 1)
	if edited == string(plan) {
		t.Fatal("fixture did not contain the contract field to edit")
	}
	if err := os.WriteFile(planPath, []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: change the reviewed allocation itself")

	requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")
}

// A carrier reference naming no section keeps whole-file semantics: there is no
// narrower span a verdict could have covered.
func TestAcceptanceProvenance_HeadinglessRefUsesWholeFile(t *testing.T) {
	if _, ok := carrierSpan("# Plan\n\n## Task 1\nbody\n", ""); !ok {
		t.Fatal("carrierSpan with no heading must succeed")
	}
	whole, _ := carrierSpan("# Plan\n\n## Task 1\nbody\n", "")
	if whole != "# Plan\n\n## Task 1\nbody\n" {
		t.Fatalf("carrierSpan = %q, want the whole file", whole)
	}
	if _, ok := carrierSpan("# Plan\n\n## Task 1\nbody\n", "Task 9"); ok {
		t.Fatal("a heading that does not resolve must refuse, not match the whole file")
	}
	if _, ok := carrierSpan("# Plan\n\n## Dup\na\n\n## Dup\nb\n", "Dup"); ok {
		t.Fatal("an ambiguous heading must refuse rather than allocate on a guess")
	}
}

// A re-pin may move a reference, but not what an approved proof resolves to.
// Source References sits outside the allocation span, so without a content
// check an edit there could redirect an approved reference at material no
// reviewer saw while every declared ID stayed intact.
func TestAcceptanceProvenance_RepointedApprovedProofRefusesAllocation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rewrite func(plan, goalHeading string) string
	}{
		{"reference repointed at a different heading", func(plan, _ string) string {
			return strings.Replace(plan,
				`- "identity": "specs/acceptance-goal.md#Identity"`,
				`- "identity": "specs/acceptance-goal.md#Replay"`, 1)
		}},
		{"reference repointed at a different file", func(plan, _ string) string {
			return strings.Replace(plan,
				`- "identity": "specs/acceptance-goal.md#Identity"`,
				`- "identity": "specs/substituted-goal.md#Identity"`, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, _, agentID, bb := completeAcceptanceScenario(t)

			// A second file the substituted reference can resolve to, so the
			// refusal is about changed content and not an unresolvable target.
			substituted := filepath.Join(root, "specs", "substituted-goal.md")
			if err := os.WriteFile(substituted, []byte("# Boundary\n\n## Identity\nDifferent identity rule no reviewer saw.\n"), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "specs/substituted-goal.md")
			testhelpers.MustGit(t, root, "commit", "-m", "test: add a substitutable target")

			// The reviewed contract must actually cite the reference, or there
			// is nothing for the substitution to subvert.
			planPath := filepath.Join(root, "specs", "acceptance-plan.md")
			plan, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			withProof := strings.Replace(string(plan), `"approved_proofs":[]`,
				`"approved_proofs":[{"obligation_id":"AC-identity","reference_id":"identity","rationale":"reviewed by inspection"}]`, 1)
			if withProof == string(plan) {
				t.Fatal("fixture did not contain approved_proofs to populate")
			}
			if err := os.WriteFile(planPath, []byte(withProof), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
			testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed contract cites an approved proof")
			reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
			if err := bb.Modify(func(state *models.State) error {
				parent := state.FindTask("acceptance-parent")
				parent.ReviewCommit, parent.MergeCommit = &reviewCommit, &reviewCommit
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			// Now substitute the reference target at integration, leaving the
			// allocation section and every declared ID untouched.
			repointed := tc.rewrite(withProof, "Identity")
			if repointed == withProof {
				t.Fatal("fixture did not contain the reference to repoint")
			}
			if err := os.WriteFile(planPath, []byte(repointed), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
			testhelpers.MustGit(t, root, "commit", "-m", "docs(plans): re-pin the identity reference")

			wt := git.New(root).GetWorktreePath(taskID)
			testhelpers.MustGit(t, wt, "rebase", "integration")
			commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")

			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, "acceptance.source")
		})
	}
}

// A child that already adopted its source must survive a re-pin too: the
// stranding this change removes at claim time reappears at reviewer assignment
// if the stored identity still means "the whole file".
func TestAcceptanceProvenance_AdoptedChildSurvivesUnrelatedSectionEdit(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)

	// Adopt the source the ordinary way.
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("initial SubmitForReview: %v", err)
	}
	if src := readAcceptanceState(t, bb).FindTask(taskID).AcceptanceSource; src == nil {
		t.Fatal("fixture did not adopt an acceptance source")
	}

	// Re-pin a reference in Source References, outside the allocation span.
	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	repinned := strings.Replace(string(plan),
		`- "identity": "specs/acceptance-goal.md#Identity"`,
		`- "identity": "specs/acceptance-goal.md#Identity" @ "`+sourceCommit+`"`, 1)
	if repinned == string(plan) {
		t.Fatal("fixture did not contain the reference to re-pin")
	}
	if err := os.WriteFile(planPath, []byte(repinned), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "docs(plans): re-pin a stale shared-contract reference")

	state := readAcceptanceState(t, bb)
	task := state.FindTask(taskID)
	if err := validateAcceptanceForAssignment(root, state, task); err != nil {
		t.Fatalf("adopted child stranded at reviewer assignment by an unrelated-section edit: %v", err)
	}
}

// The ordinary reject-rebase-resubmit cycle across a re-pin: the child has
// already adopted, then carries its work onto the edited integration. Every
// boundary must agree on what identity means, or the child is refused for an
// edit it did not make.
func TestAcceptanceProvenance_AdoptedChildResubmitsAfterRebasingPastRepin(t *testing.T) {
	root, taskID, commit, agentID, bb := completeAcceptanceScenario(t)

	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("initial SubmitForReview: %v", err)
	}
	if src := readAcceptanceState(t, bb).FindTask(taskID).AcceptanceSource; src == nil {
		t.Fatal("fixture did not adopt an acceptance source")
	}

	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	repinned := strings.Replace(string(plan),
		`- "identity": "specs/acceptance-goal.md#Identity"`,
		`- "identity": "specs/acceptance-goal.md#Identity" @ "`+sourceCommit+`"`, 1)
	if repinned == string(plan) {
		t.Fatal("fixture did not contain the reference to re-pin")
	}
	if err := os.WriteFile(planPath, []byte(repinned), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "docs(plans): re-pin a stale shared-contract reference")

	// Rejected, rebased onto current integration, resubmitted.
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.Status = models.TaskStatusImplementing
		task.AssignedTo = &agentID
		lease := time.Now().UTC().Add(time.Hour)
		task.LeaseExpires = &lease
		task.ReviewingBy, task.ReviewLeaseExpires = nil, nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wt := git.New(root).GetWorktreePath(taskID)
	testhelpers.MustGit(t, wt, "rebase", "integration")
	commit = testhelpers.MustGit(t, wt, "rev-parse", "HEAD")

	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("resubmission after rebasing past the re-pin: %v", err)
	}
}

// The identity stored in AcceptanceSource must be a real object id: the state
// validator requires immutable lowercase object IDs there, and a value of any
// other shape is accepted at write time but rejected by the next validation —
// including the one inside the re-claim-after-rejection transaction.
func TestAcceptanceProvenance_SpanIdentityIsAGitObjectID(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)

	carrier := "# Plan\n\n## Task 1\n\nbody of the reviewed allocation\n\n## Other\nunrelated\n"
	identity, ok := carrierSpanIdentity(carrier, "Task 1")
	if !ok {
		t.Fatal("carrierSpanIdentity failed on a resolvable heading")
	}
	if len(identity) != 40 || strings.ToLower(identity) != identity {
		t.Fatalf("identity = %q, want a 40-char lowercase object id the state validator accepts", identity)
	}

	// git must agree: the value is the object id of the extracted section.
	span, _ := carrierSpan(carrier, "Task 1")
	spanFile := filepath.Join(root, "span.txt")
	if err := os.WriteFile(spanFile, []byte(span), 0644); err != nil {
		t.Fatal(err)
	}
	if got := testhelpers.MustGit(t, root, "hash-object", spanFile); got != identity {
		t.Fatalf("carrierSpanIdentity = %s, git hash-object = %s", identity, got)
	}
}

// The accepted gap, made explicit: when the contract asserts no approved
// proofs, a reference backing one of its obligations may be re-pointed at
// integration and allocation still succeeds. The stricter reading blocks every
// child of the plan with no supported way to re-review a merged parent, so the
// drift is left to reference freshness at prompt build. See D13.
func TestAcceptanceProvenance_ObligationReferenceDriftWithoutProofsIsAccepted(t *testing.T) {
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)

	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), `"approved_proofs":[]`) {
		t.Fatal("fixture must assert no approved proofs for this case")
	}

	// Extend the section an obligation's reference resolves to, then re-pin the
	// reference at the revision carrying the extension — the d1bc603e shape.
	goalPath := filepath.Join(root, "specs", "acceptance-goal.md")
	goal, err := os.ReadFile(goalPath)
	if err != nil {
		t.Fatal(err)
	}
	extended := strings.Replace(string(goal), "Reject malformed identity.", "Reject malformed identity.\nAnd reject the extended case.", 1)
	if extended == string(goal) {
		t.Fatal("fixture did not contain the section to extend")
	}
	if err := os.WriteFile(goalPath, []byte(extended), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "docs: extend a shared contract section")
	extendedAt := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	repinned := strings.Replace(string(plan),
		`- "identity": "specs/acceptance-goal.md#Identity"`,
		`- "identity": "specs/acceptance-goal.md#Identity" @ "`+extendedAt+`"`, 1)
	if err := os.WriteFile(planPath, []byte(repinned), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "docs(plans): re-pin the extended reference")

	wt := git.New(root).GetWorktreePath(taskID)
	testhelpers.MustGit(t, wt, "rebase", "integration")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")

	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("allocation after an obligation-reference re-pin: %v", err)
	}
	if src := readAcceptanceState(t, bb).FindTask(taskID).AcceptanceSource; src == nil {
		t.Fatal("expected the allocation to survive the re-pin")
	}
}
