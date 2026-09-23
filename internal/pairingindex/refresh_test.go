package pairingindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/filelock"
)

// installRefreshFixture installs an index script that appends one line to
// $LIZA_TEST_RUNS per run, and returns the repository and the runs file.
func installRefreshFixture(t *testing.T, extra string) (repo, runs string) {
	t.Helper()

	repo = initGitRepo(t)
	runs = filepath.Join(t.TempDir(), "runs.log")
	t.Setenv("LIZA_TEST_RUNS", runs)
	hooksDir, err := ResolveEffectiveHooksDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\n" + extra + "printf 'run\\n' >> \"$LIZA_TEST_RUNS\"\n"
	writeFile(t, filepath.Join(hooksDir, scriptName()), script, 0o755)
	return repo, runs
}

func refreshRunCount(t *testing.T, runs string) int {
	t.Helper()

	data, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

func refreshPathsForTest(t *testing.T, repo string) refreshPaths {
	t.Helper()

	commonDir, err := gitCommonDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	return refreshPathsIn(commonDir)
}

func TestRunRefreshRunsScriptAndConsumesRequest(t *testing.T) {
	repo, runs := installRefreshFixture(t, "")

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "post-commit"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}

	if got := refreshRunCount(t, runs); got != 1 {
		t.Fatalf("script runs = %d, want 1", got)
	}
	paths := refreshPathsForTest(t, repo)
	if _, err := os.Stat(paths.request); !os.IsNotExist(err) {
		t.Fatalf("request marker stat error = %v, want consumed", err)
	}
	if log := readFile(t, paths.log); !strings.Contains(log, "trigger post-commit") {
		t.Fatalf("refresh log = %q, want the trigger recorded", log)
	}
}

func TestRunRefreshReturnsAtOnceWhileAnotherRunHoldsTheLock(t *testing.T) {
	repo, runs := installRefreshFixture(t, "")
	paths := refreshPathsForTest(t, repo)
	held, acquired, err := filelock.New(paths.lockBase).TryHold("other run")
	if err != nil || !acquired {
		t.Fatalf("TryHold() = (%v, %v), want acquired", acquired, err)
	}

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}); err != nil {
		t.Fatalf("RunRefresh() while busy error = %v", err)
	}
	if got := refreshRunCount(t, runs); got != 0 {
		t.Fatalf("script runs while busy = %d, want 0", got)
	}
	if _, err := os.Stat(paths.request); err != nil {
		t.Fatalf("request marker stat error = %v, want it left for the owner", err)
	}

	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if got := refreshRunCount(t, runs); got != 1 {
		t.Fatalf("script runs after release = %d, want 1", got)
	}
}

// A requester whose TryHold failed while the owner held the lock wrote its
// marker first, so the owner's re-check after release must pick it up.
func TestRunRefreshRerunsForARequestLandingDuringRelease(t *testing.T) {
	repo, runs := installRefreshFixture(t, "")
	paths := refreshPathsForTest(t, repo)
	landed := false
	afterRefreshRelease = func() {
		if !landed {
			landed = true
			writeFile(t, paths.request, "", 0o644)
		}
	}
	t.Cleanup(func() { afterRefreshRelease = nil })

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if got := refreshRunCount(t, runs); got != 2 {
		t.Fatalf("script runs = %d, want a rerun for the request that landed during release", got)
	}
}

func TestRunRefreshRemovesStagingLeftByInterruptedRuns(t *testing.T) {
	repo, _ := installRefreshFixture(t, "")
	paths := refreshPathsForTest(t, repo)
	leftover := filepath.Join(paths.staging, "run.interrupted")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("leftover staging stat error = %v, want removed by the lock holder", err)
	}
}

// Project cleanup removes the runtime directory; the coordination files must
// not live there, or a recreated lock file would split holders across inodes.
func TestRefreshCoordinationFilesLiveInTheGitCommonDir(t *testing.T) {
	repo := initGitRepo(t)
	commonDir, err := gitCommonDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	paths := refreshPathsIn(commonDir)
	for _, path := range []string{paths.lockBase, paths.request, paths.log, paths.staging} {
		if filepath.Dir(path) != commonDir {
			t.Fatalf("%s is outside the Git common directory %s", path, commonDir)
		}
	}
}

func TestRunRefreshDoesNothingWithoutAnInstalledScript(t *testing.T) {
	repo := initGitRepo(t)

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if _, err := os.Stat(refreshPathsForTest(t, repo).request); !os.IsNotExist(err) {
		t.Fatalf("request marker stat error = %v, want no request without a script", err)
	}
}
