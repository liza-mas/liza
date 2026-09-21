package ops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAwaitResubmission_ReadinessDoesNotContendWithWriter(t *testing.T) {
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, time.Now().UTC())}
	state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	bb := testhelpers.WriteInitialState(t, stateFile, state)
	if err := acquireReviewOwnership(bb, "reviewer-1", "task-1", nil, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	// Ownership is already persisted. A readiness observer must not contend
	// with the worker's next transaction. Holding that transaction until the
	// observation completes makes the old exclusive-read failure deterministic.
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- bb.Modify(func(*models.State) error {
			close(locked)
			<-release
			return nil
		})
	}()
	t.Cleanup(func() {
		close(release)
		if err := <-done; err != nil {
			t.Errorf("held transaction: %v", err)
		}
	})
	select {
	case <-locked:
	case err := <-done:
		// Preserve the result for cleanup, which always joins the worker.
		done <- err
		t.Fatalf("transaction exited before taking the lock: %v", err)
	}
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", nil, 10*time.Second)
}

func TestAwaitResubmission_WorkerFailureCleanup(t *testing.T) {
	const caseEnv = "AWAIT_RESUBMISSION_FAILURE_CASE"
	const cleaned = "worker joined and ownership released before fixture cleanup"
	failureCase := os.Getenv(caseEnv)
	if failureCase == "" {
		for _, tc := range []struct{ name, diagnostic string }{
			{"fatal-after-ready", "injected failure after ownership"},
			{"early-error", "await worker exited before review ownership"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAwaitResubmission_WorkerFailureCleanup$", "-test.v")
				cmd.Env = append(os.Environ(), caseEnv+"="+tc.name)
				output, err := cmd.CombinedOutput()
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("expected injected test failure, got %v\n%s", err, output)
				}
				if !strings.Contains(string(output), tc.diagnostic) || !strings.Contains(string(output), cleaned) {
					t.Fatalf("missing failure or cleanup evidence:\n%s", output)
				}
				if tc.name == "early-error" && !strings.Contains(string(output), "no rejection history") {
					t.Fatalf("lost worker precondition error:\n%s", output)
				}
			})
		}
		return
	}

	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	if failureCase == "fatal-after-ready" {
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: now, Event: models.TaskEventRejected, Agent: strPtr("reviewer-1"),
		})
	}
	state.Tasks = []models.Task{task}
	state.Agents["reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusIdle}
	bb := testhelpers.WriteInitialState(t, stateFile, state)
	var wait *resubmissionWait
	// Cleanup runs LIFO: the worker must join first, then this observer, then
	// TempDir removal. A subprocess exercises real Fatal/Goexit unwinding.
	t.Cleanup(func() {
		select {
		case <-wait.done:
		default:
			t.Error("worker still running during fixture cleanup")
			return
		}
		if failureCase == "fatal-after-ready" && !errors.Is(wait.err, context.Canceled) {
			t.Errorf("cleanup did not cancel worker: %v", wait.err)
			return
		}
		state, err := bb.Read()
		if err != nil {
			t.Errorf("fixture removed before worker joined: %v", err)
			return
		}
		task := state.FindTask("task-1")
		if task.ReviewingBy != nil || task.ReviewLeaseExpires != nil || state.Agents["reviewer-1"].CurrentTask != nil {
			t.Error("worker retained ownership during fixture cleanup")
			return
		}
		t.Log(cleaned)
	})
	wait = startResubmissionWait(t, root, 10*time.Second)
	waitForReviewOwnership(t, bb, "task-1", "reviewer-1", wait, 10*time.Second)
	t.Fatal("injected failure after ownership")
}
