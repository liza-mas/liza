package agent

import (
	"errors"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func trackedRefusal(taskID string, class ops.AcceptanceFault, digest, commit string) *ops.AcceptanceEvidenceError {
	return &ops.AcceptanceEvidenceError{
		TaskID: taskID, Field: "acceptance.source", Reason: "refused", Class: class,
		Claim: &ops.AcceptanceClaimObservation{AllocationRef: "specs/plan.md#Task 1", IntegrationCommit: commit, Digest: digest},
	}
}

func TestAcceptanceRefusalTracker_EscalationPolicy(t *testing.T) {
	t.Run("content escalates at once", func(t *testing.T) {
		if !newAcceptanceRefusalTracker().observe(trackedRefusal("task-a", ops.AcceptanceFaultContent, "d1", "c1")) {
			t.Fatal("content refusal did not escalate on first observation")
		}
	})

	t.Run("read failures never escalate", func(t *testing.T) {
		tracker := newAcceptanceRefusalTracker()
		for attempt := 1; attempt <= 2*ops.AcceptanceAllocationRefusalThreshold; attempt++ {
			if tracker.observe(trackedRefusal("task-a", "", "d1", "c1")) {
				t.Fatalf("unclassified refusal escalated on attempt %d", attempt)
			}
		}
	})

	t.Run("allocation escalates on the threshold of identical observations", func(t *testing.T) {
		tracker := newAcceptanceRefusalTracker()
		for attempt := 1; attempt < ops.AcceptanceAllocationRefusalThreshold; attempt++ {
			if tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1")) {
				t.Fatalf("allocation refusal escalated early on attempt %d", attempt)
			}
		}
		if !tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1")) {
			t.Fatal("allocation refusal did not escalate at the threshold")
		}
	})

	for _, tc := range []struct {
		name  string
		reset *ops.AcceptanceEvidenceError
	}{
		{"a changed observation restarts the count", trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d2", "c1")},
		{"moved integration restarts the count", trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c2")},
		{"an interleaved read failure restarts the count", trackedRefusal("task-a", "", "d1", "c1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := newAcceptanceRefusalTracker()
			for attempt := 1; attempt < ops.AcceptanceAllocationRefusalThreshold; attempt++ {
				tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1"))
			}
			tracker.observe(tc.reset)
			if tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1")) {
				t.Fatal("escalated although the identical refusals were not consecutive")
			}
		})
	}

	t.Run("an interleaved non-acceptance claim failure restarts the count", func(t *testing.T) {
		tracker := newAcceptanceRefusalTracker()
		for attempt := 1; attempt < ops.AcceptanceAllocationRefusalThreshold; attempt++ {
			tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1"))
		}
		if err := escalateAcceptanceRefusal("", models.AgentAuthority{ID: "coder-1"}, tracker, "task-a", errors.New("task changed before claim preparation")); err != nil {
			t.Fatalf("escalateAcceptanceRefusal() = %v", err)
		}
		if tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1")) {
			t.Fatal("escalated although a non-acceptance failure interrupted the identical refusals")
		}
	})

	t.Run("counts are per task", func(t *testing.T) {
		tracker := newAcceptanceRefusalTracker()
		for attempt := 1; attempt < ops.AcceptanceAllocationRefusalThreshold; attempt++ {
			tracker.observe(trackedRefusal("task-a", ops.AcceptanceFaultAllocation, "d1", "c1"))
		}
		if tracker.observe(trackedRefusal("task-b", ops.AcceptanceFaultAllocation, "d1", "c1")) {
			t.Fatal("another task's refusals counted toward this one")
		}
	})
}
