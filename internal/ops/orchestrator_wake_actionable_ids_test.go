package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The BLOCKED_TASKS wake names exactly the tasks its count selects (D-66).
func TestActionableBlockedTaskIDsMatchesWakeSelection(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	unassessed := testhelpers.BuildTaskByStatus("unassessed", models.TaskStatusBlocked, now.Add(-time.Hour))
	held := testhelpers.BuildTaskByStatus("held", models.TaskStatusBlocked, now.Add(-time.Hour))
	ready := testhelpers.BuildTaskByStatus("ready", models.TaskStatusReady, now.Add(-time.Hour))
	pendingHold := testhelpers.BuildTaskByStatus("pending-hold", models.TaskStatusBlocked, now.Add(-time.Hour))
	refusedHold := testhelpers.BuildTaskByStatus("refused-hold", models.TaskStatusBlocked, now.Add(-time.Hour))
	publishedHold := testhelpers.BuildTaskByStatus("published-hold", models.TaskStatusBlocked, now.Add(-time.Hour))
	state.Tasks = []models.Task{unassessed, held, ready, pendingHold, refusedHold, publishedHold}

	heldTask := state.FindTask("held")
	heldTask.History = append(heldTask.History, models.TaskHistoryEntry{
		Time: now, Event: models.TaskEventOrchestratorAssessment,
		Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(state, heldTask, currentBlockerCandidate(state, heldTask))},
	})
	for _, id := range []string{"pending-hold", "refused-hold", "published-hold"} {
		installStateLockHold(state, state.FindTask(id))
	}
	// Holds become eligible only after a later mutation publication.
	state.MutationSequence += 2
	pending := state.FindTask("pending-hold")
	pending.StateLockHold.AfterSequence = state.MutationSequence + 1
	refused := state.FindTask("refused-hold")
	refused.StateLockHold.RefusedReason = "unblock_domain_refused"
	refused.StateLockHold.RefusedFingerprint = stateLockHoldMaterial(state, refused, refused.StateLockHold.RefusedReason)

	ids := ActionableBlockedTaskIDs(state)
	if got := CountActionableBlockedTasks(state); got != len(ids) {
		t.Fatalf("count %d disagrees with named set %v", got, ids)
	}
	for _, id := range []string{"unassessed", "published-hold"} {
		if !slices.Contains(ids, id) {
			t.Errorf("%s must be named: %v", id, ids)
		}
	}
	for _, id := range []string{"held", "ready", "pending-hold", "refused-hold"} {
		if slices.Contains(ids, id) {
			t.Errorf("%s must not be named: %v", id, ids)
		}
	}
	if slices.Index(ids, "unassessed") != 0 {
		t.Errorf("named set must keep state order: %v", ids)
	}
}
