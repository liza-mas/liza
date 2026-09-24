package agent

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D69: supervisor-path state reads must outlast a transient state-lock hold
// longer than the ordinary lock timeout. These tests shorten the ordinary
// timeout, so they must not run in parallel.
const (
	patienceTestLockTimeout = 200 * time.Millisecond
	patienceTestLockHold    = 600 * time.Millisecond
)

// holdStateLockTransiently holds the state lock for patienceTestLockHold,
// starting before it returns.
func holdStateLockTransiently(t *testing.T, statePath string) {
	t.Helper()
	release := testhelpers.HoldFileLock(t, statePath)
	time.AfterFunc(patienceTestLockHold, release)
}

func TestAutoAssignAgentIDOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	holdStateLockTransiently(t, statePath)

	assignedID, err := AutoAssignAgentID(bb, "coder", 1, func(string) error { return nil })
	if err != nil {
		t.Fatalf("AutoAssignAgentID under a transient state-lock hold: %v", err)
	}
	if assignedID != "coder-1" {
		t.Fatalf("assigned %q, want coder-1", assignedID)
	}
}

func TestProviderLaunchGateOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	fixture := newProviderGenerationFixture(t)
	holdStateLockTransiently(t, fixture.statePath)

	started := false
	err := newProviderLaunchGate(fixture.config(fixture.authorityA))(context.Background(), func() error {
		started = true
		return nil
	})
	if err != nil || !started {
		t.Fatalf("provider launch under a transient state-lock hold: err=%v started=%v", err, started)
	}
}

