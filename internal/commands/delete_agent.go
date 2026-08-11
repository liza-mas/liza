package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/ops"
)

var terminateAgent = ops.TerminateAgent

// DeleteAgentCommand stops the agent process, removes it from the state
// database, and prints the result. Delegates business logic to ops.TerminateAgent.
// The stdin parameter allows for injected input in tests; pass os.Stdin for CLI usage.
func DeleteAgentCommand(projectRoot, agentID string, force bool, reason string, stdin io.Reader) error {
	if stdin == nil {
		stdin = os.Stdin
	}

	pidConfirmed := false
	if !force && agentID != "" {
		confirmed, err := confirmRunningProcess(projectRoot, agentID, stdin)
		if err != nil {
			return err
		}
		pidConfirmed = confirmed
	}

	result, err := terminateAgent(projectRoot, agentID, force, pidConfirmed, reason, 5*time.Second)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	if result.Process.IdentityUnverified {
		fmt.Fprintf(os.Stderr,
			"Warning: PID %d is still running but could not be confirmed to be agent %s, so it was left alone. Check it before reusing the slot.\n",
			result.Process.PID, result.AgentID)
	}
	fmt.Printf("Deleted agent %s\n", result.AgentID)
	return nil
}

// confirmRunningProcess prompts the user if the agent process is still running.
// Returns true if the user confirmed deletion, false if not running.
// Interactive confirmation is CLI-only — ops.DeleteAgent handles business logic validation.
func confirmRunningProcess(projectRoot, agentID string, stdin io.Reader) (bool, error) {
	running, pid, err := ops.IsAgentProcessRunning(projectRoot, agentID)
	if err != nil {
		return false, fmt.Errorf("check agent process: %w", err)
	}
	if !running {
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "Agent %s is still running with PID %d, do you want to delete the agent from the state file? (y/n): ", agentID, pid)
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		return false, fmt.Errorf("deletion cancelled")
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return false, fmt.Errorf("deletion cancelled by user")
	}
	// Only bypass PID check, not lease/task checks
	return true, nil
}
