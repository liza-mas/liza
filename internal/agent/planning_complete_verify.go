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
// Plans merged during the turn are not judged; they get their own wake.
// Undecided plans are reported as *undecidedPlansError so the caller can record
// them; a missing checkpoint leads to the self-heal checkpoint, which cannot
// expand an undispositioned plan because automatic transitions require a pass.
func verifyPlanningCompleteTurn(projectRoot string, before, after *models.State) error {
	domain, err := ops.LoadPlanHandoffDomain(projectRoot)
	if err != nil {
		return fmt.Errorf("load plan hand-off domain: %w", err)
	}

	var undecided, undecidedIDs []string
	needsCheckpoint := false
	for _, id := range before.Sprint.Scope.Planned {
		task := before.FindTask(id)
		if !domain.PlanningCompleteEligible(before, task) {
			continue
		}
		class, _ := domain.Classify(before, task)
		current := after.FindTask(id)
		if current != nil && current.PlanAmendment != nil && current.PlanAmendment.Pending != "" {
			if class == ops.PlanHandoffAmendmentReady && current.PlanAmendment.Pending == task.PlanAmendment.Pending {
				undecided = append(undecided, id+" (terminal reviewed amendment needs apply or pending replacement)")
				undecidedIDs = append(undecidedIDs, id)
			}
			continue
		}
		if class == ops.PlanHandoffAmendmentReady {
			class = ops.PlanHandoffNeedsReview
		}
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
				undecidedIDs = append(undecidedIDs, id)
			}
		case ops.PlanHandoffNeedsReconciliation:
			if !replanned && verdict != models.PlanCheckHeld {
				undecided = append(undecided, id+" (upstream changed; replan or hold it)")
				undecidedIDs = append(undecidedIDs, id)
			}
		case ops.PlanHandoffPassed, ops.PlanHandoffOutOfDomain:
			if !replanned && !transitioned {
				needsCheckpoint = true
			}
		}
	}
	checkpointMissing := needsCheckpoint && !checkpointedDuringTurn(before, after)
	if len(undecided) > 0 {
		return &undecidedPlansError{
			message:           "orchestrator completed with PLANNING_COMPLETE trigger but left plans undecided: " + strings.Join(undecided, ", "),
			Plans:             undecidedIDs,
			CheckpointMissing: checkpointMissing,
		}
	}
	if checkpointMissing {
		return fmt.Errorf("orchestrator completed with PLANNING_COMPLETE trigger and admissible plans but no checkpoint was made (sprint status %s)", after.Sprint.Status)
	}
	return nil
}

// undecidedPlansError reports the wake-time plans a PLANNING_COMPLETE turn
// left undecided, and whether it also left admissible plans uncheckpointed.
type undecidedPlansError struct {
	message           string
	Plans             []string
	CheckpointMissing bool
}

func (e *undecidedPlansError) Error() string { return e.message }

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
