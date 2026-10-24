package a5

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const harnessImport = "github.com/liza-mas/liza/internal/testhelpers/oracle"

// liveOnly skips driver tests that read committed goldens: regeneration runs the
// package in write mode on a tree whose goldens were removed.
func liveOnly(t *testing.T) {
	t.Helper()
	if os.Getenv(oracle.WriteEnv) == "1" {
		t.Skip("reads committed goldens; not applicable in write mode")
	}
}

// Operator steps use the existing operator entry: the commands below register
// no --agent-id at the starting commit.
var operatorCases = []string{"AA1", "AA2", "CS1", "CS2", "RA1", "RA2", "UR1", "UR2"}

// expectedGoldens names the goldens of one inventory row: a CLI case writes a
// state golden per captured mode, an adapter case a single state golden.
func expectedGoldens(id, modes string) []string {
	fields := strings.Fields(modes)
	withState := containsString(fields, "state")
	var out []string
	for _, mode := range fields {
		switch mode {
		case "json":
			out = append(out, "json/"+id+".json")
		case "human":
			out = append(out, "human/"+id+".txt")
		case "state":
			if len(fields) == 1 {
				out = append(out, "state/"+id+".state.yaml")
			}
			continue
		}
		if withState {
			out = append(out, "state/"+id+"."+mode+".yaml")
		}
	}
	return out
}

// TestA5Inventory fixes the 40 IDs and their modes independently of discovery,
// so a missing manifest or a dropped mode fails instead of shrinking coverage.
func TestA5Inventory(t *testing.T) {
	if len(inventory) != 40 {
		t.Fatalf("inventory has %d entries, want 40", len(inventory))
	}
	cases := loadCases(t)
	if len(cases) != 40 {
		t.Fatalf("loaded %d cases, want 40", len(cases))
	}
	for _, c := range cases {
		want := strings.Fields(inventory[c.ID])
		if strings.Join(c.Modes, " ") != strings.Join(want, " ") {
			t.Errorf("%s modes %v, want %v", c.ID, c.Modes, want)
		}
		if c.Step.Adapter != "" && strings.Join(c.Modes, " ") != "state" {
			t.Errorf("%s: adapter cases record state only", c.ID)
		}
	}
}

// TestA5GoldenCoverage requires exactly the goldens the inventory names.
func TestA5GoldenCoverage(t *testing.T) {
	liveOnly(t)
	want := map[string]bool{}
	for id, modes := range inventory {
		for _, g := range expectedGoldens(id, modes) {
			want[g] = true
		}
	}
	var got []string
	err := filepath.Walk("testdata/golden", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			got = append(got, filepath.ToSlash(strings.TrimPrefix(path, "testdata/golden"+string(filepath.Separator))))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	for _, g := range got {
		id, stderr := strings.CutSuffix(strings.TrimPrefix(g, "human/"), ".json-stderr.txt")
		if stderr && strings.HasPrefix(g, "human/") && strings.Contains(inventory[id], "json") {
			continue // stderr of a json-mode step is its own human golden
		}
		if !want[g] {
			t.Errorf("unexpected golden %s", g)
		}
		delete(want, g)
	}
	for g := range want {
		t.Errorf("missing golden %s", g)
	}
}

// TestA5Entrypoints binds each step to its allocated entry: operator commands
// carry no agent identity, agent commands carry --agent-id and only the
// generation variable, and no private seam or replacement CLI runner exists.
func TestA5Entrypoints(t *testing.T) {
	var operators []string
	for _, c := range loadCases(t) {
		steps := append(append([]step{}, c.Prepare...), c.Step)
		for i, s := range steps {
			if s.Operator {
				if s.Agent != "" || s.Adapter != "" {
					t.Errorf("%s: operator step names an agent or adapter", c.ID)
				}
				if i == len(steps)-1 {
					operators = append(operators, c.ID)
				}
			} else if s.Agent == "" && s.Adapter != "worktree-commit" {
				t.Errorf("%s: step without an agent", c.ID)
			}
			names := map[string]bool{}
			for _, kv := range s.environment() {
				name, _, _ := strings.Cut(kv, "=")
				names[name] = true
			}
			agentNames := 0
			for name := range names {
				if strings.Contains(name, "AGENT") {
					agentNames++
				}
			}
			if s.Operator && agentNames != 0 {
				t.Errorf("%s: operator step environment carries agent variables", c.ID)
			}
			if !s.Operator && (agentNames != 1 || !names[generationVariable()]) {
				t.Errorf("%s: agent step environment must carry exactly the generation variable", c.ID)
			}
		}
		if strings.HasPrefix(c.ID, "RA") && containsString(c.Step.Args, "--dry-run") != (c.ID == "RA1") {
			t.Errorf("%s: --dry-run belongs to RA1 only", c.ID)
		}
	}
	sort.Strings(operators)
	if strings.Join(operators, " ") != strings.Join(operatorCases, " ") {
		t.Fatalf("operator steps %v, want %v", operators, operatorCases)
	}

	for _, file := range driverFiles(t) {
		src := string(read(t, file))
		for _, call := range strings.Split(src, "exec.Command(")[1:] {
			if !strings.HasPrefix(call, `"git"`) {
				t.Errorf("%s starts a process other than git outside the harness", file)
			}
		}
	}
	if !strings.Contains(string(read(t, "fixture_test.go")), "oracle.RunInProject(") {
		t.Error("CLI steps must run through oracle.RunInProject")
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// driverFiles lists the non-adapter test files; sensitivity_test.go drives Go
// tooling in a scratch clone and is checked separately.
func driverFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f, "adapter") && f != "sensitivity_test.go" && f != "driver_test.go" {
			out = append(out, f)
		}
	}
	return out
}

