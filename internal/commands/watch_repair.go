package commands

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/rolemodels"
)

const autoRepairAgentPoolCachePrefix = "auto-repair-agent-pool:"
const autoRepairAgentPoolStartCountPrefix = "auto-repair-agent-pool-start-count:"
const autoRepairAgentPoolSuppressedPrefix = "auto-repair-agent-pool-suppressed:"
const autoRepairAgentPoolPendingPrefix = "auto-repair-agent-pool-pending:"

// A pending-spawn cache entry is role:pid:agentID:modelsItem after the prefix.
const (
	autoRepairPendingEntryFormat    = "%s:%d:%s:%d"
	autoRepairPendingEntrySeparator = ":"
	autoRepairPendingEntryFields    = 4
)

// Positions of the pending-entry fields after the separator split.
const (
	pendingEntryRole = iota
	pendingEntryPID
	pendingEntryAgentID
	pendingEntryModelsItem
)

// registeredProcessKeyFormat keys a registered agent process by role and PID.
const registeredProcessKeyFormat = "%s:%d"
const autoRepairAgentPoolPendingTimeoutPrefix = "auto-repair-agent-pool-pending-timeout:"
const autoRepairAgentPoolUnservablePrefix = "auto-repair-agent-pool-unservable:"
const autoRepairAgentPoolProviderBlockedPrefix = "auto-repair-agent-pool-provider-blocked:"
const autoRepairAgentPoolEnvWarningKey = "auto-repair-agent-pool-env-warning"

// autoRepairModelsErrorKey holds the last models.yaml error alerted, so an
// invalid file raises one alert per distinct error rather than one per tick.
const autoRepairModelsErrorKey = "auto-repair-models-error:"

type AutoRepairAgentPoolOutcome struct {
	Alerts          []Alert
	AttemptedRoles  []string
	SuppressedRoles []string
	// BlockedRoles have claimable work but no usable provider on this tick.
	BlockedRoles []string
	Spawned      []SpawnedAgent
	Failed       []FailedAgentSpawn
}

// RunAutoRepairAgentPool is one watcher tick of pool repair: environment
// toggle, models.yaml, then the policy (which roles are due a start) and the
// execution of those starts. The returned alerts replace the MISSING ROLE alerts
// of the roles it handled (FilterAlertsAfterAutoRepair).
func RunAutoRepairAgentPool(ctx context.Context, state *models.State, config WatchConfig) AutoRepairAgentPoolOutcome {
	var outcome AutoRepairAgentPoolOutcome
	if ctx.Err() != nil || state == nil {
		return outcome
	}
	if config.StateCache == nil {
		config.StateCache = make(map[string]time.Time)
	}
	if config.WarnWriter == nil {
		config.WarnWriter = os.Stderr
	}

	if !autoRepairEnabled(config) {
		return outcome
	}

	// Keep auto-repair decoupled from the pure alert snapshot path used by
	// the TUI so successful repairs can suppress MISSING ROLE alerts cleanly.
	pr, err := ops.LoadResolverForModels(config.ProjectRoot)
	if err != nil {
		return outcome
	}

	now := time.Now().UTC()
	roleModels, ok := loadAutoRepairRoleModels(state, pr, config, now, &outcome)
	if !ok {
		return outcome
	}
	plan := planAutoRepairStarts(state, pr, roleModels, config, now, &outcome)
	if len(plan.roles) == 0 {
		return outcome
	}
	executeAutoRepairStarts(config, plan, &outcome)
	return outcome
}

// autoRepairPlan is the policy decision of one tick: the roles due a start, and
// the started processes (with the explicit IDs they will register) that may
// already cover demand.
type autoRepairPlan struct {
	roles      []string
	pending    map[string][]int
	pendingIDs map[string]bool
}

// autoRepairEnabled reports whether the environment enables pool repair. An
// unrecognised value warns once per run of ticks; a disabled repair ends the
// episodes it kept in the cache, so re-enabling announces a still-blocked role.
func autoRepairEnabled(config WatchConfig) bool {
	enabled, envWarning := AutoRepairAgentPoolEnabledFromEnv()
	if envWarning != "" {
		if _, seen := config.StateCache[autoRepairAgentPoolEnvWarningKey]; !seen {
			fmt.Fprintf(config.WarnWriter, "WARNING: %s\n", envWarning)
			config.StateCache[autoRepairAgentPoolEnvWarningKey] = time.Now().UTC()
		}
	} else {
		delete(config.StateCache, autoRepairAgentPoolEnvWarningKey)
	}
	if !enabled {
		clearAutoRepairAgentPoolCache(config.StateCache, nil)
		autoRepairProviderBlockedAlerts(nil, config.StateCache, time.Now().UTC())
	}
	return enabled
}

