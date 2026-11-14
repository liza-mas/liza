package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
)

// The MAS init cases run the full InitCommandWithConfig (composition coverage)
// with a report, in a temp repository with a fixture global directory.

func initWithReport(t *testing.T, root string) *jsonout.Report {
	t.Helper()
	report := jsonout.NewReport()
	if err := InitCommandWithConfig(InitParams{Description: "Goal", SpecRef: "specs/vision.md", Report: report}); err != nil {
		t.Fatalf("MAS init in %s: %v", root, err)
	}
	return report
}

func pointerChecks(report *jsonout.Report) []jsonout.Check {
	var out []jsonout.Check
	for _, check := range report.Checks {
		if check.ID == "guardrails.invariant-pointer" {
			out = append(out, check)
		}
	}
	return out
}

func renderedTemplates(t *testing.T) (guardrails, invariants string) {
	t.Helper()
	root := t.TempDir()
	if err := embedded.WriteGuardrails(root); err != nil {
		t.Fatal(err)
	}
	if created, err := embedded.CreateInvariants(root); err != nil || !created {
		t.Fatalf("CreateInvariants: created=%v err=%v", created, err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return read("GUARDRAILS.md"), read("INVARIANTS.md")
}

func TestInvariantHomeMASInitFreshProjectGetsBothFiles(t *testing.T) {
	root := chdirToInitRepoForTest(t)
	wantGuardrails, wantInvariants := renderedTemplates(t)

	report := initWithReport(t, root)

	for name, want := range map[string]string{"GUARDRAILS.md": wantGuardrails, "INVARIANTS.md": wantInvariants} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != want {
			t.Errorf("%s is not the rendered template (err=%v)", name, err)
		}
	}
	if checks := pointerChecks(report); len(checks) != 0 {
		t.Errorf("fresh project reported pointer checks: %+v", checks)
	}
	if report.Degraded() {
		t.Errorf("fresh init degraded: %v", report.NotOK())
	}
}

func TestInvariantHomeMASInitReportsMissingPointer(t *testing.T) {
	root := chdirToInitRepoForTest(t)
	const userGuardrails = "# Project Guardrails\n\n## Tier 1\n\nbe careful\n"
	guardrailsPath := filepath.Join(root, "GUARDRAILS.md")
	if err := os.WriteFile(guardrailsPath, []byte(userGuardrails), 0o644); err != nil {
		t.Fatal(err)
	}
	wantGuardrails, wantInvariants := renderedTemplates(t)

	report := initWithReport(t, root)

	if got, _ := os.ReadFile(filepath.Join(root, "INVARIANTS.md")); string(got) != wantInvariants {
		t.Error("INVARIANTS.md is not the skeleton")
	}
	if got, _ := os.ReadFile(guardrailsPath); string(got) != userGuardrails {
		t.Errorf("user-owned GUARDRAILS.md was rewritten: %q", got)
	}
	checks := pointerChecks(report)
	if len(checks) != 1 {
		t.Fatalf("pointer checks = %+v, want exactly one", checks)
	}
	check := checks[0]
	if check.Status != jsonout.CheckManual || check.Severity != jsonout.SeverityWarn || filepath.Base(check.Path) != "GUARDRAILS.md" {
		t.Errorf("check = %+v, want manual warn on GUARDRAILS.md", check)
	}
	section := wantGuardrails[strings.Index(wantGuardrails, "### Invariant compliance"):strings.Index(wantGuardrails, "## Tier 2")]
	if !strings.Contains(check.Fix, strings.TrimSpace(section)) {
		t.Errorf("fix does not carry the template rule section:\n%s", check.Fix)
	}
	if !report.Degraded() {
		t.Error("report is not degraded, so the CLI would exit 0 instead of 4")
	}
}

func TestInvariantHomeMASInitLeavesExistingInvariants(t *testing.T) {
	const arbitrary = "\x00arbitrary \xff bytes\n"
	t.Run("regular file", func(t *testing.T) {
		root := chdirToInitRepoForTest(t)
		path := filepath.Join(root, "INVARIANTS.md")
		if err := os.WriteFile(path, []byte(arbitrary), 0o600); err != nil {
			t.Fatal(err)
		}
		initWithReport(t, root)
		if got, _ := os.ReadFile(path); string(got) != arbitrary {
			t.Errorf("INVARIANTS.md rewritten: %q", got)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on Windows")
		}
		root := chdirToInitRepoForTest(t)
		target := filepath.Join(t.TempDir(), "shared.md")
		if err := os.WriteFile(target, []byte(arbitrary), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "INVARIANTS.md")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		initWithReport(t, root)
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("link now points to %q (%v), want %q", got, err, target)
		}
		if got, _ := os.ReadFile(target); string(got) != arbitrary {
			t.Errorf("symlink target rewritten: %q", got)
		}
	})
}

// TestInvariantHomeMASInitReportsInvariantsWriteFailure calls
// installProviderAssets directly: full init returns from createLayout before
// the writer when the root is unwritable.
func TestInvariantHomeMASInitReportsInvariantsWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root POSIX user to make the project root unwritable")
	}
	f := newAssetFixture(t)
	if err := os.Chmod(f.root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.root, 0o755) })
	m := &masInit{
		params:            InitParams{AutoConfirm: true, Report: f.report},
		rawStdin:          strings.NewReader(""),
		stdin:             emptyStdin(),
		confirmOptions:    embedded.ConfirmOptions{AutoConfirm: true},
		selectedProviders: []providers.Provider{},
		lizaPaths:         paths.New(f.root),
	}

	m.installProviderAssets()

	assertReportedCheck(t, f.report, "init.asset", jsonout.CheckFailed, "INVARIANTS.md")
	if checks := pointerChecks(f.report); len(checks) != 0 {
		t.Errorf("pointer checks = %+v, want none: GUARDRAILS.md could not be written, so it is absent", checks)
	}
}
