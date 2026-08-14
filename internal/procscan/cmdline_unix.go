//go:build !windows

package procscan

import "errors"

// errNoNativeCommandLine reports that the host exposes no command-line source
// beyond procfs.
var errNoNativeCommandLine = errors.New("no native command line source: procfs is the only one")

// platformCommandLineSource exists for symmetry with the Windows build; nothing
// reads it here, because platformCommandLine never succeeds.
const platformCommandLineSource = "procfs"

// platformCommandLine has nothing to add here: procfs is the native source, so
// a caller reaching this point has already tried it and failed. Windows is the
// platform with a second source, and it supplies its own implementation.
func platformCommandLine(pid int) ([]string, error) {
	return nil, errNoNativeCommandLine
}
