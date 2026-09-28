package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// A quota termination interrupts the doer mid-edit. Its preserved worktree must
// not be left dirty, or the next claim blocks the task and a pause becomes a
// block that only a human can repair.
func TestRunSupervisor_QuotaTerminationCommitsDoerWIP(t *testing.T) {
	// GIVEN a claimable task whose provider session edits the task worktree
	// and then fails on provider quota
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	taskID := "task-quota-wip"
	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	worktree := lizagit.New(projectRoot).GetWorktreePath(taskID)
	const untrackedFile = "wip-new-file.txt"
	var trackedFile, headBefore string

	mock := &MockLLMAgent{
		ExitCode: 1,
		Output:   `{"type":"error","message":"You've hit your usage limit."}`,
	}
	mock.OnExecute = func(ctx context.Context, cliName, agentID, prompt, root string, additionalDirs []string) error {
		headBefore = testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
		trackedFile = strings.Fields(testhelpers.MustGit(t, worktree, "ls-files"))[0]
		trackedPath := filepath.Join(worktree, trackedFile)
		content, err := os.ReadFile(trackedPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(trackedPath, append(content, []byte("\ninterrupted edit\n")...), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(worktree, untrackedFile), []byte("new work\n"), 0o644)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// WHEN the supervisor runs the session and terminates on quota
	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             "coder",
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 10 * time.Second,
	})

	// THEN the interrupted edits are committed on the task branch
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v, want quota termination to return nil", err)
	}
	if calls := mock.GetCalls(); len(calls) != 1 {
		t.Fatalf("Execute calls = %d, want 1", len(calls))
	}
	if status := testhelpers.MustGit(t, worktree, "status", "--short"); status != "" {
		t.Fatalf("task worktree is dirty after quota termination:\n%s", status)
	}
	headAfter := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
	if headAfter == headBefore {
		t.Fatalf("worktree HEAD = %s, want a WIP commit on top of it", headAfter)
	}
	if parent := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD^"); parent != headBefore {
		t.Fatalf("WIP commit parent = %s, want the pre-session HEAD %s", parent, headBefore)
	}
	if branch := testhelpers.MustGit(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); branch != paths.TaskBranchPrefix+taskID {
		t.Fatalf("WIP commit branch = %q, want %q", branch, paths.TaskBranchPrefix+taskID)
	}
	committed := strings.Fields(testhelpers.MustGit(t, worktree, "show", "--name-only", "--format=", "HEAD"))
	for _, want := range []string{trackedFile, untrackedFile} {
		if !slices.Contains(committed, want) {
			t.Fatalf("WIP commit files = %v, want %s", committed, want)
		}
	}

	// AND the claim is released with the worktree preserved for continuation
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	task := updated.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %q not found after supervisor run", taskID)
	}
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil {
		t.Fatalf("task status/assignee = %s/%v, want released to %s", task.Status, task.AssignedTo, models.TaskStatusReady)
	}
	if task.Worktree == nil || *task.Worktree == "" {
		t.Fatal("task worktree was cleared, want it preserved for continuation")
	}

	// AND a fresh doer continues the preserved work instead of blocking on it
	testhelpers.RegisterTestAgent(t, bb, "coder-2", models.RoleCoder)
	if _, err := ops.ClaimTask(projectRoot, taskID, "coder-2"); err != nil {
		t.Fatalf("fresh claim after quota termination error = %v, want the preserved worktree adopted", err)
	}
	claimed, err := bb.Read()
	if err != nil {
		t.Fatalf("bb.Read: %v", err)
	}
	claimedTask := claimed.FindTask(taskID)
	if claimedTask.Status == models.TaskStatusBlocked {
		t.Fatalf("fresh claim blocked the task: %v", claimedTask.BlockedReason)
	}
	if claimedTask.AssignedTo == nil || *claimedTask.AssignedTo != "coder-2" {
		t.Fatalf("task assignee = %v, want coder-2", claimedTask.AssignedTo)
	}
	if got := testhelpers.MustGit(t, worktree, "rev-parse", "HEAD"); got != headAfter {
		t.Fatalf("claimed worktree HEAD = %s, want the WIP commit %s", got, headAfter)
	}
}
