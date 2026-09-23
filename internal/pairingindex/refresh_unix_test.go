//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

package pairingindex

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

const refreshHelperRepoEnv = "LIZA_TEST_REFRESH_HELPER_REPO"

// TestRefreshHelperProcess is the coordinator that
// TestRunRefreshOwnerCrashDefersRequestToNextTrigger kills mid-run.
func TestRefreshHelperProcess(t *testing.T) {
	repo := os.Getenv(refreshHelperRepoEnv)
	if repo == "" {
		t.Skip("runs only as a helper process")
	}
	_ = RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "crashing owner"})
	os.Exit(0)
}

// The accepted crash-recovery bound: when the coordinator dies, its script
// keeps the inherited lock, a request made meanwhile returns busy, nothing
// reruns when the script ends, and the next trigger consumes the request.
func TestRunRefreshOwnerCrashDefersRequestToNextTrigger(t *testing.T) {
	blockDir := t.TempDir()
	started := filepath.Join(blockDir, "started")
	release := filepath.Join(blockDir, "release")
	for _, fifo := range []string{started, release} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("mkfifo %s: %v", fifo, err)
		}
	}
	block := `if [ -n "${LIZA_TEST_BLOCK_DIR:-}" ]; then
	echo started > "$LIZA_TEST_BLOCK_DIR/started"
	read _ < "$LIZA_TEST_BLOCK_DIR/release"
fi
`
	repo, runs := installRefreshFixture(t, block)
	paths := refreshPathsForTest(t, repo)

	owner := exec.Command(os.Args[0], "-test.run=^TestRefreshHelperProcess$")
	owner.Env = append(os.Environ(), refreshHelperRepoEnv+"="+repo, "LIZA_TEST_BLOCK_DIR="+blockDir)
	if err := owner.Start(); err != nil {
		t.Fatalf("start owner: %v", err)
	}
	releaseScript := func() {
		// Opening the FIFO for writing blocks until the script reads it.
		if f, err := os.OpenFile(release, os.O_WRONLY, 0); err == nil {
			_, _ = f.WriteString("go\n")
			_ = f.Close()
		}
	}
	t.Cleanup(func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	})

	startedFIFO, err := os.Open(started)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(startedFIFO).ReadString('\n')
	_ = startedFIFO.Close()
	if err != nil || strings.TrimSpace(line) != "started" {
		t.Fatalf("script start signal = %q (err %v)", line, err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("kill owner: %v", err)
	}
	_ = owner.Wait()

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "request during orphan"}); err != nil {
		t.Fatalf("RunRefresh() while the orphan holds the lock error = %v", err)
	}
	if got := refreshRunCount(t, runs); got != 0 {
		t.Fatalf("script runs while the orphan holds the lock = %d, want 0", got)
	}

	releaseScript()
	// Wait for the orphan to exit and drop the inherited lock.
	if err := filelock.New(paths.lockBase).WithTimeout(10*time.Second).WithLockOperation("wait for orphan", func() error { return nil }); err != nil {
		t.Fatalf("orphaned script never released the lock: %v", err)
	}
	if got := refreshRunCount(t, runs); got != 1 {
		t.Fatalf("script runs after the orphan finished = %d, want only its own run", got)
	}
	if _, err := os.Stat(paths.request); err != nil {
		t.Fatalf("request marker stat error = %v, want the request still pending", err)
	}

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "next trigger"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if got := refreshRunCount(t, runs); got != 2 {
		t.Fatalf("script runs after the next trigger = %d, want the pending request run", got)
	}
	if _, err := os.Stat(paths.request); !os.IsNotExist(err) {
		t.Fatalf("request marker stat error = %v, want consumed", err)
	}
}

