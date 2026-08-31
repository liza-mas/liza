package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
)

// DeleteAgentResult contains the outcome of deleting an agent.
type DeleteAgentResult struct {
	AgentID string
	PID     int // PID of the deleted agent's process (0 if unknown).
}

// TerminateAgentResult contains the outcome of terminating and deleting an agent.
type TerminateAgentResult struct {
	AgentID      string
	PID          int
	Process      ProcessTerminationResult
	StateDeleted bool
}

// ProcessTerminationResult describes how a registered agent process was stopped.
type ProcessTerminationResult struct {
	PID      int
	Signaled bool
	Exited   bool
	Killed   bool

	// IdentityUnverified reports that the recorded PID belonged to a live
	// process which could not be confirmed to be this agent — a recycled PID,
	// or one whose command line could not be read. Such a process is
	// deliberately left alone, but the state row is still removed, so the
	// caller has to be able to say that something is still running under it.
	IdentityUnverified bool
}

type agentProcessOps struct {
	isLizaAgent func(pid int) bool
	isAlive     func(pid int) bool
	signalTree  func(pid int) error
	killTree    func(pid int) error
	waitForExit func(pid int, grace time.Duration) bool
}

var agentProcesses = agentProcessOps{
	isLizaAgent: isLizaAgentProcess,
	isAlive:     IsProcessAlive,
	signalTree:  signalAgentProcessTree,
	killTree:    killAgentProcessTree,
	waitForExit: waitForAgentProcessExit,
}

// TerminateAgent stops the registered agent process before removing it
// from state. If the process exits cleanly and unregisters itself first, the
// missing state entry is treated as success.
func TerminateAgent(projectRoot, agentID string, force, allowRunningPID bool, reason string, grace time.Duration) (*TerminateAgentResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID required")
	}

	agent, err := readAgentForDeletion(projectRoot, agentID)
	if err != nil {
		return nil, err
	}

	if !force {
		if err := validateAgentDeletion(agent, agentID); err != nil {
			return nil, err
		}
		if !allowRunningPID && agent.PID != 0 && agentProcesses.isAlive(agent.PID) {
			return nil, fmt.Errorf("agent %s is still running with PID %d, use --force to delete or confirm interactively via CLI", agentID, agent.PID)
		}
	}

	processResult, err := terminateProcess(agent.PID, grace)
	if err != nil {
		return nil, err
	}

	deleteResult, err := DeleteAgent(projectRoot, agentID, force, allowRunningPID, reason)
	if err != nil {
		if errors.IsNotFound(err) {
			return &TerminateAgentResult{
				AgentID:      agentID,
				PID:          agent.PID,
				Process:      processResult,
				StateDeleted: true,
			}, nil
		}
		return nil, err
	}

	return &TerminateAgentResult{
		AgentID:      deleteResult.AgentID,
		PID:          deleteResult.PID,
		Process:      processResult,
		StateDeleted: true,
	}, nil
}

func readAgentForDeletion(projectRoot, agentID string) (models.Agent, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	state, err := bb.Read()
	if err != nil {
		return models.Agent{}, fmt.Errorf("failed to read state: %w", err)
	}

	agent, exists := state.Agents[agentID]
	if !exists {
		return models.Agent{}, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}
	return agent, nil
}

func terminateProcess(pid int, grace time.Duration) (ProcessTerminationResult, error) {
	result := ProcessTerminationResult{PID: pid}
	if pid <= 0 {
		return result, nil
	}
	if !agentProcesses.isLizaAgent(pid) {
		// Not signalled: the PID may since have been handed to something
		// unrelated, and killing that would be worse than leaving an agent
		// behind. Whether anything is still running under it decides between
		// "the agent is already gone" and "the row is about to be removed
		// while its process is not".
		result.IdentityUnverified = agentProcesses.isAlive(pid)
		return result, nil
	}

	// PID identity can still race between /proc verification and signal delivery,
	// but checking argv avoids intentionally signaling unrelated processes.
	if err := agentProcesses.signalTree(pid); err != nil {
		return result, fmt.Errorf("signal %s agent process %d: %w", brand.BinaryName, pid, err)
	}
	result.Signaled = true

	if grace > 0 && agentProcesses.waitForExit(pid, grace) {
		result.Exited = true
		return result, nil
	}
	if grace <= 0 {
		return result, nil
	}

	if err := agentProcesses.killTree(pid); err != nil {
		return result, fmt.Errorf("kill %s agent process %d: %w", brand.BinaryName, pid, err)
	}
	result.Killed = true
	if agentProcesses.waitForExit(pid, grace) {
		result.Exited = true
		return result, nil
	}

	return result, fmt.Errorf("%s agent process %d still running after termination", brand.BinaryName, pid)
}

