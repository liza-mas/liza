package procscan

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// hideProcfs makes the host look like one without a procfs, so the tests below
// exercise the branch a Windows machine always takes.
func hideProcfs(t *testing.T) {
	t.Helper()
	old := defaultProcRoot
	defaultProcRoot = filepath.Join(t.TempDir(), "no-proc")
	t.Cleanup(func() { defaultProcRoot = old })
}

// nativeLizaPath returns an argv[0] shaped the way the current OS would
// actually report it for a running liza binary, so tests that stub the
// native (procfs-less) argv-reading fallback exercise IsLizaAgentArgv's
// real per-platform rules — trimExecutableSuffix compares case-sensitively
// except on Windows, and filepath.Base only splits on '\' there — instead of
// a fixed Windows-shaped path that can only ever match when GOOS is windows.
func nativeLizaPath() string {
	if runtime.GOOS == "windows" {
		return `C:\bin\liza.EXE`
	}
	return "/bin/liza"
}

func stubNativeCommandLine(t *testing.T, argv []string, err error) *int {
	t.Helper()
	calls := 0
	old := nativeCommandLine
	nativeCommandLine = func(int) ([]string, error) {
		calls++
		return argv, err
	}
	t.Cleanup(func() { nativeCommandLine = old })
	return &calls
}

func TestAgentProcessStatusForPID_NativeIdentityMatches(t *testing.T) {
	hideProcfs(t)
	stubNativeCommandLine(t, []string{nativeLizaPath(), "agent", "coder", "--agent-id", "coder-1"}, nil)

	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", "")

	if got.DisplayStatus() != "running" {
		t.Fatalf("DisplayStatus() = %q, want %q (detail: %s)", got.DisplayStatus(), "running", got.Detail)
	}
	if !got.Alive || !got.IsLiveMatching() {
		t.Fatalf("status = %+v, want a live matching agent", got)
	}
}

func TestAgentProcessStatusForPID_NativeIdentityMismatches(t *testing.T) {
	hideProcfs(t)
	stubNativeCommandLine(t, []string{"go", "test"}, nil)

	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", "")

	if got.DisplayStatus() != "mismatched" {
		t.Fatalf("DisplayStatus() = %q, want %q (detail: %s)", got.DisplayStatus(), "mismatched", got.Detail)
	}
	if got.IsLiveOrUnknown() {
		t.Fatalf("status = %+v, want a mismatched pid to be excluded from live-or-unknown", got)
	}
}

// TestAgentProcessStatusForPID_InjectedProcRootStaysOffTheHost locks the
// isolation the fake-procfs tests in ops, agent and commands rely on: naming a
// proc root describes the host, so the machine underneath must not be consulted.
// Without this, a fixture PID that happens to exist would read as mismatched and
// silently change what those tests assert.
func TestAgentProcessStatusForPID_InjectedProcRootStaysOffTheHost(t *testing.T) {
	calls := stubNativeCommandLine(t, []string{`C:\bin\liza.exe`, "agent", "coder"}, nil)

	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", filepath.Join(t.TempDir(), "missing-proc"))

	if *calls != 0 {
		t.Fatalf("native command line consulted %d times, want 0 for an injected proc root", *calls)
	}
	if got.DisplayStatus() != "unknown" {
		t.Fatalf("DisplayStatus() = %q, want %q (detail: %s)", got.DisplayStatus(), "unknown", got.Detail)
	}
}

func TestAgentProcessStatusForPID_NativeSourceUnavailableFallsBackToLiveness(t *testing.T) {
	hideProcfs(t)
	stubNativeCommandLine(t, nil, errors.New("no identity source"))

	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", "")

	if got.DisplayStatus() != "unknown" || !got.Alive {
		t.Fatalf("status = %+v, want a live process with unknown identity", got)
	}
}

// TestAgentProcessStatusForPID_ReadsThisProcessForReal takes no stub: the host
// itself must be able to name a running process. This test is what would have
// caught the reported symptom — every live agent reading as "unknown" on
// Windows — since the test binary is demonstrably not an agent supervisor.
func TestAgentProcessStatusForPID_ReadsThisProcessForReal(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skipf("%s exposes neither procfs nor a native command-line source, so no identity can be established", runtime.GOOS)
	}

	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", "")

	if got.DisplayStatus() != "mismatched" {
		t.Fatalf("DisplayStatus() = %q, want %q — this process is alive and is not an agent (source: %s, detail: %s)",
			got.DisplayStatus(), "mismatched", got.Source, got.Detail)
	}
}