// loadAutoRepairRoleModels loads the validated models.yaml. An invalid file
// raises one AUTO REPAIR FAILED alert per distinct error and skips the tick.
func loadAutoRepairRoleModels(state *models.State, pr models.PipelineResolver, config WatchConfig, now time.Time, outcome *AutoRepairAgentPoolOutcome) (rolemodels.File, bool) {
	roleModels, err := agent.LoadValidatedRoleModels(config.ProjectRoot, agent.RoleTypesOf(pr), state.Config)
	if err != nil {
		key := autoRepairModelsErrorKey + err.Error()
		if _, seen := config.StateCache[key]; !seen {
			clearAutoRepairModelsError(config.StateCache)
			config.StateCache[key] = now
			fmt.Fprintf(config.WarnWriter, "WARNING: auto repair skipped: %v\n", err)
			outcome.Alerts = append(outcome.Alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  alerts.CategoryAutoRepairFailed.String(),
				Message:   err.Error(),
			})
		}
		return rolemodels.File{}, false
	}
	clearAutoRepairModelsError(config.StateCache)
	return roleModels, true
}

// planAutoRepairStarts decides which roles are due a start: it settles the
// processes this watcher started, finds the demand they leave, reports the work
// no usable provider can start, unservable reviewer work and suppressed roles,
// and drops roles inside their backoff or past the start cap.
func planAutoRepairStarts(state *models.State, pr models.PipelineResolver, roleModels rolemodels.File, config WatchConfig, now time.Time, outcome *AutoRepairAgentPoolOutcome) autoRepairPlan {
	plan := autoRepairPlan{}
	plan.pending, plan.pendingIDs = resolveAutoRepairPendingSpawns(state, config.StateCache, now)
	capacity, providerBlocked := autoRepairDemand(state, pr, roleModels, plan.pendingIDs, config, now)
	missing := capacity.Missing
	outcome.Alerts = append(outcome.Alerts, autoRepairProviderBlockedAlerts(providerBlocked, config.StateCache, now)...)
	for _, work := range providerBlocked {
		if !slices.Contains(outcome.BlockedRoles, work.Role) {
			outcome.BlockedRoles = append(outcome.BlockedRoles, work.Role)
		}
	}
	// Backoff and failed-start bookkeeping lives while a role has demand or
	// started processes pending; pending ones may already cover the demand.
	episodeRoles := make(map[string]bool, len(missing)+len(plan.pending))
	for _, roleWork := range missing {
		episodeRoles[roleWork.Role] = true
	}
	for role := range plan.pending {
		episodeRoles[role] = true
	}
	clearAutoRepairAgentPoolCache(config.StateCache, episodeRoles)
	missing = subtractPendingSpawns(missing, plan.pending)
	outcome.Alerts = append(outcome.Alerts, autoRepairUnservableAlerts(capacity.Unservable, config.StateCache, now)...)
	suppressedAlerts, suppressedRoles := autoRepairSuppressedAlerts(missing, config.StateCache, now)
	outcome.Alerts = append(outcome.Alerts, suppressedAlerts...)
	outcome.SuppressedRoles = append(outcome.SuppressedRoles, suppressedRoles...)
	plan.roles = autoRepairDueRoles(missing, config.StateCache, now)
	return plan
}

// autoRepairDemand finds the roles missing capacity, the reviewer work no
// reviewer could claim, and the work whose start CLI is quota-blocked, which is
// set aside on every tick, before the due filter, so its alert episode is
// reconciled against the whole pool rather than against the roles due a start.
// A missing orchestrator joins them once its absence outlasted its grace.
func autoRepairDemand(state *models.State, pr models.PipelineResolver, roleModels rolemodels.File, pendingIDs map[string]bool, config WatchConfig, now time.Time) (RoleCapacity, []ProviderBlockedRoleWork) {
	repairCLI, blocked := RepairCLI{RoleModels: roleModels}, QuotaProviderBlock(config.ProjectRoot)
	capacity := FindRoleCapacity(state, pr, repairCLI, pendingIDs, blocked, now)
	providerBlocked := capacity.ProviderBlocked
	if roleWork, due := orchestratorRepairDue(state, pr, config.StateCache, now); due {
		if work, isBlocked := blockedOrchestratorWork(roleWork, repairCLI, state, pr, blocked); isBlocked {
			providerBlocked = append(providerBlocked, work)
		} else {
			capacity.Missing = append(capacity.Missing, roleWork)
		}
	}
	return capacity, providerBlocked
}

func clearAutoRepairModelsError(cache map[string]time.Time) {
	for key := range cache {
		if strings.HasPrefix(key, autoRepairModelsErrorKey) {
			delete(cache, key)
		}
	}
}

