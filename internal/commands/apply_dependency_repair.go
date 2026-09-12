package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// ApplyDependencyRepairCommand applies a blocked task's stored dependency
// repair and prints every committed canonical dependency list.
func ApplyDependencyRepairCommand(projectRoot, sourceTaskID, reason, agentID string) error {
	result, err := ops.ApplyDependencyRepair(projectRoot, sourceTaskID, reason, agentID)
	return printApplyDependencyRepairResult(result, err)
}

// ApplyDependencyRepairWithAuthorityCommand applies a repair using generation-fenced authority.
func ApplyDependencyRepairWithAuthorityCommand(projectRoot, sourceTaskID, reason string, authority models.AgentAuthority) error {
	return ApplyDependencyRepairWithAuthorityAndOptionsCommand(projectRoot, sourceTaskID, reason, authority, ops.LifecycleRequestOptions{})
}

func ApplyDependencyRepairWithAuthorityAndOptionsCommand(projectRoot, sourceTaskID, reason string, authority models.AgentAuthority, request ops.LifecycleRequestOptions) error {
	result, err := ops.ApplyDependencyRepairWithAuthorityAndOptions(projectRoot, sourceTaskID, reason, authority, request)
	return printApplyDependencyRepairResult(result, err)
}

func printApplyDependencyRepairResult(result *ops.ApplyDependencyRepairResult, err error) error {
	if err != nil {
		return fmt.Errorf("apply dependency repair: %w", err)
	}

	if printLifecycleResult(result.LifecycleOutcome) {
		return nil
	}

	fmt.Printf("Applied dependency repair requested by %s\n", result.SourceTaskID)
	for _, update := range result.Updates {
		fmt.Printf("%s dependencies: %s\n", update.TaskID, strings.Join(update.CanonicalDependencies, ", "))
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
