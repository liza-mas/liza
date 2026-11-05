package ops

import (
	"bytes"
	stderrors "errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The JSON envelope withholds OperationalError.Err, so a failed test-file check
// must leave git's own message on stderr for the CLI or test output to show,
// even with the logger discarded as --json does, and name git's exit status
// and failure class in the envelope details.
// Not parallel: it redirects the process-wide standard logger and os.Stderr.
func TestSubmitForReview_TestFileCheckFailureLogsGitCause(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.MustGit(t, tmpDir, "checkout", "integration")

	g := git.New(tmpDir)
	taskID := "task-test-file-check-log"
	if _, err := g.CreateWorktree(taskID, "integration"); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	wtPath := g.GetWorktreePath(taskID)
	if err := os.WriteFile(filepath.Join(wtPath, "main_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "main_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Test commit")
	wtCommit := testhelpers.MustGit(t, wtPath, "rev-parse", "HEAD")

	// A base commit absent from the repository makes the test-file diff fail.
	missingBase := strings.Repeat("0", 39) + "1"
	agentID := "coder-1"
	leaseExpires := time.Now().UTC().Add(30 * time.Minute)
	worktree := g.GetWorktreeRelPath(taskID)
	testhelpers.WriteInitialState(t, statePath, &models.State{
		Config: models.Config{IntegrationBranch: "integration", LeaseDuration: 1800},
		Tasks: []models.Task{{
			ID:           taskID,
			Type:         models.TaskTypeCoding,
			Description:  "test-file check failure logging",
			Status:       models.TaskStatusImplementing,
			RolePair:     "coding-pair",
			AssignedTo:   &agentID,
			LeaseExpires: &leaseExpires,
			Worktree:     &worktree,
			BaseCommit:   &missingBase,
			Iteration:    1,
			Created:      time.Now().UTC(),
			History: []models.TaskHistoryEntry{{
				Time:  time.Now().UTC(),
				Event: models.TaskEventPreExecutionCheckpoint,
				Agent: &agentID,
				Extra: map[string]any{
					"intent":          "test-file check failure logging",
					"validation_plan": "the git cause reaches stderr",
					"files_to_modify": []string{"main_test.go"},
				},
			}},
		}},
		Agents: map[string]models.Agent{agentID: {Status: models.AgentStatusWorking, CurrentTask: &taskID}},
	})

	// --json discards the standard logger, so the line must reach stderr
	// directly; a discarded logger proves it does not go through it.
	previousOutput := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(previousOutput) })
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStderr := os.Stderr
	os.Stderr = stderrW
	var stderrText bytes.Buffer
	copied := make(chan struct{})
	go func() { _, _ = io.Copy(&stderrText, stderrR); close(copied) }()
	_, err = SubmitForReview(tmpDir, taskID, wtCommit, agentID)
	os.Stderr = previousStderr
	_ = stderrW.Close()
	<-copied
	logged := &stderrText

	var operational *OperationalError
	if !stderrors.As(err, &operational) || operational.Phase != "check-test-files" {
		t.Fatalf("SubmitForReview() error = %v, want the check-test-files OperationalError", err)
	}
	for _, want := range []string{"test-file check failed", "git [diff --name-only", missingBase} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("stderr log lacks %q; got:\n%s", want, logged.String())
		}
	}
	// The envelope names the failure without git's text.
	details := operational.SafeDetails()
	if details["git_exit_code"] != 128 || details["git_failure"] != string(git.FailureBadRevision) {
		t.Errorf("details git_exit_code=%v git_failure=%v, want 128 and %s", details["git_exit_code"], details["git_failure"], git.FailureBadRevision)
	}
}
