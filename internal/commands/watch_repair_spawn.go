package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	lizalog "github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/paths"
)

// executeAutoRepairStarts starts the planned roles through RepairAgentPool and
// reports what it did: the roles attempted, the processes spawned and failed,
// and an AUTO REPAIR FAILED alert when RepairAgentPool returned an error.
func executeAutoRepairStarts(config WatchConfig, plan autoRepairPlan, outcome *AutoRepairAgentPoolOutcome) {
	outcome.AttemptedRoles = append(outcome.AttemptedRoles, plan.roles...)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot:     config.ProjectRoot,
		Roles:           plan.roles,
		PendingSpawns:   plan.pending,
		PendingAgentIDs: plan.pendingIDs,
	})
	now := time.Now().UTC()
	recordAutoRepairStarts(config, plan.roles, result, now, outcome)
	if err == nil {
		return
	}

	message := formatAutoRepairAgentPoolFailure(result, err)
	fmt.Fprintf(config.WarnWriter, "WARNING: %s\n", message)
	outcome.Alerts = append(outcome.Alerts, Alert{
		Timestamp: now,
		Level:     AlertLevelWarning,
		Category:  alerts.CategoryAutoRepairFailed.String(),
		Message:   message,
	})
}

// recordAutoRepairStarts stamps every attempted role, including failures, to
// avoid hammering a broken spawn path on every watch tick. This cache is
// process-local; agent registration and max-instances remain the cross-process
// safety net. Each started process stays pending until it registers or exits.
func recordAutoRepairStarts(config WatchConfig, roles []string, result *RepairAgentPoolResult, now time.Time, outcome *AutoRepairAgentPoolOutcome) {
	for _, role := range roles {
		config.StateCache[autoRepairAgentPoolCachePrefix+role] = now
	}
	if result == nil {
		return
	}
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

func spawnedAgentPIDs(spawned []SpawnedAgent) []int {
	pids := make([]int, 0, len(spawned))
	for _, agent := range spawned {
		if agent.PID > 0 {
			pids = append(pids, agent.PID)
		}
	}
	return pids
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