// Tools the index script starts must not inherit the lock descriptor: one
// left running would hold the lock and silently stop every later refresh.
func TestIndexScriptToolsDoNotInheritTheRefreshLock(t *testing.T) {
	repo := initGitRepo(t)
	if _, err := InstallIndexScript(InstallIndexScriptOptions{RepoRoot: repo}); err != nil {
		t.Fatalf("InstallIndexScript() error = %v", err)
	}
	toolLog := filepath.Join(t.TempDir(), "fd3.log")
	stub := `#!/bin/sh
if { true >&3; } 2>/dev/null; then
	printf 'fd3 open in %s\n' "$1" >> "$LIZA_TEST_FD_LOG"
fi
[ "$1" != "diff" ] || exit 1
output=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
	fi
	shift
done
[ -z "$output" ] || printf 'generated\n' > "$output"
`
	pathDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pathDir, "stacklit"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIZA_TEST_FD_LOG", toolLog)

	if err := RunRefresh(RefreshOptions{RepoRoot: repo, Trigger: "post-commit"}); err != nil {
		t.Fatalf("RunRefresh() error = %v", err)
	}
	if data, err := os.ReadFile(toolLog); err == nil && len(data) > 0 {
		t.Fatalf("tools saw the lock descriptor:\n%s", data)
	}
}

// The merge trigger runs the coordinator in its own session, from the
// repository root, with the index-refresh command and the merge trigger, and
// returns without waiting for it: a merge must never block on indexing.
func TestStartRefreshLaunchesADetachedCoordinator(t *testing.T) {
	repo, _ := installRefreshFixture(t, "")
	fifoDir := t.TempDir()
	marker := filepath.Join(fifoDir, "coordinator.out")
	release := filepath.Join(fifoDir, "release")
	for _, fifo := range []string{marker, release} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("mkfifo %s: %v", fifo, err)
		}
	}
	coordinator := filepath.Join(t.TempDir(), "coordinator")
	writeFile(t, coordinator, `#!/bin/sh
printf '%s\n%s\n%s %s\n' "$*" "$(pwd -P)" "$$" "$(ps -o pgid= -p $$ | tr -d ' ')" > "$LIZA_TEST_MARKER"
read _ < "$LIZA_TEST_RELEASE"
`, 0o755)
	t.Setenv("LIZA_TEST_MARKER", marker)
	t.Setenv("LIZA_TEST_RELEASE", release)
	t.Cleanup(SetIndexBinaryForTest(coordinator))
	// Opening a FIFO blocks until its other end opens. Opening one end
	// without blocking releases a reader or writer still waiting on the
	// other, so a failed test leaves neither this process nor the detached
	// coordinator stuck.
	unblock := func(fifo string, flag int) {
		if f, err := os.OpenFile(fifo, flag|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	}
	t.Cleanup(func() { unblock(release, os.O_WRONLY) })

	output := make(chan []byte, 1)
	go func() {
		data, _ := os.ReadFile(marker)
		output <- data
	}()
	returned := make(chan error, 1)
	go func() { returned <- StartRefresh(repo, "merge") }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("StartRefresh() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		unblock(marker, os.O_WRONLY)
		t.Fatal("StartRefresh() waited for the coordinator to finish")
	}
	var data []byte
	select {
	case data = <-output:
	case <-time.After(10 * time.Second):
		unblock(marker, os.O_WRONLY)
		t.Fatal("coordinator never ran")
	}
	// The coordinator has written its output and goes on to open the release
	// FIFO, so this blocking open returns and lets it exit.
	released := make(chan struct{})
	go func() {
		if f, err := os.OpenFile(release, os.O_WRONLY, 0); err == nil {
			_ = f.Close()
		}
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		unblock(release, os.O_RDONLY)
		t.Fatal("coordinator never waited for release")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("coordinator output = %q, want 3 lines", data)
	}
	if want := RefreshCommandName + " --trigger merge"; lines[0] != want {
		t.Fatalf("coordinator args = %q, want %q", lines[0], want)
	}
	wantDir, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if lines[1] != wantDir {
		t.Fatalf("coordinator dir = %q, want %q", lines[1], wantDir)
	}
	pid, pgid, _ := strings.Cut(lines[2], " ")
	if pid != pgid {
		t.Fatalf("coordinator pid %s runs in process group %s, want its own", pid, pgid)
	}
}