// TestA5AdapterBoundary keeps every non-stdlib, non-harness import in adapter
// files, and keeps adapters untagged so they compile against the starting tree.
func TestA5AdapterBoundary(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	adapters := 0
	for _, file := range files {
		isAdapter := strings.HasPrefix(file, "adapter")
		if isAdapter {
			adapters++
			if strings.Contains(string(read(t, file)), "//go:build") {
				t.Errorf("%s: adapters start untagged", file)
			}
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") && path != harnessImport {
				t.Errorf("%s imports %s outside an adapter file", file, path)
			}
		}
	}
	if adapters == 0 {
		t.Fatal("no adapter files")
	}
}

// TestA5Preparation proves a failed preparation fails the case before the
// tested step is invoked, and that declared exits are honoured in order.
func TestA5Preparation(t *testing.T) {
	c := caseInput{ID: "X1", Prepare: []step{{Agent: "a", Exit: 0}, {Agent: "b", Exit: 0}, {Agent: "c", Exit: 0}}, Step: step{Agent: "tested"}}
	var calls []string
	run := func(failing string) error {
		calls = nil
		_, _, err := runCase(c, "json", func(s step, mode string) oracle.Result {
			calls = append(calls, s.Agent)
			if s.Agent == failing {
				return oracle.Result{ExitCode: 7}
			}
			return oracle.Result{}
		}, func() []byte { calls = append(calls, "snapshot"); return nil })
		return err
	}
	if err := run("b"); err == nil || !strings.Contains(err.Error(), "preparation 1") {
		t.Fatalf("failed preparation was not reported with its index: %v", err)
	}
	if got := strings.Join(calls, ","); got != "a,b" {
		t.Fatalf("after a failed preparation ran %q; the tested step and snapshot must not run", got)
	}
	if err := run(""); err != nil || strings.Join(calls, ",") != "a,b,c,snapshot,tested" {
		t.Fatalf("successful preparation order %v: %v", calls, err)
	}
	// A preparation that declares a refusal exit (a quarantining submit) passes.
	c.Prepare[0].Exit = 7
	if err := run("a"); err != nil {
		t.Fatalf("declared exit rejected: %v", err)
	}
}

// TestA5Determinism checks the driver-local substitutions run before the shared
// normaliser and keep the distinctions the goldens assert.
func TestA5Determinism(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// A lease written by the step is offset from its start; one created during
	// preparation and left alone stays textually identical on both sides, and so
	// does a refreshed-looking value that did not change.
	before := []byte("task:\n  lease_expires: 2026-01-01T00:03:00Z\n  review_lease_expires: 2020-01-01T00:00:00Z\nagent:\n  lease_expires: 2099-01-01T00:00:00Z\n")
	after := []byte("task:\n  lease_expires: 2026-01-01T00:03:00Z\n  review_lease_expires: \"2026-01-01T00:30:00Z\"\nagent:\n  lease_expires: 2099-01-01T00:00:00Z\n  lease_expires: 2026-01-01T00:10:00Z\n")
	got := string(normaliseLeases(before, after, start))
	for _, want := range []string{"  lease_expires: 2026-01-01T00:03:00Z\n", "review_lease_expires: <lease:+30m>\n", "  lease_expires: 2099-01-01T00:00:00Z\n", "  lease_expires: <lease:+10m>\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("lease normalisation lost %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<lease:+3m>") {
		t.Error("an unchanged preparation lease was rewritten as a step change")
	}
	if got := string(normalisePIDs([]byte("      pid: 4242\n    pid_note: 4242\n"))); got != "      pid: <pid>\n    pid_note: 4242\n" {
		t.Errorf("pid normalisation: %q", got)
	}
	streams := normaliseStreams([]byte("2026/10/24 04:50:52 log line\nsubmit-for-review task-1: preparing; 1m2.5s since the submit started. ok\nkept\n"))
	if string(streams) != "submit-for-review task-1: preparing; <elapsed> since the submit started. ok\nkept\n" {
		t.Errorf("stream normalisation: %q", streams)
	}
	// Pinned validation sessions hash no host path.
	names := map[string]bool{}
	for _, kv := range pinnedSessionEnvironment() {
		name, value, _ := strings.Cut(kv, "=")
		names[name] = true
		if strings.Contains(value, os.TempDir()) || (os.Getenv("HOME") != "" && strings.Contains(value, os.Getenv("HOME"))) {
			t.Errorf("pinned session variable %s carries a host path", name)
		}
	}
	if !names["PATH"] || !names["HOME"] || len(names) != 2 {
		t.Errorf("pinned session variables %v, want exactly PATH and HOME", names)
	}
}

