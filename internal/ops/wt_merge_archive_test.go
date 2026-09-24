package ops

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// addArchiveBacklog appends a terminal task carrying an acceptance receipt.
func addArchiveBacklog(t *testing.T, statePath, taskID string) {
	t.Helper()
	task := archiveTestTask(taskID, models.TaskStatusMerged, time.Now().UTC())
	if err := db.New(statePath).Modify(func(state *models.State) error {
		state.Tasks = append(state.Tasks, task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func registerMergeAuthority(t *testing.T, statePath string, authority models.AgentAuthority) {
	t.Helper()
	if err := db.New(statePath).Modify(func(state *models.State) error {
		state.Agents[authority.ID] = models.Agent{Role: models.RoleOrchestrator, Status: models.AgentStatusIdle, Generation: authority.Generation}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func hasWarningContaining(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func TestMergeWorktreeArchivesTerminalReceiptsAfterNewMerge(t *testing.T) {
	recordIndexRefreshes(t, nil)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupMergeTestRepo(t, "merge-archive", "coder-1")
	addArchiveBacklog(t, statePath, "backlog")

	result, err := MergeWorktree(root, "merge-archive", "coder-1")
	if err != nil {
		t.Fatalf("MergeWorktree() error = %v", err)
	}
	if hasWarningContaining(result.Warnings, "archive") {
		t.Fatalf("unexpected archive warning: %q", result.Warnings)
	}
	backlog := readStateForTest(t, statePath).FindTask("backlog")
	if backlog.AcceptanceReceipt != nil || len(backlog.Archived) != 1 {
		t.Fatalf("backlog after merge = receipt %v, refs %+v; want archived", backlog.AcceptanceReceipt, backlog.Archived)
	}
}

func TestMergeWorktreeReportsArchiveFailureAsWarning(t *testing.T) {
	recordIndexRefreshes(t, nil)
	restore := recordArchiveDirSyncs(t, func(string) error { return errors.New("disk unavailable") })
	defer restore()
	root, statePath := setupMergeTestRepo(t, "merge-archive-warn", "coder-1")
	addArchiveBacklog(t, statePath, "backlog")

	result, err := MergeWorktree(root, "merge-archive-warn", "coder-1")
	if err != nil {
		t.Fatalf("MergeWorktree() error = %v, want the merge to succeed", err)
	}
	if result.Outcome != models.LifecycleCompleted || !hasWarningContaining(result.Warnings, "archive") {
		t.Fatalf("merge result = %+v, want COMPLETED with an archive warning", result)
	}
	state := readStateForTest(t, statePath)
	if state.FindTask("merge-archive-warn").Status != models.TaskStatusMerged {
		t.Fatal("archive failure undid the merge")
	}
	if backlog := state.FindTask("backlog"); backlog.AcceptanceReceipt == nil || len(backlog.Archived) != 0 {
		t.Fatalf("failed archive changed the backlog: %+v", backlog.Archived)
	}
}

func TestMergeWorktreeReplayDoesNotArchive(t *testing.T) {
	recordIndexRefreshes(t, nil)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupMergeTestRepo(t, "merge-archive-replay", "coder-1")
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "merge-generation"}
	registerMergeAuthority(t, statePath, authority)
	state := readStateForTest(t, statePath)
	opts := LifecycleRequestOptions{RequestID: "merge-1", ExpectedTransition: models.TaskTransitionID(state.FindTask("merge-archive-replay"))}
	if _, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-archive-replay", authority, opts); err != nil {
		t.Fatal(err)
	}
	addArchiveBacklog(t, statePath, "backlog")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	replay, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-archive-replay", authority, opts)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("merge replay = %+v, %v", replay, err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("merge replay changed state")
	}
}

func TestMergeWorktreeArchiveIsGenerationFenced(t *testing.T) {
	recordIndexRefreshes(t, nil)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupMergeTestRepo(t, "merge-archive-fence", "coder-1")
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "merge-generation"}
	registerMergeAuthority(t, statePath, authority)
	addArchiveBacklog(t, statePath, "backlog")
	hookRan := false
	postMergeArchiveTestHook = func() {
		hookRan = true
		registerMergeAuthority(t, statePath, models.AgentAuthority{ID: authority.ID, Generation: "replacement-generation"})
	}
	t.Cleanup(func() { postMergeArchiveTestHook = nil })

	result, err := MergeWorktreeWithAuthority(root, "merge-archive-fence", authority)
	if err != nil {
		t.Fatalf("MergeWorktreeWithAuthority() error = %v, want the merge to succeed", err)
	}
	if !hookRan {
		t.Fatal("post-merge archive maintenance did not run")
	}
	if result.Outcome != models.LifecycleCompleted || !hasWarningContaining(result.Warnings, "archive") {
		t.Fatalf("merge result = %+v, want COMPLETED with an archive warning", result)
	}
	if backlog := readStateForTest(t, statePath).FindTask("backlog"); backlog.AcceptanceReceipt == nil || len(backlog.Archived) != 0 {
		t.Fatalf("fenced archive changed the backlog: %+v", backlog.Archived)
	}
}

func TestMergeWorktreeWithAuthorityArchivesUnderItsGeneration(t *testing.T) {
	recordIndexRefreshes(t, nil)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupMergeTestRepo(t, "merge-archive-auth", "coder-1")
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "merge-generation"}
	registerMergeAuthority(t, statePath, authority)
	addArchiveBacklog(t, statePath, "backlog")

	result, err := MergeWorktreeWithAuthority(root, "merge-archive-auth", authority)
	if err != nil {
		t.Fatalf("MergeWorktreeWithAuthority() error = %v", err)
	}
	if result.Outcome != models.LifecycleCompleted || hasWarningContaining(result.Warnings, "archive") {
		t.Fatalf("merge result = %+v, want COMPLETED without archive warnings", result)
	}
	if backlog := readStateForTest(t, statePath).FindTask("backlog"); backlog.AcceptanceReceipt != nil || len(backlog.Archived) != 1 {
		t.Fatalf("authenticated merge did not archive the backlog: %+v", backlog.Archived)
	}
}
