package a5

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

var elapsedRE = regexp.MustCompile(`(?:\d+(?:\.\d+)?(?:ns|µs|ms|h|m|s))+ since the submit started`)

// Go's log package prefixes diagnostics with a slash-form date. Log lines are a
// free surface (goal Frozen Surfaces), so they are dropped from human captures.
var logLineRE = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)? .*\n?`)
var pidRE = regexp.MustCompile(`(?m)^(\s*pid: )\d+$`)
var leaseRE = regexp.MustCompile(`(?m)(\b(?:review_)?lease_expires: )([^\n]+)`)

// jsonObservation keeps the exit code and the complete stdout document. The raw
// message retains numeric spellings; the shared comparator decodes with UseNumber
// and sorts keys.
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
	return normaliseStreams([]byte(fmt.Sprintf("# exit_code\n%d\n# stdout\n%s\n# stderr\n%s", r.ExitCode, r.Stdout, r.Stderr)))
}

func capture(t testing.TB, f *fixture, c caseInput, mode string, r oracle.Result, before, after []byte) {
	t.Helper()
	n := f.normaliser()
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
	if strings.Contains(inventory[c.ID], "state") {
		name := c.ID + "." + mode + ".yaml"
		n.CheckState(t, name, normalisePIDs(before), normalisePIDs(normaliseLeases(before, after, f.started)))
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

func normalisePIDs(b []byte) []byte { return pidRE.ReplaceAll(b, []byte("${1}<pid>")) }

// normaliseStreams drops free log lines and replaces elapsed submit durations.
func normaliseStreams(b []byte) []byte {
	b = logLineRE.ReplaceAll(b, nil)
	return elapsedRE.ReplaceAll(b, []byte("<elapsed> since the submit started"))
}

// normaliseLeases replaces the lease fields the step changed, found by a line
// diff of the before/after pair, with their whole-minute offset from the step
// start. Unchanged leases keep their text, so both sides normalise them alike;
// fixed expired and far-future values stay under the shared timestamp handling.
func normaliseLeases(before, after []byte, start time.Time) []byte {
	lines := strings.Split(string(after), "\n")
	changed := changedLines(strings.Split(string(before), "\n"), lines)
	for i, line := range lines {
		if !changed[i] {
			continue
		}
		lines[i] = leaseRE.ReplaceAllStringFunc(line, func(field string) string {
			parts := leaseRE.FindStringSubmatch(field)
			at, err := time.Parse(time.RFC3339Nano, strings.Trim(parts[2], "\"'"))
			if err != nil || at.Before(start) || at.After(start.Add(24*time.Hour)) {
				return field
			}
			return fmt.Sprintf("%s<lease:+%dm>", parts[1], int(math.Round(at.Sub(start).Minutes())))
		})
	}
	return []byte(strings.Join(lines, "\n"))
}

// changedLines marks the lines of after that are not part of a longest common
// subsequence with before.
func changedLines(before, after []string) []bool {
	n, m := len(before), len(after)
	common := make([][]int, n+1)
	for i := range common {
		common[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if before[i] == after[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	changed := make([]bool, m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case before[i] == after[j]:
			i++
			j++
		case common[i+1][j] >= common[i][j+1]:
			i++
		default:
			changed[j] = true
			j++
		}
	}
	for ; j < m; j++ {
		changed[j] = true
	}
	return changed
}

func (f *fixture) normaliser() *oracle.Normaliser {
	n := oracle.NewNormaliser()
	n.Dir(f.root, "<ROOT>")
	for label, sha := range f.labels {
		n.Literal(sha, "<commit:"+label+">")
		n.Literal(sha[:7], "<commit:"+label+">")
	}
	return n
}

func TestA5Normalisation(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := normaliseLeases(nil, []byte("lease_expires: 2026-01-01T00:30:00Z\nreview_lease_expires: 2026-01-01T00:10:00Z\n"), start)
	if !bytes.Contains(got, []byte("<lease:+30m>")) || !bytes.Contains(got, []byte("<lease:+10m>")) {
		t.Fatal("lease durations erased")
	}
	f := fixture{labels: map[string]string{"base": strings.Repeat("a", 40), "candidate": strings.Repeat("b", 40)}}
	if got := f.normaliser().Text(strings.Repeat("a", 40) + " " + strings.Repeat("b", 40)); got != "<commit:base> <commit:candidate>" {
		t.Fatal("commit labels collapsed")
	}
}

// TestA5CaptureForms checks the local observation envelope: the stdout document
// is kept intact beside its exit code, and the human form keeps the streams apart.
func TestA5CaptureForms(t *testing.T) {
	r := oracle.Result{ExitCode: 1, Stdout: []byte(`{"ok":false,"n":12345678901234567890,"f":1.0,"error":{"code":"validation"}}`), Stderr: []byte("warning line\n")}
	b, err := jsonObservation(r)
	if err != nil {
		t.Fatal(err)
	}
	form, err := oracle.NewNormaliser().Form(oracle.JSON, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"exit_code": 1`, `"n": 12345678901234567890`, `"f": 1.0`, `"code": "validation"`} {
		if !strings.Contains(string(form), want) {
			t.Errorf("observation lost %s: %s", want, form)
		}
	}
	if _, err := jsonObservation(oracle.Result{Stdout: []byte("not json")}); err == nil {
		t.Error("non-JSON stdout accepted as a JSON observation")
	}
	human := string(humanObservation(r))
	if !strings.Contains(human, "# exit_code\n1\n# stdout\n{") || !strings.Contains(human, "\n# stderr\nwarning line\n") {
		t.Errorf("human observation does not separate exit code and streams: %q", human)
	}
}
