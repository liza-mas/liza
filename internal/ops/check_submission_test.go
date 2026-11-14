package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/toolresult"
)

// parityWant is what a row asserts: the local verdict and where it refuses, and
// how the gate answers the same fixture.
type parityWant struct {
	verdict string
	// at names the refusing check of a refused verdict.
	at string
	// row names the declared row; a row in postMarkerRows is compared under the
	// lifecycle exception, every other refusal whole.
	row string
	// skipGate leaves the gate unrun: the row's gate answer is not comparable.
	skipGate bool
	// beforeGate prepares the worktree the row's own canonical command dirtied.
	beforeGate func(t *testing.T, f *checkFixture)
}

// runParity runs the local check on the fixture, then the gate on the same
// fixture, and compares them as the row declares.
func runParity(t *testing.T, f *checkFixture, want parityWant) {
	t.Helper()
	local := f.local(t)
	requireVerdict(t, local, want.verdict, want.at)
	if f.commit != "" && local.CandidateCommit != "" && local.CandidateCommit != f.commit && want.verdict != SubmissionVerdictIncomplete {
		t.Fatalf("candidate_commit = %s, want %s", local.CandidateCommit, f.commit)
	}
	if want.skipGate {
		return
	}
	if want.beforeGate != nil {
		want.beforeGate(t, f)
	}
	result, err := f.gate()
	switch want.verdict {
	case SubmissionVerdictPass:
		if err != nil {
			t.Fatalf("the local check passed but the gate refused: %v", err)
		}
		if result.Outcome != models.LifecycleCompleted {
			t.Fatalf("gate outcome = %s, want COMPLETED", result.Outcome)
		}
		if !reflect.DeepEqual(result.DeferredCommands, local.DeferredCommands) {
			t.Fatalf("deferred commands: gate %+v, local %+v", result.DeferredCommands, local.DeferredCommands)
		}
	case SubmissionVerdictRefused:
		requirePostMarkerStage(t, want)
		requireSameRefusal(t, f, err, local.Refusal, postMarkerRows[want.row] != "")
	default:
		t.Fatalf("a row that compares the gate must pass or refuse, not %s", want.verdict)
	}
}

// requirePostMarkerStage ties a refusal row's declaration to where it refuses:
// a row is post-marker exactly when its refusing entry is one of 13-19.
func requirePostMarkerStage(t *testing.T, want parityWant) {
	t.Helper()
	declared, post := postMarkerRows[want.row]
	if want.row == "" {
		t.Fatal("a refusal row names its row")
	}
	if post && declared != want.at {
		t.Fatalf("row %s is declared post-marker at %s but refuses at %s", want.row, declared, want.at)
	}
	if post != entryRefusesAfterMarker(want.at) {
		t.Fatalf("row %s refuses at %s: post-marker declaration = %t, entry stage says %t", want.row, want.at, post, !post)
	}
}

// entryRefusesAfterMarker reports whether the gate has written its preparation
// marker when it runs the entry: entries 13-19.
func entryRefusesAfterMarker(name string) bool {
	return submissionChecks[submissionCheckIndex(name)].afterMarker()
}

