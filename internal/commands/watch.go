package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/bits"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	lizalog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/rolemodels"
)

const (
	DefaultCheckInterval         = 10 * time.Second
	StallThreshold               = 30 * time.Minute
	StaleDraftThreshold          = 30 * time.Minute
	CheckpointStaleThreshold     = 30 * time.Minute
	CheckpointStuckThreshold     = 2 * time.Hour
	CheckpointAbandonedThreshold = 8 * time.Hour
	PauseStaleThreshold          = 30 * time.Minute
	PauseForgottenThreshold      = 2 * time.Hour
	StaleSentinelThreshold       = 2 * time.Minute
	AutoRepairAgentPoolBackoff   = 60 * time.Second
	AutoRepairAgentPoolMaxStarts = 3
	// AutoRepairAgentPoolPendingTimeout is how long a started agent process
	// may stay unregistered before it counts as a failed start. It keeps
	// covering demand until it registers or is seen to exit.
	AutoRepairAgentPoolPendingTimeout = 5 * time.Minute
	// OrchestratorMissingGracePeriod absorbs launch ordering and supervisor
	// restarts before an absent orchestrator is announced.
	OrchestratorMissingGracePeriod = 60 * time.Second
)

// watchNow is the clock for time-escalated checks; tests replace it.
var watchNow = time.Now

const stuckAlertCachePrefix = "stuck-alert:"
const invalidStateCategory = "INVALID STATE"
const autoRepairAgentPoolCachePrefix = "auto-repair-agent-pool:"
const autoRepairAgentPoolStartCountPrefix = "auto-repair-agent-pool-start-count:"
const autoRepairAgentPoolSuppressedPrefix = "auto-repair-agent-pool-suppressed:"
const autoRepairAgentPoolPendingPrefix = "auto-repair-agent-pool-pending:"
const autoRepairAgentPoolPendingTimeoutPrefix = "auto-repair-agent-pool-pending-timeout:"
const autoRepairAgentPoolUnservablePrefix = "auto-repair-agent-pool-unservable:"
const autoRepairAgentPoolProviderBlockedPrefix = "auto-repair-agent-pool-provider-blocked:"
const autoRepairAgentPoolEnvWarningKey = "auto-repair-agent-pool-env-warning"

// autoRepairModelsErrorKey holds the last models.yaml error alerted, so an
// invalid file raises one alert per distinct error rather than one per tick.
const autoRepairModelsErrorKey = "auto-repair-models-error:"
const orchestratorMissingSinceKey = "orchestrator-missing:since"
const orchestratorMissingAlertedKey = "orchestrator-missing:alerted"

type AlertLevel = alerts.AlertLevel

const (
	AlertLevelWarning  = alerts.AlertLevelWarning
	AlertLevelCritical = alerts.AlertLevelCritical
)

type Alert = alerts.Alert

// AlertSnapshot contains both newly emitted alerts and the complete set of
// currently active alert identities for consumers that need freshness state.
type AlertSnapshot struct {
	Alerts     []Alert
	ActiveKeys map[string]bool
}

type AutoRepairAgentPoolOutcome struct {
	Alerts          []Alert
	AttemptedRoles  []string
	SuppressedRoles []string
	// BlockedRoles have claimable work but no usable provider on this tick.
	BlockedRoles []string
	Spawned      []SpawnedAgent
	Failed       []FailedAgentSpawn
}

// ParseAlertLine parses a line written by Alert.String() back into an Alert.
// Returns the parsed alert and true on success, or zero value and false on
// malformed input.
//
// Format: [<RFC3339>] <level> <CATEGORY>: <message>
func ParseAlertLine(line string) (Alert, bool) {
	return alerts.ParseLine(line)
}

type WatchConfig struct {
	ProjectRoot              string
	CheckInterval            time.Duration
	AlertsLog                string
	WarnWriter               io.Writer
	RecentlySpawnedAgentPIDs []int
	// StateCache is used to track seen alerts across checks
	StateCache map[string]time.Time
}

func WatchCommand(ctx context.Context, config WatchConfig) error {
	if config.CheckInterval == 0 {
		config.CheckInterval = DefaultCheckInterval
	}
	lizaPaths := paths.New(config.ProjectRoot)
	if config.AlertsLog == "" {
		config.AlertsLog = lizaPaths.AlertsLogPath()
	}
	if config.StateCache == nil {
		config.StateCache = make(map[string]time.Time)
	}
	if config.WarnWriter == nil {
		config.WarnWriter = os.Stderr
	}

	fmt.Printf("[%s] Watching %s\n",
		time.Now().UTC().Format("15:04:05"),
		lizaPaths.LizaDir())

	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	if err := runChecks(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "Check error: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runChecks(ctx, config); err != nil {
				fmt.Fprintf(os.Stderr, "Check error: %v\n", err)
			}
		}
	}
}

