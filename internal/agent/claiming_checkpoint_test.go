package agent

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestHandleApprovedMerges_DoesNotEmitCheckpointSummary guards the C7 change:
// a merge must no longer spawn the checkpoint-summary CLI. The emission moved
// to the sprint-checkpoint boundary, so the reviewer's merge loop is never
// held for a report — see maybeEmitCheckpointSummary in the orchestrator
// strategy. The merge itself must still complete.
func TestHandleApprovedMerges_DoesNotEmitCheckpointSummary(t *testing.T) {
	tmpDir, stateFile, taskID := setupAgentMergeRepo(t)

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	bb := db.New(stateFile)
	pr, err := ops.LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	if err := handleApprovedMerges(tmpDir, "code-reviewer-2", bb, pr); err != nil {
		t.Fatalf("handleApprovedMerges: %v", err)
	}

	if called {
		t.Error("merge spawned a checkpoint-summary CLI; emission belongs to the checkpoint boundary")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, filepath.FromSlash(checkpointSummaryRelPath()))); !os.IsNotExist(err) {
		t.Errorf("merge wrote a checkpoint summary: %v", err)
	}

	// Sanity: the merge really ran, so the assertions above are not vacuous.
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	mergedTask := state.FindTask(taskID)
	if mergedTask == nil {
		t.Fatal("merged task vanished from state")
	}
	if mergedTask.Status != models.TaskStatusMerged {
		t.Errorf("status = %v, want MERGED", mergedTask.Status)
	}
}

// setupAgentMergeRepo builds a minimal git repo + Liza state ready for the
// merge path: integration branch, a worktree with one commit, a task in
// APPROVED status with two approvals (quorum 2 already met by reviewer-1
// and reviewer-2). The caller passes in the agentID used for the merge.
func setupAgentMergeRepo(t *testing.T) (projectRoot, stateFile, taskID string) {
	t.Helper()
	tmpDir := t.TempDir()
	taskID = "merge-checkpoint"

	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ = testhelpers.SetupLizaDir(t, tmpDir)

	mustGit := func(args ...string) string {
		full := append([]string{"-C", tmpDir}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
		return strings.TrimSpace(string(out))
	}

	// integration branch already created by SetupTestGitRepo; just check it out.
	mustGit("checkout", "integration")

	// Create worktree on a new branch.
	wtDir := filepath.Join(tmpDir, ".worktrees", taskID)
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}
	if out, err := exec.Command(
		"git", "-C", tmpDir, "worktree", "add", wtDir, "integration", "-b", "task/"+taskID,
	).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, string(out))
	}

	// Make a commit in the worktree.
	wtFile := filepath.Join(wtDir, "feature.txt")
	if err := os.WriteFile(wtFile, []byte("feature implementation\n"), 0o644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	if out, err := exec.Command("git", "-C", wtDir, "add", "feature.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", wtDir, "commit", "-m", "feat: add feature").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, string(out))
	}
	commitSHA := strings.TrimSpace(mustGitInDir(t, wtDir, "rev-parse", "HEAD"))

	// Build the state with the task pre-approved by 2 reviewers.
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.IntegrationBranch = "integration"
	state.Goal.SpecRef = "README.md"

	worktreeRel := path.Join(".worktrees", taskID)
	baseCommit := "base"
	approvedBy := "code-reviewer-1"
	state.Tasks = []models.Task{
		{
			ID:           taskID,
			Description:  "feature work",
			Status:       models.TaskStatusApproved,
			Priority:     1,
			Created:      now,
			SpecRef:      "README.md",
			DoneWhen:     "ok",
			Scope:        "test",
			RolePair:     "coding-pair",
			Worktree:     &worktreeRel,
			BaseCommit:   &baseCommit,
			ReviewCommit: &commitSHA,
			ApprovedBy:   &approvedBy,
			Approvals: []models.Approval{
				{Agent: "code-reviewer-1", Provider: "anthropic", Timestamp: now},
				{Agent: "code-reviewer-2", Provider: "openai", Timestamp: now},
			},
			History: []models.TaskHistoryEntry{},
		},
	}

	// Register the merging reviewer so quorum checks don't trip.
	state.Agents["code-reviewer-1"] = testhelpers.RegisteredTestAgent("code-reviewer")
	state.Agents["code-reviewer-2"] = testhelpers.RegisteredTestAgent("code-reviewer")
	testhelpers.WriteInitialState(t, stateFile, state)

	return tmpDir, stateFile, taskID
}

func mustGitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
