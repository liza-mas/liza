package archguard

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Fixture tests of the C6 mutation guard. They run the guard on
// testdata/c6mutation, a module mirroring internal/models, internal/db and
// internal/ops with one violation per rule shape and every legal case, and on
// small temporary modules for the failure paths.

const c6FixtureDir = "internal/testguard/archguard/testdata/c6mutation"

// c6FixtureTable classifies the fixture db package's exported methods.
var c6FixtureTable = map[string]c6Kind{
	"ArchiveTerminalTask": c6Mutating,
	"Modify":              c6Mutating,
	"UpdateAgent":         c6Mutating,
	"UpdateTask":          c6Mutating,
	"Write":               c6Mutating,
	"GetStatePath":        c6ReadOnly,
	"Patient":             c6ReadOnly,
	"ReadSnapshot":        c6ReadOnly,
}

type c6FixtureLoad struct {
	root   string
	loaded *c6Loaded
	files  []string
}

// c6Fixture loads the fixture module once per test binary.
var c6Fixture = sync.OnceValues(func() (*c6FixtureLoad, error) {
	root := filepath.Join(repoRoot(), filepath.FromSlash(c6FixtureDir))
	loaded, err := c6Load(root, "c6fixture")
	if err != nil {
		return nil, err
	}
	files, err := productionFiles(root)
	if err != nil {
		return nil, err
	}
	return &c6FixtureLoad{root: root, loaded: loaded, files: files}, nil
})

func c6FixtureViolations(t *testing.T, table map[string]c6Kind) []string {
	t.Helper()
	f, err := c6Fixture()
	if err != nil {
		t.Fatalf("load fixture module: %v", err)
	}
	return c6Evaluate(f.loaded, table, f.files)
}

func c6Edited(edit func(map[string]c6Kind)) map[string]c6Kind {
	table := maps.Clone(c6FixtureTable)
	edit(table)
	return table
}

func c6Joined(lines []string) string { return strings.Join(lines, "\n") }

func TestC6FixtureRejectsEveryRuleShape(t *testing.T) {
	const tail = " outside internal/ops and internal/db"
	want := []string{
		// A renamed import and receiver still resolve to the Blackboard method.
		"cmd/tool/main.go:8:13: C6 M1: db.Blackboard.Write" + tail,
		// A literal, a named function variable and a variable holding the API.
		"internal/agent/callbacks.go:10:10: C6 C1: ops.ModifyWithAgentAuthority (mutation callback)" + tail,
		"internal/agent/callbacks.go:12:10: C6 C1: ops.ModifyWithAgentAuthority (mutation callback)" + tail,
		"internal/agent/callbacks.go:13:13: C6 C1: ops.ModifyWithAgentAuthority (mutation callback)" + tail,
		// Every mutator, called directly.
		"internal/agent/direct.go:12:9: C6 M1: db.Blackboard.Modify" + tail,
		"internal/agent/direct.go:13:9: C6 M1: db.Blackboard.Write" + tail,
		"internal/agent/direct.go:14:9: C6 M1: db.Blackboard.UpdateTask" + tail,
		"internal/agent/direct.go:15:9: C6 M1: db.Blackboard.UpdateAgent" + tail,
		"internal/agent/direct.go:16:12: C6 M1: db.Blackboard.ArchiveTerminalTask" + tail,
		// Interface indirection with the same name and signature.
		"internal/agent/iface.go:10:11: C6 M2: agent.store.Modify (same name and signature as a mutating db.Blackboard method)" + tail,
		// Method value, method expression, handle chain and promoted method.
		"internal/agent/indirect.go:10:33: C6 M1: db.Blackboard.Write" + tail,
		"internal/agent/indirect.go:12:28: C6 M1: db.Blackboard.Write" + tail,
		"internal/agent/indirect.go:13:18: C6 M1: db.Blackboard.Write" + tail,
		"internal/agent/indirect.go:8:14: C6 M1: db.Blackboard.Write" + tail,
		// A write in a file only the windows configuration builds.
		"internal/agent/platform_windows.go:6:9: C6 M1: db.Blackboard.Write" + tail,
		// A file no configuration builds.
		"internal/agent/unknown_tag.go: C6 B1: production file is not type-checked by any of linux, darwin, windows (unknown build constraint); the guard cannot see it",
	}
	got := c6FixtureViolations(t, c6FixtureTable)
	if c6Joined(got) != c6Joined(want) {
		t.Fatalf("fixture diagnostics differ:\n got:\n%s\nwant:\n%s", c6Joined(got), c6Joined(want))
	}
}

