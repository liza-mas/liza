package ops

import (
	"fmt"
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

const planningAcceptanceRef = "specs/plans/child.md#Task One"

func setupPlanningAcceptanceSubmission(t *testing.T, suffix string, legacy bool) (string, string, string, string, *db.Blackboard) {
	t.Helper()
	root, taskID, _, previousAgent, bb := setupSuccessfulSubmitScenario(t)
	g := git.New(root)
	wt := g.GetWorktreePath(taskID)
	base := testhelpers.MustGit(t, root, "rev-parse", "integration")
	document := fmt.Sprintf("# Plan\n\n## Source References\n\nSource revision: %q\n\n### Direct References\n\n- \"source\": \"README.md#Test\"\n\n### Obligation Coverage\n\n- \"AC-retention\" -> \"source\"\n\n## Task One\n\n### Acceptance Contract\n\n```json\n{\"version\":1,\"manifest\":\"acceptance/future.json\",\"obligations\":[\"AC-retention\"],\"validation\":[\"node --version\",\"node --test tests/future.test.mjs\"]}\n```\n", base) + suffix
	if legacy {
		document = "# Plan\n\n## Task One\n\nLegacy allocation without an acceptance declaration.\n"
	}
	path := filepath.Join(wt, "specs/plans/child.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(document), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "specs/plans/child.md")
	testhelpers.MustGit(t, wt, "commit", "-m", "Declare future coding acceptance")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	agentID := "code-planner-1"
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.Type = models.TaskTypePlanning
		task.RolePair = "code-planning-pair"
		task.Status = models.TaskStatusCodePlanning
		task.AssignedTo = &agentID
		task.History[0].Agent = &agentID
		task.Output = []models.OutputEntry{{
			Desc: "Retain work", DoneWhen: "Retention proof passes", Scope: "Owned future source, tests and manifest",
			SpecRef: "README.md", PlanRef: planningAcceptanceRef,
			Validation: []string{"node --version", "node --test tests/future.test.mjs"},
		}}
		agent := state.Agents[previousAgent]
		delete(state.Agents, previousAgent)
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, taskID, commit, agentID, bb
}

func TestSubmitForReview_PlanningOutputAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		suffix      string
		legacy      bool
		reverse     bool
		fixWorktree bool
		wantError   string
	}{
		{name: "trailing prose", suffix: "\nRun from the child worktree root.\n", wantError: "unexpected content after JSON fence"},
		{name: "uncommitted repair", suffix: "\nRun from the child worktree root.\n", fixWorktree: true, wantError: "unexpected content after JSON fence"},
		{name: "sibling heading", suffix: "\n### Execution notes\n\nRun from the child worktree root.\n"},
		{name: "legacy", legacy: true},
		{name: "ordered command mismatch", reverse: true, wantError: "ordered validation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, tc.suffix, tc.legacy)
			if tc.fixWorktree {
				path := filepath.Join(git.New(root).GetWorktreePath(taskID), "specs/plans/child.md")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				fixed := strings.Replace(string(content), tc.suffix, "\n### Execution notes\n"+tc.suffix, 1)
				if err := os.WriteFile(path, []byte(fixed), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.reverse {
				if err := bb.Modify(func(state *models.State) error {
					state.FindTask(taskID).Output[0].Validation = []string{"node --test tests/future.test.mjs", "node --version"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			// Integration advancement makes an unintended preflight rebase observable.
			if err := os.WriteFile(filepath.Join(root, "integration-new.txt"), []byte("new\n"), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "integration-new.txt")
			testhelpers.MustGit(t, root, "commit", "-m", "Advance integration")
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			result, err := SubmitForReview(root, taskID, commit, agentID)
			after, readErr := bb.Read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.wantError != "" {
				testhelpers.RequireErrorContains(t, err, tc.wantError)
				if result != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("invalid child declaration changed submission state")
				}
				head := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD")
				if head != commit {
					t.Fatal("invalid child declaration rebased the worktree")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || after.FindTask(taskID).Status != models.TaskStatusCodingPlanToReview {
				t.Fatal("valid planning output was not submitted")
			}
			for _, path := range []string{"acceptance/future.json", "tests/future.test.mjs"} {
				if _, err := os.Stat(filepath.Join(git.New(root).GetWorktreePath(taskID), path)); !os.IsNotExist(err) {
					t.Fatalf("planning submission materialized future file %s: %v", path, err)
				}
			}
		})
	}
}

func TestSubmitForReview_PlanningOutputAcceptanceAfterRebase(t *testing.T) {
	root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "", false)
	// Integration incorporates the valid declaration, then changes it while the
	// planner's committed candidate still contains the valid original section.
	testhelpers.MustGit(t, root, "cherry-pick", commit)
	path := filepath.Join(root, "specs/plans/child.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, []byte("\nUnexpected prose after the fence.\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/plans/child.md")
	testhelpers.MustGit(t, root, "commit", "-m", "Change declaration on integration")

	result, err := SubmitForReview(root, taskID, commit, agentID)
	testhelpers.RequireErrorContains(t, err, "unexpected content after JSON fence")
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if result != nil || state.FindTask(taskID).Status != models.TaskStatusCodePlanning || state.FindTask(taskID).ReviewCommit != nil {
		t.Fatal("malformed post-rebase declaration was submitted")
	}
	if lifecycle := state.FindTask(taskID).Lifecycle; lifecycle != nil && lifecycle.Preparation != nil {
		t.Fatal("post-rebase refusal stranded its submission preparation")
	}
	head := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD")
	if head == commit {
		t.Fatal("test did not exercise a changed post-rebase candidate")
	}
}

func TestSubmitForReview_PlanningOutputChangedBeforeCommit(t *testing.T) {
	root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, "", false)
	previousHook := submitReviewBeforeModifyTestHook
	t.Cleanup(func() { submitReviewBeforeModifyTestHook = previousHook })
	submitReviewBeforeModifyTestHook = func() {
		if err := bb.Modify(func(state *models.State) error {
			state.FindTask(taskID).Output[0].Scope = "Changed allocation after candidate checks"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := SubmitForReview(root, taskID, commit, agentID)
	testhelpers.RequireErrorContains(t, err, "planning output changed during submission")
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask(taskID)
	if result != nil || task.Status != models.TaskStatusCodePlanning || task.ReviewCommit != nil || task.AssignedTo == nil || *task.AssignedTo != agentID {
		t.Fatal("concurrent output change published a review boundary or released the planner")
	}
	if task.Output[0].Scope != "Changed allocation after candidate checks" {
		t.Fatal("submission overwrote the concurrent output change")
	}
	if task.Lifecycle != nil && task.Lifecycle.Preparation != nil {
		t.Fatal("output-change refusal stranded its submission preparation")
	}
}

const outputRefFragmentSection = "\n## Capability Two\n\nSecond capability.\n"

func TestSubmitForReview_OutputRefFragments(t *testing.T) {
	for _, tc := range []struct {
		name      string
		legacy    bool
		epicRef   string
		wantError string
	}{
		{name: "slug into strict carrier", epicRef: "specs/plans/child.md#capability-two", wantError: `eligible ATX heading "capability-two" is missing; the fragment must be the exact heading text, not a slug`},
		{name: "exact heading into strict carrier", epicRef: "specs/plans/child.md#Capability Two"},
		{name: "whole strict carrier", epicRef: "specs/plans/child.md"},
		{name: "slug into legacy file", legacy: true, epicRef: "specs/plans/child.md#task-one"},
		{name: "file absent from candidate", epicRef: "specs/epics/missing.md#capability-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a planner output whose epic_ref points into the committed candidate
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, outputRefFragmentSection, tc.legacy)
			if err := bb.Modify(func(state *models.State) error {
				state.FindTask(taskID).Output[0].EpicRef = tc.epicRef
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}

			// WHEN the planner submits
			result, err := SubmitForReview(root, taskID, commit, agentID)

			// THEN only an unresolvable strict fragment is refused, before any state change or rebase
			after, readErr := bb.Read()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				if result == nil || after.FindTask(taskID).Status != models.TaskStatusCodingPlanToReview {
					t.Fatal("output with resolvable refs was not submitted")
				}
				return
			}
			testhelpers.RequireErrorContains(t, err, tc.wantError)
			testhelpers.RequireErrorContains(t, err, "output[0].epic_ref")
			if result != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("unresolvable fragment changed submission state")
			}
			if head := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD"); head != commit {
				t.Fatal("unresolvable fragment rebased the worktree")
			}
		})
	}
}

func TestSubmitForReview_OutputRefFragmentAfterRebase(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    string
		wantError string
	}{
		{name: "heading removed", change: "## Capability 2", wantError: `eligible ATX heading "Capability Two" is missing`},
		{name: "heading duplicated", change: "## Capability Two\n\nDuplicate.\n\n## Capability Two", wantError: `eligible ATX heading "Capability Two" is ambiguous: 2 matches`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN integration adopts the candidate, then changes the referenced heading
			root, taskID, commit, agentID, bb := setupPlanningAcceptanceSubmission(t, outputRefFragmentSection, false)
			if err := bb.Modify(func(state *models.State) error {
				state.FindTask(taskID).Output[0].EpicRef = "specs/plans/child.md#Capability Two"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "cherry-pick", commit)
			path := filepath.Join(root, "specs/plans/child.md")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.Replace(string(content), "## Capability Two", tc.change, 1)
			if err := os.WriteFile(path, []byte(changed), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, root, "add", "specs/plans/child.md")
			testhelpers.MustGit(t, root, "commit", "-m", "Change heading on integration")

			// WHEN the planner submits its candidate, whose fragment resolved before rebase
			result, err := SubmitForReview(root, taskID, commit, agentID)

			// THEN the rebased boundary is checked too and nothing is published
			testhelpers.RequireErrorContains(t, err, tc.wantError)
			state, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if result != nil || state.FindTask(taskID).Status != models.TaskStatusCodePlanning || state.FindTask(taskID).ReviewCommit != nil {
				t.Fatal("post-rebase unresolvable fragment was submitted")
			}
			if head := testhelpers.MustGit(t, git.New(root).GetWorktreePath(taskID), "rev-parse", "HEAD"); head == commit {
				t.Fatal("test did not exercise a changed post-rebase candidate")
			}
		})
	}
}
