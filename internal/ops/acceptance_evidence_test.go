package ops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// setupAcceptanceScenario builds the independently reviewed source and the
// deliberately incomplete candidate from the original approved regression.
func setupAcceptanceScenario(t *testing.T) (string, string, string, string, *db.Blackboard) {
	t.Helper()
	requirePosixShell(t)
	root, taskID, _, agentID, bb := setupSuccessfulSubmitScenario(t)
	g := git.New(root)
	wt := g.GetWorktreePath(taskID)
	write := func(dir, name, contents string) {
		t.Helper()
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Build real immutable source and independently approved parent artifacts.
	write(root, "specs/acceptance-goal.md", "# Boundary\n\n## Identity\nReject malformed identity.\n\n## Replay\nExercise independent concurrent replay.\n")
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record boundary requirements")
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	plan := fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n- \"replay\": \"specs/acceptance-goal.md#Replay\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n- \"AC-replay\" -> \"replay\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\",\"AC-replay\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":10,\"approved_proofs\":[]}\n```\n", sourceCommit)
	write(root, "specs/acceptance-plan.md", plan)
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: independently reviewed acceptance allocation")
	parentCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, wt, "rebase", "integration")

	write(wt, "identity.txt", "human\n")
	write(wt, "boundary_test.sh", "set -eu\ntest \"$(cat identity.txt)\" = human\nprintf 'PASS identity assertion\\n'\n")
	write(wt, "acceptance/task-1.json", `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"boundary_test.sh","assertion":"identity assertion","command_index":0}]}`+"\n")
	testhelpers.MustGit(t, wt, "add", "identity.txt", "boundary_test.sh", "acceptance/task-1.json")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: green suite omits replay evidence")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	parentID, planner, approver := "acceptance-parent", "code-planner-1", "code-plan-reviewer-1"
	planRef := "specs/acceptance-plan.md#Task 1"
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.PlanRef = planRef
		task.SpecRef = "specs/acceptance-goal.md"
		task.Validation = []string{"sh boundary_test.sh"}
		task.ParentTask = &parentID
		task.BaseCommit = &parentCommit
		state.Tasks = append(state.Tasks, models.Task{
			ID: parentID, Type: models.TaskTypePlanning, RolePair: "code-planning-pair", Status: models.TaskStatusMerged,
			AssignedTo: &planner, ApprovedBy: &approver,
			Approvals:  []models.Approval{{Agent: approver, Timestamp: time.Now().UTC()}},
			BaseCommit: &sourceCommit, ReviewCommit: &parentCommit, MergeCommit: &parentCommit,
			Output:  []models.OutputEntry{{PlanRef: planRef, SpecRef: task.SpecRef, Validation: task.Validation}},
			Created: time.Now().UTC(),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, taskID, commit, agentID, bb
}

// A passing suite is insufficient when a reviewed obligation has no proof map.
func TestAcceptanceEvidence_GreenIncompleteSubmission(t *testing.T) {
	root, taskID, commit, agentID, bb := setupAcceptanceScenario(t)
	wt := git.New(root).GetWorktreePath(taskID)

	// Establish the premise against an actual executable, not a reported pass.
	cmd := exec.Command("sh", "boundary_test.sh")
	cmd.Dir = wt
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "PASS identity assertion\n" {
		t.Fatalf("fixture suite = %q, %v; want a passing identity assertion", output, err)
	}
	t.Logf("committed fixture suite passed: %s", strings.TrimSpace(string(output)))
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}

	_, err = SubmitForReview(root, taskID, commit, agentID)
	if err == nil {
		t.Fatal("SubmitForReview admitted a green suite with no acceptance mapping for AC-replay")
	}
	var evidenceErr *AcceptanceEvidenceError
	if !errors.As(err, &evidenceErr) {
		t.Fatalf("expected typed acceptance precondition, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "AC-replay") || !strings.Contains(err.Error(), "mappings") {
		t.Fatalf("expected field-level missing AC-replay mapping diagnostic, got %v", err)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.FindTask(taskID), after.FindTask(taskID)) || !reflect.DeepEqual(before.Agents[agentID], after.Agents[agentID]) {
		t.Fatal("failed admission changed task or agent state")
	}
}
