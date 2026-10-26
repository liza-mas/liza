package commands

import (
	"fmt"
	"math/bits"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

const (
	circuitBreakerAlertKey = "circuit_breaker:alert"
	sprintStalledAlertKey  = "sprint_stalled:alert"
	// approachingLimitWindow is how many cycles before the enforced cap an
	// iteration or review count starts to warn.
	approachingLimitWindow = 2
)

func checkCircuitBreakerEscalation(state *models.State, cache map[string]time.Time) []Alert {
	mode := state.Config.Mode
	if mode == "" {
		mode = models.SystemModeRunning
	}

	// Only check during active execution.
	if mode != models.SystemModeRunning || state.Sprint.Status != models.SprintStatusInProgress {
		delete(cache, circuitBreakerAlertKey)
		return nil
	}

	// Keep both checks: manual edits or interrupted writes can leave one field stale.
	// Either value indicates a previously triggered circuit-breaker state.
	if state.CircuitBreaker.Status == circuitBreakerStatusTriggered || state.CircuitBreaker.CurrentTrigger != nil {
		delete(cache, circuitBreakerAlertKey)
		return nil
	}

	patternResult, _, _ := analysis.DetectUnacknowledgedPatterns(state)
	if !patternResult.Triggered {
		delete(cache, circuitBreakerAlertKey)
		return nil
	}

	// Throttle: only alert once per triggered period.
	if _, seen := cache[circuitBreakerAlertKey]; seen {
		return nil
	}

	cache[circuitBreakerAlertKey] = time.Now().UTC()
	return []Alert{{
		Timestamp: time.Now().UTC(),
		Level:     AlertLevelCritical,
		Category:  alerts.CategoryCircuitBreaker.String(),
		Message: fmt.Sprintf("pattern=%s severity=%s — run %q then %q",
			patternResult.Pattern, patternResult.Severity, brand.Command("analyze"), brand.Command("sprint-checkpoint")),
	}}
}

func checkSprintStalled(state *models.State, cache map[string]time.Time) []Alert {
	mode := state.Config.Mode
	if mode == "" {
		mode = models.SystemModeRunning
	}

	if mode != models.SystemModeRunning || state.Sprint.Status != models.SprintStatusInProgress {
		// Clear throttle when sprint leaves IN_PROGRESS (e.g. after checkpoint).
		// This ensures that if the human resumes without unblocking tasks,
		// the next stall detection re-triggers a fresh alert.
		delete(cache, sprintStalledAlertKey)
		return nil
	}

	if !state.SprintStalled() {
		delete(cache, sprintStalledAlertKey)
		return nil
	}

	// Throttle: only alert once per stall event within a single IN_PROGRESS period.
	// The sprint status guard above resets the throttle across checkpoint/resume cycles.
	if _, seen := cache[sprintStalledAlertKey]; seen {
		return nil
	}

	blockedCount := 0
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task != nil && task.Status == models.TaskStatusBlocked {
			blockedCount++
		}
	}

	cache[sprintStalledAlertKey] = time.Now().UTC()
	return []Alert{{
		Timestamp: time.Now().UTC(),
		Level:     AlertLevelCritical,
		Category:  alerts.CategorySprintStalled.String(),
		Message: fmt.Sprintf("all %d non-terminal planned tasks are BLOCKED",
			blockedCount),
	}}
}

func checkApproachingLimits(state *models.State, resolver models.PipelineResolver) []Alert {
	var raised []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		attemptNum := task.EffectiveAttempt()
		iterationLimit, reviewLimit := ops.EffectiveIterationLimits(&task, state.Config)
		executing := models.TaskStatusImplementing
		if resolver != nil && task.RolePair != "" {
			var err error
			executing, err = resolver.ExecutingStatus(task.RolePair)
			if err != nil {
				continue
			}
		}

		// Warn during the last two cycles before the enforced cap.
		if task.Status == executing && task.Iteration > 0 && task.Iteration >= iterationLimit-approachingLimitWindow && task.Iteration < iterationLimit {
			var msg string
			if attemptNum == finalTaskAttempt {
				msg = fmt.Sprintf("%s — attempt 2 (final), iteration %d/%d", task.ID, task.Iteration, iterationLimit)
			} else {
				msg = fmt.Sprintf("%s — attempt %d, iteration %d/%d", task.ID, attemptNum, task.Iteration, iterationLimit)
			}
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryApproachingLimit.String(),
				Message:   msg,
			})
		}

		if task.ReviewCyclesCurrent > 0 && task.ReviewCyclesCurrent >= reviewLimit-approachingLimitWindow && task.ReviewCyclesCurrent < reviewLimit {
			var msg string
			if attemptNum == finalTaskAttempt {
				msg = fmt.Sprintf("%s — attempt 2 (final), review cycle %d/%d", task.ID, task.ReviewCyclesCurrent, reviewLimit)
			} else {
				msg = fmt.Sprintf("%s — attempt %d, review cycle %d/%d", task.ID, attemptNum, task.ReviewCyclesCurrent, reviewLimit)
			}
			raised = append(raised, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryApproachingLimit.String(),
				Message:   msg,
			})
		}
	}

	return raised
}

// checkStalled detects stalled progress by finding the latest task history
// timestamp across all tasks. Heartbeat writes do not create history entries,
// so this signal is immune to lease-renewal traffic. Falls back to the earliest
// task Created time when no history exists.
//
// It reports on every check while stalled; the identity is the stall episode
// (the latest progress time) and its escalation step, so the gate writes one
// line at 30, 60, 120, 240… minutes, restarting at 30 after new progress.
func checkStalled(state *models.State, pr models.PipelineResolver) []Alert {
	var raised []Alert
	now := watchNow().UTC()

	// Find latest history timestamp and check for active tasks.
	var latestProgress time.Time
	hasActive := false
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !models.IsOperationallyTerminal(task, pr) {
			hasActive = true
		}
		for j := range task.History {
			if task.History[j].Time.After(latestProgress) {
				latestProgress = task.History[j].Time
			}
		}
	}

	// A parked run makes no progress by design; AWAITING HUMAN names the remedy.
	if !hasActive || runParked(state) {
		return raised
	}

	// No history entries: fall back to earliest Created.
	if latestProgress.IsZero() {
		for i := range state.Tasks {
			created := state.Tasks[i].Created
			if latestProgress.IsZero() || created.Before(latestProgress) {
				latestProgress = created
			}
		}
	}

	if latestProgress.IsZero() {
		return raised
	}

	age := now.Sub(latestProgress)
	if age <= StallThreshold {
		return raised
	}

	// Step k covers ages in [30m·2^k, 30m·2^(k+1)).
	step := bits.Len64(uint64(age/StallThreshold)) - 1
	return append(raised, Alert{
		Timestamp: now,
		Level:     AlertLevelWarning,
		Category:  alerts.CategoryStalled.String(),
		Message:   fmt.Sprintf("no task progress for %d minutes%s", int(age.Minutes()), stallDiagnosis(state, pr, now)),
		Identity:  fmt.Sprintf("%s|step %d", latestProgress.UTC().Format(time.RFC3339Nano), step),
	})
}
