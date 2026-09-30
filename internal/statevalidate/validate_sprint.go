package statevalidate

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
)

// validateSprint checks sprint-level invariants: valid status, non-negative
// number, goal_ref matching the active goal, referential integrity of planned
// and stretch scope entries, and a non-zero timeline start. Prevents the sprint
// from referencing tasks or goals that do not exist and catches misconfigured
// sprint metadata.
func validateSprint(v *violations, state *models.State) {
	// Sprint status must be valid
	if !state.Sprint.Status.IsValid() {
		v.add(fmt.Errorf("unknown sprint status '%s'", state.Sprint.Status))
	}

	// Sprint number must be >= 1 (0 is tolerated for legacy pre-multi-sprint state)
	if state.Sprint.Number < 0 {
		v.add(fmt.Errorf("sprint.number must be non-negative (got %d)", state.Sprint.Number))
	}

	// Sprint goal_ref must match goal.id
	if state.Sprint.GoalRef != state.Goal.ID {
		v.add(fmt.Errorf("sprint.goal_ref (%s) does not match goal.id (%s)", state.Sprint.GoalRef, state.Goal.ID))
	}

	taskIDs := buildTaskIDSet(state.Tasks)

	// Sprint scope.planned tasks must exist
	for _, taskID := range state.Sprint.Scope.Planned {
		if !taskIDs[taskID] {
			v.add(fmt.Errorf("sprint.scope.planned references non-existent task '%s'", taskID))
		}
	}

	// Sprint scope.stretch tasks must exist
	for _, taskID := range state.Sprint.Scope.Stretch {
		if !taskIDs[taskID] {
			v.add(fmt.Errorf("sprint.scope.stretch references non-existent task '%s'", taskID))
		}
	}

	// Sprint timeline.started must be set
	if state.Sprint.Timeline.Started.IsZero() {
		v.add(fmt.Errorf("sprint.timeline.started is required"))
	}

	validateSprintHistory(v, state)
}

// validateSprintHistory checks that sprint history entries have unique IDs,
// positive numbers (>= 1), valid statuses, and non-zero start/end times.
// Prevents duplicate or malformed historical records that would corrupt
// sprint metrics and progress tracking.
func validateSprintHistory(v *violations, state *models.State) {
	seenIDs := make(map[string]bool)
	for i, summary := range state.SprintHistory {
		if summary.ID == "" {
			v.add(fmt.Errorf("sprint_history[%d]: missing id", i))
		} else if seenIDs[summary.ID] {
			v.add(fmt.Errorf("sprint_history: duplicate sprint id '%s'", summary.ID))
		}
		seenIDs[summary.ID] = true

		if summary.Number < 1 {
			v.add(fmt.Errorf("sprint_history[%d]: number must be >= 1 (got %d)", i, summary.Number))
		}
		if !summary.Status.IsValid() {
			v.add(fmt.Errorf("sprint_history[%d]: invalid status '%s'", i, summary.Status))
		}
		if summary.Started.IsZero() {
			v.add(fmt.Errorf("sprint_history[%d]: missing started time", i))
		}
		if summary.Ended.IsZero() {
			v.add(fmt.Errorf("sprint_history[%d]: missing ended time", i))
		}
	}
}
