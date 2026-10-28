package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// ClearResumedTransitionCheckpoint clears the sprint's checkpoint trigger in
// the generation-fenced transaction when the locked sprint is still the resumed
// transition checkpoint the caller observed before its turn (see
// SameResumedTransitionCheckpoint). Any other sprint keeps its trigger.
func ClearResumedTransitionCheckpoint(bb *db.Blackboard, authority models.AgentAuthority, observed models.Sprint) error {
	return ModifyWithAgentAuthority(bb, authority, func(s *models.State) error {
		if SameResumedTransitionCheckpoint(s.Sprint, observed) {
			s.Sprint.CheckpointTrigger = ""
		}
		return nil
	})
}

// ConsumeRenderedHumanNotes stamps the operator notes a completed orchestrator
// turn consumed as seen, and returns how many it stamped. before is the state
// the turn's prompt was built from and rendered the count of its notes, so
// notes added during the turn stay unseen. allRendered is true for a
// HUMAN_NOTE turn, which consumed every rendered note; any other turn consumed
// only the notes aimed at a task it assessed, plus the notes for all targets
// when it assessed any task. The assessment comparison runs against the locked
// state.
func ConsumeRenderedHumanNotes(bb *db.Blackboard, authority models.AgentAuthority, before *models.State, rendered int, allRendered bool) (int, error) {
	stamped := 0
	err := ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		consumed := func(*models.HumanNote) bool { return true }
		if !allRendered {
			assessed := TasksAssessedBetween(before, state)
			consumed = func(note *models.HumanNote) bool {
				return assessed[note.For] || (note.For == humanNoteForAll && len(assessed) > 0)
			}
		}
		stamped = MarkHumanNotesSeen(state, time.Now().UTC(), rendered, consumed)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return stamped, nil
}

// UndecidedPlanRecord is one recorded undecided-plan observation: the task as
// committed and the observation to log and alert on after the transaction.
type UndecidedPlanRecord struct {
	Task        models.Task
	Observation PlanHandoffUndecided
}

// RecordUndecidedPlans persists, in the generation-fenced transaction, the
// observation of each plan the turn left undecided, judged against the state
// the turn started from. It returns the recorded plans; the caller logs and
// alerts after the commit. now is read by the caller before the transaction.
func RecordUndecidedPlans(bb *db.Blackboard, authority models.AgentAuthority, domain PlanHandoffDomain, before *models.State, planIDs []string, now time.Time) ([]UndecidedPlanRecord, error) {
	var records []UndecidedPlanRecord
	err := ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		records = nil
		for _, id := range planIDs {
			if observation := domain.RecordUndecided(before, state, id, now); observation != nil {
				records = append(records, UndecidedPlanRecord{Task: *state.FindTask(id), Observation: *observation})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}