func autoRepairDueRoles(missing []MissingRoleWork, cache map[string]time.Time, now time.Time) []string {
	roles := make([]string, 0, len(missing))
	for _, roleWork := range missing {
		role := roleWork.Role
		if autoRepairStartCount(cache, role) >= AutoRepairAgentPoolMaxStarts {
			continue
		}
		lastAttempt, seen := cache[autoRepairAgentPoolCachePrefix+role]
		if seen && now.Sub(lastAttempt) < AutoRepairAgentPoolBackoff {
			continue
		}
		roles = append(roles, role)
	}
	return roles
}

func autoRepairSuppressedAlerts(missing []MissingRoleWork, cache map[string]time.Time, now time.Time) ([]Alert, []string) {
	var out []Alert
	var roles []string
	for _, roleWork := range missing {
		role := roleWork.Role
		if autoRepairStartCount(cache, role) < AutoRepairAgentPoolMaxStarts {
			delete(cache, autoRepairAgentPoolSuppressedPrefix+role)
			continue
		}
		key := autoRepairAgentPoolSuppressedPrefix + role
		if suppressedAt, seen := cache[key]; seen {
			if now.Sub(suppressedAt) >= AutoRepairAgentPoolSuppressionCooldown {
				delete(cache, key)
				delete(cache, autoRepairAgentPoolStartCountPrefix+role)
				delete(cache, autoRepairAgentPoolCachePrefix+role)
				continue
			}
			roles = append(roles, role)
			continue
		}
		cache[key] = now
		roles = append(roles, role)
		out = append(out, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  alerts.CategoryAutoRepairFailed.String(),
			Message: fmt.Sprintf("auto repair suppressed for role %s after %d started agent process(es) did not register; automatic retry after %s cooldown",
				role, AutoRepairAgentPoolMaxStarts, AutoRepairAgentPoolSuppressionCooldown),
		})
	}
	return out, roles
}

func clearAutoRepairAgentPoolCache(cache map[string]time.Time, missingSet map[string]bool) {
	for key := range cache {
		var role string
		switch {
		case strings.HasPrefix(key, autoRepairAgentPoolCachePrefix):
			role = strings.TrimPrefix(key, autoRepairAgentPoolCachePrefix)
		case strings.HasPrefix(key, autoRepairAgentPoolStartCountPrefix):
			role = strings.TrimPrefix(key, autoRepairAgentPoolStartCountPrefix)
		case strings.HasPrefix(key, autoRepairAgentPoolSuppressedPrefix):
			role = strings.TrimPrefix(key, autoRepairAgentPoolSuppressedPrefix)
		default:
			continue
		}
		if missingSet == nil || !missingSet[role] {
			delete(cache, key)
		}
	}
}

// recordAutoRepairFailedStart counts one started agent process that did not
// register. The count is consecutive: a registration resets it.
func recordAutoRepairFailedStart(cache map[string]time.Time, role string, now time.Time) {
	count := autoRepairStartCount(cache, role) + 1
	cache[autoRepairAgentPoolStartCountPrefix+role] = autoRepairCountTime(count, now)
}

// autoRepairPendingKey names one started process: its role, PID, the
// explicit ID it was started under (if any) and the models.yaml list item it
// was started with (0 for none).
func autoRepairPendingKey(role string, pid int, agentID string, modelsItem int) string {
	return fmt.Sprintf(autoRepairAgentPoolPendingPrefix+autoRepairPendingEntryFormat, role, pid, agentID, modelsItem)
}

// autoRepairPendingProcessStatus observes a started agent process; tests
// replace it.
var autoRepairPendingProcessStatus = func(pid int, role string) procscan.AgentProcessStatus {
	return procscan.AgentProcessStatusForPID(pid, role, "", "")
}

