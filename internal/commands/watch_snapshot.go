package commands

import (
	"errors"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// pipelineConfigErrorKey holds the pipeline config error already alerted.
const pipelineConfigErrorKey = "pipeline-config-error"

// AlertSnapshot contains both newly emitted alerts and the complete set of
// currently active alert identities for consumers that need freshness state.
type AlertSnapshot struct {
	Alerts     []Alert
	ActiveKeys map[string]bool
}

// RunChecksWithState runs all 13 anomaly checks plus circuit breaker,
// sprint stalled, and state validity checks against the provided state.
// The config.StateCache is modified in place for alert throttling.
func RunChecksWithState(state *models.State, config WatchConfig) []Alert {
	return RunChecksWithStateSnapshot(state, config).Alerts
}

// RunChecksWithStateSnapshot runs checks and returns emitted alerts plus active
// alert keys before throttling. The config.StateCache is modified in place.
func RunChecksWithStateSnapshot(state *models.State, config WatchConfig) AlertSnapshot {
	if config.StateCache == nil {
		config.StateCache = make(map[string]time.Time)
	}

	var raised []Alert

	// Load pipeline resolver once for checks that need it.
	pr, prErr := ops.LoadResolverForModels(config.ProjectRoot)
	pipelineCacheKey := pipelineConfigErrorKey
	if prErr != nil && !errors.Is(prErr, pipeline.ErrConfigNotFound) {
		// Malformed config: emit one-time alert, don't spam every 10s tick.
		if _, seen := config.StateCache[pipelineCacheKey]; !seen {
			raised = append(raised, Alert{
				Timestamp: time.Now().UTC(),
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryPipelineConfig.String(),
				Message:   prErr.Error(),
			})
			config.StateCache[pipelineCacheKey] = time.Now().UTC()
		}
	} else {
		// Clear on success (or ErrConfigNotFound) so a later regression re-alerts.
		delete(config.StateCache, pipelineCacheKey)
	}
	// pr is nil on any error — pipeline-aware checks skip gracefully.

	// Pool repair alerts on an unreadable models.yaml; here it binds nothing.
	roleModels, _ := rolemodels.Load(config.ProjectRoot)
	checks := []func() []Alert{
		func() []Alert { return checkExpiredLeases(state) },
		func() []Alert { return checkRegisteredAgentsWithoutLiveProcess(state) },
		func() []Alert { return checkRunningTasksWithoutLiveProcess(state, pr) },
		func() []Alert { return checkAwaitingHuman(state) },
		func() []Alert { return checkBlockedTasks(state, config.StateCache) },
		func() []Alert { return checkOrphanedRejected(state, pr) },
		func() []Alert { return checkReviewLoops(state) },
		func() []Alert { return checkIntegrationFailures(state, config.ProjectRoot) },
		func() []Alert { return checkHypothesisExhaustion(state) },
		func() []Alert { return checkReassigned(state, config.StateCache) },
		func() []Alert { return checkApproachingLimits(state, pr) },
		func() []Alert { return checkStaleSentinels(state, config.StateCache) },
		func() []Alert { return checkStalled(state, pr) },
		func() []Alert { return checkStaleDrafts(state) },
		func() []Alert { return checkImmediateDiscoveries(state) },
		func() []Alert { return checkMissingRoles(state, pr, roleModels, config.StateCache) },
		func() []Alert { return checkMissingOrchestrator(state, pr, config.StateCache, time.Now().UTC()) },
	}
	for _, check := range checks {
		raised = append(raised, check()...)
	}

	raised = append(raised, checkCircuitBreakerEscalation(state, config.StateCache)...)
	raised = append(raised, checkSprintStalled(state, config.StateCache)...)

	// Validate the snapshot the other checks saw, rather than reading and
	// decoding state.yaml again.
	if err := ValidateLoadedState(state, config.ProjectRoot, ValidateOptions{
		SkipSpecFileCheck:        true,
		WarnWriter:               config.WarnWriter,
		RecentlySpawnedAgentPIDs: config.RecentlySpawnedAgentPIDs,
	}); err != nil {
		raised = append(raised, Alert{
			Timestamp: time.Now().UTC(),
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryInvalidState.String(),
			Message:   err.Error(),
		})
	}

	activeKeys := activeAlertKeys(raised)
	return AlertSnapshot{
		Alerts:     reconcileStuckAlerts(raised, config.StateCache),
		ActiveKeys: activeKeys,
	}
}