func runChecks(ctx context.Context, config WatchConfig) error {
	lizaPaths := paths.New(config.ProjectRoot)
	statePath := lizaPaths.StatePath()

	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil
	}

	bb := db.For(statePath)
	state, err := bb.ReadSnapshot()
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	repairOutcome := RunAutoRepairAgentPool(ctx, state, config)
	config.RecentlySpawnedAgentPIDs = spawnedAgentPIDs(repairOutcome.Spawned)
	snapshot := RunChecksWithStateSnapshot(state, config)
	emitted := FilterAlertsAfterAutoRepair(snapshot.Alerts, repairOutcome)
	emitted = append(emitted, repairOutcome.Alerts...)

	for _, a := range emitted {
		if err := WriteAlert(config.AlertsLog, a); err != nil {
			var ledgerErr *alerts.LedgerUnavailableError
			if !errors.As(err, &ledgerErr) {
				return fmt.Errorf("failed to write alert: %w", err)
			}
			fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		}
		fmt.Fprintln(os.Stderr, a.String())
	}

	return nil
}

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
		// End blocked episodes, so re-enabling announces a still-blocked role.
		autoRepairProviderBlockedAlerts(nil, config.StateCache, time.Now().UTC())
		return outcome
	}

	// Keep auto-repair decoupled from the pure alert snapshot path used by
	// the TUI so successful repairs can suppress MISSING ROLE alerts cleanly.
	pr, err := ops.LoadResolverForModels(config.ProjectRoot)
	if err != nil {
		return outcome
	}

	now := time.Now().UTC()
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
				Category:  "AUTO REPAIR FAILED",
				Message:   err.Error(),
			})
		}
		return outcome
	}
	clearAutoRepairModelsError(config.StateCache)
	pending, pendingIDs := resolveAutoRepairPendingSpawns(state, config.StateCache, now)
	// Work whose start CLI is quota-blocked is set aside on every tick, before
	// the due filter below, so its alert episode is reconciled against the
	// whole pool rather than against the roles due for a start.
	repairCLI, blocked := RepairCLI{RoleModels: roleModels}, QuotaProviderBlock(config.ProjectRoot)
	capacity := FindRoleCapacity(state, pr, repairCLI, pendingIDs, blocked, now)
	missing, unservable, providerBlocked := capacity.Missing, capacity.Unservable, capacity.ProviderBlocked
	if roleWork, due := orchestratorRepairDue(state, pr, config.StateCache, now); due {
		if work, isBlocked := blockedOrchestratorWork(roleWork, repairCLI, state, pr, blocked); isBlocked {
			providerBlocked = append(providerBlocked, work)
		} else {
			missing = append(missing, roleWork)
		}
	}
	outcome.Alerts = append(outcome.Alerts, autoRepairProviderBlockedAlerts(providerBlocked, config.StateCache, now)...)
	for _, work := range providerBlocked {
		if !slices.Contains(outcome.BlockedRoles, work.Role) {
			outcome.BlockedRoles = append(outcome.BlockedRoles, work.Role)
		}
	}
	// Backoff and failed-start bookkeeping lives while a role has demand or
	// started processes pending; pending ones may already cover the demand.
	episodeRoles := make(map[string]bool, len(missing)+len(pending))
	for _, roleWork := range missing {
		episodeRoles[roleWork.Role] = true
	}
	for role := range pending {
		episodeRoles[role] = true
	}
	clearAutoRepairAgentPoolCache(config.StateCache, episodeRoles)
	missing = subtractPendingSpawns(missing, pending)
	outcome.Alerts = append(outcome.Alerts, autoRepairUnservableAlerts(unservable, config.StateCache, now)...)
	suppressedAlerts, suppressedRoles := autoRepairSuppressedAlerts(missing, config.StateCache, now)
	outcome.Alerts = append(outcome.Alerts, suppressedAlerts...)
	outcome.SuppressedRoles = append(outcome.SuppressedRoles, suppressedRoles...)
	roles := autoRepairDueRoles(missing, config.StateCache, now)
	if len(roles) == 0 {
		return outcome
	}
	outcome.AttemptedRoles = append(outcome.AttemptedRoles, roles...)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot:     config.ProjectRoot,
		Roles:           roles,
		PendingSpawns:   pending,
		PendingAgentIDs: pendingIDs,
	})
	now = time.Now().UTC()
	// Stamp every attempted role, including failures, to avoid hammering a
	// broken spawn path on every watch tick. This cache is process-local; agent
	// registration and max-instances remain the cross-process safety net.
	for _, role := range roles {
		config.StateCache[autoRepairAgentPoolCachePrefix+role] = now
	}
	if result != nil {
		outcome.Spawned = append(outcome.Spawned, result.Spawned...)
		outcome.Failed = append(outcome.Failed, result.Failed...)
		for _, spawned := range result.Spawned {
			if spawned.PID > 0 {
				config.StateCache[autoRepairPendingKey(spawned.Role, spawned.PID, spawned.AgentID, spawned.Item)] = now
			}
		}
		if logErr := logAutoRepairAgentPoolSpawn(config.ProjectRoot, result.Spawned); logErr != nil {
			fmt.Fprintf(config.WarnWriter, "WARNING: failed to log auto-repair spawn: %v\n", logErr)
		}
	}
	if err == nil {
		return outcome
	}

	message := formatAutoRepairAgentPoolFailure(result, err)
	fmt.Fprintf(config.WarnWriter, "WARNING: %s\n", message)
	outcome.Alerts = append(outcome.Alerts, Alert{
		Timestamp: now,
		Level:     AlertLevelWarning,
		Category:  "AUTO REPAIR FAILED",
		Message:   message,
	})
	return outcome
}

func clearAutoRepairModelsError(cache map[string]time.Time) {
	for key := range cache {
		if strings.HasPrefix(key, autoRepairModelsErrorKey) {
			delete(cache, key)
		}
	}
}

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

