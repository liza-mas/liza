package main

import (
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/spf13/cobra"
)

// degradedOutcomeExit makes main exit with ExitDegraded without printing an
// error: the outcome was already reported, as checks on stdout in JSON mode
// or as warnings on stderr otherwise.
type degradedOutcomeExit struct{}

func (degradedOutcomeExit) Error() string { return jsonout.ErrDegraded.Error() }

// runWithOutcome runs a command that reports outcome checks
// (specs/protocols/outcome-checks.md). In JSON mode everything the run prints
// to stdout goes to stderr, and stdout carries exactly one envelope whose
// result is the report. A run with a non-ok check exits with ExitDegraded; a
// hard failure keeps its error path and exits 1.
func runWithOutcome(cmd *cobra.Command, name string, run func(report *jsonout.Report) error) error {
	report := jsonout.NewReport()
	if !isJSON(cmd) {
		if err := run(report); err != nil {
			return err
		}
		return degradedResult(cmd.ErrOrStderr(), name, report)
	}
	stdout, err := withStdoutToStderr(func() error { return run(report) })
	if err != nil {
		return jsonout.WriteResult(stdout, nil, nil, err)
	}
	if err := jsonout.WriteResult(stdout, report, nil, nil); err != nil {
		return err
	}
	if report.Degraded() {
		return degradedOutcomeExit{}
	}
	return nil
}

// degradedResult closes a human-mode run: the warnings are already on
// stderr, so a degraded run adds one summary line and exits ExitDegraded.
func degradedResult(stderr io.Writer, name string, report *jsonout.Report) error {
	notOK := report.NotOK()
	if len(notOK) == 0 {
		return nil
	}
	fmt.Fprintf(stderr, "%s completed with %d degraded check(s):\n", name, len(notOK))
	for _, check := range notOK {
		fmt.Fprintf(stderr, "  - %s\n", check)
	}
	return degradedOutcomeExit{}
}

// withStdoutToStderr runs fn with os.Stdout pointed at stderr and returns the
// original stdout, so a JSON-mode command keeps stdout for its envelope while
// the human output of the code it calls still reaches the user. Commands run
// one per process; nothing else may write os.Stdout concurrently.
func withStdoutToStderr(fn func() error) (*os.File, error) {
	stdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = stdout }()
	return stdout, fn()
}
