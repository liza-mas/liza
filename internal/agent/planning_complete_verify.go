package agent

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// verifyPlanningCompleteTurn checks a PLANNING_COMPLETE turn against the plans
// eligible when it woke, each judged by its wake-time hand-off class:
//   - needs_review must now carry a disposition or be replanned;
//   - needs_reconciliation must now be replanned or held;
//   - a plan passed (before or during the turn) or out of the reviewed domain
//     needs a checkpoint made during the turn, unless its hand-off already ran.
//
// Plans merged during the turn are not judged; they get their own wake. A
// failure leads to the self-heal checkpoint, which cannot expand an
// undispositioned plan because automatic transitions require a pass.
func verifyPlanningCompleteTurn(projectRoot string, before, after *models.State) error {
	domain, err := ops.LoadPlanHandoffDomain(projectRoot)
	if err != nil {
		return fmt.Errorf("load plan hand-off domain: %w", err)
	}

	var undecided []string
	needsCheckpoint := false
	for _, id := range before.Sprint.Scope.Planned {
		task := before.FindTask(id)
		if !domain.PlanningCompleteEligible(before, task) {
			continue
		}
		class, _ := domain.Classify(before, task)
		current := after.FindTask(id)
		replanned := current != nil && current.TransitionsExecuted["replanned"]
		transitioned := current != nil && !replanned && !domain.Pending(current)
		verdict := current.PlanCheckVerdictOf()
		switch class {
		case ops.PlanHandoffNeedsReview:
			switch {
			case replanned || transitioned || verdict == models.PlanCheckHeld:
			case verdict == models.PlanCheckPassed:
				needsCheckpoint = true
			default:
				undecided = append(undecided, id+" (no disposition)")
			}
		case ops.PlanHandoffNeedsReconciliation:
			if !replanned && verdict != models.PlanCheckHeld {
				undecided = append(undecided, id+" (upstream changed; replan or hold it)")
			}
		case ops.PlanHandoffPassed, ops.PlanHandoffOutOfDomain:
			if !replanned && !transitioned {
				needsCheckpoint = true
			}
		}
	}
	if len(undecided) > 0 {
		return fmt.Errorf("orchestrator completed with PLANNING_COMPLETE trigger but left plans undecided: %s", strings.Join(undecided, ", "))
	}
	if needsCheckpoint && !checkpointedDuringTurn(before, after) {
		return fmt.Errorf("orchestrator completed with PLANNING_COMPLETE trigger and admissible plans but no checkpoint was made (sprint status %s)", after.Sprint.Status)
	}
	return nil
}

// checkpointedDuringTurn reports a checkpoint made during the turn. Auto-resume
// may already have moved the sprint back to IN_PROGRESS by the time the turn
// is verified; checkpoint_at survives the resume.
func checkpointedDuringTurn(before, after *models.State) bool {
	at := after.Sprint.Timeline.CheckpointAt
	if at == nil {
		return false
	}
	previous := before.Sprint.Timeline.CheckpointAt
	if previous == nil || at.After(*previous) {
		return true
	}
	return after.Sprint.Status == models.SprintStatusCheckpoint || after.Sprint.Status == models.SprintStatusCompleted
}
