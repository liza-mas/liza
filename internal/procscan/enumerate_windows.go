//go:build windows

package procscan

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// enumerateAgentImagePIDs returns the PIDs whose image name is the configured
// binary, on a host with no procfs to walk.
//
// The image name is a pre-filter, not an identity: it narrows a process table
// of several hundred entries down to the handful worth asking for a command
// line, which is what actually decides whether a process is an agent
// supervisor. Without it every scan would open every process on the machine
// only to discard almost all of them.
//
// Toolhelp is used rather than EnumProcesses because it reports the image name
// in the same pass as the PID; EnumProcesses would need a second open per
// process just to learn the name this filter needs.
func enumerateAgentImagePIDs() ([]int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("read first process entry: %w", err)
	}

	var pids []int
	for {
		if isAgentImageName(filepath.Base(windows.UTF16ToString(entry.ExeFile[:]))) {
			pids = append(pids, int(entry.ProcessID))
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if err == windows.ERROR_NO_MORE_FILES {
				break
			}
			return nil, fmt.Errorf("read next process entry: %w", err)
		}
	}
	return pids, nil
}
