package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

// sentinelCachePrefix keys the first sighting of a transition sentinel.
const sentinelCachePrefix = "sentinel:"

// AwaitingHumanNotice names the remedy when the run is parked until a human
// acts, or returns "" when it is not. Agents keep their registrations while
// parked, so without this the run reads as staffed but idle.
//
// Under auto_resume a checkpoint needs no human unless it outlives
// autoResumeCheckpointGrace: supervisors resume it once the orchestrator's
// checkpoint summary (bounded at 5 minutes) is emitted.
func AwaitingHumanNotice(state *models.State) string {
	if state.Config.Mode == models.SystemModePaused {
		return fmt.Sprintf("system mode is PAUSED; run %q", brand.Command("resume"))
	}
	if state.Config.AutoResume && state.Sprint.Status == models.SprintStatusCheckpoint {
		at := state.Sprint.Timeline.CheckpointAt
		if at != nil && time.Since(*at) < autoResumeCheckpointGrace {
			return ""
		}
	}
	return checkpointNotice(state.Sprint)
}

// autoResumeCheckpointGrace covers the checkpoint-summary timeout plus a
// supervisor poll; a checkpoint older than this failed to auto-resume.
const autoResumeCheckpointGrace = 6 * time.Minute

// runParked reports whether a pause gate holds every role. A transition
// checkpoint holds only the orchestrator; doers and reviewers keep working.
func runParked(state *models.State) bool {
	if state.Config.Mode == models.SystemModePaused {
		return true
	}
	return state.Sprint.Status == models.SprintStatusCheckpoint &&
		!models.IsTransitionCheckpointTrigger(state.Sprint.CheckpointTrigger)
}

// checkAwaitingHuman is emitted every check while active; reconcileStuckAlerts
// writes it once per episode and the TUI clears it on resume. A plan the
// orchestrator held for a human action raises its own alert, keyed by task and
// ask, because nothing expands it until an operator clears the hold. So does a
// BLOCKED task whose episode names a human action (D65), keyed by the ask's
// occurrence and logged once across watchers through the once-ledger.
func checkAwaitingHuman(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()
	if notice := AwaitingHumanNotice(state); notice != "" {
		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryAwaitingHuman.String(),
			Message:   notice,
		})
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if alert, ok := blockedAwaitingHumanAlert(task, now); ok {
			raised = append(raised, alert)
			continue
		}
		if task.Status != models.TaskStatusMerged || task.PlanCheckVerdictOf() != models.PlanCheckHeld {
			continue
		}
		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryAwaitingHuman.String(),
			Message: fmt.Sprintf("plan %s held before its children exist: %s; after doing it, run %q",
				task.ID, task.PlanCheck.Ask, brand.Command("plan-check", task.ID, "--clear")),
		})
	}
	return raised
}

func blockedAwaitingHumanAlert(task *models.Task, now time.Time) (Alert, bool) {
	human, ok := models.CurrentAwaitingHuman(task)
	if !ok {
		return Alert{}, false
	}
	key := alerts.AwaitingHumanOccurrenceKey(task.ID, human.Index, human.At, human.Ask)
	return Alert{
		Timestamp: now,
		Level:     AlertLevelCritical,
		Category:  alerts.CategoryAwaitingHuman.String(),
		Message:   fmt.Sprintf("%s blocked on a human action: %s", task.ID, human.Ask),
		Identity:  key,
		OnceKey:   key,
	}, true
}

func checkStaleSentinels(state *models.State, cache map[string]time.Time) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	activeSentinels := make(map[string]bool)

	for _, task := range state.Tasks {
		if task.AssignedTo == nil || !strings.HasPrefix(*task.AssignedTo, sentinelAssigneePrefix) {
			continue
		}
		activeSentinels[task.ID] = true

		cacheKey := sentinelCachePrefix + task.ID
		firstSeen, seen := cache[cacheKey]
		if !seen {
			cache[cacheKey] = now
			continue
		}
		if now.Sub(firstSeen) > StaleSentinelThreshold {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryStaleSentinel.String(),
				Message:   fmt.Sprintf("%s stuck in transition — manual repair needed", task.ID),
				Identity:  task.ID + alertIdentitySeparator + *task.AssignedTo,
			})
		}
	}

	// Clear cache entries for sentinels that resolved.
	for key := range cache {
		if !strings.HasPrefix(key, sentinelCachePrefix) {
			continue
		}
		taskID := strings.TrimPrefix(key, sentinelCachePrefix)
		if !activeSentinels[taskID] {
			delete(cache, key)
		}
	}

	return raised
}
