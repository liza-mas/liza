package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/models"
)

const (
	// legacyBlockedCachePrefix keyed the one-shot BLOCKED cache of an earlier
	// implementation; checkBlockedTasks clears any such key.
	legacyBlockedCachePrefix = "blocked:"
	finalAttemptCachePrefix  = "attempt2:"
	// reviewLoopCliffCycles is the review cycle count at which REVIEW LOOP fires.
	reviewLoopCliffCycles = 5
)

func checkBlockedTasks(state *models.State, cache map[string]time.Time) []Alert {
	var blocked []Alert
	now := time.Now().UTC()

	// BLOCKED alerts are emitted every check so RunChecksWithStateSnapshot can
	// compute active freshness keys before reconcileStuckAlerts dedupes log writes.
	// Clear legacy cache keys from the previous one-shot implementation.
	for key := range cache {
		if strings.HasPrefix(key, legacyBlockedCachePrefix) {
			delete(cache, key)
		}
	}

	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusBlocked {
			continue
		}

		reason := "no reason"
		if task.BlockedReason != nil {
			reason = *task.BlockedReason
		}
		message := fmt.Sprintf("%s — %s", task.ID, reason)
		alert := Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  alerts.CategoryBlocked.String(),
			Message:   message,
		}
		// The blocked history entry names the episode: a new one alerts even
		// when no unblocked check was observed, and its key lets MarkBlocked
		// and every watcher log one line per episode between them.
		if episodeStart := models.LatestHistoryTime(&task, models.TaskEventBlocked); !episodeStart.IsZero() {
			alert.Identity = message + alertIdentitySeparator + episodeStart.UTC().Format(time.RFC3339Nano)
			alert.OnceKey = alerts.BlockedEpisodeKey(task.ID, episodeStart, message)
		}
		blocked = append(blocked, alert)
	}

	return blocked
}

// checkOrphanedRejected reports a rejected task whose assigned doer is not
// reworking it once the verdict handoff grace has passed. The grace runs from
// the verdict, not from first sighting; a missing verdict time grants none.
// The identity is the rejection episode, so the doer row flipping between
// WAITING and IDLE does not re-alert.
func checkOrphanedRejected(state *models.State, pr models.PipelineResolver) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !isRejectedStatus(task, pr) {
			continue
		}
		// A sentinel assignee (e.g. "$transitioning") is a transition in
		// progress, not an orphaned assignment.
		if task.AssignedTo == nil || strings.HasPrefix(*task.AssignedTo, sentinelAssigneePrefix) {
			continue
		}

		assignee := *task.AssignedTo
		agent, exists := state.Agents[assignee]
		agentStatus := "MISSING"
		if exists {
			agentStatus = string(agent.Status)
		}
		if agentStatus == string(models.AgentStatusWorking) || models.InVerdictHandoff(task, now) {
			continue
		}

		verdictAt := models.LatestHistoryTime(task, models.TaskEventRejected)
		raised = append(raised, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryOrphanedRejected.String(),
			Message: fmt.Sprintf("%s — assigned to %s but agent is %s (no rework %dm+ after verdict)",
				task.ID, assignee, agentStatus, int(models.VerdictHandoffGrace.Minutes())),
			Identity: fmt.Sprintf("%s|%s|%s", task.ID, assignee, verdictAt.UTC().Format(time.RFC3339Nano)),
		})
	}

	return raised
}

// isRejectedStatus reports whether the task sits in its role pair's rejected
// status. Without a pipeline resolver only the coding pair's status is known.
func isRejectedStatus(task *models.Task, pr models.PipelineResolver) bool {
	if pr != nil && task.RolePair != "" {
		rejected, err := pr.RejectedStatus(task.RolePair)
		return err == nil && task.Status == rejected
	}
	return task.Status == models.TaskStatusRejected
}

func checkReviewLoops(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.ReviewCyclesCurrent >= reviewLoopCliffCycles {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryReviewLoop.String(),
				Message:   fmt.Sprintf("%s — %d cycles (at cliff)", task.ID, task.ReviewCyclesCurrent),
			})
		}
	}

	return raised
}

func checkIntegrationFailures(state *models.State, projectRoot string) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status == models.TaskStatusIntegrationFailed {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryIntegrationFailed.String(),
				Message:   integrationFailureAlertMessage(&task, projectRoot),
			})
		}
	}

	return raised
}

func checkHypothesisExhaustion(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.Status == models.TaskStatusBlocked {
			continue
		}
		if len(task.FailedBy) >= hypothesisExhaustionFailures {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryHypothesisExhaustion.String(),
				Message:   fmt.Sprintf("%s — requires rescope", task.ID),
				// Another failed doer is a new condition.
				Identity: task.ID + alertIdentitySeparator + strings.Join(slices.Sorted(slices.Values(task.FailedBy)), ","),
			})
		}
	}

	return raised
}

func checkReassigned(state *models.State, cache map[string]time.Time) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.EffectiveAttempt() != finalTaskAttempt {
			continue
		}

		cacheKey := finalAttemptCachePrefix + task.ID
		if _, seen := cache[cacheKey]; !seen {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryAttempt.String(),
				Message:   fmt.Sprintf("%s — attempt 2 (final attempt)", task.ID),
			})
			cache[cacheKey] = now
		}
	}

	return raised
}

func checkStaleDrafts(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusDraft {
			continue
		}

		age := now.Sub(task.Created)
		if age > StaleDraftThreshold {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryStaleDraft.String(),
				Message: fmt.Sprintf("%s — created %dmin ago, never finalized (Orchestrator crash?)",
					task.ID, int(age.Minutes())),
				// The message carries the age; the draft is the condition.
				Identity: task.ID + alertIdentitySeparator + task.Created.UTC().Format(time.RFC3339Nano),
			})
		}
	}

	return raised
}

func checkImmediateDiscoveries(state *models.State) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, disc := range state.Discovered {
		if disc.Urgency == models.DiscoveryUrgencyImmediate && disc.ConvertedToTask == nil {
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  alerts.CategoryImmediateDiscovery.String(),
				Message:   fmt.Sprintf("%s — %s (Orchestrator should wake)", disc.ID, disc.Description),
			})
		}
	}

	return raised
}
