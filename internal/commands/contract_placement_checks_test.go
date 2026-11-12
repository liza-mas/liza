package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/providers"
)

// placementFixture is one placement run: a git project, an isolated HOME and
// a contract target file.
type placementFixture struct {
	project string
	home    string
	target  string
}

func newPlacementFixture(t *testing.T) placementFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	f := placementFixture{project: setupGitRepo(t), home: home, target: filepath.Join(home, "global", "CORE.md")}
	f.write(t, f.target, "contract\n")
	return f
}

func (f placementFixture) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f placementFixture) link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// place runs the placement with a report and returns the single check of
// providerID.
func (f placementFixture) place(t *testing.T, agents []providers.Provider, options contractSymlinkOptions, providerID string) jsonout.Check {
	t.Helper()
	report := jsonout.NewReport()
	options.Report = report
	if _, err := createContractSymlinksForProviders(f.project, f.target, agents, options); err != nil {
		t.Fatalf("placement failed: %v", err)
	}
	var found []jsonout.Check
	for _, check := range report.Checks {
		if check.ID != "contract.placement" {
			t.Fatalf("unexpected check id %q", check.ID)
		}
		if check.Provider == providerID {
			found = append(found, check)
		}
	}
	if len(found) != 1 {
		t.Fatalf("checks for %s = %v, want exactly one", providerID, report.Checks)
	}
	return found[0]
}

func placementProvider(id, repoFile, global, local string, preferGlobal bool) providers.Provider {
	return providers.Provider{ID: id, Setup: providers.Setup{Contract: providers.ContractLinks{
		RepoFile:       repoFile,
		GlobalFallback: global,
		LocalFallback:  local,
		PreferGlobal:   &preferGlobal,
	}}}
}

func assertPlacementCheck(t *testing.T, check jsonout.Check, status jsonout.CheckStatus, path, messagePart string) {
	t.Helper()
	if check.Status != status || check.Path != path {
		t.Fatalf("check = %+v, want status %s at %s", check, status, path)
	}
	wantSeverity := jsonout.SeverityWarn
	if status == jsonout.CheckOK {
		wantSeverity = jsonout.SeverityInfo
	}
	if check.Severity != wantSeverity {
		t.Fatalf("check severity = %s, want %s", check.Severity, wantSeverity)
	}
	if !strings.Contains(check.Message, messagePart) {
		t.Fatalf("check message = %q, want it to contain %q", check.Message, messagePart)
	}
}

