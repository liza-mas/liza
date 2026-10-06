package pairingindex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// installRefreshFixture installs an index script that appends one line to
// $LIZA_TEST_RUNS per run, and returns the repository and the runs file.
func installRefreshFixture(t *testing.T, extra string) (repo, runs string) {
	t.Helper()
	t.Setenv(semble.EnvEnableSemble, "false")

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

func TestRefreshPreparesSembleCorpusAndKeepsFailuresIndependent(t *testing.T) {
	for _, tt := range []struct {
		name, sembleExit, scriptExtra string
		wantError                     bool
	}{
		{"success", "0", "", false},
		{"optional corpus failure", "1", "", false},
		{"other index failure", "0", "exit 7\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo, runs := installRefreshFixture(t, tt.scriptExtra)
			t.Setenv(semble.EnvEnableSemble, "true")
			if !semble.EnsureProjectRootIgnore(repo).Safe {
				t.Fatal("root ignore safety failed")
			}
			bin := t.TempDir()
			argsPath := filepath.Join(t.TempDir(), "semble-args")
			t.Setenv("SEMBLE_TEST_ARGS", filepath.ToSlash(argsPath))
			testhelpers.WriteShellStub(t, filepath.Join(bin, "semble"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SEMBLE_TEST_ARGS\"\nprintf 'offline=%s\\n' \"$HF_HUB_OFFLINE\" >> \"$SEMBLE_TEST_ARGS\"\nprintf 'UNTRUSTED_CORPUS_OUTPUT\\n'\nexit "+tt.sembleExit+"\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var output bytes.Buffer
			err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge", Echo: &output})
			if (err != nil) != tt.wantError {
				t.Fatalf("refresh error = %v, want error %v", err, tt.wantError)
			}
			args := readFile(t, argsPath)
			want := "search\n__semble_prewarm__\n" + repo + "\n--top-k\n1\n--content\nall\noffline=1\n"
			if args != want {
				t.Fatalf("actual corpus command = %q, want %q", args, want)
			}
			if !tt.wantError && refreshRunCount(t, runs) != 1 {
				t.Fatal("optional Semble failure blocked the index script")
			}
			if strings.Contains(output.String(), "UNTRUSTED_CORPUS_OUTPUT") {
				t.Fatal("repository chunks leaked into refresh diagnostics")
			}
			if tt.sembleExit != "0" && !strings.Contains(output.String(), "semble repository: query failed") {
				t.Fatalf("optional corpus failure missing from log: %s", output.String())
			}
		})
	}
}

func TestRefreshWaitsForUnsuccessfulReadinessProbe(t *testing.T) {
	repo, runs := installRefreshFixture(t, "")
	t.Setenv(semble.EnvEnableSemble, "true")
	if !semble.EnsureProjectRootIgnore(repo).Safe {
		t.Fatal("root ignore safety failed")
	}
	bin := t.TempDir()
	prepared := filepath.Join(t.TempDir(), "prepared")
	t.Setenv("SEMBLE_TEST_PREPARED", filepath.ToSlash(prepared))
	testhelpers.WriteShellStub(t, filepath.Join(bin, "semble"), "#!/bin/sh\nprintf ready > \"$SEMBLE_TEST_PREPARED\"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	started, release := make(chan struct{}), make(chan struct{})
	probeDone := make(chan semble.ValidationResult, 1)
	go func() {
		probeDone <- semble.CheckRepositoryReadiness(semble.ValidationOptions{
			TargetRoot: repo, Runner: func(semble.CommandPlan) (semble.CommandResult, error) {
				close(started)
				<-release
				return semble.CommandResult{ExitCode: 1}, errors.New("changed corpus probe failed")
			},
		})
	}()
	<-started
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "merge"}) }()
	paths := refreshPathsForTest(t, repo)
	deadline := time.Now().Add(3 * time.Second)
	// Observe request consumption while the unsuccessful probe still holds
	// the corpus lock. Old code completes here without ever preparing it.
	for {
		_, requestErr := os.Stat(paths.request)
		_, logErr := os.Stat(paths.log)
		if os.IsNotExist(requestErr) && logErr == nil {
			select {
			case err := <-refreshDone:
				close(release)
				<-probeDone
				t.Fatalf("refresh discarded preparation during readiness: %v", err)
			case <-time.After(100 * time.Millisecond):
				close(release)
			}
			break
		}
		if time.Now().After(deadline) {
			close(release)
			<-probeDone
			t.Fatal("refresh did not consume its request")
		}
		select {
		case err := <-refreshDone:
			close(release)
			<-probeDone
			t.Fatalf("refresh completed before readiness released: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if result := <-probeDone; result.Ready {
		t.Fatal("unsuccessful readiness advertised corpus")
	}
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared); err != nil || refreshRunCount(t, runs) != 1 {
		t.Fatalf("overlapping preparation never completed: %v", err)
	}
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

// A missing coordinator would make the launch fail, so a nil error proves
// StartRefresh launched nothing.
func TestStartRefreshDoesNothingWithoutAnInstalledScript(t *testing.T) {
	repo := initGitRepo(t)
	t.Cleanup(SetIndexBinaryForTest(filepath.Join(t.TempDir(), "missing-coordinator")))

	if err := StartRefresh(repo, "merge"); err != nil {
		t.Fatalf("StartRefresh() error = %v", err)
	}
}

func TestStartRefreshReportsAMissingCoordinator(t *testing.T) {
	repo, _ := installRefreshFixture(t, "")
	t.Cleanup(SetIndexBinaryForTest(""))

	if err := StartRefresh(repo, "merge"); err == nil {
		t.Fatal("StartRefresh() error = nil, want the missing coordinator reported")
	}
}