func spawnedAgentPIDs(spawned []SpawnedAgent) []int {
	pids := make([]int, 0, len(spawned))
	for _, agent := range spawned {
		if agent.PID > 0 {
			pids = append(pids, agent.PID)
		}
	}
	return pids
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
		if _, seen := cache[key]; seen {
			roles = append(roles, role)
			continue
		}
		cache[key] = now
		roles = append(roles, role)
		out = append(out, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  "AUTO REPAIR FAILED",
			Message: fmt.Sprintf("auto repair suppressed for role %s after %d started agent process(es) did not register; start the role manually or restart the TUI to retry",
				role, AutoRepairAgentPoolMaxStarts),
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
	return fmt.Sprintf("%s%s:%d:%s:%d", autoRepairAgentPoolPendingPrefix, role, pid, agentID, modelsItem)
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
			registered[fmt.Sprintf("%s:%d", agentState.Role, agentState.PID)] = true
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
		parts := strings.Split(entry, ":")
		pid, item := 0, 0
		if len(parts) == 4 {
			pid, _ = strconv.Atoi(parts[1])
			item, _ = strconv.Atoi(parts[3])
		}
		if pid <= 0 || parts[0] == "" {
			delete(cache, key)
			delete(cache, timeoutKey)
			continue
		}
		role, agentID := parts[0], parts[2]
		switch {
		case registered[fmt.Sprintf("%s:%d", role, pid)]:
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
			Category:  "NO USABLE PROVIDER",
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
			Category:  "AUTO REPAIR UNSERVABLE",
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

func logAutoRepairAgentPoolSpawn(projectRoot string, spawned []SpawnedAgent) error {
	if len(spawned) == 0 {
		return nil
	}
	logger := lizalog.New(paths.New(projectRoot).LogPath())
	for _, started := range spawned {
		detail := fmt.Sprintf("%s pid=%d", started.Command, started.PID)
		if started.PID == 0 {
			detail = started.Command
		}
		if err := logger.Append(lizalog.Entry{
			Agent:  "system",
			Action: "auto_repair_agent_spawned",
			Detail: detail,
		}); err != nil {
			return err
		}
	}
	return nil
}

func formatAutoRepairAgentPoolFailure(result *RepairAgentPoolResult, err error) string {
	if result == nil || len(result.Failed) == 0 {
		return fmt.Sprintf("auto repair agent pool failed: %v", err)
	}

	failures := make([]string, 0, len(result.Failed))
	for _, failed := range result.Failed {
		failures = append(failures, fmt.Sprintf("%s: %s", failed.Role, failed.Error))
	}

	spawned := make([]string, 0, len(result.Spawned))
	for _, started := range result.Spawned {
		spawned = append(spawned, started.Role)
	}

	message := fmt.Sprintf("auto repair agent pool failed for role(s): %s", strings.Join(failures, "; "))
	if len(spawned) > 0 {
		message += fmt.Sprintf("; already started role(s): %s", strings.Join(spawned, ", "))
	}
	return message
}

func FilterAlertsAfterAutoRepair(alertsIn []Alert, repairOutcome AutoRepairAgentPoolOutcome) []Alert {
	handledRoles := make(map[string]bool, len(repairOutcome.AttemptedRoles)+len(repairOutcome.SuppressedRoles)+len(repairOutcome.BlockedRoles))
	for _, role := range slices.Concat(repairOutcome.AttemptedRoles, repairOutcome.SuppressedRoles, repairOutcome.BlockedRoles) {
		handledRoles[role] = true
	}
	if len(handledRoles) == 0 {
		return alertsIn
	}

	filtered := alertsIn[:0]
	for _, alert := range alertsIn {
		if alert.Category == "MISSING ROLE" && missingRoleAlertHandled(alert, handledRoles) {
			continue
		}
		filtered = append(filtered, alert)
	}
	return filtered
}

func missingRoleAlertHandled(alert Alert, handledRoles map[string]bool) bool {
	for role := range handledRoles {
		if containsRoleToken(alert.Message, role) {
			return true
		}
	}
	return false
}

func containsRoleToken(message, role string) bool {
	needle := "role " + role
	for offset := 0; offset < len(message); {
		idx := strings.Index(message[offset:], needle)
		if idx < 0 {
			return false
		}
		end := offset + idx + len(needle)
		if end == len(message) || isRoleTokenBoundary(message[end]) {
			return true
		}
		offset = end
	}
	return false
}

func isRoleTokenBoundary(ch byte) bool {
	return !((ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_')
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

	var alerts []Alert

	// Load pipeline resolver once for checks that need it.
	pr, prErr := ops.LoadResolverForModels(config.ProjectRoot)
	pipelineCacheKey := "pipeline-config-error"
	if prErr != nil && !errors.Is(prErr, pipeline.ErrConfigNotFound) {
		// Malformed config: emit one-time alert, don't spam every 10s tick.
		if _, seen := config.StateCache[pipelineCacheKey]; !seen {
			alerts = append(alerts, Alert{
				Timestamp: time.Now().UTC(),
				Level:     AlertLevelWarning,
				Category:  "PIPELINE CONFIG",
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
	lizaPaths := paths.New(config.ProjectRoot)
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
		alerts = append(alerts, check()...)
	}

	alerts = append(alerts, checkCircuitBreakerEscalation(state, config.StateCache)...)
	alerts = append(alerts, checkSprintStalled(state, config.StateCache)...)

	statePath := lizaPaths.StatePath()
	if err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck:        true,
		WarnWriter:               config.WarnWriter,
		RecentlySpawnedAgentPIDs: config.RecentlySpawnedAgentPIDs,
	}); err != nil {
		alerts = append(alerts, Alert{
			Timestamp: time.Now().UTC(),
			Level:     AlertLevelCritical,
			Category:  invalidStateCategory,
			Message:   err.Error(),
		})
	}

	activeKeys := activeAlertKeys(alerts)
	return AlertSnapshot{
		Alerts:     reconcileStuckAlerts(alerts, config.StateCache),
		ActiveKeys: activeKeys,
	}
}

func activeAlertKeys(alerts []Alert) map[string]bool {
	keys := make(map[string]bool, len(alerts))
	for _, alert := range alerts {
		if !isFreshnessTrackedAlertCategory(alert.Category) {
			continue
		}
		keys[AlertKey(alert)] = true
	}
	return keys
}

// AlertKey returns a stable identity for an alert condition.
func AlertKey(alert Alert) string {
	return alerts.Key(alert)
}

func isFreshnessTrackedAlertCategory(category string) bool {
	// Only track categories that are recomputed and emitted on every check while
	// active, or are deduped after building a full active set. Categories with
	// internal throttle caches, such as MISSING ROLE or STALE SENTINEL, need their
	// checks to expose active identities before the TUI can safely resolve them.
	if isStuckAlertCategory(category) {
		return true
	}
	switch category {
	case "BLOCKED", "LEASE EXPIRED", "REVIEW LEASE EXPIRED":
		return true
	default:
		return false
	}
}

// reconcileStuckAlerts emits each condition once while it stays active: an
// alert whose gate identity was active on the previous check is suppressed, and
// an identity absent from a check retires, so a condition that resolves and
// recurs alerts again.
//
// INVALID STATE is the exception: validation reports only its first error, so
// an error missing from a failing validation may merely be masked. Its
// identities retire only on a clean validation (no INVALID STATE alert).
// Limitation: an error that resolves and recurs while another error persists
// throughout is not re-announced until a clean validation intervenes.
func reconcileStuckAlerts(alerts []Alert, cache map[string]time.Time) []Alert {
	if cache == nil {
		return alerts
	}

	now := time.Now().UTC()
	activeKeys := make(map[string]bool)
	deduped := make([]Alert, 0, len(alerts))
	validationClean := true

	for _, alert := range alerts {
		if alert.Category == invalidStateCategory {
			validationClean = false
		}
		key := stuckAlertCacheKey(alert)
		activeKeys[key] = true
		if _, seen := cache[key]; seen {
			continue
		}
		cache[key] = now
		deduped = append(deduped, alert)
	}

	invalidStatePrefix := stuckAlertCachePrefix + invalidStateCategory + ":"
	for key := range cache {
		if !strings.HasPrefix(key, stuckAlertCachePrefix) || activeKeys[key] {
			continue
		}
		if !validationClean && strings.HasPrefix(key, invalidStatePrefix) {
			continue
		}
		delete(cache, key)
	}

	return deduped
}

func isStuckAlertCategory(category string) bool {
	switch category {
	case "AWAITING HUMAN", "BLOCKED", "HYPOTHESIS EXHAUSTION", "INTEGRATION FAILED", invalidStateCategory, "DEAD AGENT PROCESS", "REGISTERED AGENT PROCESS":
		return true
	case "INVALID AGENT OWNERSHIP":
		return true
	default:
		return false
	}
}

func stuckAlertCacheKey(alert Alert) string {
	return stuckAlertCachePrefix + alerts.GateKey(alert)
}

func checkCircuitBreakerEscalation(state *models.State, cache map[string]time.Time) []Alert {
	mode := state.Config.Mode
	if mode == "" {
		mode = models.SystemModeRunning
	}

	// Only check during active execution.
	if mode != models.SystemModeRunning || state.Sprint.Status != models.SprintStatusInProgress {
		delete(cache, "circuit_breaker:alert")
		return nil
	}

	// Keep both checks: manual edits or interrupted writes can leave one field stale.
	// Either value indicates a previously triggered circuit-breaker state.
	if state.CircuitBreaker.Status == "TRIGGERED" || state.CircuitBreaker.CurrentTrigger != nil {
		delete(cache, "circuit_breaker:alert")
		return nil
	}

	patternResult, _, _ := analysis.DetectUnacknowledgedPatterns(state)
	if !patternResult.Triggered {
		delete(cache, "circuit_breaker:alert")
		return nil
	}

	// Throttle: only alert once per triggered period.
	if _, seen := cache["circuit_breaker:alert"]; seen {
		return nil
	}

	cache["circuit_breaker:alert"] = time.Now().UTC()
	return []Alert{{
		Timestamp: time.Now().UTC(),
		Level:     AlertLevelCritical,
		Category:  "CIRCUIT BREAKER",
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
		delete(cache, "sprint_stalled:alert")
		return nil
	}

	if !state.SprintStalled() {
		delete(cache, "sprint_stalled:alert")
		return nil
	}

	// Throttle: only alert once per stall event within a single IN_PROGRESS period.
	// The sprint status guard above resets the throttle across checkpoint/resume cycles.
	if _, seen := cache["sprint_stalled:alert"]; seen {
		return nil
	}

	blockedCount := 0
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task != nil && task.Status == models.TaskStatusBlocked {
			blockedCount++
		}
	}

	cache["sprint_stalled:alert"] = time.Now().UTC()
	return []Alert{{
		Timestamp: time.Now().UTC(),
		Level:     AlertLevelCritical,
		Category:  "SPRINT STALLED",
		Message: fmt.Sprintf("all %d non-terminal planned tasks are BLOCKED",
			blockedCount),
	}}
}

func checkExpiredLeases(state *models.State) []Alert {
	var alerts []Alert
	now := time.Now().UTC()
	graceDeadline := now.Add(-models.LeaseExpiryGracePeriod)

	for agentID, agent := range state.Agents {
		if agent.CurrentTask == nil {
			continue
		}
		if agent.LeaseExpires == nil {
			continue
		}
		if agent.LeaseExpires.Before(graceDeadline) {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "LEASE EXPIRED",
				Message:   fmt.Sprintf("%s on %s", agentID, *agent.CurrentTask),
				// A different lease expiring is a new condition even when no
				// renewal was observed in between.
				Identity: fmt.Sprintf("%s on %s|%s", agentID, *agent.CurrentTask, agent.LeaseExpires.UTC().Format(time.RFC3339Nano)),
			})
		}
	}

	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusReviewing {
			continue
		}
		if task.ReviewingBy == nil {
			continue
		}
		if task.ReviewLeaseExpires == nil {
			continue
		}
		if task.ReviewLeaseExpires.Before(graceDeadline) {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "REVIEW LEASE EXPIRED",
				Message:   fmt.Sprintf("%s on %s — review can be reclaimed", *task.ReviewingBy, task.ID),
			})
		}
	}

	return alerts
}

func checkRegisteredAgentsWithoutLiveProcess(state *models.State) []Alert {
	var alerts []Alert
	now := time.Now().UTC()
	for agentID, agent := range state.Agents {
		if agent.LeaseExpires == nil || !agent.LeaseExpires.After(now) {
			continue
		}
		if agent.CurrentTask != nil && *agent.CurrentTask != "" {
			switch agent.Status {
			case models.AgentStatusWorking, models.AgentStatusReviewing, models.AgentStatusHandoff:
				continue
			}
		}
		observation := ops.AgentProcessOwnership(agentID, agent, now)
		if observation.Raw.IsLiveOrUnknown() {
			continue
		}
		processDescription := "no live process"
		if observation.Raw.State == "mismatched" {
			processDescription = "mismatched process"
		}
		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  "REGISTERED AGENT PROCESS",
			Message: fmt.Sprintf("agent %s has active lease but %s; %s",
				agentID, processDescription, observation.Diagnostic(agent.PID)),
		})
	}
	return alerts
}

