package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The invariant-home cases of refresh-project (FR8) run the built command on
// a repository prepared by the real init, like cmd_refresh_project_test.go.

const userGuardrailsWithoutPointer = "# Project Guardrails\n\n## Tier 1\n\nbe careful\n"

func TestRefreshProjectInvariantsCreatesSkeletonAndReportsMissingPointer(t *testing.T) {
	// GIVEN a prepared project with a GUARDRAILS.md that never names INVARIANTS.md and no INVARIANTS.md
	f := initRefreshFixture(t)
	guardrails := filepath.Join(f.repo, "GUARDRAILS.md")
	invariants := filepath.Join(f.repo, "INVARIANTS.md")
	writeFixtureFile(t, guardrails, userGuardrailsWithoutPointer)

	t.Run("json", func(t *testing.T) {
		// WHEN refreshing with --json
		stdout, _, code := f.run(t, "refresh-project", "--json")

		// THEN the run is degraded
		if code != exitDegraded {
			t.Fatalf("exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
		}
		env := decodeSingleEnvelope(t, stdout)
		// AND the skeleton exists with its altitude rule, reported by one ok refresh.asset check
		skeleton := readFixtureFile(t, invariants)
		if !strings.HasPrefix(skeleton, "# Invariants\n") || !strings.Contains(skeleton, "\n## Altitude rule\n") {
			t.Errorf("INVARIANTS.md is not the skeleton:\n%s", skeleton)
		}
		if check := refreshChecks(t, env)[invariants]; check == nil || check["status"] != "ok" {
			t.Errorf("refresh.asset check for %s = %v, want ok", invariants, check)
		}
		// AND GUARDRAILS.md is byte-identical
		if got := readFixtureFile(t, guardrails); got != userGuardrailsWithoutPointer {
			t.Errorf("GUARDRAILS.md was rewritten: %q", got)
		}
		// AND exactly one manual pointer check carries the rule to add
		pointers := invariantPointerChecks(t, env)
		if len(pointers) != 1 {
			t.Fatalf("pointer checks = %v, want exactly one", pointers)
		}
		fix, _ := pointers[0]["fix"].(string)
		if pointers[0]["status"] != "manual" || pointers[0]["severity"] != "warn" || pointers[0]["path"] != guardrails || !strings.Contains(fix, "### Invariant compliance") {
			t.Errorf("pointer check = %v, want a manual warn on %s whose fix holds the rule section", pointers[0], guardrails)
		}
	})

	t.Run("human", func(t *testing.T) {
		// GIVEN the skeleton gone again
		if err := os.Remove(invariants); err != nil {
			t.Fatal(err)
		}

		// WHEN refreshing without --json
		stdout, stderr, code := f.run(t, "refresh-project")

		// THEN stderr warns about the missing pointer and stdout lists the created file
		if code != exitDegraded {
			t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, exitDegraded, stderr)
		}
		if !strings.Contains(stderr, "Warning: GUARDRAILS.md has no rule pointing to INVARIANTS.md") || !strings.Contains(stderr, "### Invariant compliance") {
			t.Errorf("stderr lacks the pointer warning and rule:\n%s", stderr)
		}
		if !strings.Contains(stdout, "  "+invariants+"\n") {
			t.Errorf("stdout does not list %s:\n%s", invariants, stdout)
		}
	})
}

func TestRefreshProjectInvariantsLeavesExistingFile(t *testing.T) {
	const arbitrary = "\x00mine \xff entirely\n"
	t.Run("regular file", func(t *testing.T) {
		f := initRefreshFixture(t)
		path := filepath.Join(f.repo, "INVARIANTS.md")
		writeFixtureFile(t, path, arbitrary)

		stdout, _, code := f.run(t, "refresh-project", "--json")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
		}
		if got := readFixtureFile(t, path); got != arbitrary {
			t.Errorf("INVARIANTS.md rewritten: %q", got)
		}
		if check := refreshChecks(t, decodeSingleEnvelope(t, stdout))[path]; check != nil {
			t.Errorf("refresh.asset check for the kept %s = %v, want none", path, check)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on Windows")
		}
		f := initRefreshFixture(t)
		link := filepath.Join(f.repo, "INVARIANTS.md")
		target := filepath.Join(t.TempDir(), "shared.md")
		writeFixtureFile(t, target, arbitrary)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}

		stdout, _, code := f.run(t, "refresh-project", "--json")

		if code != 0 {
			t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
		}
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("link now points to %q (%v), want %q", got, err, target)
		}
		if got := readFixtureFile(t, target); got != arbitrary {
			t.Errorf("symlink target rewritten: %q", got)
		}
		if check := refreshChecks(t, decodeSingleEnvelope(t, stdout))[link]; check != nil {
			t.Errorf("refresh.asset check for the kept %s = %v, want none", link, check)
		}
	})
}

func TestRefreshProjectInvariantsWithoutGuardrails(t *testing.T) {
	// GIVEN a prepared project with neither file
	f := initRefreshFixture(t)

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN INVARIANTS.md is created, GUARDRAILS.md stays absent (refresh never writes it) and nothing is degraded
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "INVARIANTS.md")); err != nil {
		t.Errorf("INVARIANTS.md not created: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.repo, "GUARDRAILS.md")); !os.IsNotExist(err) {
		t.Errorf("refresh-project wrote GUARDRAILS.md (stat err = %v)", err)
	}
	if pointers := invariantPointerChecks(t, decodeSingleEnvelope(t, stdout)); len(pointers) != 0 {
		t.Errorf("pointer checks = %v, want none: an absent GUARDRAILS.md is not lacking", pointers)
	}
}

func TestRefreshProjectInvariantsFailureIsHard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block writes on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	// GIVEN a prepared project whose root is not writable
	f := initRefreshFixture(t)
	if err := os.Chmod(f.repo, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.repo, 0o755) })

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN exit 1 with an error envelope naming INVARIANTS.md's path
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s", code, stdout)
	}
	env := decodeSingleEnvelope(t, stdout)
	errDetail, _ := env["error"].(map[string]any)
	message, _ := errDetail["message"].(string)
	if want := filepath.Join(f.repo, "INVARIANTS.md"); env["ok"] != false || !strings.Contains(message, want) {
		t.Fatalf("envelope = %v, want ok:false with a message naming %s", env, want)
	}
}