func TestC6FixtureAcceptsEveryLegalCase(t *testing.T) {
	for _, line := range c6FixtureViolations(t, c6FixtureTable) {
		for _, legal := range []string{
			"internal/legal/",       // read-only methods, lock callbacks, unrelated Write, side records
			"internal/ops/",         // the owner of the boundary, callbacks included
			"internal/db/",          // the persistence mechanics
			"internal/testhelpers/", // test support
			"_test.go",              // tests
		} {
			if strings.Contains(line, legal) {
				t.Errorf("legal case rejected: %s", line)
			}
		}
	}
}

func TestC6FixtureDiagnosticsNameOnlyRelativePaths(t *testing.T) {
	f, err := c6Fixture()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range c6FixtureViolations(t, c6FixtureTable) {
		if strings.Contains(line, f.root) || strings.HasPrefix(line, "/") {
			t.Errorf("diagnostic names an absolute path: %s", line)
		}
	}
}

// The table, not the method name, decides what a mutator is: reclassifying
// Write removes exactly its diagnostics.
func TestC6ClassificationDrivesTheMutatorRule(t *testing.T) {
	table := c6Edited(func(m map[string]c6Kind) { m["Write"] = c6ReadOnly })
	for _, line := range c6FixtureViolations(t, table) {
		if strings.Contains(line, "db.Blackboard.Write") {
			t.Errorf("read-only Write still reported: %s", line)
		}
	}
}

func TestC6UnclassifiedBlackboardMethodFailsK1(t *testing.T) {
	table := c6Edited(func(m map[string]c6Kind) { delete(m, "ReadSnapshot") })
	got := c6Joined(c6FixtureViolations(t, table))
	if !strings.Contains(got, "C6 K1: exported db.Blackboard.ReadSnapshot has no classification") {
		t.Fatalf("missing K1 for an unclassified method:\n%s", got)
	}
}

func TestC6StaleClassificationEntryFailsK1(t *testing.T) {
	table := c6Edited(func(m map[string]c6Kind) { m["Retired"] = c6Mutating })
	got := c6Joined(c6FixtureViolations(t, table))
	if !strings.Contains(got, "C6 K1: classification names db.Blackboard.Retired") {
		t.Fatalf("missing K1 for a stale entry:\n%s", got)
	}
}

func TestC6FullTableRaisesNoK1(t *testing.T) {
	if got := c6Joined(c6FixtureViolations(t, c6FixtureTable)); strings.Contains(got, "K1") {
		t.Fatalf("complete table reported K1:\n%s", got)
	}
}

// The real table names the five mutators of the Blackboard, ArchiveTerminalTask
// included (it publishes an archive object and rewrites its task argument).
func TestC6BlackboardTableMarksTheMutators(t *testing.T) {
	var mutating []string
	for name, kind := range c6BlackboardMethods {
		if kind == c6Mutating {
			mutating = append(mutating, name)
		}
	}
	want := []string{"ArchiveTerminalTask", "Modify", "UpdateAgent", "UpdateTask", "Write"}
	if got := c6Joined(sortedCopy(mutating)); got != c6Joined(want) {
		t.Fatalf("mutating methods = %s, want %s", got, c6Joined(want))
	}
}

