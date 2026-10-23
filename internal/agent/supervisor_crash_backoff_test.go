package agent

import (
	"context"
	stderrors "errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestCrashRestartDelay_DoublesToCap(t *testing.T) {
	for _, tc := range []struct {
		crashes int
		want    time.Duration
	}{
		{0, 5 * time.Second},
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 20 * time.Second},
		{6, 160 * time.Second},
		{7, 5 * time.Minute},
		{8, 5 * time.Minute},
		{1 << 30, 5 * time.Minute},
	} {
		if got := crashRestartDelay(tc.crashes); got != tc.want {
			t.Errorf("crashRestartDelay(%d) = %v, want %v", tc.crashes, got, tc.want)
		}
	}
}

// A failure shared by every task (#184: an unrecognised provider limit) must
// keep backing off when the crash-loop block moves the supervisor to the next
// task, instead of restarting the next task at the base delay.
func TestRunSupervisorCrashBackoffSpansTasks(t *testing.T) {
	mock := &MockLLMAgent{ExitCode: 1, ExitError: stderrors.New("provider exit status 1")}

	delays := runCrashingSupervisor(t, []string{"task-a", "task-b"}, 1, mock)

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second}
	if !slices.Equal(delays, want) {
		t.Fatalf("supervisor delays = %v, want %v", delays, want)
	}
}

// A session that does not crash ends the streak: the next crash restarts at
// the base delay.
func TestRunSupervisorCrashBackoffResetsAfterCleanExit(t *testing.T) {
	mock := &MockLLMAgent{}
	calls := 0
	mock.OnExecute = func(context.Context, string, string, string, string, []string) error {
		calls++
		mock.ExitCode, mock.ExitError = 1, stderrors.New("provider exit status 1")
		if calls == 3 {
			mock.ExitCode, mock.ExitError = 0, nil
		}
		return nil
	}

	delays := runCrashingSupervisor(t, []string{"task-a"}, 2, mock)

	want := []time.Duration{5 * time.Second, 10 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}
	if !slices.Equal(delays, want) {
		t.Fatalf("supervisor delays = %v, want %v", delays, want)
	}
}

// runCrashingSupervisor runs a coder supervisor over READY tasks until it
// finds no more work, and returns every delay it waited.
func runCrashingSupervisor(t *testing.T, taskIDs []string, crashThreshold int, mock *MockLLMAgent) []time.Duration {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 1
	state.Config.LeaseDuration = 300
	state.Config.CrashRestartThreshold = crashThreshold
	state.Config.SpinningRestartThreshold = 10
	now := time.Now().UTC()
	state.Tasks = nil
	for _, id := range taskIDs {
		state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, now))
	}
	testhelpers.WriteInitialState(t, statePath, state)
	recorded := recordSupervisorDelays(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), supervisorRunHangGuard)
	defer cancel()
	if err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          "coder-1",
		Role:             models.RoleCoder,
		ProjectRoot:      projectRoot,
		StatePath:        statePath,
		LogPath:          filepath.Join(projectRoot, paths.ProjectDirName(), "log.yaml"),
		SpecsDir:         filepath.Join(projectRoot, "specs"),
		CLIName:          "codex",
		LLMAgent:         mock,
		ExecutionTimeout: 10 * time.Second,
	}); err != nil {
		t.Fatalf("RunSupervisor: %v", err)
	}
	return recorded()
}
