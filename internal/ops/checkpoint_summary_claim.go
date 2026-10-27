package ops

import (
	"sort"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// checkpointSummaryScopeLimit bounds the tasks one automatic summary covers.
// A one-shot summary that reads everything the run produced outgrows any
// fixed bound (#171).
const checkpointSummaryScopeLimit = 25

// CheckpointSummaryScope is the slice of the run one automatic summary covers:
// the tasks merged after Since and up to the checkpoint, newest first.
type CheckpointSummaryScope struct {
	Since   time.Time
	TaskIDs []string
	// Omitted counts the tasks merged in the window beyond the limit.
	Omitted int
}

// CheckpointSummaryClaim is the obligation a caller claimed: the checkpoint's
// own trigger (possibly empty) and the scope its summary covers.
type CheckpointSummaryClaim struct {
	Trigger string
	Scope   CheckpointSummaryScope
}

// ClaimCheckpointSummary claims the pending checkpoint-summary obligation: it
// builds the scope from the locked state and clears the obligation in the same
// transaction, so one observer claims it. It returns nil when nothing is
// pending. The claim is unfenced; the in-flight slot and the background
// emission stay with the caller.
func ClaimCheckpointSummary(bb *db.Blackboard) (*CheckpointSummaryClaim, error) {
	var claim *CheckpointSummaryClaim
	err := bb.Modify(func(s *models.State) error {
		claim = nil
		// Re-read under the lock: another observation may have claimed it.
		if s.PendingCheckpointSummary == nil {
			return nil
		}
		pending := s.PendingCheckpointSummary
		claim = &CheckpointSummaryClaim{Trigger: pending.Trigger, Scope: buildCheckpointSummaryScope(s, pending)}
		s.PendingCheckpointSummary = nil
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// buildCheckpointSummaryScope selects the tasks whose latest merge falls after
// the previous checkpoint and no later than this one, newest first. Without a
// previous checkpoint the window starts at the sprint start.
func buildCheckpointSummaryScope(state *models.State, pending *models.PendingCheckpointSummary) CheckpointSummaryScope {
	scope := CheckpointSummaryScope{Since: state.Sprint.Timeline.Started}
	if pending.Since != nil {
		scope.Since = *pending.Since
	}
	type merge struct {
		id string
		at time.Time
	}
	var merges []merge
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for j := len(task.History) - 1; j >= 0; j-- {
			entry := task.History[j]
			if entry.Event != models.TaskEventMerged {
				continue
			}
			if entry.Time.After(scope.Since) && !entry.Time.After(pending.At) {
				merges = append(merges, merge{id: task.ID, at: entry.Time})
			}
			break
		}
	}
	sort.SliceStable(merges, func(a, b int) bool { return merges[a].at.After(merges[b].at) })
	for i, m := range merges {
		if i == checkpointSummaryScopeLimit {
			scope.Omitted = len(merges) - i
			break
		}
		scope.TaskIDs = append(scope.TaskIDs, m.id)
	}
	return scope
}