func writeC6Module(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A module without db.Blackboard or the callback API must fail the self-check
// instead of passing vacuously.
func TestC6SelfCheckFailsWhenBlackboardIsUnresolved(t *testing.T) {
	root := writeC6Module(t, map[string]string{
		"go.mod":           "module c6empty\n\ngo 1.22\n",
		"cmd/tool/main.go": "package main\n\nfunc main() {}\n",
	})
	got, err := c6Run(root, c6FixtureTable)
	if err != nil {
		t.Fatalf("c6Run: %v", err)
	}
	text := c6Joined(got)
	for _, what := range []string{"package internal/db", "package internal/ops"} {
		if !strings.Contains(text, "C6 S1: "+what+" unresolved") {
			t.Errorf("no S1 for %s:\n%s", what, text)
		}
	}
}

// A listing failure names the configuration and no absolute path.
func TestC6ListingFailureNamesConfigurationWithoutAbsolutePaths(t *testing.T) {
	root := writeC6Module(t, map[string]string{
		"go.mod":           "module c6broken\n\nthis is not a go.mod\n",
		"cmd/tool/main.go": "package main\n\nfunc main() {}\n",
	})
	_, err := c6Run(root, c6FixtureTable)
	if err == nil {
		t.Fatal("a broken module must fail the guard, not pass it")
	}
	if !strings.Contains(err.Error(), "GOOS=") {
		t.Errorf("failure does not name the configuration: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(root)
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), resolved) {
		t.Errorf("failure names an absolute path: %v", err)
	}
}

// A package that does not type-check fails the guard: an unchecked package
// would hide a write.
func TestC6TypeErrorFailsTheGuard(t *testing.T) {
	root := writeC6Module(t, map[string]string{
		"go.mod":              "module c6typeerr\n\ngo 1.22\n",
		"internal/bad/bad.go": "package bad\n\nvar x int = \"not an int\"\n",
	})
	_, err := c6Run(root, c6FixtureTable)
	if err == nil || !strings.Contains(err.Error(), "internal/bad") {
		t.Fatalf("a type error must fail the guard naming the package, got %v", err)
	}
}

// c6SyntheticEnviron is an explicitly constructed environment: the variables the
// child needs, plus synthetic stand-ins for values that must never be echoed.
func c6SyntheticEnviron() []string {
	return []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=/tmp",
		"GOROOT=/c6-synthetic-goroot-marker",
		"GOFLAGS=-c6-synthetic-flags-marker",
		"C6_SYNTHETIC_SECRET=c6-synthetic-secret-marker",
	}
}

// The child inherits only the allowlisted variables.
func TestC6ChildEnvWithholdsEverythingOutsideTheAllowlist(t *testing.T) {
	env, _ := c6ChildEnv(c6SyntheticEnviron(), "linux")
	joined := strings.Join(env, "\n")
	for _, withheld := range []string{"GOFLAGS", "C6_SYNTHETIC_SECRET", "c6-synthetic-secret-marker", "c6-synthetic-flags-marker"} {
		if strings.Contains(joined, withheld) {
			t.Errorf("child environment carries %s", withheld)
		}
	}
	for _, kept := range []string{"HOME=", "PATH=", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"} {
		if !strings.Contains(joined, kept) {
			t.Errorf("child environment lacks %s", kept)
		}
	}
}

// A listing failure that echoes a retained variable's value (a bad GOROOT)
// reports the variable name, never the value, however short the value is, and
// still names the configuration.
func TestC6ListingFailureNeverEchoesInheritedEnvironmentValues(t *testing.T) {
	root := writeC6Module(t, map[string]string{
		"go.mod":           "module c6env\n\ngo 1.22\n",
		"cmd/tool/main.go": "package main\n\nfunc main() {}\n",
	})
	for _, goroot := range []string{"/c6-synthetic-goroot-marker", "abc", "x"} {
		environ := append(c6SyntheticEnviron()[:3:3], "GOROOT="+goroot,
			"GOFLAGS=-c6-synthetic-flags-marker", "C6_SYNTHETIC_SECRET=c6-synthetic-secret-marker")
		_, err := c6List(root, "linux", environ)
		if err == nil {
			t.Fatalf("GOROOT=%q must fail the listing", goroot)
		}
		text := err.Error()
		if strings.Contains(text, "cannot find GOROOT directory: "+goroot) || strings.Contains(text, "directory: /"+goroot) {
			t.Errorf("failure echoes the value %q: %v", goroot, err)
		}
		for _, leaked := range []string{"c6-synthetic-flags-marker", "c6-synthetic-secret-marker", "c6-synthetic-goroot-marker"} {
			if strings.Contains(text, leaked) {
				t.Errorf("failure echoes an environment value (%s) for GOROOT=%q: %v", leaked, goroot, err)
			}
		}
		if !strings.Contains(text, "GOOS=linux") || !strings.Contains(text, "$GOROOT") {
			t.Errorf("failure lost the configuration or the variable name for GOROOT=%q: %v", goroot, err)
		}
	}
}

// A withheld variable can no longer break the child: a host GOFLAGS the guard
// does not read is ignored instead of failing with its value in the message.
func TestC6HostGoflagsDoNotReachTheListing(t *testing.T) {
	root := writeC6Module(t, map[string]string{
		"go.mod":           "module c6env\n\ngo 1.22\n",
		"cmd/tool/main.go": "package main\n\nfunc main() {}\n",
	})
	environ := append(c6SyntheticEnviron()[:3:3], "GOFLAGS=-c6-synthetic-flags-marker")
	if _, err := c6List(root, "linux", environ); err != nil {
		t.Fatalf("withheld GOFLAGS still reached go list: %v", err)
	}
}
