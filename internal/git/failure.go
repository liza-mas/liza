package git

import (
	"errors"
	"os/exec"
	"strings"
)

// FailureClass names, from a closed set, why a git invocation failed. It lets a
// diagnostic that must not carry git's own text (paths, refs, file contents)
// still tell the failures apart.
type FailureClass string

const (
	// FailureConfigUnreadable: git could not read a configuration file, for
	// example while another process was replacing it.
	FailureConfigUnreadable FailureClass = "config_unreadable"
	// FailureLockContention: another git process held a `.lock` file.
	FailureLockContention FailureClass = "lock_contention"
	// FailureBadRevision: a revision or range did not resolve.
	FailureBadRevision FailureClass = "bad_revision"
	// FailurePermissionDenied: the OS refused access to another repository file.
	FailurePermissionDenied FailureClass = "permission_denied"
	// FailureNotRun: git did not exit with a status (it could not start, or
	// was killed).
	FailureNotRun FailureClass = "not_run"
	// FailureOther: none of the above.
	FailureOther FailureClass = "other"
)

// failureMarkers maps git's (LC_ALL=C, see gitenv) messages to classes, checked
// in order: an unreadable config also says "Permission denied".
var failureMarkers = []struct {
	class   FailureClass
	markers []string
}{
	{FailureConfigUnreadable, []string{"error occurred while reading the configuration files", "unable to access '", "bad config line"}},
	{FailureLockContention, []string{"could not lock config file", ".lock': File exists"}},
	{FailureBadRevision, []string{"Invalid revision range", "unknown revision", "bad revision", "bad object", "Needed a single revision", "not a valid object name"}},
	{FailurePermissionDenied, []string{"Permission denied"}},
}

// ClassifyFailure returns git's exit status (-1 when it did not exit with one)
// and the failure class of err, an error from this package's command runners,
// whose text carries git's output.
func ClassifyFailure(err error) (int, FailureClass) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() < 0 {
		return -1, FailureNotRun
	}
	text := err.Error()
	for _, f := range failureMarkers {
		for _, marker := range f.markers {
			if strings.Contains(text, marker) {
				return exitErr.ExitCode(), f.class
			}
		}
	}
	return exitErr.ExitCode(), FailureOther
}
