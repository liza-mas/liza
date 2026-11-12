package jsonout

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the outcome-check golden file")

func TestCheckDefinitionAppliesSeverityRule(t *testing.T) {
	definition := CheckDefinition{ID: "x.y", Severity: SeverityWarn}
	if got := definition.Check(CheckOK, "", "", "fine", "").Severity; got != SeverityInfo {
		t.Fatalf("ok check severity = %q, want info", got)
	}
	for _, status := range []CheckStatus{CheckMissing, CheckFailed, CheckManual} {
		if got := definition.Check(status, "", "", "not fine", "").Severity; got != SeverityWarn {
			t.Fatalf("%s check severity = %q, want the declared warn", status, got)
		}
	}
	if got := (CheckDefinition{ID: "x.z", Severity: SeverityError}).Check(CheckFailed, "", "", "", "").Severity; got != SeverityError {
		t.Fatalf("declared error severity = %q, want error", got)
	}
}

func TestReportOutcome(t *testing.T) {
	var nilReport *Report
	nilReport.Add(Check{ID: "ignored", Status: CheckFailed})
	if nilReport.Degraded() || nilReport.NotOK() != nil {
		t.Fatal("a nil report must collect nothing and be not degraded")
	}

	report := NewReport()
	definition := CheckDefinition{ID: "contract.placement", Severity: SeverityWarn}
	report.Add(definition.Check(CheckOK, "codex", "/r/AGENTS.md", "active", ""))
	if report.Degraded() {
		t.Fatal("an all-ok report must not be degraded")
	}
	report.Add(definition.Check(CheckFailed, "claude", "/h/.claude/CLAUDE.md", "occupied", "move it aside"))
	if !report.Degraded() || len(report.NotOK()) != 1 || report.NotOK()[0].Provider != "claude" {
		t.Fatalf("NotOK = %v, want the one failed claude check", report.NotOK())
	}
}

// TestOutcomeEnvelopeGolden pins the wire format of a degraded outcome. The
// CLI callers parse it, so a change here is a breaking CLI change
// (specs/protocols/outcome-checks.md). Regenerate with -update only for an
// intended format change.
func TestOutcomeEnvelopeGolden(t *testing.T) {
	report := NewReport()
	placement := CheckDefinition{ID: "contract.placement", Severity: SeverityWarn}
	report.Add(placement.Check(CheckOK, "codex", "/repo/AGENTS.md", "contract is active at /repo/AGENTS.md", ""))
	report.Add(placement.Check(CheckFailed, "claude", "/home/u/.claude/CLAUDE.md",
		"contract is active at /repo/CLAUDE.md, not at /home/u/.claude/CLAUDE.md: it is occupied by a non-product file",
		"move /home/u/.claude/CLAUDE.md aside, then re-run init"))
	report.Add(CheckDefinition{ID: "init.asset", Severity: SeverityWarn}.Check(CheckFailed, "", "",
		"failed to write codex hooks: permission denied", "resolve the error, then re-run init"))

	var buf bytes.Buffer
	if err := WriteResult(&buf, report, nil, nil); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "outcome-envelope.golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("outcome envelope changed; this format crosses repositories\ngot:\n%s\nwant:\n%s", buf.Bytes(), want)
	}
}