func checkRunningTasksWithoutLiveProcess(state *models.State, pr models.PipelineResolver) []Alert {
	if pr == nil {
		return nil
	}

	var alerts []Alert
	now := time.Now().UTC()
	skipReverseAgentIDs := make(map[string]bool)

	for i := range state.Tasks {
		task := &state.Tasks[i]
		ownerID, ownerKind, ok := runningTaskOwner(task, pr)
		if !ok {
			continue
		}

		agent, exists := state.Agents[ownerID]
		if !exists {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "DEAD AGENT PROCESS",
				Message: fmt.Sprintf("%s — status %s has %s %s but no registered agent",
					task.ID, task.Status, ownerKind, ownerID),
			})
			continue
		}
		if reason := activeTaskOwnerMismatch(state, task, ownerID, ownerKind, agent, pr); reason != "" {
			skipReverseAgentIDs[ownerID] = true
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "INVALID AGENT OWNERSHIP",
				Message: fmt.Sprintf("%s — status %s has %s %s with invalid agent row: %s",
					task.ID, task.Status, ownerKind, ownerID, reason),
			})
		}
		processStatus := ops.AgentProcessStatus(ownerID, agent)
		if processStatus.IsLiveOrUnknown() {
			continue
		}
		processDescription := "no live process"
		if processStatus.State == "mismatched" {
			processDescription = "mismatched process"
		}

		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  "DEAD AGENT PROCESS",
			Message: fmt.Sprintf("%s — status %s has %s %s but %s (pid %d: %s)",
				task.ID, task.Status, ownerKind, ownerID, processDescription, agent.PID, processStatus.Detail),
		})
	}

	alerts = append(alerts, checkReverseActiveAgentOwnership(state, pr, now, skipReverseAgentIDs)...)

	return alerts
}

