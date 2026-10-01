package agent

import (
	"context"
	"fmt"
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
	const untrackedFile = "wip-new-file.txt"
	// interruptSession edits the task worktree like a doer cut off mid-edit and
	// returns the pre-session HEAD and the edited tracked file.
	interruptSession := func(worktree string) (headBefore, trackedFile string, err error) {
		headBefore = testhelpers.MustGit(t, worktree, "rev-parse", "HEAD")
		trackedFile = strings.Fields(testhelpers.MustGit(t, worktree, "ls-files"))[0]
		trackedPath := filepath.Join(worktree, trackedFile)
		content, err := os.ReadFile(trackedPath)
		if err != nil {
			return "", "", err
		}
		if err := os.WriteFile(trackedPath, append(content, []byte("\ninterrupted edit\n")...), 0o644); err != nil {
			return "", "", err
		}
		return headBefore, trackedFile, os.WriteFile(filepath.Join(worktree, untrackedFile), []byte("new work\n"), 0o644)
	}

	t.Run("failed session", func(t *testing.T) {
		runQuotaTerminationScenario(t, untrackedFile, func(t *testing.T, worktree string) (LLMAgent, func() (string, string, int)) {
			var headBefore, trackedFile string
			mock := &MockLLMAgent{
				ExitCode: 1,
				Output:   `{"type":"error","message":"You've hit your usage limit."}`,
			}
			mock.OnExecute = func(ctx context.Context, cliName, agentID, prompt, root string, additionalDirs []string) (err error) {
				headBefore, trackedFile, err = interruptSession(worktree)
				return err
			}
			return mock, func() (string, string, int) { return headBefore, trackedFile, len(mock.GetCalls()) }
		})
	})

	// The provider prints its quota failure but exits 0: the CLI backend must
	// still report a failed session so the same termination path runs.
	t.Run("quota printed on exit 0", func(t *testing.T) {
		runQuotaTerminationScenario(t, untrackedFile, func(t *testing.T, worktree string) (LLMAgent, func() (string, string, int)) {
			record := t.TempDir()
			turn := filepath.Join(record, "turn.jsonl")
			if err := os.WriteFile(turn, []byte(`{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}`+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			binDir := t.TempDir()
			testhelpers.WriteShellStub(t, filepath.Join(binDir, "codex"), fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then echo 'codex-cli 0.154.0'; exit 0; fi
wt=%[1]q
rec=%[2]q
git -C "$wt" rev-parse HEAD > "$rec/head"
tracked=$(git -C "$wt" ls-files | head -n 1)
printf '%%s' "$tracked" > "$rec/tracked"
printf '\ninterrupted edit\n' >> "$wt/$tracked"
printf 'new work\n' > "$wt/%[3]s"
echo run >> "$rec/calls"
cat %[4]q
exit 0
`, worktree, record, untrackedFile, turn))
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			return NewCLIAgent(""), func() (string, string, int) {
				read := func(name string) string {
					data, _ := os.ReadFile(filepath.Join(record, name))
					return strings.TrimSpace(string(data))
				}
				return read("head"), read("tracked"), len(strings.Fields(read("calls")))
			}
		})
	})
}

// runQuotaTerminationScenario runs one claimable task through a supervisor whose
// provider session edits the task worktree and then fails on provider quota.
// newAgent returns the agent and an observer of what the session recorded.
func runQuotaTerminationScenario(t *testing.T, untrackedFile string, newAgent func(t *testing.T, worktree string) (LLMAgent, func() (headBefore, trackedFile string, calls int))) {
	t.Helper()
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
	agent, observed := newAgent(t, worktree)

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
		LLMAgent:         agent,
		ExecutionTimeout: 10 * time.Second,
	})

	// THEN the interrupted edits are committed on the task branch
	if err != nil {
		t.Fatalf("RunSupervisor() error = %v, want quota termination to return nil", err)
	}
	headBefore, trackedFile, calls := observed()
	if calls != 1 {
		t.Fatalf("provider sessions = %d, want 1", calls)
	}
	if !CheckQuotaSignal(projectRoot, "codex") {
		t.Fatal("codex quota signal not set, want the provider parked until its reset")
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
