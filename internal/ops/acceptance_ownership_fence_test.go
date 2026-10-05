//go:build !windows

package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The shared fixture must publish when its canonical command passes, so a
// failure in the fence test below is about the fence, not the fixture.
func TestAcceptanceSubmitScenarioPublishesPassingValidation(t *testing.T) {
	t.Parallel()
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "printf 'PASS identity assertion\\n'\n", 30)

	result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
	if err != nil {
		t.Fatalf("SubmitForReviewWithAuthority() error = %v", err)
	}

	task := readAcceptanceState(t, scenario.BB).FindTask(scenario.TaskID)
	if task.Status != models.TaskStatusReadyForReview || task.AcceptanceReceipt == nil || task.AcceptanceReceipt.ReviewCommit != result.ReviewCommit {
		t.Fatalf("task status=%s receipt=%v, want READY_FOR_REVIEW with a receipt at %s", task.Status, task.AcceptanceReceipt, result.ReviewCommit)
	}
}

// D-38: a submit whose claim is released mid-validation must stop its canonical
// command instead of running it to completion for a publication it cannot make.
// Supervisor exit also removes the registration, so the caller is told to stop.
func TestSubmitForReviewStopsCanonicalValidationWhenClaimIsLost(t *testing.T) {
	err := runClaimLossDuringValidation(t, releaseClaimAsSupervisorExit)

	requireFencedSubmit(t, err, models.LifecycleStaleCaller, "stop")
}

// A claim that moves while the submitting agent stays registered is a state
// change the caller can requery.
func TestSubmitForReviewStopsCanonicalValidationWhenClaimMovesToAnotherAgent(t *testing.T) {
	err := runClaimLossDuringValidation(t, func(t *testing.T, scenario testhelpers.AcceptanceSubmitScenario) {
		t.Helper()
		other := "coder-2"
		if err := scenario.BB.Modify(func(state *models.State) error {
			state.Agents[other] = testhelpers.RegisteredTestAgent(models.RoleCoder)
			state.FindTask(scenario.TaskID).AssignedTo = &other
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	requireFencedSubmit(t, err, models.LifecycleStateChanged, "requery")
}

// runClaimLossDuringValidation starts a submit whose quiet canonical command
// would run 60 s, applies release once the command runs, and returns the
// submit's error. It fails unless the submit returns within 10 s with the
// command's whole process group gone and the task unpublished.
func runClaimLossDuringValidation(t *testing.T, release func(*testing.T, testhelpers.AcceptanceSubmitScenario)) error {
	t.Helper()
	// GIVEN a canonical command that records its PID and stays quiet
	pidFile := filepath.Join(t.TempDir(), "validation.pid")
	script := fmt.Sprintf("echo $$ > '%s'\nexec sleep 60\n", pidFile)
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, script, 120)
	previous := acceptanceOwnershipPollInterval
	acceptanceOwnershipPollInterval = 100 * time.Millisecond
	t.Cleanup(func() { acceptanceOwnershipPollInterval = previous })

	done := make(chan error, 1)
	go func() {
		_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
		done <- err
	}()
	pid := waitForValidationPID(t, pidFile, 30*time.Second)
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
		}
	})

	// WHEN the claim is lost while the command runs
	release(t, scenario)
	released := time.Now()

	// THEN submit returns promptly, the command's group is gone, nothing published
	select {
	case err = <-done:
		finished = true
	case <-time.After(10 * time.Second):
		t.Fatalf("submit still validating %s after the claim was lost", time.Since(released).Round(time.Second))
	}
	if syscall.Kill(-pgid, 0) == nil {
		t.Fatalf("canonical command process group %d still running after submit returned", pgid)
	}
	task := readAcceptanceState(t, scenario.BB).FindTask(scenario.TaskID)
	if task.AcceptanceReceipt != nil || task.Status == models.TaskStatusReadyForReview {
		t.Fatalf("task was published after losing its claim: status=%s receipt=%v", task.Status, task.AcceptanceReceipt)
	}
	return err
}

func requireFencedSubmit(t *testing.T, err error, outcome, safeAction string) {
	t.Helper()
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != outcome || lifecycleErr.Outcome.SafeAction != safeAction {
		t.Fatalf("submit error = %T %v, want %s (safe_action=%s)", err, err, outcome, safeAction)
	}
	if !strings.Contains(err.Error(), "ownership lost during canonical validation") {
		t.Fatalf("submit error = %v, want it to name the fenced validation", err)
	}
}

// releaseClaimAsSupervisorExit mirrors the agent package's unregister path,
// the one D-38 observed: a direct state write that releases the doer claim and
// removes the agent. release-claim cannot be used here, because submit holds
// the task's ownership lock for the whole canonical execution.
func releaseClaimAsSupervisorExit(t *testing.T, scenario testhelpers.AcceptanceSubmitScenario) {
	t.Helper()
	resolver, _, err := loadResolver(scenario.Root)
	if err != nil {
		t.Fatal(err)
	}
	transitions := BuildPipelineTransitions(resolver)
	if err := scenario.BB.Modify(func(state *models.State) error {
		task := state.FindTask(scenario.TaskID)
		_, released := ResolveDoerReleaseStatus(task, resolver)
		if err := task.TransitionWith(released, transitions); err != nil {
			return err
		}
		reason := "agent interrupted"
		task.AssignedTo = nil
		task.LeaseExpires = nil
		models.AdvanceLifecycle(task)
		state.ReleaseAgent(scenario.AgentID)
		delete(state.Agents, scenario.AgentID)
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimReleased, Agent: &scenario.AgentID, Reason: &reason})
		return nil
	}); err != nil {
		t.Fatalf("release claim as supervisor exit: %v", err)
	}
}

func waitForValidationPID(t *testing.T, pidFile string, timeout time.Duration) int {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	expired := time.After(timeout)
	for {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		select {
		case <-ticker.C:
		case <-expired:
			t.Fatalf("canonical command never started (no pid in %s)", pidFile)
			return 0
		}
	}
}