func activeTaskOwnerMismatch(state *models.State, task *models.Task, ownerID string, ownerKind string, agent models.Agent, pr models.PipelineResolver) string {
	if ownerKind == "doer" {
		return models.ActiveDoerOwnershipReason(state, task, ownerID, pr)
	}

	expectedRole, ok := expectedOwnerRole(task, ownerKind, pr)
	if ok && agent.Role != expectedRole {
		return fmt.Sprintf("agent role %q, want %q", agent.Role, expectedRole)
	}

	expectedStatus, ok := expectedOwnerStatus(ownerKind)
	if ok && agent.Status != expectedStatus {
		return fmt.Sprintf("agent status %s, want %s", agent.Status, expectedStatus)
	}

	if agent.CurrentTask == nil {
		return fmt.Sprintf("current_task <none>, want %q", task.ID)
	}
	if *agent.CurrentTask != task.ID {
		return fmt.Sprintf("current_task %q, want %q", *agent.CurrentTask, task.ID)
	}

	return ""
}

func expectedOwnerRole(task *models.Task, ownerKind string, pr models.PipelineResolver) (string, bool) {
	switch ownerKind {
	case "doer":
		role, err := pr.DoerRole(task.RolePair)
		return role, err == nil
	case "reviewer":
		role, err := pr.ReviewerRole(task.RolePair)
		return role, err == nil
	default:
		return "", false
	}
}

func expectedOwnerStatus(ownerKind string) (models.AgentStatus, bool) {
	switch ownerKind {
	case "doer":
		return models.AgentStatusWorking, true
	case "reviewer":
		return models.AgentStatusReviewing, true
	default:
		return "", false
	}
}

func checkReverseActiveAgentOwnership(state *models.State, pr models.PipelineResolver, now time.Time, skipAgentIDs map[string]bool) []Alert {
	tasksByID := make(map[string]*models.Task, len(state.Tasks))
	for i := range state.Tasks {
		task := &state.Tasks[i]
		tasksByID[task.ID] = task
	}

	var alerts []Alert
	for agentID, agent := range state.Agents {
		if skipAgentIDs[agentID] {
			continue
		}
		if agent.CurrentTask == nil || *agent.CurrentTask == "" {
			continue
		}
		if models.IsOrchestratorAgent(agent, pr) {
			continue
		}
		taskID := *agent.CurrentTask
		task, exists := tasksByID[taskID]
		if !exists {
			alerts = append(alerts, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID, "task is missing"))
			continue
		}

		switch agent.Status {
		case models.AgentStatusWorking:
			if !models.IsExecutingStatus(task, pr) {
				alerts = append(alerts, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task status %s is not executing", task.Status)))
				continue
			}
			if task.AssignedTo == nil || *task.AssignedTo != agentID {
				alerts = append(alerts, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task has doer %s", ownerValue(task.AssignedTo))))
			}
		case models.AgentStatusReviewing:
			if !isReviewerActiveStatus(task, pr) {
				alerts = append(alerts, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task status %s is not active review", task.Status)))
				continue
			}
			if task.ReviewingBy == nil || *task.ReviewingBy != agentID {
				alerts = append(alerts, invalidReverseOwnershipAlert(now, agentID, agent.Status, taskID,
					fmt.Sprintf("task has reviewer %s", ownerValue(task.ReviewingBy))))
			}
		}
	}

	return alerts
}

func invalidReverseOwnershipAlert(now time.Time, agentID string, status models.AgentStatus, taskID string, reason string) Alert {
	return Alert{
		Timestamp: now,
		Level:     AlertLevelCritical,
		Category:  "INVALID AGENT OWNERSHIP",
		Message:   fmt.Sprintf("agent %s says %s %s, but %s", agentID, status, taskID, reason),
	}
}

func ownerValue(owner *string) string {
	if owner == nil || *owner == "" {
		return "<none>"
	}
	return *owner
}

func runningTaskOwner(task *models.Task, pr models.PipelineResolver) (string, string, bool) {
	if models.IsExecutingStatus(task, pr) {
		if task.AssignedTo == nil || *task.AssignedTo == "" {
			return "", "", false
		}
		if strings.HasPrefix(*task.AssignedTo, "$") {
			return "", "", false
		}
		return *task.AssignedTo, "doer", true
	}
	if isReviewerActiveStatus(task, pr) {
		if task.ReviewingBy == nil || *task.ReviewingBy == "" {
			return "", "", false
		}
		return *task.ReviewingBy, "reviewer", true
	}
	return "", "", false
}

func isReviewerActiveStatus(task *models.Task, pr models.PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	reviewing, err := pr.ReviewingStatus(task.RolePair)
	if err == nil && task.Status == reviewing {
		return true
	}
	reviewing2, err := pr.Reviewing2Status(task.RolePair)
	return err == nil && task.Status == reviewing2
}

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
	var alerts []Alert
	now := time.Now().UTC()
	if notice := AwaitingHumanNotice(state); notice != "" {
		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  "AWAITING HUMAN",
			Message:   notice,
		})
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if alert, ok := blockedAwaitingHumanAlert(task, now); ok {
			alerts = append(alerts, alert)
			continue
		}
		if task.Status != models.TaskStatusMerged || task.PlanCheckVerdictOf() != models.PlanCheckHeld {
			continue
		}
		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  "AWAITING HUMAN",
			Message: fmt.Sprintf("plan %s held before its children exist: %s; after doing it, run %q",
				task.ID, task.PlanCheck.Ask, brand.Command("plan-check", task.ID, "--clear")),
		})
	}
	return alerts
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
		Category:  "AWAITING HUMAN",
		Message:   fmt.Sprintf("%s blocked on a human action: %s", task.ID, human.Ask),
		Identity:  key,
		OnceKey:   key,
	}, true
}

