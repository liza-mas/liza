package commands

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
)

const orchestratorMissingSinceKey = "orchestrator-missing:since"
const orchestratorMissingAlertedKey = "orchestrator-missing:alerted"

// orchestratorRepairDue reports a missing orchestrator once its absence has
// outlasted OrchestratorMissingGracePeriod, the grace the ORCHESTRATOR MISSING
// alert uses. The orchestrator allows one instance, so repairing a fresh
// absence would race launchers that start the watcher and the orchestrator
// together, and registration would refuse the operator's orchestrator.
func orchestratorRepairDue(state *models.State, pr models.PipelineResolver, cache map[string]time.Time, now time.Time) (MissingRoleWork, bool) {
	presence := evaluateOrchestratorPresence(state, pr, now)
	since, absent := orchestratorAbsentSince(presence, cache, now)
	if !absent || now.Sub(since) < OrchestratorMissingGracePeriod {
		return MissingRoleWork{}, false
	}
	return orchestratorRoleWork(presence), true
}

// orchestratorPresence answers whether a running goal needs an orchestrator
// and whether one holds effective ownership. The ORCHESTRATOR MISSING check
// and pool repair share it so the alert and the restart agree on absence.
type orchestratorPresence struct {
	Roles     []string // pipeline roles typed orchestrator, sorted
	Required  bool
	Present   bool
	StaleRows []string // "<id> <effective ownership>" of unoccupied rows
}

// evaluateOrchestratorPresence requires an orchestrator while the goal is
// IN_PROGRESS, the system is RUNNING and the pipeline declares an
// orchestrator-type role. Presence is lease-first, as at registration: a fresh
// lease whose process looks dead still counts, because a replacement would be
// refused until the lease expires.
func evaluateOrchestratorPresence(state *models.State, pr models.PipelineResolver, now time.Time) orchestratorPresence {
	var presence orchestratorPresence
	for _, role := range pr.AllRoleNames() {
		if roleType, err := pr.RoleType(role); err == nil && roleType == roles.RoleTypeOrchestrator {
			presence.Roles = append(presence.Roles, role)
		}
	}
	slices.Sort(presence.Roles)
	mode := state.Config.Mode
	if mode == "" {
		mode = models.SystemModeRunning
	}
	if len(presence.Roles) == 0 || mode != models.SystemModeRunning || state.Goal.Status != models.GoalStatusInProgress {
		return presence
	}
	presence.Required = true

	for _, agentID := range slices.Sorted(maps.Keys(state.Agents)) {
		agent := state.Agents[agentID]
		if !slices.Contains(presence.Roles, agent.Role) {
			continue
		}
		observation := ops.AgentProcessOwnership(agentID, agent, now, models.AgentLivenessWindow(state.Config))
		if observation.Occupied() {
			presence.Present = true
			return presence
		}
		presence.StaleRows = append(presence.StaleRows, fmt.Sprintf("%s %s", agentID, observation.Effective))
	}
	return presence
}

// orchestratorAbsentSince advances the absence episode in the watcher's cache
// and returns when it began. Auto-repair and the alert check both call it on
// every tick; the calls are idempotent, so they share one episode and one
// grace period. Callers must not call it without a resolver: an unloadable
// pipeline is not a resolution, so the episode is kept.
func orchestratorAbsentSince(presence orchestratorPresence, cache map[string]time.Time, now time.Time) (time.Time, bool) {
	if !presence.Required || presence.Present {
		endOrchestratorMissingEpisode(cache)
		return time.Time{}, false
	}
	since, seen := cache[orchestratorMissingSinceKey]
	if !seen {
		cache[orchestratorMissingSinceKey] = now
		return now, true
	}
	return since, true
}

// checkMissingOrchestrator raises one alert per episode in which a running
// goal has no orchestrator holding effective ownership. Role-pair demand never
// names the orchestrator and an exited supervisor deletes its own row, so no
// other check can see this absence. The episode lives in the watcher's cache,
// so the once-per-episode guarantee is per observer.
func checkMissingOrchestrator(state *models.State, pr models.PipelineResolver, cache map[string]time.Time, now time.Time) []Alert {
	if pr == nil {
		return nil
	}

	presence := evaluateOrchestratorPresence(state, pr, now)
	since, absent := orchestratorAbsentSince(presence, cache, now)
	if !absent || now.Sub(since) < OrchestratorMissingGracePeriod {
		return nil
	}
	if _, alerted := cache[orchestratorMissingAlertedKey]; alerted {
		return nil
	}
	cache[orchestratorMissingAlertedKey] = now

	msg := fmt.Sprintf("no live %s agent while goal is IN_PROGRESS (absent since %s",
		strings.Join(presence.Roles, "/"), since.UTC().Format(time.RFC3339))
	if len(presence.StaleRows) > 0 {
		msg += "; last row: " + strings.Join(presence.StaleRows, ", ")
	}
	msg += "); planning, blocked-task assessment and checkpoints are not being handled; "
	command := brand.Command("agent", presence.Roles[0])
	// The environment is verifiable here; whether a restart succeeds is not
	// (spawns can be refused or suppressed), so the tail names only the former.
	if enabled, _ := AutoRepairAgentPoolEnabledFromEnv(); enabled {
		msg += fmt.Sprintf("auto-repair is enabled — if no orchestrator returns, start one with `%s`", command)
	} else {
		msg += fmt.Sprintf("auto-repair is disabled by %s — start one with `%s`", EnvAutoRepairAgentPool, command)
	}

	return []Alert{{
		Timestamp: now,
		Level:     AlertLevelCritical,
		Category:  alerts.CategoryOrchestratorMissing.String(),
		Message:   msg,
	}}
}

func endOrchestratorMissingEpisode(cache map[string]time.Time) {
	delete(cache, orchestratorMissingSinceKey)
	delete(cache, orchestratorMissingAlertedKey)
}
