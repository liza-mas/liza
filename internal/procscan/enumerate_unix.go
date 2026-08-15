//go:build !windows

package procscan

// enumerateAgentImagePIDs reports that this host offers no substitute for
// procfs.
//
// Every Unix that runs the orchestrator mounts procfs, so reaching here means
// the scan already failed to read it and there is nothing further to try. The
// Windows build carries the real implementation.
func enumerateAgentImagePIDs() ([]int, error) {
	return nil, ErrProcessScanUnavailable
}
