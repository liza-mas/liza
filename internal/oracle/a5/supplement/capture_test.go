package supplement

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var elapsedRE = regexp.MustCompile(`(?:\d+(?:\.\d+)?(?:ns|µs|ms|h|m|s))+ since the submit started`)

// Go's log package prefixes diagnostics with a slash-form date. Log lines are a
// free surface (goal Frozen Surfaces), so they are dropped from human captures.
var logLineRE = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? .*\n?`)
var pidRE = regexp.MustCompile(`(?m)^(\s*pid: )\d+$`)

// jsonObservation keeps the exit code and the complete stdout document.
func jsonObservation(r oracle.Result) ([]byte, error) {
	if !json.Valid(r.Stdout) {
		return nil, fmt.Errorf("JSON stdout is not one document: %s", r.Stdout)
	}
	return json.Marshal(struct {
		Exit   int             `json:"exit_code"`
		Stdout json.RawMessage `json:"stdout"`
	}{r.ExitCode, r.Stdout})
}

// humanObservation separates the exit code and both streams with fixed driver
// markers, so streams are never concatenated without a boundary.
func humanObservation(r oracle.Result) []byte {
	return normaliseStreams(fmt.Appendf(nil, "# exit_code\n%d\n# stdout\n%s\n# stderr\n%s", r.ExitCode, r.Stdout, r.Stderr))
}

// normaliseStreams drops free log lines and replaces elapsed submit durations.
func normaliseStreams(b []byte) []byte {
	b = logLineRE.ReplaceAll(b, nil)
	return elapsedRE.ReplaceAll(b, []byte("<elapsed> since the submit started"))
}

func normalisePIDs(b []byte) []byte { return pidRE.ReplaceAll(b, []byte("${1}<pid>")) }

func capture(t testing.TB, f *fixture, c caseInput, mode string, r oracle.Result, before, after []byte) {
	t.Helper()
	n := f.normaliser()
	if c.Step.Adapter != "" {
		n.CheckState(t, c.ID+".state.yaml", normalisePIDs(before), normalisePIDs(after))
		if slices.Contains(c.Modes, "alerts") {
			n.Check(t, oracle.Human, c.ID+".alerts.txt", f.alertsLog(t))
		}
		if slices.Contains(c.Modes, "outbox") {
			n.Check(t, oracle.Human, c.ID+".outbox.txt", f.outbox)
		}
		return
	}
	switch mode {
	case "json":
		b, err := jsonObservation(r)
		if err != nil {
			t.Fatalf("%s %v", c.ID, err)
		}
		n.Check(t, oracle.JSON, c.ID+".json", b)
		checkJSONStderr(t, n, c.ID, r.Stderr)
	case "human":
		n.Check(t, oracle.Human, c.ID+".txt", humanObservation(r))
	}
	if slices.Contains(c.Modes, "state") {
		n.CheckState(t, c.ID+"."+mode+".yaml", normalisePIDs(before), normalisePIDs(after))
	}
}

// checkJSONStderr compares the stderr of a json-mode step whenever the step
// wrote any or a committed golden exists, so lost stderr fails like new stderr.
func checkJSONStderr(t testing.TB, n *oracle.Normaliser, id string, stderr []byte) {
	t.Helper()
	name := id + ".json-stderr.txt"
	_, err := os.Stat(filepath.Join("testdata", "golden", "human", name))
	if len(stderr) > 0 || err == nil {
		n.Check(t, oracle.Human, name, normaliseStreams(stderr))
	}
}

// alertsLog is the project's alerts log, empty when no alert was written.
func (f *fixture) alertsLog(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile(alertsLogPath(f.project))
	if os.IsNotExist(err) {
		return []byte("<no alerts>\n")
	}
	if err != nil {
		t.Fatal(err)
	}
	return b
}
