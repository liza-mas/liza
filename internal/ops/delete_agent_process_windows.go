//go:build windows

package ops

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/liza-mas/liza/internal/procscan"
)

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

// killAgentProcessTree terminates pid and its descendants, deepest first.
//
// An agent spawns a provider CLI as a child, and that child may spawn its own.
// Terminating only the recorded PID leaves them running and reparented, which
// is what "deleted the agent" would otherwise mean on Windows.
func killAgentProcessTree(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	tree, err := processTreeDeepestFirst(uint32(pid))
	if err != nil {
		return err
	}

	var firstErr error
	for _, member := range tree {
		if err := terminateProcessByPID(member); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// processTreeDeepestFirst returns root and its descendants, children before
// their parents so that nothing is reparented onto a still-running ancestor
// midway through termination.
//
// Descendants are read from a Toolhelp snapshot, which reports a parent PID per
// process. That field is not authoritative on its own: it keeps pointing at a
// number after the parent exits, and Windows recycles PIDs aggressively, so an
// unrelated process started later can appear to be a child. Comparing creation
// times settles it — a real child cannot predate its parent.
func processTreeDeepestFirst(root uint32) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	childrenByParent := map[uint32][]uint32{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("read first process entry: %w", err)
	}
	for {
		if entry.ProcessID != entry.ParentProcessID {
			childrenByParent[entry.ParentProcessID] = append(childrenByParent[entry.ParentProcessID], entry.ProcessID)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if err == windows.ERROR_NO_MORE_FILES {
				break
			}
			return nil, fmt.Errorf("read next process entry: %w", err)
		}
	}

	order := orderProcessTree(root, childrenByParent, processCreationTime)

	for left, right := 0, len(order)-1; left < right; left, right = left+1, right-1 {
		order[left], order[right] = order[right], order[left]
	}
	return order, nil
}

// orderProcessTree walks childrenByParent breadth-first from root, returning
// root followed by its descendants in traversal order (reversed by the
// caller to get children before their parents).
//
// visited guards against a cycle in childrenByParent: stale parent pointers
// plus PID recycling can make A's recorded parent B and B's recorded parent
// A, and the creation-time check below only breaks that when both
// processes' times are readable via creationTime. Without visited, the
// order slice would grow without bound and this would never return.
//
// Extracted from processTreeDeepestFirst so the walk itself — the part a
// cycle can hang — is testable without a real Toolhelp snapshot.
func orderProcessTree(root uint32, childrenByParent map[uint32][]uint32, creationTime func(uint32) (int64, bool)) []uint32 {
	order := []uint32{root}
	visited := map[uint32]bool{root: true}
	for i := 0; i < len(order); i++ {
		parent := order[i]
		parentStart, parentOK := creationTime(parent)
		for _, child := range childrenByParent[parent] {
			if visited[child] {
				continue
			}
			childStart, childOK := creationTime(child)
			if parentOK && childOK && childStart < parentStart {
				// The parent PID was recycled: this process predates the agent.
				continue
			}
			visited[child] = true
			order = append(order, child)
		}
	}
	return order
}

// processCreationTime reports when a process started, as a comparable value.
// The second result is false when the process is gone or cannot be opened, in
// which case the caller has no evidence either way and keeps the candidate.
func processCreationTime(pid uint32) (int64, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	return creation.Nanoseconds(), true
}

// terminateProcessByPID stops one process. A process that has already exited is
// not an error: the goal is that it is not running afterwards.
func terminateProcessByPID(pid uint32) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return fmt.Errorf("open process %d for termination: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.TerminateProcess(handle, 1); err != nil {
		// TerminateProcess reports access denied for a process that has already
		// exited, which is indistinguishable from a real permission failure
		// until the exit code is checked.
		if exited, codeErr := processHasExited(handle); codeErr == nil && exited {
			return nil
		}
		return fmt.Errorf("terminate process %d: %w", pid, err)
	}
	return nil
}

func processHasExited(handle windows.Handle) (bool, error) {
	const stillActive = 259

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false, err
	}
	return code != stillActive, nil
}