// TestA5Isolation materialises one case twice, as separate modes do, and shows
// the two share no directory, label or state.
func TestA5Isolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixtures use shell commands and POSIX paths")
	}
	var c caseInput
	for _, candidate := range loadCases(t) {
		if candidate.ID == "SV1" {
			c = candidate
		}
	}
	a, b := materialise(t, c), materialise(t, c)
	if a.root == b.root || a.project == b.project || a.home == b.home || a.tmp == b.tmp {
		t.Fatal("two materialisations share a directory")
	}
	write(t, filepath.Join(a.project, "marker"), []byte("a"))
	if _, err := os.Stat(filepath.Join(b.project, "marker")); !os.IsNotExist(err) {
		t.Fatal("a marker written in one materialisation is visible in the other")
	}
	if home := os.Getenv("HOME"); home != "" && (strings.HasPrefix(a.home, home+string(filepath.Separator)) || a.home == home) {
		t.Fatal("case HOME lies under the host HOME")
	}
	for _, label := range []string{"base", "candidate"} {
		if len(a.labels[label]) != 40 || a.labels[label] != b.labels[label] {
			t.Errorf("label %s must be a fixed-recipe commit, identical across materialisations", label)
		}
	}
	if a.labels["base"] == a.labels["candidate"] {
		t.Error("labelled commits collapsed")
	}
	if string(read(t, a.statePath())) != string(read(t, b.statePath())) && strings.Contains(string(read(t, a.statePath())), a.root) {
		t.Error("fixture state carries its case root")
	}
}

// failureRecorder captures a fatal failure of a harness call without failing
// the enclosing test.
type failureRecorder struct {
	testing.TB
	message string
}

func (r *failureRecorder) Helper() {}
func (r *failureRecorder) Fatalf(format string, args ...any) {
	r.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
func (r *failureRecorder) Fatal(args ...any) { r.Fatalf("%s", fmt.Sprint(args...)) }

func fails(t *testing.T, call func(oracle.Result, testing.TB)) string {
	t.Helper()
	r := &failureRecorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		call(oracle.Result{}, r)
	}()
	<-done
	return r.message
}

// TestA5FailureSignals shows the signals a reviewer relies on: a missing golden
// and a difference each fail naming the golden path and, for a difference, the
// first differing line.
func TestA5FailureSignals(t *testing.T) {
	liveOnly(t)
	missing := fails(t, func(_ oracle.Result, rec testing.TB) {
		oracle.Check(rec, oracle.Human, "no-such-a5-case.txt", []byte("x\n"))
	})
	if !strings.Contains(missing, "testdata/golden/human/no-such-a5-case.txt") || !strings.Contains(missing, "is missing") {
		t.Errorf("missing golden signal: %q", missing)
	}
	differs := fails(t, func(_ oracle.Result, rec testing.TB) {
		oracle.Check(rec, oracle.Human, "SR1.txt", []byte("not the golden\n"))
	})
	if !strings.Contains(differs, "testdata/golden/human/SR1.txt") || !strings.Contains(differs, "differs at line 1") {
		t.Errorf("difference signal: %q", differs)
	}
}

// TestA5JSONStderr requires lost frozen stderr to fail like new stderr.
func TestA5JSONStderr(t *testing.T) {
	liveOnly(t)
	n := oracle.NewNormaliser()
	lost := fails(t, func(_ oracle.Result, rec testing.TB) { checkJSONStderr(rec, n, "SR1", nil) })
	if !strings.Contains(lost, "testdata/golden/human/SR1.json-stderr.txt") || !strings.Contains(lost, "differs at line") {
		t.Errorf("lost stderr did not fail against its golden: %q", lost)
	}
	appeared := fails(t, func(_ oracle.Result, rec testing.TB) { checkJSONStderr(rec, n, "SR2X", []byte("new\n")) })
	if !strings.Contains(appeared, "is missing") {
		t.Errorf("new stderr without a golden did not fail: %q", appeared)
	}
	if quiet := fails(t, func(_ oracle.Result, rec testing.TB) { checkJSONStderr(rec, n, "SR2X", nil) }); quiet != "" {
		t.Errorf("no stderr and no golden must pass: %q", quiet)
	}
}
