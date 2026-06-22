package procscan

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// AgentProcessState classifies a registered agent PID using the strongest
// available host evidence.
type AgentProcessState string

const (
	AgentProcessLiveMatching AgentProcessState = "live_matching"
	AgentProcessDead         AgentProcessState = "dead"
	AgentProcessMismatched   AgentProcessState = "mismatched"
	AgentProcessUnknown      AgentProcessState = "unknown"
)

// AgentProcessStatus reports both process existence and whether the process
// identity matches the registered agent row.
type AgentProcessStatus struct {
	State  AgentProcessState
	Source string
	Detail string
	Alive  bool
}

func (s AgentProcessStatus) IsLiveMatching() bool {
	return s.State == AgentProcessLiveMatching
}

func (s AgentProcessStatus) IsDeadOrMismatched() bool {
	return s.State == AgentProcessDead || s.State == AgentProcessMismatched
}

// IsLiveOrUnknown preserves legacy signal(0)-based behavior when procfs cannot
// prove process identity, while still excluding proven-dead and proven-mismatched
// PIDs.
func (s AgentProcessStatus) IsLiveOrUnknown() bool {
	return s.State == AgentProcessLiveMatching || (s.State == AgentProcessUnknown && s.Alive)
}

func (s AgentProcessStatus) DisplayStatus() string {
	switch s.State {
	case AgentProcessLiveMatching:
		return "running"
	case AgentProcessDead:
		return "stopped"
	case AgentProcessMismatched:
		return "mismatched"
	default:
		return "unknown"
	}
}

// AgentProcessStatusForPID checks whether pid identifies the expected agent
// supervisor. Procfs identity is authoritative when available; signal(0) is used
// only to distinguish dead from unknown/unavailable process identity.
func AgentProcessStatusForPID(pid int, role, agentID, procRoot string) AgentProcessStatus {
	if pid <= 0 {
		return AgentProcessStatus{
			State:  AgentProcessUnknown,
			Source: "pid",
			Detail: "no pid recorded",
		}
	}
	if procRoot == "" {
		procRoot = "/proc"
	}

	cmdlinePath := filepath.Join(procRoot, strconv.Itoa(pid), "cmdline")
	data, err := os.ReadFile(cmdlinePath)
	if err == nil {
		argv := ParseCmdlineBytes(data)
		if MatchesLizaAgentIdentity(argv, role, agentID) {
			return AgentProcessStatus{
				State:  AgentProcessLiveMatching,
				Source: "procfs",
				Detail: "cmdline matches expected agent supervisor",
				Alive:  true,
			}
		}
		return AgentProcessStatus{
			State:  AgentProcessMismatched,
			Source: "procfs",
			Detail: "pid exists but cmdline does not match expected agent supervisor",
			Alive:  true,
		}
	}

	procfsUnavailable := false
	if _, statErr := os.Stat(procRoot); os.IsNotExist(statErr) {
		procfsUnavailable = true
	}

	return signalProcessStatus(pid, err, procfsUnavailable)
}

func signalProcessStatus(pid int, identityErr error, procfsUnavailable bool) AgentProcessStatus {
	process, err := os.FindProcess(pid)
	if err != nil {
		return AgentProcessStatus{
			State:  AgentProcessDead,
			Source: "os.FindProcess",
			Detail: err.Error(),
		}
	}

	err = process.Signal(syscall.Signal(0))
	if err == nil {
		detail := fmt.Sprintf("process accepted signal 0; identity unavailable: %v", identityErr)
		if procfsUnavailable {
			detail = "process accepted signal 0; procfs unavailable"
		}
		return AgentProcessStatus{
			State:  AgentProcessUnknown,
			Source: "signal(0)",
			Detail: detail,
			Alive:  true,
		}
	}
	if stderrors.Is(err, syscall.EPERM) {
		detail := fmt.Sprintf("process exists but signal permission was denied; identity unavailable: %v", identityErr)
		if procfsUnavailable {
			detail = "process exists but signal permission was denied; procfs unavailable"
		}
		return AgentProcessStatus{
			State:  AgentProcessUnknown,
			Source: "signal(0)",
			Detail: detail,
			Alive:  true,
		}
	}
	if stderrors.Is(err, syscall.ESRCH) {
		return AgentProcessStatus{
			State:  AgentProcessDead,
			Source: "signal(0)",
			Detail: "process does not exist",
		}
	}
	return AgentProcessStatus{
		State:  AgentProcessDead,
		Source: "signal(0)",
		Detail: err.Error(),
	}
}