func checkBlockedTasks(state *models.State, cache map[string]time.Time) []Alert {
	var blocked []Alert
	now := time.Now().UTC()

	// BLOCKED alerts are emitted every check so RunChecksWithStateSnapshot can
	// compute active freshness keys before reconcileStuckAlerts dedupes log writes.
	// Clear legacy cache keys from the previous one-shot implementation.
	for key := range cache {
		if strings.HasPrefix(key, "blocked:") {
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
			Category:  "BLOCKED",
			Message:   message,
		}
		// The blocked history entry names the episode: a new one alerts even
		// when no unblocked check was observed, and its key lets MarkBlocked
		// and every watcher log one line per episode between them.
		if episodeStart := models.LatestHistoryTime(&task, models.TaskEventBlocked); !episodeStart.IsZero() {
			alert.Identity = message + "|" + episodeStart.UTC().Format(time.RFC3339Nano)
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
	var alerts []Alert
	now := time.Now().UTC()

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !isRejectedStatus(task, pr) {
			continue
		}
		// A sentinel assignee (e.g. "$transitioning") is a transition in
		// progress, not an orphaned assignment.
		if task.AssignedTo == nil || strings.HasPrefix(*task.AssignedTo, "$") {
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
		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelCritical,
			Category:  "ORPHANED REJECTED",
			Message: fmt.Sprintf("%s — assigned to %s but agent is %s (no rework %dm+ after verdict)",
				task.ID, assignee, agentStatus, int(models.VerdictHandoffGrace.Minutes())),
			Identity: fmt.Sprintf("%s|%s|%s", task.ID, assignee, verdictAt.UTC().Format(time.RFC3339Nano)),
		})
	}

	return alerts
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
	var alerts []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.ReviewCyclesCurrent >= 5 {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "REVIEW LOOP",
				Message:   fmt.Sprintf("%s — %d cycles (at cliff)", task.ID, task.ReviewCyclesCurrent),
			})
		}
	}

	return alerts
}

func checkIntegrationFailures(state *models.State, projectRoot string) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status == models.TaskStatusIntegrationFailed {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "INTEGRATION FAILED",
				Message:   integrationFailureAlertMessage(&task, projectRoot),
			})
		}
	}

	return alerts
}

func checkHypothesisExhaustion(state *models.State) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.Status == models.TaskStatusBlocked {
			continue
		}
		if len(task.FailedBy) >= 2 {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "HYPOTHESIS EXHAUSTION",
				Message:   fmt.Sprintf("%s — requires rescope", task.ID),
				// Another failed doer is a new condition.
				Identity: task.ID + "|" + strings.Join(slices.Sorted(slices.Values(task.FailedBy)), ","),
			})
		}
	}

	return alerts
}

func checkReassigned(state *models.State, cache map[string]time.Time) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if task.EffectiveAttempt() != 2 {
			continue
		}

		cacheKey := "attempt2:" + task.ID
		if _, seen := cache[cacheKey]; !seen {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "ATTEMPT",
				Message:   fmt.Sprintf("%s — attempt 2 (final attempt)", task.ID),
			})
			cache[cacheKey] = now
		}
	}

	return alerts
}

func checkApproachingLimits(state *models.State, resolver models.PipelineResolver) []Alert {
	var alerts []Alert
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
		if task.Status == executing && task.Iteration > 0 && task.Iteration >= iterationLimit-2 && task.Iteration < iterationLimit {
			var msg string
			if attemptNum == 2 {
				msg = fmt.Sprintf("%s — attempt 2 (final), iteration %d/%d", task.ID, task.Iteration, iterationLimit)
			} else {
				msg = fmt.Sprintf("%s — attempt %d, iteration %d/%d", task.ID, attemptNum, task.Iteration, iterationLimit)
			}
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "APPROACHING LIMIT",
				Message:   msg,
			})
		}

		if task.ReviewCyclesCurrent > 0 && task.ReviewCyclesCurrent >= reviewLimit-2 && task.ReviewCyclesCurrent < reviewLimit {
			var msg string
			if attemptNum == 2 {
				msg = fmt.Sprintf("%s — attempt 2 (final), review cycle %d/%d", task.ID, task.ReviewCyclesCurrent, reviewLimit)
			} else {
				msg = fmt.Sprintf("%s — attempt %d, review cycle %d/%d", task.ID, attemptNum, task.ReviewCyclesCurrent, reviewLimit)
			}
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "APPROACHING LIMIT",
				Message:   msg,
			})
		}
	}

	return alerts
}

