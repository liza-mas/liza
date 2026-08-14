package procscan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// defaultProcRoot is the procfs mount every caller means when it does not name
// one. Tests name a directory of their own instead, which is what tells the
// two apart; a test that needs this host to look procfs-less repoints it.
var defaultProcRoot = "/proc"

// nativeCommandLine reads a live process's argv without going through procfs.
// It is a variable so a test can stand in for the host.
var nativeCommandLine = platformCommandLine

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
		procRoot = defaultProcRoot
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

	// Procfs did not answer. A host without one can still name its processes,
	// and without asking, every live agent reads as unknown — indistinguishable
	// from a PID that has been handed to something unrelated. Only the real
	// proc root is consulted this way: an injected one means the caller is
	// describing the host, and the machine underneath is not it.
	if procRoot == defaultProcRoot {
		if argv, nativeErr := nativeCommandLine(pid); nativeErr == nil {
			if MatchesLizaAgentIdentity(argv, role, agentID) {
				return AgentProcessStatus{
					State:  AgentProcessLiveMatching,
					Source: platformCommandLineSource,
					Detail: "command line matches expected agent supervisor",
					Alive:  true,
				}
			}
			return AgentProcessStatus{
				State:  AgentProcessMismatched,
				Source: platformCommandLineSource,
				Detail: "pid exists but command line does not match expected agent supervisor",
				Alive:  true,
			}
		}
	}

	procfsUnavailable := false
	if _, statErr := os.Stat(procRoot); os.IsNotExist(statErr) {
		procfsUnavailable = true
	}

	return signalProcessStatus(pid, err, procfsUnavailable)
}

// FindExplicitAgentIdentityPIDs returns observer-visible processes whose
// command line explicitly names the expected agent ID. Auto-assigned processes
// without --agent-id are intentionally excluded because they cannot be
// correlated to a recorded registration from process evidence alone.
func FindExplicitAgentIdentityPIDs(role, agentID, procRoot string) []int {
	if agentID == "" {
		return nil
	}
	if procRoot == "" {
		procRoot = "/proc"
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}

	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		argv := ParseCmdlineBytes(data)
		if !IsLizaAgentArgv(argv) || (role != "" && roleFromArgv(argv) != role) {
			continue
		}
		if flagValue(argv, "--agent-id") == agentID {
			pids = append(pids, pid)
		}
	}

	sort.Ints(pids)
	return pids
}

func signalProcessStatus(pid int, identityErr error, procfsUnavailable bool) AgentProcessStatus {
	alive, permDenied, err := ProcessAlive(pid)
	if err != nil {
		// Could not even attempt the probe (e.g. os.FindProcess failed). Treat
		// as dead so we don't hold onto a phantom agent row.
		return AgentProcessStatus{
			State:  AgentProcessDead,
			Source: "process-probe",
			Detail: err.Error(),
		}
	}

	if alive && !permDenied {
		detail := fmt.Sprintf("process is alive; identity unavailable: %v", identityErr)
		if procfsUnavailable {
			detail = "process is alive; procfs unavailable"
		}
		return AgentProcessStatus{
			State:  AgentProcessUnknown,
			Source: processProbeSource(),
			Detail: detail,
			Alive:  true,
		}
	}
	if alive && permDenied {
		detail := fmt.Sprintf("process exists but probe permission was denied; identity unavailable: %v", identityErr)
		if procfsUnavailable {
			detail = "process exists but probe permission was denied; procfs unavailable"
		}
		return AgentProcessStatus{
			State:  AgentProcessUnknown,
			Source: processProbeSource(),
			Detail: detail,
			Alive:  true,
		}
	}
	return AgentProcessStatus{
		State:  AgentProcessDead,
		Source: processProbeSource(),
		Detail: "process does not exist",
	}
}
