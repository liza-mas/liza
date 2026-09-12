package commands

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// RetargetDependencyCommand retargets one task dependency edge and prints the
// result to stdout.
func RetargetDependencyCommand(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string) error {
	result, err := ops.RetargetDependency(projectRoot, taskID, oldDependency, newDependencies, reason, agentID)
	return printRetargetDependencyResult(result, err)
}

// RetargetDependencyWithAuthorityCommand retargets an edge using generation-fenced authority.
func RetargetDependencyWithAuthorityCommand(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority) error {
	return RetargetDependencyWithAuthorityAndOptionsCommand(projectRoot, taskID, oldDependency, newDependencies, reason, authority, ops.LifecycleRequestOptions{})
}

func RetargetDependencyWithAuthorityAndOptionsCommand(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.RetargetDependencyWithAuthorityAndOptions(projectRoot, taskID, oldDependency, newDependencies, reason, authority, request)
	return printRetargetDependencyResult(result, err)
}

func printRetargetDependencyResult(result *ops.RetargetDependencyResult, err error) error {
	if err != nil {
		return fmt.Errorf("retarget dependency: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Retargeted dependency for %s: %s -> %s\n",
		result.TaskID, result.OldDependency, strings.Join(result.NewDependencies, ", "))
	if !slices.Equal(result.NewDependencies, result.CanonicalDependencies) {
		fmt.Printf("Canonical dependencies: %s\n", strings.Join(result.CanonicalDependencies, ", "))
	}
	if result.RepairRequestCleared {
		fmt.Println("Cleared matching repair request")
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
