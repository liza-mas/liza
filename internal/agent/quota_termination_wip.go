package agent

import (
	"context"
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/providersignal"
)

// commitQuotaTerminationWIP commits the work of a session that a quota cutoff
// ended mid-edit. The supervisor calls it after every exit, before
// resetAgentAfterExit releases the claim: a claim released with a dirty
// preserved worktree blocks the task on its next claim. A skipped or failed
// commit only logs, leaving that block for a human to repair, as before.
func commitQuotaTerminationWIP(ctx context.Context, config SupervisorConfig, exitCode int, taskID, output string) {
	if exitCode == 0 || exitCode == exitCodeGracefulAbort || taskID == "" {
		return
	}
	qe := providersignal.DetectQuotaExhaustion(output, config.CLIName)
	if qe == nil {
		return
	}
	message := fmt.Sprintf("WIP: provider quota exhausted (%s), auto-committed by supervisor, hooks skipped\n\nTask: %s\nAgent: %s\n",
		qe.Provider, taskID, config.AgentID)
	result, err := ops.CommitTerminatedDoerWIP(ctx, config.ProjectRoot, config.Authority, taskID, message)
	if err != nil {
		GetLogger().Warn("Failed to commit quota-terminated work in progress", "error", err, "task_id", taskID, "agent_id", config.AgentID)
		return
	}
	if !result.Committed {
		GetLogger().Info("Quota-terminated work in progress not committed", "reason", result.SkipReason, "task_id", taskID, "agent_id", config.AgentID)
		return
	}
	GetLogger().Info("Committed quota-terminated work in progress", "commit", result.SHA, "task_id", taskID, "agent_id", config.AgentID)
}
