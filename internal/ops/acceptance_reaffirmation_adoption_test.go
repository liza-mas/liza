package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReaffirmedProofSurvivesClaimSubmissionAndReviewerAssignment(t *testing.T) {
	t.Parallel()
	root, taskID, agentID, bb := proofDriftScenario(t)
	reaffirmed := recordTestReaffirmation(t, root, bb, taskID, "identity")
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, nil)
	if _, err := ClaimTask(root, taskID, agentID); err != nil {
		t.Fatalf("claim with reaffirmed proof: %v", err)
	}
	adopted := readAcceptanceState(t, bb).FindTask(taskID).AcceptanceSource
	if adopted == nil || reaffirmed.ReviewedSection == reaffirmed.CurrentSection {
		t.Fatal("claim must adopt the source with an authorized changed proof")
	}
	if err := WriteCheckpoint(root, &WriteCheckpointInput{
		TaskID: taskID, AgentID: agentID,
		Intent:         "Implement the reviewed identity boundary",
		ValidationPlan: "Run the declared boundary tests and inspect acceptance mappings",
		FilesToModify:  []string{"boundary_test.sh", "acceptance/task-1.json"},
		TDDNotRequired: "Candidate already contains the executable boundary regression tests",
	}); err != nil {
		t.Fatal(err)
	}
	if err := submitProofDriftCandidate(t, root, taskID, agentID); err != nil {
		t.Fatalf("submission after adopting reaffirmed source: %v", err)
	}
	submitted := readAcceptanceState(t, bb).FindTask(taskID)
	if submitted.AcceptanceReceipt == nil || !reflect.DeepEqual(adopted, submitted.AcceptanceSource) || !reflect.DeepEqual(*adopted, submitted.AcceptanceReceipt.Source) {
		t.Fatal("submission lost or rewrote the adopted reaffirmed authority")
	}
	receipt := submitted.AcceptanceReceipt
	registerAcceptanceReviewer(t, bb, "code-reviewer-1")
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, TaskID: taskID, AgentID: "code-reviewer-1"}); err != nil {
		t.Fatalf("reviewer assignment after adopting reaffirmed source: %v", err)
	}
	reviewing := readAcceptanceState(t, bb).FindTask(taskID)
	if !reflect.DeepEqual(adopted, reviewing.AcceptanceSource) || !reflect.DeepEqual(receipt, reviewing.AcceptanceReceipt) {
		t.Fatal("reviewer assignment rewrote historical acceptance evidence")
	}

	// The original grant authorizes only the adopted proof, not another repin.
	goalPath := filepath.Join(root, "specs/acceptance-goal.md")
	goal, err := os.ReadFile(goalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalPath, []byte(strings.Replace(string(goal), "And reject the extended case.", "And reject the extended case.\nUnapproved later proof change.", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: unapproved proof drift after adoption")
	moved := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	planPath := filepath.Join(root, "specs/acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	repinned := regexp.MustCompile(`- "identity": "specs/acceptance-goal\.md#Identity" @ "[0-9a-f]{40}"`).
		ReplaceAllString(string(plan), `- "identity": "specs/acceptance-goal.md#Identity" @ "`+moved+`"`)
	if repinned == string(plan) {
		t.Fatal("fixture did not repin the proof onto the later change")
	}
	if err := os.WriteFile(planPath, []byte(repinned), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: repin onto unapproved proof drift")
	before := readAcceptanceState(t, bb)
	err = validateAcceptanceForAssignment(root, before, before.FindTask(taskID))
	requireAcceptanceError(t, err, taskID)
	requireAcceptanceStateUnchanged(t, bb, before)
	if !reflect.DeepEqual(adopted, before.FindTask(taskID).AcceptanceSource) || !reflect.DeepEqual(receipt, before.FindTask(taskID).AcceptanceReceipt) {
		t.Fatal("later drift rewrote stored acceptance evidence")
	}
}
