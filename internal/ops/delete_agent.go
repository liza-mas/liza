package ops

import (
	"fmt"
	"os"
	"syscall"
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

// SignalProcess sends SIGTERM to the deleted agent's process if it had a known PID.
// Verifies the process is an agent via /proc/<pid>/cmdline before signaling,
// preventing accidental kills from PID reuse. Safe to call unconditionally.
func (r *DeleteAgentResult) SignalProcess() bool {
	if r.PID <= 0 {
		return false
	}
	if !agentProcesses.isLizaAgent(r.PID) {
		return false
	}
	proc, err := os.FindProcess(r.PID)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.SIGTERM) == nil
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
	if pid <= 0 || !agentProcesses.isLizaAgent(pid) {
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

// isLizaAgentProcess checks if the process with the given PID is a liza agent
// by reading /proc/<pid>/cmdline. Returns false if the process doesn't exist,
// is unreadable, or isn't a liza agent.
// Linux-only: returns false on platforms without procfs (documented no-op).
func isLizaAgentProcess(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return procscan.IsLizaAgentArgv(procscan.ParseCmdlineBytes(data))
}

// IsProcessAlive checks if a process with the given PID is running.
//
// Delegates to procscan.ProcessAlive, which uses signal(0) on Unix and
// OpenProcess on Windows. This is the single cross-platform source of truth
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
