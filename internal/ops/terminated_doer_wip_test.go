package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

type terminatedDoerWIPFixture struct {
	projectRoot string
	bb          *db.Blackboard
	taskID      string
	worktreeDir string
	authority   models.AgentAuthority
}

// newTerminatedDoerWIPFixture claims a task for coder-1 and leaves an
// interrupted edit in its worktree, as a quota-terminated session would.
func newTerminatedDoerWIPFixture(t *testing.T) terminatedDoerWIPFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := ClaimTask(projectRoot, "task-1", "coder-1"); err != nil {
		t.Fatalf("ClaimTask() error: %v", err)
	}
	worktreeDir := git.New(projectRoot).GetWorktreePath("task-1")
	writeTestFile(t, worktreeDir, "README.md", "interrupted edit\n")
	return terminatedDoerWIPFixture{
		projectRoot: projectRoot,
		bb:          bb,
		taskID:      "task-1",
		worktreeDir: worktreeDir,
		authority:   models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration},
	}
}

func (f terminatedDoerWIPFixture) commit(t *testing.T) *TerminatedDoerWIPResult {
	t.Helper()
	result, err := CommitTerminatedDoerWIP(context.Background(), f.projectRoot, f.authority, f.taskID, "WIP: test")
	if err != nil {
		t.Fatalf("CommitTerminatedDoerWIP() error: %v", err)
	}
	return result
}

// worktreeSnapshot captures HEAD, branch, index, working-tree changes and file
// content, so a refusal can be shown to have mutated none of them.
func worktreeSnapshot(t *testing.T, worktreeDir string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(worktreeDir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return strings.Join([]string{
		testhelpers.MustGit(t, worktreeDir, "rev-parse", "HEAD"),
		testhelpers.MustGit(t, worktreeDir, "status", "--porcelain=v1", "--branch"),
		testhelpers.MustGit(t, worktreeDir, "ls-files", "--stage"),
		testhelpers.MustGit(t, worktreeDir, "diff"),
		string(content),
	}, "\n---\n")
}

func assertWIPSkipped(t *testing.T, result *TerminatedDoerWIPResult, reason string) {
	t.Helper()
	if result.Committed || !strings.Contains(result.SkipReason, reason) {
		t.Fatalf("result = %+v, want skipped with reason containing %q", result, reason)
	}
}

func TestCommitTerminatedDoerWIP_CommitsOwnedWorktree(t *testing.T) {
	f := newTerminatedDoerWIPFixture(t)
	headBefore := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD")

	result := f.commit(t)

	if !result.Committed || result.SHA == "" {
		t.Fatalf("result = %+v, want a WIP commit", result)
	}
	if head := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD"); head != result.SHA {
		t.Fatalf("HEAD = %s, want reported WIP commit %s", head, result.SHA)
	}
	if parent := testhelpers.MustGit(t, f.worktreeDir, "rev-parse", "HEAD^"); parent != headBefore {
		t.Fatalf("WIP commit parent = %s, want %s", parent, headBefore)
	}
	if status := testhelpers.MustGit(t, f.worktreeDir, "status", "--short"); status != "" {
		t.Fatalf("worktree still dirty after WIP commit:\n%s", status)
	}
}

func TestCommitTerminatedDoerWIP_StaleGenerationLeavesWorktreeUntouched(t *testing.T) {
	f := newTerminatedDoerWIPFixture(t)
	if err := f.bb.Modify(func(state *models.State) error {
		agent := state.Agents["coder-1"]
		agent.Generation = "replacement-generation"
		state.Agents["coder-1"] = agent
		return nil
	}); err != nil {
		t.Fatalf("replace registration: %v", err)
	}
	before := worktreeSnapshot(t, f.worktreeDir)

	result := f.commit(t)

	assertWIPSkipped(t, result, "coder-1")
	if after := worktreeSnapshot(t, f.worktreeDir); after != before {
		t.Fatalf("stale generation mutated the worktree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Ownership that moves while another holder has the task's claim-worktree lock
// is observed after that holder releases it: the WIP commit re-reads state
// under the lock and refuses, whichever side reaches the lock first.
func TestCommitTerminatedDoerWIP_OwnershipMovedUnderTaskLockLeavesWorktreeUntouched(t *testing.T) {
	f := newTerminatedDoerWIPFixture(t)
	before := worktreeSnapshot(t, f.worktreeDir)

	type outcome struct {
		result *TerminatedDoerWIPResult
		err    error
	}
	done := make(chan outcome, 1)
	lock := filelock.New(claimTaskWorktreeLockPath(paths.New(f.projectRoot).StatePath(), f.taskID))
	if err := lock.WithLockOperation("test-ownership-transfer", func() error {
		go func() {
			result, err := CommitTerminatedDoerWIP(context.Background(), f.projectRoot, f.authority, f.taskID, "WIP: test")
			done <- outcome{result, err}
		}()
		return f.bb.Modify(func(state *models.State) error {
			other := "coder-2"
			state.FindTask(f.taskID).AssignedTo = &other
			return nil
		})
	}); err != nil {
		t.Fatalf("transfer ownership under task lock: %v", err)
	}

	got := <-done
	if got.err != nil {
		t.Fatalf("CommitTerminatedDoerWIP() error: %v", got.err)
	}
	assertWIPSkipped(t, got.result, "not assigned to coder-1")
	if after := worktreeSnapshot(t, f.worktreeDir); after != before {
		t.Fatalf("moved ownership mutated the worktree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCommitTerminatedDoerWIP_RefusesUnsafeArtifactsWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, worktreeDir string)
		reason string
	}{
		{
			name: "detached HEAD",
			setup: func(t *testing.T, worktreeDir string) {
				testhelpers.MustGit(t, worktreeDir, "checkout", "--detach")
			},
			reason: `on branch ""`,
		},
		{
			name: "wrong branch",
			setup: func(t *testing.T, worktreeDir string) {
				testhelpers.MustGit(t, worktreeDir, "checkout", "-b", "not-the-task-branch")
			},
			reason: `on branch "not-the-task-branch"`,
		},
		{
			name: "unresolved merge",
			setup: func(t *testing.T, worktreeDir string) {
				testhelpers.MustGit(t, worktreeDir, "checkout", "--", "README.md")
				testhelpers.MustGit(t, worktreeDir, "checkout", "-b", "side")
				writeTestFile(t, worktreeDir, "README.md", "side change\n")
				testhelpers.MustGit(t, worktreeDir, "commit", "--no-verify", "-am", "side")
				testhelpers.MustGit(t, worktreeDir, "checkout", "-")
				writeTestFile(t, worktreeDir, "README.md", "task change\n")
				testhelpers.MustGit(t, worktreeDir, "commit", "--no-verify", "-am", "task")
				if out, err := gitenv.CombinedOutput(worktreeDir, "merge", "side"); err == nil {
					t.Fatalf("merge side succeeded, want a conflict:\n%s", out)
				}
			},
			reason: "mid-merge",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTerminatedDoerWIPFixture(t)
			tt.setup(t, f.worktreeDir)
			before := worktreeSnapshot(t, f.worktreeDir)

			result := f.commit(t)

			assertWIPSkipped(t, result, tt.reason)
			if after := worktreeSnapshot(t, f.worktreeDir); after != before {
				t.Fatalf("refusal mutated the worktree:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
