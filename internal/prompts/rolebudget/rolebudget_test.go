package rolebudget_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/prompts/rolebudget"
)

const baselinePath = "testdata/baseline.json"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test directory")
		}
		dir = parent
	}
}

// TestRoleBudget measures every role variant and compares it with the
// committed baseline. While the baseline's gate is false the table is
// reported only; once it is true, growth above the ceiling in either column
// fails the test with the exact bytes, which is the overrun report the goal
// asks for. Regenerate deliberately with ROLEBUDGET_UPDATE=1.
func TestRoleBudget(t *testing.T) {
	current, err := rolebudget.Measure(repoRoot(t))
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(current.Variants) == 0 {
		t.Fatal("no role variants measured")
	}

	raw, err := os.ReadFile(baselinePath)
	if os.IsNotExist(err) || os.Getenv("ROLEBUDGET_UPDATE") != "" {
		if err == nil {
			var previous rolebudget.Report
			if jsonErr := json.Unmarshal(raw, &previous); jsonErr == nil {
				current.Gate = previous.Gate
			}
		}
		if os.Getenv("ROLEBUDGET_GATE") != "" {
			current.Gate = true
		}
		out, err := current.JSON()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselinePath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("baseline written to %s (gate=%v)", baselinePath, current.Gate)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var baseline rolebudget.Report
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}

	deltas := rolebudget.Compare(baseline, current)
	table := rolebudget.Table(deltas)
	t.Logf("role budget (gate=%v, ceiling %.0f%%):\n%s", baseline.Gate, 100*rolebudget.Ceiling, table)

	for _, d := range deltas {
		if d.MissingInBaseline {
			t.Errorf("variant %q is not in the baseline; regenerate with ROLEBUDGET_UPDATE=1", d.Variant)
		}
		if d.MissingInCurrent {
			t.Errorf("variant %q is in the baseline but no longer measured; a role or wake trigger disappeared", d.Variant)
		}
	}
	if !baseline.Gate {
		return
	}
	var over []string
	for _, d := range deltas {
		if d.ExceedsCeiling {
			over = append(over, d.Variant)
		}
	}
	if len(over) > 0 {
		t.Errorf("context budget exceeded for %s:\n%s\nReport the exact overrun for a scoped decision; do not remove binding rules to pass.",
			strings.Join(over, ", "), table)
	}
}

// TestCompareFlagsCeilingPerColumn pins the gate semantics: either column
// over the ceiling flags the variant, exactly at the ceiling does not, and a
// variant absent from the baseline is reported rather than silently passed.
func TestCompareFlagsCeilingPerColumn(t *testing.T) {
	baseline := rolebudget.Report{Variants: []rolebudget.RoleVariant{
		{Variant: "a", RenderedBytes: 1000, MandatoryReadBytes: 2000},
		{Variant: "b", RenderedBytes: 1000, MandatoryReadBytes: 2000},
		{Variant: "c", RenderedBytes: 1000, MandatoryReadBytes: 2000},
	}}
	current := rolebudget.Report{Variants: []rolebudget.RoleVariant{
		{Variant: "a", RenderedBytes: 1050, MandatoryReadBytes: 2000}, // exactly 5%: allowed
		{Variant: "b", RenderedBytes: 1000, MandatoryReadBytes: 2101}, // reads over
		{Variant: "c", RenderedBytes: 1051, MandatoryReadBytes: 1900}, // prompt over, reads shrank
		{Variant: "d", RenderedBytes: 1, MandatoryReadBytes: 1},
	}}
	baseline.Variants = append(baseline.Variants, rolebudget.RoleVariant{Variant: "gone", RenderedBytes: 1, MandatoryReadBytes: 1})
	deltas := rolebudget.Compare(baseline, current)
	want := map[string][3]bool{"a": {false, false, false}, "b": {true, false, false}, "c": {true, false, false}, "d": {false, true, false}, "gone": {false, false, true}}
	if len(deltas) != len(want) {
		t.Fatalf("got %d deltas, want %d", len(deltas), len(want))
	}
	for _, d := range deltas {
		w := want[d.Variant]
		if d.ExceedsCeiling != w[0] || d.MissingInBaseline != w[1] || d.MissingInCurrent != w[2] {
			t.Errorf("%s: exceeds=%v missingInBaseline=%v missingInCurrent=%v, want %v", d.Variant, d.ExceedsCeiling, d.MissingInBaseline, d.MissingInCurrent, w)
		}
	}
	table := rolebudget.Table(deltas)
	for _, marker := range []string{"OVER", "not in baseline", "no longer measured"} {
		if !strings.Contains(table, marker) {
			t.Errorf("table lacks marker %q:\n%s", marker, table)
		}
	}
}
