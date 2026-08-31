package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestStartSupervisorHeartbeat_StopWaitsForWorker(t *testing.T) {
	var logs bytes.Buffer
	restoreLogger := UseLoggerOutput(&logs)
	defer restoreLogger()

	cancelObserved := make(chan struct{})
	releaseWorker := make(chan struct{})
	stopHeartbeat := startSupervisorHeartbeat(
		context.Background(),
		func(ctx context.Context) error {
			<-ctx.Done()
			close(cancelObserved)
			<-releaseWorker
			GetLogger().Info("heartbeat shutdown tail")
			return ctx.Err()
		},
		func(err error) {
			t.Errorf("unexpected heartbeat error callback: %v", err)
		},
	)

	stopped := make(chan struct{})
	go func() {
		stopHeartbeat()
		close(stopped)
	}()

	waitForLifecycleSignal(t, cancelObserved, "heartbeat cancellation")
	select {
	case <-stopped:
		t.Fatal("heartbeat shutdown returned before worker exit")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseWorker)
	waitForLifecycleSignal(t, stopped, "heartbeat shutdown")

	if !strings.Contains(logs.String(), "heartbeat shutdown tail") {
		t.Fatalf("heartbeat tail log missing after shutdown: %q", logs.String())
	}
}

func TestExecutionProgressWatchdogStop_WaitsForWorkerAndIsIdempotent(t *testing.T) {
	var logs bytes.Buffer
	restoreLogger := UseLoggerOutput(&logs)
	defer restoreLogger()

	watchCtx, cancelWatchdog := context.WithCancel(context.Background())
	cancelObserved := make(chan struct{})
	releaseWorker := make(chan struct{})
	resultCh := make(chan executionProgressWatchdogResult, 1)
	want := executionProgressWatchdogResult{Blocked: true, Reason: "delayed result"}
	go func() {
		<-watchCtx.Done()
		close(cancelObserved)
		<-releaseWorker
		GetLogger().Info("watchdog shutdown tail")
		resultCh <- want
	}()
	stopWatchdog := newExecutionProgressWatchdogStop(cancelWatchdog, resultCh)

	stopped := make(chan executionProgressWatchdogResult, 1)
	go func() {
		stopped <- stopWatchdog()
	}()

	waitForLifecycleSignal(t, cancelObserved, "watchdog cancellation")
	select {
	case result := <-stopped:
		t.Fatalf("watchdog shutdown returned before worker exit: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseWorker)

	select {
	case result := <-stopped:
		if result != want {
			t.Fatalf("watchdog result = %+v, want %+v", result, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watchdog shutdown")
	}
	if !strings.Contains(logs.String(), "watchdog shutdown tail") {
		t.Fatalf("watchdog tail log missing after shutdown: %q", logs.String())
	}

	secondResult := make(chan executionProgressWatchdogResult, 1)
	go func() { secondResult <- stopWatchdog() }()
	select {
	case result := <-secondResult:
		if result != want {
			t.Fatalf("second watchdog result = %+v, want %+v", result, want)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second watchdog stop call blocked")
	}
}

func TestExecuteAgent_PanicWaitsForProgressWatchdog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Git process inspection is Unix-specific")
	}

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	taskID := "task-panic-watchdog"
	agentID := "coder-1"
	git := lizagit.New(projectRoot)
	baseCommit, err := git.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("CreateWorktree() error: %v", err)
	}
	worktree := git.GetWorktreeRelPath(taskID)
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = &agentID
	task.BaseCommit = &baseCommit
	task.Worktree = &worktree
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents = map[string]models.Agent{
		agentID: {
			Role:        models.RoleCoder,
			Status:      models.AgentStatusWorking,
			CurrentTask: &taskID,
			Heartbeat:   now,
		},
	}
	testhelpers.WriteInitialState(t, statePath, state)

	binDir := t.TempDir()
	pidPath := filepath.Join(t.TempDir(), "watchdog-git.pid")
	fakeGit := filepath.Join(binDir, "git")
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$WATCHDOG_GIT_PID_FILE\"\nexec sleep 30\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("WATCHDOG_GIT_PID_FILE", pidPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	originalWaitDelay := gitenv.DefaultCommandWaitDelay
	gitenv.DefaultCommandWaitDelay = 50 * time.Millisecond
	t.Cleanup(func() { gitenv.DefaultCommandWaitDelay = originalWaitDelay })
	t.Cleanup(func() {
		if pid, readErr := readProcessID(pidPath); readErr == nil {
			if process, findErr := os.FindProcess(pid); findErr == nil {
				_ = process.Kill()
			}
		}
	})

	mock := &MockLLMAgent{
		OnExecute: func(context.Context, string, string, string, string, []string) error {
			if _, waitErr := waitForProcessID(pidPath, 2*time.Second); waitErr != nil {
				return waitErr
			}
			panic("provider panic")
		},
	}
	config := SupervisorConfig{
		AgentID:                  agentID,
		Role:                     models.RoleCoder,
		ProjectRoot:              projectRoot,
		StatePath:                statePath,
		CLIName:                  "codex",
		LLMAgent:                 mock,
		ExecutionTimeout:         5 * time.Second,
		ExecutionProgressTimeout: time.Minute,
	}

	type panicOutcome struct {
		recovered any
		err       error
	}
	outcomeCh := make(chan panicOutcome, 1)
	go func() {
		var runErr error
		defer func() {
			outcomeCh <- panicOutcome{recovered: recover(), err: runErr}
		}()
		_, _, runErr = executeAgent(context.Background(), config, "prompt", nil, taskID, state.Config)
	}()

	var outcome panicOutcome
	select {
	case outcome = <-outcomeCh:
	case <-time.After(3 * time.Second):
		t.Fatal("executeAgent did not finish panic cleanup within 3s")
	}
	if outcome.err != nil {
		t.Fatalf("executeAgent returned error instead of propagating provider panic: %v", outcome.err)
	}
	if outcome.recovered != "provider panic" {
		t.Fatalf("recovered panic = %v, want provider panic", outcome.recovered)
	}

	pid, err := readProcessID(pidPath)
	if err != nil {
		t.Fatalf("read fake Git PID: %v", err)
	}
	if err := waitForProcessExit(pid, 200*time.Millisecond); err != nil {
		if process, findErr := os.FindProcess(pid); findErr == nil {
			_ = process.Kill()
		}
		t.Fatalf("watchdog Git process outlived panic cleanup: %v", err)
	}
}

func waitForProcessID(path string, timeout time.Duration) (int, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		pid, err := readProcessID(path)
		if err == nil {
			return pid, nil
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return 0, fmt.Errorf("timed out waiting for fake Git PID")
		}
	}
}

func readProcessID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse PID: %w", err)
	}
	return pid, nil
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		alive, _, err := procscan.ProcessAlive(pid)
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return fmt.Errorf("process %d is still running", pid)
		}
	}
}

func waitForLifecycleSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
