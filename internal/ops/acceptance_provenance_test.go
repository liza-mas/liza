package ops

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
			requireAcceptanceSubmitRejected(t, root, taskID, commit, agentID, bb, tc.field)
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
	requireAcceptanceStateUnchanged(t, bb, competingState)
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
	requireAcceptanceStateUnchanged(t, bb, competingState)
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
