// Package supplement adds oracle coverage for A5 acceptance-evidence and
// validation-readiness refusals that the delivered 40-case A5 suite does not
// pin (goal-tests "coverage where needed"). It follows the delivered suite's
// conventions: committed inputs, goldens generated from the starting commit by
// the regeneration entry point, a CLI or adapter step per case, and no private
// seam. Base projects are the delivered suite's committed inputs, read
// unchanged from ../testdata/fixtures.
package supplement

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) { oracle.RunMain(m) }

// adapters maps a step's Adapter name to its driver. Each adapter file registers
// its own in init, so a later unit adds files and edits none.
var adapters = map[string]func(*testing.T, *fixture, step){}

type step struct {
	Agent string `json:"agent"`
	// Args is the CLI invocation; Adapter names an in-process operation instead.
	Args    []string `json:"args,omitempty"`
	Adapter string   `json:"adapter,omitempty"`
	// Params is the adapter's own configuration, decoded by the adapter.
	Params json.RawMessage `json:"params,omitempty"`
	Exit   int             `json:"exit"`
	// Contains must appear in the CLI output, or in the adapter's error text.
	Contains string `json:"contains,omitempty"`
}

type recipeStep struct {
	Label  string            `json:"label"`
	Files  map[string]string `json:"files"`
	Remove []string          `json:"remove,omitempty"`
}

type caseInput struct {
	ID    string          `json:"id"`
	Base  string          `json:"base"`
	Delta json.RawMessage `json:"state_delta"`
	// RecipeExtra appends labelled integration commits to the base recipe.
	RecipeExtra []recipeStep `json:"recipe_extra,omitempty"`
	Step        step         `json:"step"`
	Modes       []string     `json:"modes"`
}

// This allocation is independent of fixture discovery: removing a manifest or a
// capture mode must fail instead of silently shrinking the pinned surface.
//
// SA*: a submit refused by loadDeclaredAcceptanceInput (allocation review).
// VR*: a validation preflight refused or recorded by prepareValidationPreflight.
var inventory = map[string]string{
	"SA1": "json human state", "SA2": "json human state", "SA3": "json human state", "SA4": "json human state",
	"SA5": "json human state", "SA6": "json human state", "SA7": "json human state", "SA8": "json human state",
	"SA9": "json human state", "SA10": "json human state",
	"VR1": "state", "VR2": "state alerts", "VR3": "state alerts", "VR4": "state alerts",
	"VR5": "state alerts", "VR6": "state alerts",
}

func loadCases(t *testing.T) []caseInput {
	t.Helper()
	paths, err := filepath.Glob("testdata/fixtures/cases/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(inventory) {
		t.Fatalf("inventory: got %d manifests, want exactly %d", len(paths), len(inventory))
	}
	seen := map[string]bool{}
	var cases []caseInput
	for _, path := range paths {
		var c caseInput
		if err := json.Unmarshal(read(t, path), &c); err != nil {
			t.Fatal(err)
		}
		modes, ok := inventory[c.ID]
		if !ok || seen[c.ID] || !reflect.DeepEqual(c.Modes, strings.Fields(modes)) {
			t.Fatalf("inventory: invalid ID/modes in %s", path)
		}
		seen[c.ID] = true
		if c.Base == "" || (c.Step.Adapter == "") == (len(c.Step.Args) == 0) {
			t.Fatalf("incomplete case %s", c.ID)
		}
		cases = append(cases, c)
	}
	return cases
}

func TestOracleA5Supplement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixtures use shell commands and POSIX paths")
	}
	for _, c := range loadCases(t) {
		t.Run(c.ID, func(t *testing.T) {
			// "state" and "alerts" are extra captures of a run, not runs of their own.
			modes := slices.DeleteFunc(slices.Clone(c.Modes), func(m string) bool { return m == "state" || m == "alerts" })
			if c.Step.Adapter != "" {
				// An adapter step has no stdout mode: it records state and alerts once.
				modes = []string{"adapter"}
			}
			for _, mode := range modes {
				t.Run(mode, func(t *testing.T) {
					f := materialise(t, c)
					before := read(t, f.statePath())
					r := f.invoke(t, c.Step, mode)
					if r.ExitCode != c.Step.Exit {
						t.Fatalf("%s step exit=%d want=%d: %s %s", c.ID, r.ExitCode, c.Step.Exit, r.Stdout, r.Stderr)
					}
					if c.Step.Adapter == "" && c.Step.Contains != "" && !strings.Contains(string(r.Stdout)+string(r.Stderr), c.Step.Contains) {
						t.Fatalf("%s step did not reach %q: %s %s", c.ID, c.Step.Contains, r.Stdout, r.Stderr)
					}
					capture(t, f, c, mode, r, before, read(t, f.statePath()))
				})
			}
		})
	}
}

// liveOnly skips driver tests that read committed goldens: regeneration runs the
// package in write mode on a tree whose goldens were removed.
func liveOnly(t *testing.T) {
	t.Helper()
	if os.Getenv(oracle.WriteEnv) == "1" {
		t.Skip("reads committed goldens; not applicable in write mode")
	}
}

// expectedGoldens names the goldens of one inventory row.
func expectedGoldens(c caseInput) []string {
	var out []string
	if c.Step.Adapter != "" {
		out = append(out, "state/"+c.ID+".state.yaml")
		if slices.Contains(c.Modes, "alerts") {
			out = append(out, "human/"+c.ID+".alerts.txt")
		}
		return out
	}
	for _, mode := range c.Modes {
		switch mode {
		case "json":
			out = append(out, "json/"+c.ID+".json")
		case "human":
			out = append(out, "human/"+c.ID+".txt")
		default:
			continue
		}
		if slices.Contains(c.Modes, "state") {
			out = append(out, "state/"+c.ID+"."+mode+".yaml")
		}
	}
	return out
}

// TestSupplementGoldenCoverage requires exactly the goldens the inventory names.
func TestSupplementGoldenCoverage(t *testing.T) {
	liveOnly(t)
	want := map[string]bool{}
	for _, c := range loadCases(t) {
		for _, g := range expectedGoldens(c) {
			want[g] = true
		}
	}
	var got []string
	root := filepath.FromSlash("testdata/golden")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			got = append(got, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	for _, g := range got {
		if strings.HasSuffix(g, ".json-stderr.txt") {
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

func read(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func write(t testing.TB, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}

const harnessImport = "github.com/liza-mas/liza/internal/testhelpers/oracle"

// TestSupplementAdapterBoundary keeps every non-stdlib, non-harness import in
// adapter files, and keeps adapters untagged so they compile against the
// starting tree.
func TestSupplementAdapterBoundary(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	adapters := 0
	for _, file := range files {
		if strings.HasPrefix(file, "adapter") {
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
