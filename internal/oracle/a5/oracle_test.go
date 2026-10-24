package a5

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) { oracle.RunMain(m) }

type step struct {
	Agent string `json:"agent,omitempty"`
	// Operator steps use the existing operator entry: no --agent-id and no
	// agent identity or generation environment.
	Operator bool `json:"operator,omitempty"`
	// StaleGeneration presents a generation the registered agent does not hold.
	StaleGeneration bool     `json:"stale_generation,omitempty"`
	Args            []string `json:"args,omitempty"`
	Adapter         string   `json:"adapter,omitempty"`
	// Commit is the repository delta of the worktree-commit adapter.
	Commit *recipeStep `json:"commit,omitempty"`
	// Capture stores the first group of Pattern, matched against the step's
	// output, under Label for later {{capture:Label}} substitution.
	Capture  *captureSpec `json:"capture,omitempty"`
	Exit     int          `json:"exit"`
	Contains string       `json:"contains,omitempty"`
}

type captureSpec struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
}

type caseInput struct {
	ID              string          `json:"id"`
	Base            string          `json:"base"`
	Delta           json.RawMessage `json:"state_delta"`
	RepositoryDelta []recipeStep    `json:"repository_delta,omitempty"`
	// RecipeExtra appends labelled integration commits to the base recipe.
	RecipeExtra []recipeStep `json:"recipe_extra,omitempty"`
	Prepare     []step       `json:"prepare"`
	Step        step         `json:"step"`
	Modes       []string     `json:"modes"`
}

// This allocation is independent of fixture discovery. Removing a manifest or
// a capture mode must fail instead of silently shrinking the frozen surface.
var inventory = map[string]string{
	"SR1": "json human state", "SR2": "json human state",
	"AE1": "json state", "AE2": "json human state", "AE3": "state",
	"SV1": "json human state", "SV2": "json human state", "SV3": "json state", "SV4": "json state", "SV5": "json human state", "SV6": "json state",
	"AV1": "json human state", "AV2": "json human", "AV3": "json",
	"AR1": "json human state", "AR2": "json", "AR3": "json",
	"RC1": "state", "RC2": "state", "RC3": "state", "RC4": "state", "RC5": "state",
	"RCA1": "json human state", "RCA2": "json", "RCA3": "json human state", "RCA4": "json",
	"VP1": "state", "VP2": "state", "UR1": "json human state", "UR2": "json",
	"CS1": "json human state", "CS2": "human", "RV1": "json human state", "RV2": "json",
	"RP1": "json human state", "RP2": "json", "AA1": "json human state", "AA2": "human", "RA1": "human", "RA2": "human state",
}

func loadCases(t *testing.T) []caseInput {
	t.Helper()
	paths, err := filepath.Glob("testdata/fixtures/cases/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 40 || len(inventory) != 40 {
		t.Fatalf("inventory: got %d manifests, want exactly 40", len(paths))
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
		if c.Base == "" || (c.Step.Adapter == "" && len(c.Step.Args) == 0) {
			t.Fatalf("incomplete case %s", c.ID)
		}
		cases = append(cases, c)
	}
	return cases
}

func TestOracleA5(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixtures use shell commands and POSIX paths")
	}
	for _, c := range loadCases(t) {
		t.Run(c.ID, func(t *testing.T) {
			modes := c.Modes
			if c.Step.Adapter == "" {
				modes = withoutState(modes)
			}
			for _, mode := range modes {
				t.Run(mode, func(t *testing.T) {
					f := materialise(t, c)
					invoke := func(s step, mode string) oracle.Result {
						r := f.invoke(t, s, mode)
						f.remember(t, s, r)
						return r
					}
					before, r, err := runCase(c, mode, invoke, func() []byte { return read(t, f.statePath()) })
					if err != nil {
						t.Fatal(err)
					}
					if r.ExitCode != c.Step.Exit {
						t.Fatalf("%s step exit=%d want=%d: %s %s", c.ID, r.ExitCode, c.Step.Exit, r.Stdout, r.Stderr)
					}
					if c.Step.Contains != "" && !strings.Contains(string(r.Stdout)+string(r.Stderr), f.resolve(c.Step.Contains)) {
						t.Fatalf("%s step did not reach %s: %s %s", c.ID, c.Step.Contains, r.Stdout, r.Stderr)
					}
					capture(t, f, c, mode, r, before, read(t, f.statePath()))
				})
			}
		})
	}
}

// runCase is the case protocol: every preparation step reaches its declared
// exit before the before-state is read, and the tested step runs only then.
func runCase(c caseInput, mode string, invoke func(step, string) oracle.Result, snapshot func() []byte) ([]byte, oracle.Result, error) {
	for i, prep := range c.Prepare {
		if r := invoke(prep, "json"); r.ExitCode != prep.Exit {
			return nil, r, fmt.Errorf("%s preparation %d exit=%d want=%d before tested step: %s %s", c.ID, i, r.ExitCode, prep.Exit, r.Stdout, r.Stderr)
		}
	}
	before := snapshot()
	return before, invoke(c.Step, mode), nil
}

func withoutState(modes []string) []string {
	var out []string
	for _, mode := range modes {
		if mode != "state" {
			out = append(out, mode)
		}
	}
	return out
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