// resolveAutoRepairPendingSpawns settles the agent processes this watcher
// started, independently of current demand, and returns per role those still
// pending. A process that registered resets its role's failed-start count; one
// seen to exit (dead, or its PID now names another program) counts as a failed
// start. A live or unobservable process stays pending, since no evidence says
// it stopped consuming capacity; past AutoRepairAgentPoolPendingTimeout it
// also counts once as a failed start, which can trigger suppression but never
// frees its capacity. Each pending process is listed as the models.yaml list
// item it was started with. It also returns the explicit IDs pending
// processes will register, which new starts must not reuse.
func resolveAutoRepairPendingSpawns(state *models.State, cache map[string]time.Time, now time.Time) (map[string][]int, map[string]bool) {
	registered := make(map[string]bool, len(state.Agents))
	for _, agentState := range state.Agents {
		if agentState.PID > 0 {
			registered[fmt.Sprintf(registeredProcessKeyFormat, agentState.Role, agentState.PID)] = true
		}
	}
	pending := make(map[string][]int)
	pendingIDs := make(map[string]bool)
	for _, key := range slices.Sorted(maps.Keys(cache)) {
		entry, ok := strings.CutPrefix(key, autoRepairAgentPoolPendingPrefix)
		if !ok {
			continue
		}
		timeoutKey := autoRepairAgentPoolPendingTimeoutPrefix + entry
		_, timeoutCounted := cache[timeoutKey]
		parts := strings.Split(entry, autoRepairPendingEntrySeparator)
		pid, item := 0, 0
		if len(parts) == autoRepairPendingEntryFields {
			pid, _ = strconv.Atoi(parts[pendingEntryPID])
			item, _ = strconv.Atoi(parts[pendingEntryModelsItem])
		}
		if pid <= 0 || parts[pendingEntryRole] == "" {
			delete(cache, key)
			delete(cache, timeoutKey)
			continue
		}
		role, agentID := parts[pendingEntryRole], parts[pendingEntryAgentID]
		switch {
		case registered[fmt.Sprintf(registeredProcessKeyFormat, role, pid)]:
			delete(cache, key)
			delete(cache, timeoutKey)
			delete(cache, autoRepairAgentPoolStartCountPrefix+role)
		case autoRepairPendingProcessStatus(pid, role).IsDeadOrMismatched():
			delete(cache, key)
			delete(cache, timeoutKey)
			if !timeoutCounted {
				recordAutoRepairFailedStart(cache, role, now)
			}
		default:
			pending[role] = append(pending[role], item)
			if agentID != "" {
				pendingIDs[agentID] = true
			}
			if !timeoutCounted && now.Sub(cache[key]) >= AutoRepairAgentPoolPendingTimeout {
				cache[timeoutKey] = now
				recordAutoRepairFailedStart(cache, role, now)
			}
		}
	}
	return pending, pendingIDs
}

// autoRepairProviderBlockedAlerts raises one NO USABLE PROVIDER alert per
// episode: a role, list item and CLI blocked until one expiry. blocked must
// be the whole pool's blocked work for this tick; an episode absent from it
// has ended, so a later block of the same work alerts again.
func autoRepairProviderBlockedAlerts(blocked []ProviderBlockedRoleWork, cache map[string]time.Time, now time.Time) []Alert {
	current := make(map[string]bool, len(blocked))
	var out []Alert
	for _, work := range blocked {
		key := fmt.Sprintf("%s%s#%d@%s@%s", autoRepairAgentPoolProviderBlockedPrefix, work.Role, work.Item, work.CLI, work.Until.UTC().Format(time.RFC3339))
		current[key] = true
		if _, seen := cache[key]; seen {
			continue
		}
		cache[key] = now
		summary, hint := work.describe()
		out = append(out, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  alerts.CategoryNoUsableProvider.String(),
			Message:   summary + "; " + hint,
		})
	}
	for key := range cache {
		if strings.HasPrefix(key, autoRepairAgentPoolProviderBlockedPrefix) && !current[key] {
			delete(cache, key)
		}
	}
	return out
}

// autoRepairUnservableAlerts warns once per role while reviewer work stays
// unservable by the configured spawn CLI.
func autoRepairUnservableAlerts(unservable []UnservableRoleWork, cache map[string]time.Time, now time.Time) []Alert {
	current := make(map[string]bool, len(unservable))
	var out []Alert
	for _, work := range unservable {
		kind := work.Role
		if work.Item > 0 {
			kind += "#" + strconv.Itoa(work.Item)
		}
		current[kind] = true
		key := autoRepairAgentPoolUnservablePrefix + kind
		if _, seen := cache[key]; seen {
			continue
		}
		cache[key] = now
		remedy := fmt.Sprintf("start one with `%s`", brand.Command("agent", work.Role, "--cli", "<other-cli>"))
		if work.Item > 0 {
			remedy = "change that item in " + paths.ModelsFileName
		}
		out = append(out, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  alerts.CategoryAutoRepairUnservable.String(),
			Message: fmt.Sprintf("%d %s task(s) (%s) cannot be claimed by a new reviewer started with %s; auto repair does not start one. They wait for an eligible reviewer, or %s",
				len(work.TaskIDs), work.Role, strings.Join(work.TaskIDs, ", "), work.startedWith(), remedy),
		})
	}
	for key := range cache {
		if kind, ok := strings.CutPrefix(key, autoRepairAgentPoolUnservablePrefix); ok && !current[kind] {
			delete(cache, key)
		}
	}
	return out
}

func autoRepairStartCount(cache map[string]time.Time, role string) int {
	if cache == nil {
		return 0
	}
	stamp, ok := cache[autoRepairAgentPoolStartCountPrefix+role]
	if !ok {
		return 0
	}
	return stamp.Nanosecond()
}

func autoRepairCountTime(count int, now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), count, time.UTC)
}