func TestCheckSubmissionParityPass(t *testing.T) {
	t.Run(rowF01AllPass, func(t *testing.T) {
		// GIVEN two passing commands with spaces and quotes, a reusable input
		// that is available, and a deferred command
		log := func(name string) string { return filepath.Join(t.TempDir(), name) }
		probe := filepath.ToSlash(log("runs.log"))
		first := `sh -c 'printf "%s\n" "first: $0 and $1" >> ` + probe + `; test "$REUSABLE_FIXTURE" = shared-value' 'two words' "a \"quoted\" arg"`
		second := `sh -c 'echo "second" >> ` + probe + `'`
		deferred := `sh -c 'echo "deferred ran" >> ` + probe + `'`
		f := newStrictFixture(t, strictSpec{
			commands: []string{first, second, deferred},
			deferred: []int{2},
			inputs: []models.RuntimeInput{{ID: "reusable", Commands: []string{first}, Recipe: "project.reusable",
				Consumption: models.RuntimeInputReusable, Env: []string{"REUSABLE_FIXTURE"}}},
			recordInputs: map[string]string{"reusable": "REUSABLE_FIXTURE=shared-value\n"},
			// A fix commit with a well-formed prevention record, which entry
			// prevention_records checks at the gate and locally.
			commitMsg: "fix: reject a malformed identity\n\nThe check trusted its input.\n\nPrevention-Cause: the identity check trusted its caller\nPrevention-Concept: internal/identity\nPrevention-Disposition: removed\nPrevention-Class: gap\n",
		})
		// WHEN the local check runs, then the gate
		local := f.local(t)
		requireVerdict(t, local, SubmissionVerdictPass, "")
		if got := runsIn(t, probe); len(got) != 2 || !strings.HasPrefix(got[0], "first: two words and a \"quoted\" arg") || got[1] != "second" {
			t.Fatalf("local run executed %q, want the two admitted commands with the input delivered and the deferred one skipped", got)
		}
		if len(local.DeferredCommands) != 1 || local.DeferredCommands[0].Index != 2 {
			t.Fatalf("deferred = %+v, want command 2", local.DeferredCommands)
		}
		result, err := f.gate()
		// THEN the gate publishes with the same deferred commands
		if err != nil || result.Outcome != models.LifecycleCompleted {
			t.Fatalf("gate = %+v, %v, want COMPLETED", result, err)
		}
		if !reflect.DeepEqual(result.DeferredCommands, local.DeferredCommands) {
			t.Fatalf("deferred commands: gate %+v, local %+v", result.DeferredCommands, local.DeferredCommands)
		}
		if f.task(t).AcceptanceReceipt == nil {
			t.Fatal("the gate recorded no receipt")
		}
	})

	t.Run(rowREBUntrackedOnly, func(t *testing.T) {
		f := baseFixture(t)
		f.writeFile(t, "scratch.txt", "untracked\n")
		runParity(t, f, parityWant{verdict: SubmissionVerdictPass})
	})
}