// TestContractPlacementChecks covers each placement outcome: the check comes
// from where the contract ended up, compared with what the action accepts.
func TestContractPlacementChecks(t *testing.T) {
	t.Run("preferred global, free: ok at global", func(t *testing.T) {
		f := newPlacementFixture(t)
		p := placementProvider("g", "G.md", ".g/G.md", "", true)
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{}, "g")
		assertPlacementCheck(t, check, jsonout.CheckOK, filepath.Join(f.home, ".g", "G.md"), "is active at")
	})
	t.Run("preferred global occupied by a file: repo activation is degraded", func(t *testing.T) {
		f := newPlacementFixture(t)
		global := filepath.Join(f.home, ".g", "G.md")
		f.write(t, global, "user\n")
		check := f.place(t, []providers.Provider{placementProvider("g", "G.md", ".g/G.md", "", true)}, contractSymlinkOptions{}, "g")
		assertPlacementCheck(t, check, jsonout.CheckFailed, global, "file")
		if !strings.Contains(check.Message, filepath.Join(f.project, "G.md")) || !strings.Contains(check.Fix, global) {
			t.Fatalf("check = %+v, want the actual repo location in the message and the occupied path in the fix", check)
		}
	})
	t.Run("preferred global occupied by a foreign symlink: degraded", func(t *testing.T) {
		f := newPlacementFixture(t)
		global := filepath.Join(f.home, ".g", "G.md")
		f.link(t, filepath.Join(f.home, "elsewhere.md"), global)
		check := f.place(t, []providers.Provider{placementProvider("g", "G.md", ".g/G.md", "", true)}, contractSymlinkOptions{}, "g")
		assertPlacementCheck(t, check, jsonout.CheckFailed, global, "symlink")
	})
	t.Run("preferred global unresolvable: degraded without a path", func(t *testing.T) {
		f := newPlacementFixture(t)
		t.Setenv("DEV945_TEST_ROOT", "relative/root")
		p := placementProvider("g", "G.md", ".g/G.md", "", true)
		p.Setup.Contract.GlobalFallbackEnv = "DEV945_TEST_ROOT"
		p.Setup.Contract.GlobalFallbackEnvSuffix = "G.md"
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{}, "g")
		assertPlacementCheck(t, check, jsonout.CheckFailed, "", "cannot be resolved")
	})
	t.Run("repo first, free: ok at repo", func(t *testing.T) {
		f := newPlacementFixture(t)
		check := f.place(t, []providers.Provider{placementProvider("r", "R.md", ".r/R.md", "", false)}, contractSymlinkOptions{}, "r")
		assertPlacementCheck(t, check, jsonout.CheckOK, filepath.Join(f.project, "R.md"), "is active at")
	})
	t.Run("repo first, already linked: ok", func(t *testing.T) {
		f := newPlacementFixture(t)
		repo := filepath.Join(f.project, "R.md")
		f.link(t, f.target, repo)
		check := f.place(t, []providers.Provider{placementProvider("r", "R.md", "", "", false)}, contractSymlinkOptions{}, "r")
		assertPlacementCheck(t, check, jsonout.CheckOK, repo, "is active at")
	})
	t.Run("repo first occupied: global fallback is degraded", func(t *testing.T) {
		f := newPlacementFixture(t)
		repo := filepath.Join(f.project, "R.md")
		f.write(t, repo, "user\n")
		check := f.place(t, []providers.Provider{placementProvider("r", "R.md", ".r/R.md", "", false)}, contractSymlinkOptions{}, "r")
		assertPlacementCheck(t, check, jsonout.CheckFailed, repo, filepath.Join(f.home, ".r", "R.md"))
	})
	t.Run("both occupied: local fallback is degraded", func(t *testing.T) {
		f := newPlacementFixture(t)
		global := filepath.Join(f.home, ".g", "G.md")
		f.write(t, global, "user\n")
		f.write(t, filepath.Join(f.project, "G.md"), "user\n")
		check := f.place(t, []providers.Provider{placementProvider("g", "G.md", ".g/G.md", "G.local.md", true)}, contractSymlinkOptions{}, "g")
		assertPlacementCheck(t, check, jsonout.CheckFailed, global, filepath.Join(f.project, "G.local.md"))
	})
	t.Run("repo link creation fails: local fallback is degraded", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a non-root POSIX user to make a directory unwritable")
		}
		f := newPlacementFixture(t)
		readOnly := filepath.Join(f.project, "ro")
		if err := os.MkdirAll(readOnly, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(readOnly, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
		check := f.place(t, []providers.Provider{placementProvider("r", "ro/R.md", "", "R.local.md", false)}, contractSymlinkOptions{}, "r")
		assertPlacementCheck(t, check, jsonout.CheckFailed, filepath.Join(readOnly, "R.md"), "could not be linked")
	})
	t.Run("local action: ok at local", func(t *testing.T) {
		f := newPlacementFixture(t)
		f.write(t, filepath.Join(f.project, "G.md"), "user\n")
		p := placementProvider("g", "G.md", ".g/G.md", "G.local.md", true)
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{ProviderActions: map[string]ContractAction{"g": ContractActionLocal}}, "g")
		assertPlacementCheck(t, check, jsonout.CheckOK, filepath.Join(f.project, "G.local.md"), "is active at")
	})
	t.Run("rename action: ok at repo", func(t *testing.T) {
		f := newPlacementFixture(t)
		f.write(t, filepath.Join(f.project, "R.md"), "user\n")
		p := placementProvider("r", "R.md", "", "", false)
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{ProviderActions: map[string]ContractAction{"r": ContractActionRename}}, "r")
		assertPlacementCheck(t, check, jsonout.CheckOK, filepath.Join(f.project, "R.md"), "is active at")
	})
	t.Run("global action on a repo-first conflict: ok at global", func(t *testing.T) {
		f := newPlacementFixture(t)
		f.write(t, filepath.Join(f.project, "R.md"), "user\n")
		p := placementProvider("r", "R.md", ".r/R.md", "", false)
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{ProviderActions: map[string]ContractAction{"r": ContractActionGlobal}}, "r")
		assertPlacementCheck(t, check, jsonout.CheckOK, filepath.Join(f.home, ".r", "R.md"), "is active at")
	})
	t.Run("skip action with an active global link: ok", func(t *testing.T) {
		f := newPlacementFixture(t)
		global := filepath.Join(f.home, ".r", "R.md")
		f.link(t, f.target, global)
		f.write(t, filepath.Join(f.project, "R.md"), "user\n")
		p := placementProvider("r", "R.md", ".r/R.md", "", false)
		check := f.place(t, []providers.Provider{p}, contractSymlinkOptions{ProviderActions: map[string]ContractAction{"r": ContractActionSkip}}, "r")
		assertPlacementCheck(t, check, jsonout.CheckOK, global, "is active at")
	})
	t.Run("shared repo file: preferred global and repo-only providers are both ok", func(t *testing.T) {
		f := newPlacementFixture(t)
		agents := []providers.Provider{
			placementProvider("g", "SHARED.md", ".g/SHARED.md", "", true),
			placementProvider("r", "SHARED.md", "", "", false),
		}
		report := jsonout.NewReport()
		if _, err := createContractSymlinksForProviders(f.project, f.target, agents, contractSymlinkOptions{Report: report}); err != nil {
			t.Fatal(err)
		}
		if report.Degraded() || len(report.Checks) != 2 {
			t.Fatalf("checks = %+v, want two ok checks", report.Checks)
		}
	})
	t.Run("no location at all stays a hard failure", func(t *testing.T) {
		f := newPlacementFixture(t)
		f.write(t, filepath.Join(f.project, "R.md"), "user\n")
		report := jsonout.NewReport()
		_, err := createContractSymlinksForProviders(f.project, f.target, []providers.Provider{placementProvider("r", "R.md", "", "", false)}, contractSymlinkOptions{Report: report})
		if err == nil {
			t.Fatal("placement error = nil, want the unplaced-provider error")
		}
		if len(report.Checks) != 0 {
			t.Fatalf("checks = %+v, want none on a hard failure", report.Checks)
		}
	})
}
