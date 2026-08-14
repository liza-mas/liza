//go:build windows

package procscan

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessCommandLine returns the argv of a running process.
//
// Windows has no /proc, and the usual substitute — reading the target's PEB
// through ReadProcessMemory — needs PROCESS_VM_READ and has to know the layout
// of undocumented structures. NtQueryInformationProcess answers the same
// question directly from ProcessCommandLineInformation, needs no more access
// than the PROCESS_QUERY_LIMITED_INFORMATION the liveness probe already asks
// for, and has been available since Windows 8.1.
//
// The returned slice is the command line split the way the target itself would
// have split it, so argv[0] is the image as invoked and the rest are its
// arguments — the same shape ParseCmdlineBytes produces from procfs.
func ProcessCommandLine(pid int) ([]string, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid pid %d", pid)
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	// Asking with no buffer reports the size needed through retLen. The result
	// is an NTUnicodeString whose Buffer points into the same allocation, so it
	// has to be read back in place rather than copied out field by field.
	var needed uint32
	err = windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, nil, 0, &needed)
	if err != nil && err != windows.STATUS_INFO_LENGTH_MISMATCH {
		return nil, fmt.Errorf("query command line size for process %d: %w", pid, err)
	}
	if needed < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) {
		return nil, fmt.Errorf("query command line size for process %d: implausible size %d", pid, needed)
	}

	buf := make([]byte, needed)
	if err := windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)), &needed); err != nil {
		return nil, fmt.Errorf("query command line for process %d: %w", pid, err)
	}

	value := (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0]))
	if value.Buffer == nil || value.Length == 0 {
		return nil, nil
	}
	// Length counts bytes, not UTF-16 code units, and the buffer is not
	// guaranteed to be NUL-terminated.
	commandLine := windows.UTF16ToString(unsafe.Slice(value.Buffer, value.Length/2))

	argv, err := windows.DecomposeCommandLine(commandLine)
	if err != nil {
		return nil, fmt.Errorf("split command line for process %d: %w", pid, err)
	}
	return argv, nil
}

// platformCommandLineSource names the mechanism behind platformCommandLine, so
// a status carries how its identity was established.
const platformCommandLineSource = "NtQueryInformationProcess"

// platformCommandLine gives the identity check a source on a host that has no
// procfs to read.
func platformCommandLine(pid int) ([]string, error) {
	return ProcessCommandLine(pid)
}