func runsIn(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// parityCase is one refusal row.
type parityCase struct {
	id    string
	build func(t *testing.T) *checkFixture
	want  parityWant
}

// parityRefusalCases are the rows of entries 1-11, 16 and 17: one fixture per
// entry that the candidate fails.
func parityRefusalCases() []parityCase {
	return []parityCase{
		{rowE01WrongGeneration, func(t *testing.T) *checkFixture {
			f := newStrictFixture(t, strictSpec{commands: []string{"true"}})
			f.authority = &models.AgentAuthority{ID: f.agentID, Generation: "another-generation"}
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkAgentAuthority}},
		{rowE02NoRolePair, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			modifyTask(t, f, func(task *models.Task) { task.RolePair = "" })
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkPipeline}},
		{rowE03aNotAssigned, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			other := "coder-2"
			modifyTask(t, f, func(task *models.Task) { task.AssignedTo = &other })
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkOwnership}},
		{rowE03bNoCheckpoint, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			modifyTask(t, f, func(task *models.Task) { task.History = nil })
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkOwnership}},
		{rowE04OtherBranch, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			testhelpers.MustGit(t, f.worktree(), "checkout", "-b", "elsewhere")
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkWorktreeBranchName}},
		{rowE05UnparsableChildContract, func(t *testing.T) *checkFixture {
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "\nRun from the child worktree root.\n", false)
			return &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkPlanningOutputAcceptanceName}},
		{rowE06RuntimeInputWithoutRecipe, func(t *testing.T) *checkFixture {
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "", false)
			f := &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
			modifyTask(t, f, func(task *models.Task) {
				task.Output[0].RuntimeInputs = []models.RuntimeInput{{ID: "w03", Commands: []string{"node --version"},
					Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}}
			})
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkPlanningOutputRuntimeInputName}},
		{rowE07SlugFragment, func(t *testing.T) *checkFixture {
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, outputRefFragmentSection, false)
			f := &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
			modifyTask(t, f, func(task *models.Task) { task.Output[0].EpicRef = "specs/plans/child.md#capability-two" })
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkOutputRefFragmentsName}},
		{rowE08NoTestFile, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			testhelpers.MustGit(t, f.worktree(), "rm", "feature_test.go")
			f.recommit(t, "Drop the test file")
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkTDD}},
		{rowE09FixCommitWithoutRecord, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			f.writeFile(t, "feature.go", "package main\n\nfunc fixed() {}\n")
			testhelpers.MustGit(t, f.worktree(), "add", "feature.go")
			f.recommit(t, "fix: repair the feature")
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkPreventionRecordsName}},
		{rowE10CarrierPinNamesNoCommit, func(t *testing.T) *checkFixture {
			f := baseFixture(t)
			carrier := fmt.Sprintf("# Plan\n\n## Source References\n\nSource revision: %q\n\n### Direct References\n\n- \"source\": \"README.md#Test\"\n\n### Obligation Coverage\n\n- \"AC-retention\" -> \"source\"\n\n## Task One\n\nWork.\n", strings.Repeat("0", 40))
			f.writeFile(t, "specs/plans/extra.md", carrier)
			testhelpers.MustGit(t, f.worktree(), "add", "specs/plans/extra.md")
			f.recommit(t, "Add a carrier pinned to a commit that does not exist")
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkCandidateCarriers}},
		{rowE11ManifestMissingAMapping, func(t *testing.T) *checkFixture {
			root, taskID, commit, agentID, bb := setupAcceptanceScenario(t)
			return &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkAcceptanceContract}},
		{rowE17BoundedCorrectionOutsideContract, func(t *testing.T) *checkFixture {
			// A correction of a reviewed architecture may change only its CONTRACT
			// subsection; this candidate also adds source and test files.
			f := baseFixture(t)
			contract := "# Contract\n\n## Scope 0\n**Boundary:** Original ownership.\n\n### CONTRACT\nOriginal contract.\n"
			if err := os.WriteFile(filepath.Join(f.root, "specs-contract.md"), []byte(contract), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(f.root, "specs"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(f.root, "specs-contract.md"), filepath.Join(f.root, "specs/contract.md")); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, f.root, "add", "specs/contract.md")
			testhelpers.MustGit(t, f.root, "commit", "-m", "test: reviewed contract")
			reviewed := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			testhelpers.MustGit(t, f.worktree(), "rebase", "integration")
			f.commit = testhelpers.MustGit(t, f.worktree(), "rev-parse", "HEAD")
			if err := f.bb.Modify(func(state *models.State) error {
				approver := "architect-reviewer-1"
				state.Tasks = append(state.Tasks, models.Task{
					ID: "original-architecture", Type: models.TaskTypeArchitecture, RolePair: "architecture-pair", Status: models.TaskStatusMerged,
					ArchRef: "specs/contract.md#Scope 0", ApprovedBy: &approver, Approvals: []models.Approval{{Agent: approver, Timestamp: time.Now().UTC()}},
					BaseCommit: &reviewed, ReviewCommit: &reviewed, MergeCommit: &reviewed, Created: time.Now().UTC(),
					PlanAmendment: &models.PlanAmendment{Pending: f.taskID, Corrections: []string{f.taskID}},
				})
				task := state.FindTask(f.taskID)
				task.AmendsPlan, task.AmendmentMode = "original-architecture", models.PlanAmendmentContract
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkBoundedContractCorrectionName}},
		{rowE16PendingPlanAmendment, func(t *testing.T) *checkFixture {
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "\n### Execution notes\n\nRun from the child worktree root.\n", false)
			f := &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
			if err := bb.Modify(func(state *models.State) error {
				state.Tasks = append(state.Tasks, models.Task{ID: "amended-parent", Status: models.TaskStatusMerged, PlanAmendment: &models.PlanAmendment{Pending: "correction"}})
				state.FindTask(taskID).DependsOn = []string{"amended-parent"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return f
		}, parityWant{verdict: SubmissionVerdictRefused, at: checkPlanAmendmentName}},
	}
}

func TestCheckSubmissionParityRefusals(t *testing.T) {
	for _, tc := range parityRefusalCases() {
		t.Run(tc.id, func(t *testing.T) {
			tc.want.row = tc.id
			runParity(t, tc.build(t), tc.want)
		})
	}
}

// modifyTask edits the fixture's task in state.
func modifyTask(t *testing.T, f *checkFixture, edit func(*models.Task)) {
	t.Helper()
	if err := f.bb.Modify(func(state *models.State) error {
		edit(state.FindTask(f.taskID))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Row IDs: the parity and disclosure rows the coverage table ties entries to.
const (
	rowE19aFailingCommand                  = "E19a_failing_command"
	rowE19bDirtyAfterCommand               = "E19b_dirty_worktree_after_command"
	rowE01WrongGeneration                  = "E01_wrong_generation"
	rowE02NoRolePair                       = "E02_no_role_pair"
	rowE03aNotAssigned                     = "E03a_not_assigned"
	rowE03bNoCheckpoint                    = "E03b_no_checkpoint"
	rowE04OtherBranch                      = "E04_other_branch"
	rowE05UnparsableChildContract          = "E05_unparsable_child_contract"
	rowE06RuntimeInputWithoutRecipe        = "E06_runtime_input_without_recipe"
	rowE07SlugFragment                     = "E07_slug_fragment"
	rowE08NoTestFile                       = "E08_no_test_file"
	rowE09FixCommitWithoutRecord           = "E09_fix_commit_without_record"
	rowE10CarrierPinNamesNoCommit          = "E10_carrier_pin_names_no_commit"
	rowE11ManifestMissingAMapping          = "E11_manifest_missing_a_mapping"
	rowE17BoundedCorrectionOutsideContract = "E17_bounded_correction_outside_contract"
	rowE16PendingPlanAmendment             = "E16_pending_plan_amendment"
	rowF01AllPass                          = "F01_all_pass"
	rowREBUntrackedOnly                    = "REB_untracked_only"
	rowPREBExecutableMissing               = "PRE_b_executable_missing"
	rowD7FailingOutput                     = "D7_failing_output"
	rowPREARuntimeInputUnavailable         = "PRE_a_runtime_input_unavailable"
	rowREBUnstaged                         = "REB_unstaged"
	rowREBStaged                           = "REB_staged"
	rowREBMergeHead                        = "REB_merge_head"
	rowREBPreRebaseHook                    = "REB_pre_rebase_hook"
	rowREBBehind                           = "REB_behind"
	rowREBMergeCommit                      = "REB_merge_commit"
	rowDISSingleUseFirstFails              = "DIS_single_use_first_fails"
	rowDISSingleUseFirstPasses             = "DIS_single_use_first_passes"
	rowDISAdmissionFull                    = "DIS_admission_full"
)

// slotCommand names the admission class its text matches.
const slotCommand = "sh -c ': heavy-marker; echo ran >> %s/ran.log'"

func TestCheckSubmissionParityCanonicalValidation(t *testing.T) {
	t.Run(rowE19aFailingCommand, func(t *testing.T) {
		f := newStrictFixture(t, strictSpec{commands: []string{`sh -c 'echo before-failure; exit 3'`}})
		runParity(t, f, parityWant{row: rowE19aFailingCommand, verdict: SubmissionVerdictRefused, at: checkCanonicalValidation})
	})

	t.Run(rowE19bDirtyAfterCommand, func(t *testing.T) {
		f := newStrictFixture(t, strictSpec{commands: []string{`sh -c 'echo stray > stray.txt'`}})
		f.writesWorktree = true
		runParity(t, f, parityWant{row: rowE19bDirtyAfterCommand, verdict: SubmissionVerdictRefused, at: checkCanonicalValidation,
			beforeGate: func(t *testing.T, f *checkFixture) {
				// The local run's command left its file, as the gate's will.
				if err := os.Remove(filepath.Join(f.worktree(), "stray.txt")); err != nil {
					t.Fatal(err)
				}
			}})
	})

	t.Run(rowPREBExecutableMissing, func(t *testing.T) {
		f := newStrictFixture(t, strictSpec{commands: []string{"no-such-executable-for-check-submission --version"}})
		runParity(t, f, parityWant{row: rowPREBExecutableMissing, verdict: SubmissionVerdictRefused, at: checkCanonicalValidation})
	})

	t.Run(rowD7FailingOutput, func(t *testing.T) {
		f := newStrictFixture(t, strictSpec{commands: []string{`sh -c 'echo masked-output-line; exit 4'`}})
		local := f.local(t)
		requireVerdict(t, local, SubmissionVerdictRefused, checkCanonicalValidation)
		_, gateErr := f.gate()
		requireSameRefusal(t, f, gateErr, local.Refusal, postMarkerRows[rowD7FailingOutput] != "")
		reason := refusalCause(local.Refusal).Error()
		if reason != refusalCause(gateErr).Error() {
			t.Fatalf("reasons differ:\ngate:  %s\nlocal: %s", refusalCause(gateErr).Error(), reason)
		}
		_, after, found := strings.Cut(reason, "tool-result read ")
		if !found {
			t.Fatalf("reason names no stored output: %s", reason)
		}
		hash := strings.Fields(after)[0]
		store, err := toolresult.New(projectToolResultsDir(f.root), toolresult.Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := store.Read(hash, 0, 4096)
		if err != nil || !strings.Contains(stored, "masked-output-line") {
			t.Fatalf("stored output = %q, %v, want the failing command's masked output", stored, err)
		}
	})

	t.Run(rowPREARuntimeInputUnavailable, func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		command := fmt.Sprintf("sh -c 'touch %s'", filepath.ToSlash(marker))
		f := newStrictFixture(t, strictSpec{
			commands: []string{command},
			inputs: []models.RuntimeInput{{ID: "absent", Commands: []string{command}, Recipe: "project.absent",
				Consumption: models.RuntimeInputReusable, Env: []string{"ABSENT_FIXTURE"}}},
		})
		local := f.local(t)
		requireVerdict(t, local, SubmissionVerdictRefused, checkCanonicalValidation)
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the local check ran a command whose runtime input is unavailable")
		}
		_, gateErr := f.gate()
		requireSameRefusal(t, f, gateErr, local.Refusal, postMarkerRows[rowPREARuntimeInputUnavailable] != "")
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the gate ran a command whose runtime input is unavailable")
		}
		// Only the gate records its anomaly and blocks the task.
		state, err := f.bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		if anomalies := runtimeInputAnomalies(state); len(anomalies) != 1 || state.FindTask(f.taskID).Status != models.TaskStatusBlocked {
			t.Fatalf("gate anomalies = %d, status = %s, want one anomaly and BLOCKED", len(anomalies), state.FindTask(f.taskID).Status)
		}
	})
}

// disclosureRowIDs are the rows that prove an incomplete verdict.
var disclosureRowIDs = []string{
	rowREBUnstaged, "REB_staged", "REB_merge_head", "REB_pre_rebase_hook", "REB_behind", "REB_merge_commit",
	rowDISSingleUseFirstFails, "DIS_single_use_first_passes", "DIS_admission_full",
}

func TestCheckSubmissionDisclosures(t *testing.T) {
	// D1: the rebase is never performed, only proven a no-op.
	t.Run(rowREBUnstaged, func(t *testing.T) {
		f := baseFixture(t)
		f.writeFile(t, "feature.go", "package main\n\n// unstaged\n")
		requireNotRun(t, f.local(t), checkRebase, NotRunRebaseUnverified+":"+rebaseConditionTracked)
		requireGateRefusesAtRebase(t, f)
	})
	t.Run(rowREBStaged, func(t *testing.T) {
		f := baseFixture(t)
		f.writeFile(t, "feature.go", "package main\n\n// staged\n")
		testhelpers.MustGit(t, f.worktree(), "add", "feature.go")
		requireNotRun(t, f.local(t), checkRebase, NotRunRebaseUnverified+":"+rebaseConditionTracked)
		requireGateRefusesAtRebase(t, f)
	})
	t.Run(rowREBMergeHead, func(t *testing.T) {
		f := baseFixture(t)
		path := testhelpers.MustGit(t, f.worktree(), "rev-parse", "--git-path", "MERGE_HEAD")
		if !filepath.IsAbs(path) {
			path = filepath.Join(f.worktree(), path)
		}
		if err := os.WriteFile(path, []byte(f.commit+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		requireNotRun(t, f.local(t), checkRebase, NotRunRebaseUnverified+":"+rebaseConditionOperation)
	})
	t.Run(rowREBPreRebaseHook, func(t *testing.T) {
		f := baseFixture(t)
		dir := testhelpers.MustGit(t, f.worktree(), "rev-parse", "--git-path", "hooks")
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(f.worktree(), dir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pre-rebase"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		requireNotRun(t, f.local(t), checkRebase, NotRunRebaseUnverified+":"+rebaseConditionHook)
	})
	t.Run(rowREBPreRebaseHook+"/home_relative_hooks_path", func(t *testing.T) {
		// A canonical command that would leave a marker proves none ran.
		marker := filepath.Join(t.TempDir(), "ran")
		f := newStrictFixture(t, strictSpec{commands: []string{fmt.Sprintf("sh -c 'touch %s'", filepath.ToSlash(marker))}})
		home := t.TempDir()
		t.Setenv("HOME", home)
		hooks := filepath.Join(home, "hooks")
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, "pre-rebase"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.worktree(), "config", "core.hooksPath", "~/hooks")
		requireNotRun(t, f.local(t), checkRebase, NotRunRebaseUnverified+":"+rebaseConditionHook)
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a canonical command ran although the rebase could not be proven a no-op")
		}
	})
	t.Run(rowREBBehind, func(t *testing.T) {
		f := baseFixture(t)
		if err := os.WriteFile(filepath.Join(f.root, "integration-new.txt"), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", "integration-new.txt")
		testhelpers.MustGit(t, f.root, "commit", "-m", "Advance integration")
		// The harness proves the task branch and HEAD were not rebased.
		requireNotRun(t, f.local(t), checkRebase, NotRunRebasePending)
	})
	t.Run(rowREBMergeCommit, func(t *testing.T) {
		f := baseFixture(t)
		wt := f.worktree()
		testhelpers.MustGit(t, wt, "checkout", "-b", "side", "integration")
		f.writeFile(t, "side.txt", "side\n")
		testhelpers.MustGit(t, wt, "add", "side.txt")
		testhelpers.MustGit(t, wt, "commit", "-m", "Side work")
		testhelpers.MustGit(t, wt, "checkout", "task/"+f.taskID)
		testhelpers.MustGit(t, wt, "merge", "--no-ff", "-m", "Merge side", "side")
		requireNotRun(t, f.local(t), checkRebase, NotRunRebasePending)
	})

	// SC3: a command that spends a single_use input is never run locally.
	t.Run(rowDISSingleUseFirstFails, func(t *testing.T) {
		first, second := `sh -c 'exit 5'`, `sh -c 'echo ran >> /dev/null'`
		f := newStrictFixture(t, strictSpec{
			commands: []string{first, second},
			inputs: []models.RuntimeInput{{ID: "w03", Commands: []string{second}, Recipe: "project.w03",
				Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}},
			recordInputs: map[string]string{"w03": "W03_FIXTURE=fixture-0001\n"},
		})
		runParity(t, f, parityWant{row: rowDISSingleUseFirstFails, verdict: SubmissionVerdictRefused, at: checkCanonicalValidation})
	})
	t.Run(rowDISSingleUseFirstPasses, func(t *testing.T) {
		probe := filepath.Join(t.TempDir(), "runs.log")
		first, second := fmt.Sprintf("sh -c 'echo first >> %s'", filepath.ToSlash(probe)), fmt.Sprintf("sh -c 'echo second >> %s'", filepath.ToSlash(probe))
		f := newStrictFixture(t, strictSpec{
			commands: []string{first, second},
			inputs: []models.RuntimeInput{{ID: "w03", Commands: []string{second}, Recipe: "project.w03",
				Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}}},
			recordInputs: map[string]string{"w03": "W03_FIXTURE=fixture-0001\n"},
		})
		result := f.local(t)
		requireNotRun(t, result, checkCanonicalValidation, NotRunSingleUseInput+":1")
		if runs := runsIn(t, probe); len(runs) != 1 || runs[0] != "first" {
			t.Fatalf("local run executed %q, want only the command before the single_use one", runs)
		}
		// The harness proves the ledger, inside the state file, is byte for byte unchanged.
		for _, instance := range readState(t, f).RuntimeInputs {
			if instance.State != models.RuntimeInputAvailable {
				t.Fatalf("instance %s = %s, want available", instance.InputID, instance.State)
			}
		}
	})
	t.Run(rowDISAdmissionFull, func(t *testing.T) {
		marker := t.TempDir()
		command := fmt.Sprintf(slotCommand, filepath.ToSlash(marker))
		limits := []models.AdmissionLimit{{Class: "heavy", MaxConcurrent: 1, Match: []string{"heavy-marker"}}}
		f := newStrictFixture(t, strictSpec{commands: []string{command}, limits: limits})
		held := newAdmissionSlots(60)
		if err := held.acquire(context.Background(), f.root, models.AdmissionLimitsFor(models.Config{AdmissionLimits: limits}, []string{command})); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(held.release)
		requireNotRun(t, f.local(t), checkCanonicalValidation, NotRunAdmissionFull)
		if _, err := os.Stat(filepath.Join(marker, "ran.log")); err == nil {
			t.Fatal("a command ran although its admission class was full")
		}
	})
}

func readState(t *testing.T, f *checkFixture) *models.State {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// requireGateRefusesAtRebase proves the gate refuses the same worktree at its
// rebase, which is why the local check cannot call it a pass.
func requireGateRefusesAtRebase(t *testing.T, f *checkFixture) {
	t.Helper()
	_, err := f.gate()
	var operational *OperationalError
	if !errors.As(err, &operational) || operational.Phase != "rebase" {
		t.Fatalf("gate error = %v, want an operational refusal at its rebase", err)
	}
}

// TestCheckSubmissionPostMarkerRefusalReportsTheLocalLifecycle proves the local
// run reports its own lifecycle for a refusal by entries 13-19 whatever the
// cause's type, and the gate's policy for entries 1-11, which the gate refuses
// before its marker.
func TestCheckSubmissionPostMarkerRefusalReportsTheLocalLifecycle(t *testing.T) {
	task := &models.Task{ID: "task-1", Status: models.TaskStatusImplementing}
	// A cause that already carries a lifecycle, as the ownership fence's does:
	// the gate's, which describes a run that wrote a marker.
	gateLifecycle := &LifecycleError{
		Outcome: NewLifecycleOutcome("", task, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown),
		Err:     errors.New("an inner lifecycle's cause"),
	}
	causes := map[string]error{
		"plain": errors.New("a refusal"), "precondition": &PreconditionError{Reason: "a correctable refusal"},
		"lifecycle": gateLifecycle, "wrapped_lifecycle": fmt.Errorf("outer: %w", gateLifecycle),
	}
	for _, entry := range submissionChecks {
		if entry.name == checkRebase {
			continue // never refuses locally (D1)
		}
		for kind, cause := range causes {
			if !entry.afterMarker() && strings.HasSuffix(kind, "lifecycle") {
				continue // the gate's own policy keeps a lifecycle before its marker
			}
			t.Run(entry.name+"/"+kind, func(t *testing.T) {
				refusal := classifyLocalRefusal(task, entry.name, cause)
				var lifecycle *LifecycleError
				if !errors.As(refusal, &lifecycle) {
					t.Fatal("refusal carries no lifecycle outcome")
				}
				got := lifecycle.Outcome
				wantOutcome, wantAction := models.LifecycleStateChanged, LifecycleSafeActionRequery
				if entry.afterMarker() || kind == "precondition" {
					wantOutcome, wantAction = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
				}
				if got.Outcome != wantOutcome || got.SafeAction != string(wantAction) || got.Effects != string(LifecycleEffectsNone) || got.Operation != integrationOperationSubmitForReview {
					t.Fatalf("outcome = %s/%s/%s for %q, want %s/%s/none for %q", got.Outcome, got.SafeAction, got.Effects, got.Operation, wantOutcome, wantAction, integrationOperationSubmitForReview)
				}
				if kind == "lifecycle" || kind == "wrapped_lifecycle" {
					if !errors.Is(refusal, gateLifecycle.Err) || !strings.Contains(refusal.Error(), gateLifecycle.Err.Error()) {
						t.Fatalf("refusal %q lost the underlying cause", refusal)
					}
				}
			})
		}
	}
}

// TestCheckSubmissionOwnershipLossDuringValidationReportsTheLocalLifecycle runs
// the real canonical path: the task is reassigned while a canonical command
// runs, so the shared ownership fence stops validation with a lifecycle of its
// own, STATE_CHANGED / requery / unknown. The local run wrote no marker and
// reports its own lifecycle, with the fence's reason.
func TestCheckSubmissionOwnershipLossDuringValidationReportsTheLocalLifecycle(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	f := newStrictFixture(t, strictSpec{commands: []string{fmt.Sprintf("sh -c 'touch %s; sleep 30'", filepath.ToSlash(started))}})
	previous := acceptanceOwnershipPollInterval
	acceptanceOwnershipPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { acceptanceOwnershipPollInterval = previous })
	go func() {
		waitForFile(t, started, 20*time.Second)
		other := "coder-2"
		_ = f.bb.Modify(func(state *models.State) error {
			state.FindTask(f.taskID).AssignedTo = &other
			return nil
		})
	}()
	begun := time.Now()
	result, err := CheckSubmission(f.root, CheckSubmissionInput{TaskID: f.taskID, AgentID: f.agentID, Authority: f.authority})
	if err != nil {
		t.Fatalf("CheckSubmission() error = %v", err)
	}
	if elapsed := time.Since(begun); elapsed > 20*time.Second {
		t.Fatalf("the local check ran %s after the claim was lost", elapsed.Round(time.Second))
	}
	requireVerdict(t, result, SubmissionVerdictRefused, checkCanonicalValidation)
	var lifecycle *LifecycleError
	if !errors.As(result.Refusal, &lifecycle) {
		t.Fatalf("refusal %T carries no lifecycle", result.Refusal)
	}
	got := lifecycle.Outcome
	if got.Outcome != models.LifecycleInvalidInput || got.SafeAction != string(LifecycleSafeActionCorrectInput) || got.Effects != string(LifecycleEffectsNone) || got.Operation != integrationOperationSubmitForReview {
		t.Fatalf("outcome = %s/%s/%s for %q, want INVALID_INPUT/correct_input/none for submit-for-review", got.Outcome, got.SafeAction, got.Effects, got.Operation)
	}
	if !strings.Contains(result.Refusal.Error(), "submission ownership lost") {
		t.Fatalf("refusal %q lost the fence's reason", result.Refusal)
	}
}
