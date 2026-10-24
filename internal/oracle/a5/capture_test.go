package a5

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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

func capture(t *testing.T, f *fixture, c caseInput, mode string, r oracle.Result, before, after []byte) {
	t.Helper()
	n := f.normaliser()
	switch mode {
	case "json":
		b, err := jsonObservation(r)
		if err != nil {
			t.Fatalf("%s %v", c.ID, err)
		}
		n.Check(t, oracle.JSON, c.ID+".json", b)
		if len(r.Stderr) > 0 {
			n.Check(t, oracle.Human, c.ID+".json-stderr.txt", normaliseStreams(r.Stderr))
		}
	case "human":
		n.Check(t, oracle.Human, c.ID+".txt", humanObservation(r))
	}
	if strings.Contains(inventory[c.ID], "state") {
		name := c.ID + "." + mode + ".yaml"
		n.CheckState(t, name, normalisePIDs(before), normalisePIDs(normaliseLeases(after, f.started)))
	}
}

func normalisePIDs(b []byte) []byte { return pidRE.ReplaceAll(b, []byte("${1}<pid>")) }

// normaliseStreams drops free log lines and replaces elapsed submit durations.
func normaliseStreams(b []byte) []byte {
	b = logLineRE.ReplaceAll(b, nil)
	return elapsedRE.ReplaceAll(b, []byte("<elapsed> since the submit started"))
}

// Only newly written leases near this invocation are replaced. Fixed expired
// and far-future fixture leases remain subject to ordinary timestamp handling.
func normaliseLeases(b []byte, start time.Time) []byte {
	return leaseRE.ReplaceAllFunc(b, func(line []byte) []byte {
		parts := leaseRE.FindSubmatch(line)
		value := strings.Trim(string(parts[2]), "\"'")
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || at.Before(start) || at.After(start.Add(24*time.Hour)) {
			return line
		}
		minutes := int(math.Round(at.Sub(start).Minutes()))
		return []byte(fmt.Sprintf("%s<lease:+%dm>", parts[1], minutes))
	})
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
	got := normaliseLeases([]byte("lease_expires: 2026-01-01T00:30:00Z\nreview_lease_expires: 2026-01-01T00:10:00Z\n"), start)
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
