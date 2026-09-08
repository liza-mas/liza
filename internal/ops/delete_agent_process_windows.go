//go:build windows

package ops

import "github.com/liza-mas/liza/internal/procscan"

// isLizaAgentProcess reports whether pid is an agent supervisor, by reading the
// process command line. Returns false when the process is gone or unreadable,
// which callers treat as "do not signal this PID".
func isLizaAgentProcess(pid int) bool {
	argv, err := procscan.ProcessCommandLine(pid)
	if err != nil {
		return false
	}
	return procscan.IsLizaAgentArgv(argv)
}

// signalAgentProcessTree stops the agent and everything it started.
//
// There is no graceful step to offer here. Windows has no SIGTERM, and the
// console control events that come closest can only be raised for a process
// group by a process sharing its console — which a separate `delete` invocation
// does not. CREATE_NEW_PROCESS_GROUP, set when agents are spawned, governs
// those same events and so does not help either. The caller's grace period
// still applies: it is how long the exit is waited for, not how gently it is
// asked.
func signalAgentProcessTree(pid int) error {
	return killAgentProcessTree(pid)
}

// killAgentProcessTree terminates the agent and waits for its descendants to exit.
func killAgentProcessTree(pid int) error {
	return procscan.KillProcessTree(pid)
}
