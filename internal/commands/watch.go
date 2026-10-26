package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/paths"
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
	// AutoRepairAgentPoolSuppressionCooldown bounds failed-start suppression
	// so transient launch failures do not strand demand until watcher restart.
	AutoRepairAgentPoolSuppressionCooldown = 5 * time.Minute
	// OrchestratorMissingGracePeriod absorbs launch ordering and supervisor
	// restarts before an absent orchestrator is announced.
	OrchestratorMissingGracePeriod = 60 * time.Second
)

// Task-state thresholds and key shapes the watch checks share.
const (
	// finalTaskAttempt is the last attempt a task gets; its limit warnings and
	// the reassignment alert name it.
	finalTaskAttempt = 2
	// hypothesisExhaustionFailures is how many failed agents exhaust a task's
	// hypotheses.
	hypothesisExhaustionFailures = 2
	// sentinelAssigneePrefix marks an assignee that is a lifecycle sentinel
	// rather than an agent ID.
	sentinelAssigneePrefix = "$"
	// alertIdentitySeparator joins the parts of an alert identity.
	alertIdentitySeparator = "|"
)

// watchNow is the clock for time-escalated checks; tests replace it.
var watchNow = time.Now

type AlertLevel = alerts.AlertLevel

const (
	AlertLevelWarning  = alerts.AlertLevelWarning
	AlertLevelCritical = alerts.AlertLevelCritical
)

type Alert = alerts.Alert

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
