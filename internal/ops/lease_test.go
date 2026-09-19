package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// `assigned_to` and `lease_expires` are valid only together, so renewing a
// lease on an unassigned task would publish a state that global validation
// rejects from then on.
func TestRenewLeaseKeepsLeaseAndAssignmentTogether(t *testing.T) {
	t.Parallel()
	state := &models.State{Config: models.Config{LeaseDuration: 60}}
	owner := "coder-1"
	stale := time.Now().UTC().Add(-time.Hour)

	assigned := &models.Task{ID: "task-1", AssignedTo: &owner}
	renewLease(state, assigned)
	if assigned.LeaseExpires == nil || !assigned.LeaseExpires.After(time.Now().UTC()) {
		t.Fatalf("assigned task did not get a fresh lease: %v", assigned.LeaseExpires)
	}

	released := &models.Task{ID: "task-2"}
	renewLease(state, released)
	if released.LeaseExpires != nil {
		t.Fatalf("unassigned task was given a lease: %v", released.LeaseExpires)
	}

	dangling := &models.Task{ID: "task-3", LeaseExpires: &stale}
	renewLease(state, dangling)
	if dangling.LeaseExpires != nil {
		t.Fatalf("dangling lease was refreshed instead of cleared: %v", dangling.LeaseExpires)
	}

	unconfigured := &models.Task{ID: "task-4", AssignedTo: &owner}
	renewLease(&models.State{}, unconfigured)
	if unconfigured.LeaseExpires == nil ||
		!unconfigured.LeaseExpires.After(time.Now().UTC().Add(time.Duration(models.DefaultLeaseDurationSeconds-5)*time.Second)) {
		t.Fatalf("unconfigured duration did not fall back to the default: %v", unconfigured.LeaseExpires)
	}
}

// A doer can exit between submitting and the verdict landing — a provider quota
// kill did exactly that in a live run. The rejection must not leave the task
// carrying a lease nobody holds.
func TestSubmitVerdictRejectionOnReleasedTaskLeavesNoDanglingLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	task.AssignedTo = nil
	task.LeaseExpires = nil
	state.Tasks = []models.Task{task}
	state.Agents["code-reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusWorking}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := SubmitVerdict(tmpDir, "task-1", "REJECTED", "Missing error handling", "code-reviewer-1", ""); err != nil {
		t.Fatalf("SubmitVerdict() error: %v", err)
	}

	bb := db.New(stateFile)
	after, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	rejected := after.FindTask("task-1")
	if rejected.AssignedTo != nil {
		t.Fatalf("rejection assigned a doer to a released task: %q", *rejected.AssignedTo)
	}
	if rejected.LeaseExpires != nil {
		t.Fatalf("rejection left lease_expires without assigned_to: %v", rejected.LeaseExpires)
	}
}
