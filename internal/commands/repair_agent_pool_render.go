package commands

import (
	"fmt"
	"strings"
)

// printRepairAgentPoolResult renders the capacity observations, then the work
// not covered, then what was started (or would be). A blank line separates a
// section from the missing-role list that follows it.
func printRepairAgentPoolResult(result *RepairAgentPoolResult) {
	if len(result.Missing) == 0 && len(result.Degraded) == 0 && len(result.Validation) == 0 && len(result.Unservable) == 0 && len(result.ProviderBlocked) == 0 {
		fmt.Println("No role needs more agents for claimable work.")
		return
	}
	printValidationCapacity(result.Validation)
	printDegradedCapacity(result)
	printUnservableWork(result)
	printProviderBlockedWork(result)

	if len(result.Missing) == 0 {
		return
	}
	printMissingRoles(result.Missing)

	if result.DryRun {
		fmt.Println()
		fmt.Println("Dry run; would spawn:")
		for _, command := range result.Commands {
			fmt.Printf("  %s\n", command)
		}
		return
	}

	fmt.Println()
	printStartOutcomes(result)
}

func printValidationCapacity(validation []ValidationAgentCapacity) {
	if len(validation) == 0 {
		return
	}
	fmt.Println("Task validation capacity (last observation):")
	for _, entry := range validation {
		fmt.Printf("  %s (%s), task %s: %s %s\n    hint: %s\n", entry.AgentID, entry.Role, entry.TaskID, entry.Status, entry.Code, entry.RecoverHint)
	}
}

func printDegradedCapacity(result *RepairAgentPoolResult) {
	if len(result.Degraded) == 0 {
		return
	}
	fmt.Println("Degraded agent capacity:")
	for _, degraded := range result.Degraded {
		fmt.Printf("  %s (%s): %s\n", degraded.AgentID, degraded.Role, degraded.Reason)
		if degraded.RecoverHint != "" {
			fmt.Printf("    hint: %s\n", degraded.RecoverHint)
		}
	}
	if len(result.Missing) > 0 {
		fmt.Println()
	}
}

func printUnservableWork(result *RepairAgentPoolResult) {
	if len(result.Unservable) == 0 {
		return
	}
	fmt.Println("Reviewer work a new agent could not claim (not started):")
	for _, work := range result.Unservable {
		fmt.Printf("  %s with %s: %d task(s) (%s)\n    hint: %s\n", work.Role, work.startedWith(), len(work.TaskIDs), strings.Join(work.TaskIDs, ", "), work.Reason)
	}
	if len(result.Missing) > 0 {
		fmt.Println()
	}
}

func printProviderBlockedWork(result *RepairAgentPoolResult) {
	if len(result.ProviderBlocked) == 0 {
		return
	}
	fmt.Println("Roles with claimable work and no usable provider (not started):")
	for _, work := range result.ProviderBlocked {
		summary, hint := work.describe()
		fmt.Printf("  %s\n    hint: %s\n", summary, hint)
	}
	if len(result.Missing) > 0 {
		fmt.Println()
	}
}

func printMissingRoles(missing []MissingRoleWork) {
	printedHeading := false
	for _, roleWork := range missing {
		// Only a missing orchestrator is demand without tasks.
		if len(roleWork.TaskIDs) == 0 && roleWork.Reason != "" {
			fmt.Printf("Missing orchestrator: %s (%s)\n", roleWork.Role, roleWork.Reason)
			continue
		}
		if !printedHeading {
			fmt.Println("Roles with claimable work not covered by idle agents:")
			printedHeading = true
		}
		line := fmt.Sprintf("  %s: %d task(s) (%s); starting %d agent(s)", roleWork.Role, roleWork.TaskCount, strings.Join(roleWork.TaskIDs, ", "), roleWork.SpawnCount)
		if roleWork.Reason != "" {
			line += ": " + roleWork.Reason
		}
		fmt.Println(line)
	}
}

func printStartOutcomes(result *RepairAgentPoolResult) {
	if len(result.Spawned) > 0 {
		fmt.Println("Started agent processes:")
		for _, spawned := range result.Spawned {
			if spawned.PID != 0 {
				fmt.Printf("  %s (pid %d)\n", spawned.Command, spawned.PID)
				continue
			}
			fmt.Printf("  %s\n", spawned.Command)
		}
	}
	if len(result.Failed) > 0 {
		fmt.Println("Failed to start agent processes:")
		for _, failed := range result.Failed {
			fmt.Printf("  %s: %s\n", failed.Command, failed.Error)
		}
	}
}
