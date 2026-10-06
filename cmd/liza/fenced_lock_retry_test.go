package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestFencedLockRetryCLIWaitsAndCompletesOriginalRequestOnce(t *testing.T) {
	// Keep a real acquisition deadline, shortened only for this sequential CLI
	// test. The holder outlasts it; retry must happen inside the invocation.
	t.Cleanup(db.SetDefaultLockTimeoutForTest(50 * time.Millisecond))
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	})
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	before := readState(t, statePath).FindTask("target")
	original := models.TaskTransitionID(before)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	priorContext := assessBlockedCmd.Context()
	assessBlockedCmd.SetContext(ctx)
	t.Cleanup(func() { assessBlockedCmd.SetContext(priorContext) })
	release := testhelpers.HoldFileLock(t, statePath)
	var observedTimeout atomic.Bool
	t.Cleanup(filelock.SetAcquisitionTimeoutHookForTest(statePath, func() {
		observedTimeout.Store(true)
		release()
	}))
	defer release()

	stdout, err := executeRootCommandCapture(t, root, "assess-blocked", "target", "--note", "inspected",
		"--agent-id", "orchestrator-1", "--request-id", "fenced-retry", "--expected-transition", original, "--json")
	result := parseEnvelope(t, stdout)["result"].(map[string]any)
	if err != nil {
		t.Fatalf("fenced request returned before recovery: %v (outcome=%v effects=%v)", err, result["outcome"], result["effects"])
	}
	if !observedTimeout.Load() {
		t.Fatal("holder released before an actual acquisition timeout")
	}
	if result["outcome"] != models.LifecycleCompleted || result["request_id"] != "fenced-retry" {
		t.Fatalf("request did not complete with its original identity: outcome=%v request=%v", result["outcome"], result["request_id"])
	}
	after := readState(t, statePath).FindTask("target")
	if len(after.History) != len(before.History)+1 || after.Lifecycle == nil || len(after.Lifecycle.Receipts) != 1 {
		t.Fatal("one request did not produce exactly one assessment and receipt")
	}
	receipt := after.Lifecycle.Receipts[0]
	if receipt.RequestID != "fenced-retry" || receipt.ExpectedTransition != original {
		t.Fatal("contention changed the retained request identity or expected boundary")
	}
}

func TestFencedLockRetryCLICancellationLeavesStateUnchanged(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(50 * time.Millisecond))
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	})
	before := readStateBytes(t, statePath)
	original := models.TaskTransitionID(readState(t, statePath).FindTask("target"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	priorContext := assessBlockedCmd.Context()
	assessBlockedCmd.SetContext(ctx)
	t.Cleanup(func() { assessBlockedCmd.SetContext(priorContext) })
	testhelpers.HoldFileLock(t, statePath)
	// Cancellation happens after an actual failed acquisition, while retry is
	// waiting; it must retain the original error identity and publish nothing.
	t.Cleanup(filelock.SetAcquisitionTimeoutHookForTest(statePath, cancel))
	err := executeRootCommand(t, root, "assess-blocked", "target", "--note", "inspected",
		"--agent-id", "orchestrator-1", "--request-id", "cancelled-retry", "--expected-transition", original)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fenced acquisition did not preserve cancellation: %v", err)
	}
	if readStateBytes(t, statePath) != before {
		t.Fatal("canceled acquisition changed state")
	}
}

func TestFencedLockRetryWithoutRequestPairStaysBounded(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(50 * time.Millisecond))
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	})
	before := readStateBytes(t, statePath)
	testhelpers.HoldFileLock(t, statePath)
	err := executeRootCommand(t, root, "assess-blocked", "target", "--note", "inspected", "--agent-id", "orchestrator-1")
	var lifecycle *ops.LifecycleError
	if !errors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleRetryable || lifecycle.Outcome.Effects != "none" {
		t.Fatalf("unfenced invocation lost bounded no-effect timeout policy: %v", err)
	}
	if readStateBytes(t, statePath) != before {
		t.Fatal("timed-out acquisition changed state")
	}
}