func checkStaleSentinels(state *models.State, cache map[string]time.Time) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	activeSentinels := make(map[string]bool)

	for _, task := range state.Tasks {
		if task.AssignedTo == nil || !strings.HasPrefix(*task.AssignedTo, "$") {
			continue
		}
		activeSentinels[task.ID] = true

		cacheKey := "sentinel:" + task.ID
		firstSeen, seen := cache[cacheKey]
		if !seen {
			cache[cacheKey] = now
			continue
		}
		if now.Sub(firstSeen) > StaleSentinelThreshold {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "STALE SENTINEL",
				Message:   fmt.Sprintf("%s stuck in transition — manual repair needed", task.ID),
				Identity:  task.ID + "|" + *task.AssignedTo,
			})
		}
	}

	// Clear cache entries for sentinels that resolved.
	for key := range cache {
		if !strings.HasPrefix(key, "sentinel:") {
			continue
		}
		taskID := strings.TrimPrefix(key, "sentinel:")
		if !activeSentinels[taskID] {
			delete(cache, key)
		}
	}

	return alerts
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
	var alerts []Alert
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
		return alerts
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
		return alerts
	}

	age := now.Sub(latestProgress)
	if age <= StallThreshold {
		return alerts
	}

	// Step k covers ages in [30m·2^k, 30m·2^(k+1)).
	step := bits.Len64(uint64(age/StallThreshold)) - 1
	return append(alerts, Alert{
		Timestamp: now,
		Level:     AlertLevelWarning,
		Category:  "STALLED",
		Message:   fmt.Sprintf("no task progress for %d minutes%s", int(age.Minutes()), stallDiagnosis(state, pr, now)),
		Identity:  fmt.Sprintf("%s|step %d", latestProgress.UTC().Format(time.RFC3339Nano), step),
	})
}

// stallDiagnosis explains a stall in the terms that decide what to do about it.
//
// "No task progress" is the same sentence whether work is unstaffed or being
// refused, and those need opposite responses: spawn capacity, or read why the
// claim fails. Deriving that by hand from supervisor logs cost five hours and
// forty minutes on 2026-09-20, while validate, analyze and anomalies all read
// healthy. Counts come from models.GetTaskReadiness, so they agree with what
// repair-agent-pool would act on rather than inventing a second notion of
// claimable.
//
// Returns "" when no useful distinction can be drawn, leaving the bare message.
//
// now is the lease/heartbeat clock for agent liveness only: models.GetTaskReadiness
// reads wall time internally, so a test that moves now shifts idle-capacity
// liveness without shifting readiness. They agree in production, where both are
// real time; do not build an injected-clock test on the assumption they move
// together.
func stallDiagnosis(state *models.State, pr models.PipelineResolver, now time.Time) string {
	if state == nil || pr == nil {
		return ""
	}
	readiness := models.GetTaskReadiness(state, pr)
	if readiness.Claimable == 0 && readiness.Reviewable == 0 {
		blocked, waiting := countStallHolds(state, pr, now)
		human := ""
		if ids := humanOwnedBlockers(state); len(ids) > 0 {
			human = "; awaiting human: " + strings.Join(ids, ", ")
		}
		switch {
		case blocked > 0 && waiting > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) held (read blocked_reason), %d task(s) waiting on dependencies%s", blocked, waiting, human)
		case blocked > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) held (read blocked_reason)%s", blocked, human)
		case waiting > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) waiting on dependencies", waiting)
		}
		return " — no claimable work"
	}

	idle := idleAgentsByRole(state, now)
	var refused, unstaffed []string
	for _, role := range append(slices.Clone(readiness.ClaimableByRole), readiness.ReviewableByRole...) {
		if role.Count == 0 {
			continue
		}
		if n := idle[role.Role]; n > 0 {
			refused = append(refused, fmt.Sprintf("%d for %s with %d idle", role.Count, role.Role, n))
			continue
		}
		unstaffed = append(unstaffed, fmt.Sprintf("%d for %s", role.Count, role.Role))
	}

	reason := "read supervisor logs for the refusal reason"
	if failed := failedValidationPreflights(state, pr, now); len(failed) > 0 {
		reason = "validation preflight failed for " + strings.Join(failed, ", ")
	}
	switch {
	case len(refused) > 0 && len(unstaffed) > 0:
		return fmt.Sprintf(" — claims refused (%s) and unstaffed (%s): %s",
			strings.Join(refused, ", "), strings.Join(unstaffed, ", "), reason)
	case len(refused) > 0:
		return fmt.Sprintf(" — %s: claims are being refused, not unstaffed; %s",
			strings.Join(refused, ", "), reason)
	case len(unstaffed) > 0:
		return fmt.Sprintf(" — %s: no live agent for that role", strings.Join(unstaffed, ", "))
	default:
		return ""
	}
}

// failedValidationPreflights names the idle, live agents whose current
// preflight of a task they could claim failed, as "task (agent: code)". Only
// observations CurrentValidationReadiness accepts count: an expired or
// superseded record is history, not the reason for this refusal.
func failedValidationPreflights(state *models.State, pr models.PipelineResolver, now time.Time) []string {
	window := agentLivenessWindow(state.Config)
	var failed []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if len(task.ValidationPrerequisites) == 0 {
			continue
		}
		for agentID, agentState := range state.Agents {
			if agentState.Status != models.AgentStatusIdle || !agentHasLiveRegistration(agentState, now, window) ||
				!models.IsRoleTaskReady(state, task, agentState.Role, pr, now) {
				continue
			}
			if record, ok := models.CurrentValidationReadiness(state, task, agentID, now); ok && record.Result == "failed" {
				failed = append(failed, fmt.Sprintf("%s (%s: %s)", task.ID, agentID, record.Code))
			}
		}
	}
	slices.Sort(failed)
	return failed
}

// idleAgentsByRole counts live, healthy, idle agents per role — the capacity
// that should have claimed the ready work and did not.
func idleAgentsByRole(state *models.State, now time.Time) map[string]int {
	idle := make(map[string]int)
	window := agentLivenessWindow(state.Config)
	for agentID, agentState := range state.Agents {
		if agentState.Status != models.AgentStatusIdle {
			continue
		}
		if !agentHasLiveRegistration(agentState, now, window) {
			continue
		}
		if agentHealthIsCurrentDegraded(state.AgentHealth[agentID], agentState) {
			continue
		}
		idle[agentState.Role]++
	}
	return idle
}

