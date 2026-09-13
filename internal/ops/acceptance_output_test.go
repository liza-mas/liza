package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAcceptanceSubmissionPreservesIndentedOutput(t *testing.T) {
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
