package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
)

// The refresh-project tests run the built command in the isolated machine of
// degraded_outcome_cli_test.go, so exit codes and the single-envelope rule
// are observed as omni sees them (DEV-960 comment 10919).

// initRefreshFixture is a repository prepared by the real pairing init for
// Claude and Codex.
func initRefreshFixture(t *testing.T) *degradedFixture {
	t.Helper()
	f := newDegradedFixture(t)
	// The command reports paths under git's resolved toplevel: /private/var on
	// macOS, long names for Windows 8.3 temp paths (RUNNER~1).
	repo, err := filepath.EvalSymlinks(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	f.repo = repo
	if _, stderr, code := f.run(t, "setup", "--yes", "--claude", "--codex"); code != 0 {
		t.Fatalf("setup precondition: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := f.run(t, "init", "--claude", "--codex", "--yes"); code != 0 {
		t.Fatalf("init precondition: exit %d\n%s", code, stderr)
	}
	return f
}

// in returns a copy of f that runs in dir.
func (f *degradedFixture) in(dir string) *degradedFixture {
	c := *f
	c.repo = dir
	return &c
}

// refreshChecks returns result.checks of a refresh-project envelope by path.
func refreshChecks(t *testing.T, env map[string]any) map[string]map[string]any {
	t.Helper()
	result, _ := env["result"].(map[string]any)
	raw, ok := result["checks"].([]any)
	if !ok {
		t.Fatalf("envelope has no result.checks list: %v", env)
	}
	checks := map[string]map[string]any{}
	for _, item := range raw {
		check, _ := item.(map[string]any)
		if check["id"] != "refresh.asset" {
			t.Fatalf("unexpected check %v", check)
		}
		path, _ := check["path"].(string)
		checks[path] = check
	}
	return checks
}

func symlinkToUserScript(t *testing.T, link, target string) {
	t.Helper()
	writeFixtureFile(t, target, "#!/bin/bash\n# mine, no bypass\n")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestRefreshProjectJSONReportsKeptAndRefreshedFiles(t *testing.T) {
	// GIVEN an initialized project in system-prompt mode whose sources changed,
	// with one stale script and two gate scripts symlinked to user scripts that
	// lack the bypass
	f := initRefreshFixture(t)
	if _, stderr, code := f.run(t, "use-system-prompt"); code != 0 {
		t.Fatalf("use-system-prompt precondition: exit %d\n%s", code, stderr)
	}
	writeFixtureFile(t, filepath.Join(f.repo, "GUARDRAILS.md"), "# guardrails\n\nnew rule\n")
	staleGuard := filepath.Join(f.repo, ".claude", "hooks", "git-guard.sh")
	initGuard := readFixtureFile(t, staleGuard)
	writeFixtureFile(t, staleGuard, "#!/bin/bash\n# an older release\n")
	userScripts := t.TempDir()
	links := map[string]string{
		filepath.Join(f.repo, ".claude", "hooks", "session-context.sh"): filepath.Join(userScripts, "context.sh"),
		filepath.Join(f.repo, ".codex", "hooks", "enforce-init.sh"):     filepath.Join(userScripts, "gate.sh"),
	}
	for link, target := range links {
		symlinkToUserScript(t, link, target)
	}

	// WHEN refreshing with --json
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN the run is degraded and stdout is one envelope
	if code != exitDegraded {
		t.Fatalf("exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
	}
	env := decodeSingleEnvelope(t, stdout)
	if env["ok"] != true {
		t.Fatalf("envelope ok = %v, want true", env["ok"])
	}
	checks := refreshChecks(t, env)
	// AND each link is a failed warn check with a fix naming it, link and target untouched
	for link, target := range links {
		check := checks[link]
		fix, _ := check["fix"].(string)
		if check == nil || check["status"] != "failed" || check["severity"] != "warn" || !strings.Contains(fix, link) || !strings.Contains(fix, "refresh-project") {
			t.Errorf("check for %s = %v, want failed warn with a fix naming it", link, check)
		}
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("%s now points to %q (%v), want %s", link, got, err, target)
		}
		if readFixtureFile(t, target) != "#!/bin/bash\n# mine, no bypass\n" {
			t.Errorf("symlink target %s was written", target)
		}
	}
	// AND the stale script is back to init's bytes, and it and every bundle have an ok check
	if readFixtureFile(t, staleGuard) != initGuard {
		t.Error("git-guard.sh differs from what init wrote")
	}
	written := []string{staleGuard, filepath.Join(f.repo, paths.ProjectDirName(), "system-prompt-mode.json")}
	for _, mode := range []string{"multi_agent", "pairing", "subagent"} {
		bundle := filepath.Join(f.repo, paths.ProjectDirName(), "system-prompts", mode+".md")
		written = append(written, bundle)
		if !strings.Contains(readFixtureFile(t, bundle), "new rule") {
			t.Errorf("%s was not rebuilt from the changed GUARDRAILS.md", bundle)
		}
	}
	for _, path := range written {
		if check := checks[path]; check == nil || check["status"] != "ok" {
			t.Errorf("check for %s = %v, want ok", path, check)
		}
	}
	if len(checks) != len(links)+len(written) {
		t.Errorf("got %d checks, want %d: %v", len(checks), len(links)+len(written), checks)
	}
}

func TestRefreshProjectHumanModeNamesWrittenAndKeptFiles(t *testing.T) {
	// GIVEN an initialized project with one stale script and one symlinked script
	f := initRefreshFixture(t)
	stale := filepath.Join(f.repo, ".claude", "hooks", "git-guard.sh")
	writeFixtureFile(t, stale, "#!/bin/bash\n# an older release\n")
	link := filepath.Join(f.repo, ".claude", "hooks", "session-context.sh")
	symlinkToUserScript(t, link, filepath.Join(t.TempDir(), "context.sh"))

	// WHEN refreshing without --json
	stdout, stderr, code := f.run(t, "refresh-project")

	// THEN it is degraded, lists the written file and names the kept one
	if code != exitDegraded {
		t.Fatalf("exit = %d, want %d\nstderr:\n%s", code, exitDegraded, stderr)
	}
	if !strings.Contains(stdout, "refreshed 1 release-owned project assets in ") || !strings.Contains(stdout, "  "+stale+"\n") {
		t.Errorf("stdout does not list %s:\n%s", stale, stdout)
	}
	if !strings.Contains(stderr, "Warning: left "+link+" alone") || !strings.Contains(stderr, "refresh-project completed with 1 degraded check") {
		t.Errorf("stderr lacks the warning or the degraded summary:\n%s", stderr)
	}
}

func TestRefreshProjectJSONInUnpreparedRepositoryWritesNothing(t *testing.T) {
	// GIVEN a repository init never prepared
	f := newDegradedFixture(t)

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN exit 0, an empty check list, and a clean working tree
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	if checks := refreshChecks(t, decodeSingleEnvelope(t, stdout)); len(checks) != 0 {
		t.Fatalf("checks = %v, want none", checks)
	}
	status := exec.Command("git", "status", "--porcelain", "--ignored")
	status.Dir, status.Env = f.repo, f.env
	if out, err := status.CombinedOutput(); err != nil || len(out) != 0 {
		t.Fatalf("git status = %q (%v), want a clean tree", out, err)
	}
}

func TestRefreshProjectJSONIgnoresUnownedMalformedSettings(t *testing.T) {
	// GIVEN a repository init never prepared, whose own Claude and Codex configs do not parse
	f := newDegradedFixture(t)
	for _, dir := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(f.repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixtureFile(t, filepath.Join(f.repo, ".claude", "settings.json"), "{")
	writeFixtureFile(t, filepath.Join(f.repo, ".codex", "hooks.json"), "{")

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN it is the no-op: exit 0 and no checks
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	if checks := refreshChecks(t, decodeSingleEnvelope(t, stdout)); len(checks) != 0 {
		t.Fatalf("checks = %v, want none", checks)
	}
}

func TestRefreshProjectJSONOutsideRepositoryWritesNothing(t *testing.T) {
	// GIVEN a directory outside any git repository
	dir := t.TempDir()
	f := newDegradedFixture(t).in(dir)

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN exit 0, an empty check list, and nothing written
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	if checks := refreshChecks(t, decodeSingleEnvelope(t, stdout)); len(checks) != 0 {
		t.Fatalf("checks = %v, want none", checks)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("directory holds %v (%v), want it empty", entries, err)
	}
}

func TestRefreshProjectRefreshesTheLinkedWorktreeItRunsIn(t *testing.T) {
	// GIVEN a project whose installed hooks are committed, and a linked
	// worktree of it; both checkouts hold a stale script
	f := initRefreshFixture(t)
	f.git(t, "add", "-f", ".claude", ".codex")
	f.git(t, "-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "-m", "hooks")
	worktree := filepath.Join(filepath.Dir(f.repo), "worktree")
	f.git(t, "worktree", "add", "-q", worktree)
	mainGuard := filepath.Join(f.repo, ".claude", "hooks", "git-guard.sh")
	worktreeGuard := filepath.Join(worktree, ".claude", "hooks", "git-guard.sh")
	initGuard := readFixtureFile(t, mainGuard)
	for _, path := range []string{mainGuard, worktreeGuard} {
		writeFixtureFile(t, path, "#!/bin/bash\n# an older release\n")
	}

	// WHEN refreshing from the worktree
	stdout, _, code := f.in(worktree).run(t, "refresh-project", "--json")

	// THEN only the worktree's script is refreshed and reported
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s", code, stdout)
	}
	if readFixtureFile(t, worktreeGuard) != initGuard {
		t.Error("the worktree's git-guard.sh was not refreshed")
	}
	if readFixtureFile(t, mainGuard) != "#!/bin/bash\n# an older release\n" {
		t.Error("refreshing the worktree wrote the main checkout")
	}
	checks := refreshChecks(t, decodeSingleEnvelope(t, stdout))
	if len(checks) != 1 || checks[worktreeGuard] == nil {
		t.Fatalf("checks = %v, want one for %s", checks, worktreeGuard)
	}
}

func TestRefreshProjectJSONReportsMalformedHookConfig(t *testing.T) {
	// GIVEN an initialized project whose Claude settings no longer parse
	f := initRefreshFixture(t)
	settings := filepath.Join(f.repo, ".claude", "settings.json")
	writeFixtureFile(t, settings, "{")

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN it is degraded with a failed check on the config
	if code != exitDegraded {
		t.Fatalf("exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
	}
	check := refreshChecks(t, decodeSingleEnvelope(t, stdout))[settings]
	if check == nil || check["status"] != "failed" || !strings.Contains(check["fix"].(string), settings) {
		t.Fatalf("check for %s = %v, want failed with a fix naming it", settings, check)
	}
}

func TestRefreshProjectJSONFailsNamingTheUnwritableScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block writes on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	// GIVEN a stale script in a read-only hooks directory
	f := initRefreshFixture(t)
	hooksDir := filepath.Join(f.repo, ".claude", "hooks")
	stale := filepath.Join(hooksDir, "git-guard.sh")
	writeFixtureFile(t, stale, "#!/bin/bash\n# an older release\n")
	if err := os.Chmod(hooksDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o755) })

	// WHEN refreshing
	stdout, _, code := f.run(t, "refresh-project", "--json")

	// THEN exit 1 with an error envelope naming the script
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s", code, stdout)
	}
	env := decodeSingleEnvelope(t, stdout)
	errDetail, _ := env["error"].(map[string]any)
	message, _ := errDetail["message"].(string)
	if env["ok"] != false || !strings.Contains(message, stale) {
		t.Fatalf("envelope = %v, want ok:false with a message naming %s", env, stale)
	}
}
