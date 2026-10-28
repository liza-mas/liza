package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/process"
)

// repairAgentPoolSpawn starts one agent on cli; an empty agentID lets the
// agent auto-assign its ID. cliFromConfig starts it without --cli, so it
// resolves its models.yaml entry, model included, itself; a non-zero
// modelsItem picks that item of the role's entry list.
var repairAgentPoolSpawn = func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error) {
	var extraArgs []string
	if agentID != "" {
		extraArgs = []string{agentFlagAgentID, agentID}
	}
	if modelsItem > 0 {
		extraArgs = append(extraArgs, agentFlagModelsItem, strconv.Itoa(modelsItem))
	}
	spawn := process.SpawnAgent
	if cliFromConfig {
		spawn = process.SpawnConfiguredAgent
	}
	cmd, err := spawn(projectRoot, role, cli, extraArgs...)
	if err != nil {
		return 0, err
	}
	if cmd.Process == nil {
		return 0, nil
	}
	return cmd.Process.Pid, nil
}

// SetRepairAgentPoolSpawnForTest replaces the agent launcher so tests outside
// this package can observe the IDs agents are started under. It returns the
// restore function.
func SetRepairAgentPoolSpawnForTest(spawn func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error)) func() {
	previous := repairAgentPoolSpawn
	repairAgentPoolSpawn = spawn
	return func() { repairAgentPoolSpawn = previous }
}

func repairAgentPoolSpawnError(result *RepairAgentPoolResult) error {
	failures := make([]string, 0, len(result.Failed))
	for _, failure := range result.Failed {
		failures = append(failures, fmt.Sprintf("%s: %s", failure.Role, failure.Error))
	}
	planned := 0
	for _, roleWork := range result.Missing {
		planned += roleWork.SpawnCount
	}
	return fmt.Errorf("started %d of %d planned agent(s): %s",
		len(result.Spawned), planned, strings.Join(failures, "; "))
}

// executeRoleStarts starts every planned agent of role and records each outcome
// in result.
func executeRoleStarts(projectRoot, role string, plan roleStartPlan, result *RepairAgentPoolResult) {
	failedItems := make(map[int]bool)
	for _, start := range plan.starts {
		if failedItems[start.Item] {
			continue
		}
		pid, err := repairAgentPoolSpawn(projectRoot, role, start.CLI, start.AgentID, plan.spawnCLI == "", start.Item)
		if err != nil {
			// The next start of the same selection would fail the same
			// way; another list item may still start.
			result.Failed = append(result.Failed, FailedAgentSpawn{
				Role:    role,
				CLI:     start.CLI,
				Command: start.Command,
				Error:   err.Error(),
			})
			failedItems[start.Item] = true
			continue
		}
		start.PID = pid
		result.Spawned = append(result.Spawned, start)
	}
}