func waitForAgentProcessExit(pid int, grace time.Duration) bool {
	if !IsProcessAlive(pid) {
		return true
	}
	if grace <= 0 {
		return false
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		delay := 50 * time.Millisecond
		if remaining < delay {
			delay = remaining
		}
		time.Sleep(delay)
		if !IsProcessAlive(pid) {
			return true
		}
	}
	return !IsProcessAlive(pid)
}

// IsProcessAlive checks if a process with the given PID is running.
//
// Delegates to procscan.ProcessAlive, which uses signal(0) on Unix and an
// exit-code probe on Windows. This is the single cross-platform source of truth
// for process-existence checks; os.Process.Signal(0) does not work on Windows
// ("not supported"), so callers must not bypass this helper.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	alive, _, _ := procscan.ProcessAlive(pid)
	return alive
}

// validateAgentDeletion checks whether an agent can be safely deleted based on
// lease and task state. Does not check PID liveness (callers handle that separately).
func validateAgentDeletion(agent models.Agent, agentID string) error {
	now := time.Now().UTC()
	if agent.LeaseExpires != nil && agent.LeaseExpires.After(now) {
		return fmt.Errorf("agent %s has active lease (expires %v), use --force to delete", agentID, agent.LeaseExpires.Format(time.RFC3339))
	}
	if agent.CurrentTask != nil {
		return fmt.Errorf("agent %s is working on task %s, use --force to delete", agentID, *agent.CurrentTask)
	}
	return nil
}

// DeleteAgent removes an agent from state. Without force, refuses if the agent
// has an active lease, current task, or running process. The allowRunningPID
// flag bypasses only the PID liveness check (for interactive CLI confirmation)
// without bypassing lease/task safety checks. Callers should check
// IsAgentProcessRunning for interactive confirmation first. No terminal I/O.
func DeleteAgent(projectRoot, agentID string, force, allowRunningPID bool, reason string) (*DeleteAgentResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent ID required")
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	state, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	agent, exists := state.Agents[agentID]
	if !exists {
		return nil, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}

	if !force {
		if err := validateAgentDeletion(agent, agentID); err != nil {
			return nil, err
		}
		if !allowRunningPID && agent.PID != 0 && IsProcessAlive(agent.PID) {
			return nil, fmt.Errorf("agent %s is still running with PID %d, use --force to delete or confirm interactively via CLI", agentID, agent.PID)
		}
	}

	err = bb.Modify(func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		if !force {
			if err := validateAgentDeletion(agent, agentID); err != nil {
				return err
			}
		}

		delete(state.Agents, agentID)

		humanNote := models.HumanNote{
			Timestamp: time.Now().UTC(),
			Message:   fmt.Sprintf("Agent %s deleted: %s", agentID, reason),
			For:       agentID,
		}
		state.HumanNotes = append(state.HumanNotes, humanNote)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to delete agent: %w", err)
	}

	return &DeleteAgentResult{
		AgentID: agentID,
		PID:     agent.PID,
	}, nil
}

// IsAgentProcessRunning checks if the agent's registered PID is alive. Use before
// DeleteAgent to prompt for interactive confirmation.
func IsAgentProcessRunning(projectRoot, agentID string) (bool, int, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	state, err := bb.Read()
	if err != nil {
		return false, 0, fmt.Errorf("failed to read state: %w", err)
	}

	agent, exists := state.Agents[agentID]
	if !exists {
		return false, 0, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}

	if agent.PID != 0 && IsProcessAlive(agent.PID) {
		return true, agent.PID, nil
	}

	return false, agent.PID, nil
}
