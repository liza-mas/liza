package commands

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// buildRespawnArgs returns the argv for the branded agent command.
func buildRespawnArgs(role, agentID, cli string) []string {
	return []string{
		brand.BinaryName, "agent", role,
		"--agent-id", agentID,
		"--cli", cli,
	}
}

// RecoverAgentCommand recovers a crashed agent (release claims, remove worktree,
// delete agent) and optionally respawns it via syscall.Exec.
func RecoverAgentCommand(projectRoot, agentID string, force bool, cli, reason string) error {
	return RecoverAgentWithOptionsCommand(projectRoot, agentID, force, cli, reason, ops.LifecycleRequestOptions{})
}

func RecoverAgentWithOptionsCommand(projectRoot, agentID string, force bool, cli, reason string, request ops.LifecycleRequestOptions) error {
	result, err := ops.RecoverAgentWithOptions(projectRoot, agentID, force, reason, request)
	if err != nil {
		return fmt.Errorf("recover agent: %w", err)
	}

	// A failed process replacement must emit only its final requery policy.
	if cli == "" || result.Outcome != models.LifecycleCompleted {
		if printLifecycleResult(result.LifecycleOutcome) {
			return nil
		}
	}

	if result.AlreadyClean {
		fmt.Printf("Agent %s already clean (not found in state)\n", agentID)
	} else {
		fmt.Printf("Recovered agent %s (role: %s)\n", result.AgentID, result.Role)
		if result.TaskID != "" {
			fmt.Printf("  Task: %s\n", result.TaskID)
		}
		if result.ClaimReleased {
			fmt.Printf("  Claim released: yes\n")
		}
		if result.WorktreeRemoved {
			fmt.Printf("  Worktree removed: yes\n")
		}
		if result.AgentDeleted {
			fmt.Printf("  Agent deleted: yes\n")
		}
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "  Warning: %s\n", w)
		}
	}

	if cli != "" {
		return respawnRecoveredAgent(result, cli, syscall.Exec)
	}
	return nil
}

// The recovery transaction has already committed. Process replacement failure
// cannot invite replaying that mutation, and must retain its completion receipt.
func respawnRecoveredAgent(result *ops.RecoverAgentResult, cli string, execProcess func(string, []string, []string) error) (retErr error) {
	defer func() {
		if retErr != nil {
			outcome := result.LifecycleOutcome
			outcome.Outcome, outcome.SafeAction, outcome.Effects = models.LifecycleStateChanged, "requery", "committed"
			retErr = &ops.LifecycleError{Outcome: outcome, Err: retErr}
		}
	}()
	if result.AlreadyClean || result.Role == "" {
		return fmt.Errorf("cannot respawn: agent role unknown (agent was already clean)")
	}
	fmt.Printf("Respawning agent %s as %s with %s...\n", result.AgentID, result.Role, cli)
	binary, err := os.Executable()
	if err != nil {
		binary, err = exec.LookPath(brand.BinaryName)
		if err != nil {
			return fmt.Errorf("cannot find %s binary: %w", brand.BinaryName, err)
		}
	}
	return execProcess(binary, buildRespawnArgs(result.Role, result.AgentID, cli), os.Environ())
}
