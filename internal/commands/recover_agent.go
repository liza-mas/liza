package commands

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/liza-mas/liza/internal/brand"
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
	result, err := ops.RecoverAgent(projectRoot, agentID, force, reason)
	if err != nil {
		return fmt.Errorf("recover agent: %w", err)
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

	// Respawn if --cli provided
	if cli != "" {
		if result.AlreadyClean || result.Role == "" {
			return fmt.Errorf("cannot respawn: agent role unknown (agent was already clean)")
		}

		fmt.Printf("Respawning agent %s as %s with %s...\n", agentID, result.Role, cli)

		// Build the branded agent command.
		lizaBin, err := os.Executable()
		if err != nil {
			// Fall back to finding the branded binary in PATH.
			lizaBin, err = exec.LookPath(brand.BinaryName)
			if err != nil {
				return fmt.Errorf("cannot find %s binary: %w", brand.BinaryName, err)
			}
		}

		args := buildRespawnArgs(result.Role, agentID, cli)

		// Replace current process with the new agent
		return syscall.Exec(lizaBin, args, os.Environ())
	}

	return nil
}
