package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/toolresult"
)

// longFailingValidation prints a recognizable first line, a credential and
// well over the excerpt limit of passing-step noise, then names its failing
// step last and exits 2, like `make check` running pre-commit (issue #214).
const longFailingValidation = `set -eu
# identity assertion, replay assertion
printf 'FIRST LINE of the validation\n'
i=0
while [ "$i" -lt 300 ]; do printf 'hook-%03d ........................................ Passed\n' "$i"; i=$((i+1)); done
printf 'credential in the middle: %s\n' "$ACCEPTANCE_FAILURE_OUTPUT_TOKEN"
while [ "$i" -lt 600 ]; do printf 'hook-%03d ........................................ Passed\n' "$i"; i=$((i+1)); done
printf 'FAILING STEP: lint exited 2\n' >&2
exit 2
`

var acceptanceOutputReferencePattern = regexp.MustCompile(`tool-result read ([0-9a-f]{64})`)

// diagnosticTail keeps a failure message readable: the last 600 bytes of a
// diagnostic and its length.
func diagnosticTail(diagnostic string) string {
	if len(diagnostic) <= 600 {
		return diagnostic
	}
	return fmt.Sprintf("[%d bytes, last 600:] %s", len(diagnostic), diagnostic[len(diagnostic)-600:])
}

// submitLongFailingValidation commits longFailingValidation as the canonical
// command and returns the submission's acceptance refusal.
func submitLongFailingValidation(t *testing.T, root, taskID, agentID string) *AcceptanceEvidenceError {
	t.Helper()
	wt := git.New(root).GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wt, "boundary_test.sh"), []byte(longFailingValidation), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "boundary_test.sh")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: long validation failing at its last step")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	_, err := SubmitForReview(root, taskID, commit, agentID)
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Field != "acceptance.execution" || !strings.Contains(refusal.Reason, "acceptance.execution[0]") {
		t.Fatalf("want an acceptance.execution[0] refusal, got %v", err)
	}
	if !strings.Contains(refusal.Reason, "FIRST LINE of the validation") || !strings.Contains(refusal.Reason, "FAILING STEP: lint exited 2") {
		t.Fatalf("refusal reason lost the head or the failing last step:\n%s", diagnosticTail(refusal.Reason))
	}
	return refusal
}

// A failed canonical command's full masked output is kept in the project
// tool-result store and named in the refusal (issue #214).
func TestAcceptanceFailureStoresFullMaskedOutput(t *testing.T) {
	t.Setenv("ACCEPTANCE_FAILURE_OUTPUT_TOKEN", "synthetic-failure-output-value")
	root, taskID, _, agentID, _ := completeAcceptanceScenario(t)
	refusal := submitLongFailingValidation(t, root, taskID, agentID)
	match := acceptanceOutputReferencePattern.FindStringSubmatch(refusal.Error())
	if match == nil {
		t.Fatalf("refusal does not name the stored full output:\n%s", diagnosticTail(refusal.Reason))
	}
	storeDir := filepath.Join(root, paths.ProjectDirName(), "tool-results")
	store, err := toolresult.New(storeDir, toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	full, err := store.Read(match[1], 0, toolresult.MaxReadBytes)
	if err != nil {
		t.Fatalf("named output is not readable: %v", err)
	}
	for _, want := range []string{"FIRST LINE of the validation", "hook-299 ", "credential in the middle: ***", "hook-599 ", "FAILING STEP: lint exited 2"} {
		if !strings.Contains(full, want) {
			t.Fatalf("stored output is missing %q", want)
		}
	}
	if strings.Contains(full, "synthetic-failure-output-value") || strings.Contains(refusal.Reason, "synthetic-failure-output-value") {
		t.Fatal("stored output or refusal exposed the credential")
	}
	if events, _ := filepath.Glob(filepath.Join(storeDir, "events", "*.json")); len(events) != 0 {
		t.Fatalf("stored acceptance output wrote tool-result events: %v", events)
	}
}

// A store that cannot keep the output leaves the original failure intact and
// says the full output was not stored, without claiming a reference.
func TestAcceptanceFailureReportsUnstoredOutput(t *testing.T) {
	t.Setenv("ACCEPTANCE_FAILURE_OUTPUT_TOKEN", "synthetic-failure-output-value")
	root, taskID, _, agentID, _ := completeAcceptanceScenario(t)
	if err := os.WriteFile(filepath.Join(root, paths.ProjectDirName(), "tool-results"), []byte("not a directory\n"), 0644); err != nil {
		t.Fatal(err)
	}
	refusal := submitLongFailingValidation(t, root, taskID, agentID)
	if !strings.Contains(refusal.Reason, "full masked output not stored: ") || strings.Contains(refusal.Reason, "tool-result read") {
		t.Fatalf("refusal does not report the unstored output honestly:\n%s", diagnosticTail(refusal.Reason))
	}
	if !strings.Contains(refusal.Reason, "command failed (exit 2)") {
		t.Fatalf("store failure obscured the command failure:\n%s", diagnosticTail(refusal.Reason))
	}
}

func TestAcceptanceSubmissionPreservesIndentedOutput(t *testing.T) {
	t.Parallel()
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	wt := git.New(root).GetWorktreePath(taskID)
	const output = "  Determining projects to restore...\nTest run for assembly\nPassed!\n"
	if err := os.WriteFile(filepath.Join(wt, "boundary_test.sh"), []byte("printf '"+output+"'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "boundary_test.sh")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: emit indented validation output")
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	if _, err := SubmitForReview(root, taskID, commit, agentID); err != nil {
		t.Fatalf("passing validation output prevented submission: %v", err)
	}
	state := readAcceptanceState(t, bb)
	task := state.FindTask(taskID)
	if task.Status != models.TaskStatusReadyForReview || task.AcceptanceReceipt == nil {
		t.Fatal("submission did not persist review admission and receipt")
	}
	commands := task.AcceptanceReceipt.Commands
	if len(commands) != 1 || commands[0].Output != output || commands[0].ExitCode != 0 {
		t.Fatal("submission did not preserve successful execution output")
	}
	if err := validateAcceptanceForAssignment(root, state, task); err != nil {
		t.Fatalf("saved receipt is not reviewable: %v", err)
	}
}