func TestReviewExecutionWatchdogStartOutlastsStateLockTimeout(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	config, _, _ := reviewExecutionFixture(t)
	holdStateLockTransiently(t, config.StatePath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := startReviewExecutionWatchdog(ctx, config, "review-task", time.Hour, cancel)
	if err != nil {
		t.Fatalf("review watchdog start under a transient state-lock hold: %v", err)
	}
	if stop() {
		t.Fatal("review ownership reported lost")
	}
}

// D75: a supervisor usually exits because a state-lock wait timed out, so its
// exit-time claim release must outlast the same saturation.

func claimedTaskFixture(taskID string, status models.TaskStatus, now time.Time) models.Task {
	return models.Task{
		ID:          taskID,
		Description: "claimed when its supervisor exits",
		Status:      status,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "Test",
		RolePair:    "coding-pair",
	}
}

func TestUnregisterAgentReleasesDoerClaimUnderTransientLockHold(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a coder working on an owned executing task
	now := time.Now().UTC()
	lease := now.Add(30 * time.Minute)
	agentID, taskID := "coder-1", "task-1"
	state := testhelpers.CreateValidState()
	task := claimedTaskFixture(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = testhelpers.StringPtr(agentID)
	task.LeaseExpires = &lease
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role: "coder", Status: models.AgentStatusWorking, CurrentTask: &taskID,
		Heartbeat: now, LeaseExpires: &lease,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	authority := testAgentAuthority(t, bb, agentID)

	// WHEN it unregisters while the state lock is held past the lock timeout
	holdStateLockTransiently(t, statePath)
	if err := unregisterAgent(bb, authority, tmpDir); err != nil {
		t.Fatalf("unregisterAgent under a transient state-lock hold: %v", err)
	}

	// THEN the claim is released and the agent row is gone
	got, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Agents[agentID]; exists {
		t.Fatalf("agent %s still registered", agentID)
	}
	released := got.FindTask(taskID)
	if released.Status != models.TaskStatusReady {
		t.Fatalf("task status %s, want %s", released.Status, models.TaskStatusReady)
	}
	if released.AssignedTo != nil || released.LeaseExpires != nil {
		t.Fatalf("doer claim still present: assigned=%v lease=%v", released.AssignedTo, released.LeaseExpires)
	}
	last := released.History[len(released.History)-1]
	if last.Event != models.TaskEventClaimReleased || last.Agent == nil || *last.Agent != agentID {
		t.Fatalf("last history entry %+v, want claim release by %s", last, agentID)
	}
}

func TestUnregisterAgentReleasesReviewerClaimUnderTransientLockHold(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a code reviewer actively reviewing a task
	now := time.Now().UTC()
	lease := now.Add(30 * time.Minute)
	agentID, taskID := "code-reviewer-1", "task-1"
	state := testhelpers.CreateValidState()
	task := claimedTaskFixture(taskID, models.TaskStatusReviewing, now)
	task.ReviewingBy = testhelpers.StringPtr(agentID)
	task.ReviewLeaseExpires = &lease
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role: "code-reviewer", Status: models.AgentStatusReviewing, CurrentTask: &taskID,
		Heartbeat: now, LeaseExpires: &lease,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	authority := testAgentAuthority(t, bb, agentID)

	// WHEN it unregisters while the state lock is held past the lock timeout
	holdStateLockTransiently(t, statePath)
	if err := unregisterAgent(bb, authority, tmpDir); err != nil {
		t.Fatalf("unregisterAgent under a transient state-lock hold: %v", err)
	}

	// THEN the review claim is released and the agent row is gone
	got, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Agents[agentID]; exists {
		t.Fatalf("agent %s still registered", agentID)
	}
	released := got.FindTask(taskID)
	if released.Status != models.TaskStatusReadyForReview {
		t.Fatalf("task status %s, want %s", released.Status, models.TaskStatusReadyForReview)
	}
	if released.ReviewingBy != nil || released.ReviewLeaseExpires != nil {
		t.Fatalf("review claim still present: reviewing_by=%v lease=%v", released.ReviewingBy, released.ReviewLeaseExpires)
	}
}

func TestUnregisterAgentRejectsReplacementRegisteredDuringLockWait(t *testing.T) {
	t.Cleanup(db.SetDefaultLockTimeoutForTest(patienceTestLockTimeout))
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// GIVEN a coder whose registration is replaced while its release waits for the lock
	now := time.Now().UTC()
	lease := now.Add(30 * time.Minute)
	agentID, taskID := "coder-1", "task-1"
	state := testhelpers.CreateValidState()
	task := claimedTaskFixture(taskID, models.TaskStatusImplementing, now)
	task.AssignedTo = testhelpers.StringPtr(agentID)
	task.LeaseExpires = &lease
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = models.Agent{
		Role: "coder", Status: models.AgentStatusWorking, CurrentTask: &taskID,
		Heartbeat: now, LeaseExpires: &lease,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	stale := testAgentAuthority(t, bb, agentID)
	original, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	oldGeneration := []byte("generation: " + stale.Generation)
	if !bytes.Contains(original, oldGeneration) {
		t.Fatalf("state file does not record %q", oldGeneration)
	}
	replaced := bytes.Replace(original, oldGeneration, []byte("generation: replacement-generation"), 1)

	// WHEN the stale supervisor starts unregistering against its own
	// registration while another holder owns the lock, and that holder
	// publishes a replacement registration before unlocking
	release := testhelpers.HoldFileLock(t, statePath)
	done := make(chan error, 1)
	go func() { done <- unregisterAgent(bb, stale, tmpDir) }()
	time.AfterFunc(patienceTestLockHold, release)
	// No hook reports that unregister is blocked on the lock, so publish
	// inside the ordinary lock-timeout window and prove it is still waiting.
	select {
	case err := <-done:
		t.Fatalf("unregisterAgent returned before the replacement was published: %v", err)
	case <-time.After(patienceTestLockTimeout / 2):
	}
	if err := os.WriteFile(statePath, replaced, 0o644); err != nil {
		t.Fatal(err)
	}
	err = <-done

	// THEN the generation fence rejects it after the wait and state is untouched
	if !ops.IsAgentAuthorityError(err) {
		t.Fatalf("unregisterAgent with a replaced registration: err=%v, want agent authority error", err)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, replaced) {
		t.Fatal("state changed after a rejected stale unregister")
	}
}