// countStallHolds separates the two reasons nothing is claimable, because they
// demand opposite responses and IsTerminal does not tell them apart: BLOCKED is
// not terminal, so a held task is otherwise indistinguishable from one waiting
// on its graph. Calling a BLOCKED task "waiting on dependencies" tells
// the operator there is nothing to do in exactly the case where they must read
// the reason and intervene.
func countStallHolds(state *models.State, pr models.PipelineResolver, now time.Time) (blocked, waiting int) {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status.IsTerminal() || task.RolePair == "" {
			continue
		}
		// Only BLOCKED lands here. INTEGRATION_FAILED is also non-terminal and
		// also needs attention, but it stays claimable — the doer re-claims it
		// through the integration-fix path — so readiness already reports it as
		// refused or unstaffed, which says more than "held" would.
		if task.Status == models.TaskStatusBlocked {
			blocked++
			continue
		}
		doerRole, err := pr.DoerRole(task.RolePair)
		if err != nil {
			continue
		}
		if !models.IsRoleTaskReady(state, task, doerRole, pr, now) && task.AssignedTo == nil {
			waiting++
		}
	}
	return blocked, waiting
}

// humanOwnedBlockers names the BLOCKED tasks only a human can clear, sorted, so
// a stall caused by them says who must act rather than "read blocked_reason".
func humanOwnedBlockers(state *models.State) []string {
	var ids []string
	for i := range state.Tasks {
		if _, ok := models.CurrentAwaitingHuman(&state.Tasks[i]); ok {
			ids = append(ids, state.Tasks[i].ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func checkStaleDrafts(state *models.State) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusDraft {
			continue
		}

		age := now.Sub(task.Created)
		if age > StaleDraftThreshold {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelWarning,
				Category:  "STALE DRAFT",
				Message: fmt.Sprintf("%s — created %dmin ago, never finalized (Orchestrator crash?)",
					task.ID, int(age.Minutes())),
				// The message carries the age; the draft is the condition.
				Identity: task.ID + "|" + task.Created.UTC().Format(time.RFC3339Nano),
			})
		}
	}

	return alerts
}

func checkImmediateDiscoveries(state *models.State) []Alert {
	var alerts []Alert
	now := time.Now().UTC()

	for _, disc := range state.Discovered {
		if disc.Urgency == "immediate" && disc.ConvertedToTask == nil {
			alerts = append(alerts, Alert{
				Timestamp: now,
				Level:     AlertLevelCritical,
				Category:  "IMMEDIATE DISCOVERY",
				Message:   fmt.Sprintf("%s — %s (Orchestrator should wake)", disc.ID, disc.Description),
			})
		}
	}

	return alerts
}

// checkMissingRoles alerts when claimable tasks exist but no agent of the
// required role is registered. This catches a common first-user mistake (e.g.,
// starting only a coder but not a code-planner).
//
// Design trade-off: Uses IsClaimable which checks both status AND dependency
// satisfaction, so this only alerts when tasks are *immediately* stuck. Tasks
// blocked by unmet deps won't trigger an alert even if the needed role is
// missing — the alert fires later when deps resolve. This is conservative
// (fewer false positives) at the cost of delayed detection.
func checkMissingRoles(state *models.State, pr models.PipelineResolver, roleModels rolemodels.File, cache map[string]time.Time) []Alert {
	if pr == nil {
		return nil
	}

	// Emit alerts for each missing role, throttled by cache.
	var alerts []Alert
	now := time.Now().UTC()

	missingRoles := FindMissingRolesWithClaimableWork(state, pr, roleModels)
	missingRoleSet := make(map[string]bool, len(missingRoles))
	for _, roleWork := range missingRoles {
		role := roleWork.Role
		taskIDs := roleWork.TaskIDs
		missingRoleSet[role] = true
		cacheKey := "missing-role:" + role
		if _, seen := cache[cacheKey]; seen {
			continue
		}

		// Format task list, capping at 5 IDs.
		const maxListed = 5
		listed := taskIDs
		suffix := ""
		if len(taskIDs) > maxListed {
			listed = taskIDs[:maxListed]
			suffix = fmt.Sprintf("... and %d more", len(taskIDs)-maxListed)
		}
		msg := fmt.Sprintf("no registered agent for role %s — %d task(s) waiting (%s",
			role, roleWork.TaskCount, strings.Join(listed, ", "))
		if suffix != "" {
			msg += ", " + suffix
		}
		msg += fmt.Sprintf("); the TUI auto-repairs by default; run `%s` to preview manually", brand.Command("repair-agent-pool", "--dry-run"))

		alerts = append(alerts, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  "MISSING ROLE",
			Message:   msg,
		})
		cache[cacheKey] = now
	}

	// Clear cache entries for roles no longer in the missing set — either because
	// an agent appeared or because the waiting tasks stopped being claimable
	// (merged, abandoned, deps unmet, etc.). Without this, a stale cache entry
	// would suppress the alert if a *new* task later becomes claimable for the
	// same absent role.
	for key := range cache {
		if !strings.HasPrefix(key, "missing-role:") {
			continue
		}
		role := strings.TrimPrefix(key, "missing-role:")
		if !missingRoleSet[role] {
			delete(cache, key)
		}
	}

	return alerts
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
		if roleType, err := pr.RoleType(role); err == nil && roleType == "orchestrator" {
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
		observation := ops.AgentProcessOwnership(agentID, agent, now)
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
		Category:  "ORCHESTRATOR MISSING",
		Message:   msg,
	}}
}

func endOrchestratorMissingEpisode(cache map[string]time.Time) {
	delete(cache, orchestratorMissingSinceKey)
	delete(cache, orchestratorMissingAlertedKey)
}

// WriteAlert appends an alert to the alerts log file.
func WriteAlert(alertsLog string, a Alert) error {
	return alerts.Write(alertsLog, a)
}
