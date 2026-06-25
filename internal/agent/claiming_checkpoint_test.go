package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestHandleApprovedMerges_AutoEmitsCheckpointSummary is the end-to-end wiring
// test for bug fix #3 (auto-emit checkpoint-summary on merge). It builds a
// minimal git repo + state with an APPROVED task ready to merge, swaps in a
// deterministic checkpoint-summary runner that writes a sentinel file, runs
// handleApprovedMerges, and asserts the runner was invoked with the merged
// task ID. The default (config flag unset) must trigger emission.
func TestHandleApprovedMerges_AutoEmitsCheckpointSummary(t *testing.T) {
	tmpDir, stateFile, taskID := setupAgentMergeRepo(t)

	var runnerCalled bool
	var gotProjectRoot, gotPrompt string
	withFakeCheckpointSummaryRunner(t, func(projectRoot, cliName, prompt string, _ models.Config) error {
		runnerCalled = true
		gotProjectRoot = projectRoot
		gotPrompt = prompt
		// Simulate the CLI's job: write the report file so any downstream
		// post-run sanity check would be satisfied.
		reportPath := filepath.Join(projectRoot, filepath.FromSlash(checkpointSummaryRelPath()))
		if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(reportPath, []byte("# checkpoint summary\nfake report\n"), 0o644)
	})

	bb := db.New(stateFile)
	pr, err := ops.LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}

	if err := handleApprovedMerges(tmpDir, "code-reviewer-2", bb, pr); err != nil {
		t.Fatalf("handleApprovedMerges: %v", err)
	}

	if !runnerCalled {
		t.Fatal("expected checkpoint-summary runner to fire after merge")
	}
	if gotProjectRoot != tmpDir {
		t.Errorf("projectRoot = %q, want %q", gotProjectRoot, tmpDir)
	}
	if !strings.Contains(gotPrompt, taskID) {
		t.Errorf("prompt did not mention task ID %q: %q", taskID, gotPrompt)
	}

	// Verify the report file actually landed where the prompt asked for it.
	reportPath := filepath.Join(tmpDir, filepath.FromSlash(checkpointSummaryRelPath()))
	if _, err := os.Stat(reportPath); err != nil {
		t.Errorf("expected report at %s, got: %v", reportPath, err)
	}

	// And the task is genuinely MERGED in state (sanity: we didn't just
	// short-circuit before the merge).
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

// TestHandleApprovedMerges_RespectsAutoCheckpointOptOut confirms the opt-out
// path: a project that explicitly sets auto_checkpoint_summary: false must
// not invoke the runner even when a merge succeeds.
func TestHandleApprovedMerges_RespectsAutoCheckpointOptOut(t *testing.T) {
	tmpDir, stateFile, _ := setupAgentMergeRepo(t)

	// Patch the config in-place to disable auto checkpoint.
	bb := db.New(stateFile)
	if err := bb.Modify(func(s *models.State) error {
		off := false
		s.Config.AutoCheckpointSummary = &off
		return nil
	}); err != nil {
		t.Fatalf("bb.Modify: %v", err)
	}

	called := false
	withFakeCheckpointSummaryRunner(t, func(string, string, string, models.Config) error {
		called = true
		return nil
	})

	pr, err := ops.LoadResolverForModels(tmpDir)
	if err != nil {
		t.Fatalf("LoadResolverForModels: %v", err)
	}
	if err := handleApprovedMerges(tmpDir, "code-reviewer-2", bb, pr); err != nil {
		t.Fatalf("handleApprovedMerges: %v", err)
	}

	if called {
		t.Fatal("runner fired despite AutoCheckpointSummary=false")
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

	worktreeRel := filepath.Join(".worktrees", taskID)
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
