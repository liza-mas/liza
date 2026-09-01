package procscan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestFindZombieAgents_DetectsScopedUnregisteredSupervisor(t *testing.T) {
	projectRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, projectRoot, []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 1 {
		t.Fatalf("zombie count = %d, want 1: %+v", len(result.Zombies), result.Zombies)
	}
	got := result.Zombies[0]
	if got.PID != 1234 || got.Role != "coder" || got.CLI != "codex" || got.GoalID != "goal-1" {
		t.Fatalf("zombie = %+v, want pid/role/cli/goal populated", got)
	}
	if got.Reason != "not_registered_in_state" {
		t.Fatalf("reason = %q, want not_registered_in_state", got.Reason)
	}
}

func TestFindZombieAgents_SkipsRegisteredPID(t *testing.T) {
	projectRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, projectRoot, []string{"liza", "agent", "coder", "--cli", "codex"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot:    projectRoot,
		RegisteredPIDs: map[int]bool{1234: true},
		ProcRoot:       procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 {
		t.Fatalf("zombie count = %d, want 0: %+v", len(result.Zombies), result.Zombies)
	}
}

func TestFindZombieAgents_SkipsOtherProjectWithoutGoalMatch(t *testing.T) {
	projectRoot := t.TempDir()
	otherRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, otherRoot, []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "other-goal"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 {
		t.Fatalf("zombie count = %d, want 0: %+v", len(result.Zombies), result.Zombies)
	}
}

func TestFindZombieAgents_SkipsOtherProjectWithSameGoalWhenCWDReadable(t *testing.T) {
	projectRoot := t.TempDir()
	otherRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, otherRoot, []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 {
		t.Fatalf("zombie count = %d, want 0: %+v", len(result.Zombies), result.Zombies)
	}
}

func TestFindZombieAgents_ReportsUnknownScopeWhenProjectRootSetAndCWDUnreadable(t *testing.T) {
	projectRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProcWithUnreadableCWD(t, procRoot, 1234, []string{"liza", "agent", "coder", "--cli", "codex", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 {
		t.Fatalf("zombie count = %d, want 0: %+v", len(result.Zombies), result.Zombies)
	}
	if len(result.UnknownScope) != 1 {
		t.Fatalf("unknown-scope count = %d, want 1: %+v", len(result.UnknownScope), result.UnknownScope)
	}
	got := result.UnknownScope[0]
	if got.PID != 1234 || got.Role != "coder" || got.GoalID != "goal-1" || got.Reason != ScopeReasonCWDUnreadable {
		t.Fatalf("unknown scope = %+v, want pid/role/goal and cwd-unreadable reason", got)
	}
}

func TestFindZombieAgents_SkipsProcessThatDisappearsBeforeCWDRead(t *testing.T) {
	projectRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProcWithoutCWD(t, procRoot, 1234, []string{"liza", "agent", "coder", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 || len(result.UnknownScope) != 0 {
		t.Fatalf("result = %+v, want vanished process excluded", result)
	}
}

func TestFindZombieAgents_GoalFallbackWithoutProjectRootDoesNotRequireCWD(t *testing.T) {
	procRoot := t.TempDir()
	writeProcWithoutCWD(t, procRoot, 1234, []string{"liza", "agent", "coder", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		GoalID:   "goal-1",
		ProcRoot: procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 1 || result.Zombies[0].PID != 1234 {
		t.Fatalf("zombies = %+v, want pid 1234", result.Zombies)
	}
	if len(result.UnknownScope) != 0 {
		t.Fatalf("unknown scope = %+v, want none", result.UnknownScope)
	}
}

func TestFindZombieAgents_ClassifiesCurrentForeignAndUnknownScope(t *testing.T) {
	projectRoot := t.TempDir()
	otherRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, projectRoot, []string{"liza", "agent", "coder", "--goal-id", "goal-1"})
	writeProc(t, procRoot, 2345, otherRoot, []string{"liza", "agent", "reviewer", "--goal-id", "goal-1"})
	writeProcWithUnreadableCWD(t, procRoot, 3456, []string{"liza", "agent", "architect", "--goal-id", "goal-1"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 1 || result.Zombies[0].PID != 1234 {
		t.Fatalf("zombies = %+v, want only pid 1234", result.Zombies)
	}
	if len(result.UnknownScope) != 1 || result.UnknownScope[0].PID != 3456 {
		t.Fatalf("unknown scope = %+v, want only pid 3456", result.UnknownScope)
	}
}

func TestFindZombieAgents_LegacyCWDMatchWithoutGoalID(t *testing.T) {
	projectRoot := t.TempDir()
	procRoot := t.TempDir()
	writeProc(t, procRoot, 1234, projectRoot, []string{"liza", "agent", "code-reviewer", "--cli=codex"})

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: projectRoot,
		GoalID:      "goal-1",
		ProcRoot:    procRoot,
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 1 {
		t.Fatalf("zombie count = %d, want 1: %+v", len(result.Zombies), result.Zombies)
	}
	if result.Zombies[0].GoalID != "" || result.Zombies[0].CLI != "codex" {
		t.Fatalf("zombie = %+v, want legacy goal empty and cli parsed", result.Zombies[0])
	}
}

func TestFindZombieAgents_ProcfsUnavailable(t *testing.T) {
	_, err := FindZombieAgents(ZombieScanOptions{ProcRoot: filepath.Join(t.TempDir(), "missing")})
	if !errors.Is(err, ErrProcessScanUnavailable) {
		t.Fatalf("FindZombieAgents() error = %v, want ErrProcessScanUnavailable", err)
	}
}

// stubCandidatePIDs stands in for the host process table and reports how many
// times the scan asked for it.
func stubCandidatePIDs(t *testing.T, pids []int, err error) *int {
	t.Helper()
	calls := 0
	old := enumerateCandidatePIDs
	enumerateCandidatePIDs = func() ([]int, error) {
		calls++
		return pids, err
	}
	t.Cleanup(func() { enumerateCandidatePIDs = old })
	return &calls
}

// TestFindZombieAgents_NativeScanMatchesOnGoalID covers the host that has no
// procfs. There is no working directory to scope by there, so the goal ID the
// candidate carries in its own argv has to do it — note that ProjectRoot is set
// and deliberately not what decides the match.
func TestFindZombieAgents_NativeScanMatchesOnGoalID(t *testing.T) {
	hideProcfs(t)
	stubCandidatePIDs(t, []int{1234}, nil)
	stubNativeCommandLine(t, []string{nativeLizaPath(), "agent", "coder", "--cli", "codex", "--goal-id", "goal-1"}, nil)

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: t.TempDir(),
		GoalID:      "goal-1",
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 1 {
		t.Fatalf("zombie count = %d, want 1: %+v", len(result.Zombies), result)
	}
	if len(result.UnknownScope) != 0 {
		t.Errorf("unknown scope = %+v, want none for a goal that matches", result.UnknownScope)
	}
	if result.Zombies[0].PID != 1234 || result.Zombies[0].Role != "coder" || result.Zombies[0].CLI != "codex" {
		t.Errorf("zombie = %+v, want pid 1234 role coder cli codex", result.Zombies[0])
	}
}

// TestFindZombieAgents_NativeScanReportsCandidateWithoutGoal keeps the third
// outcome reachable on this path: a candidate carrying no goal is the one
// nothing can be said about, so it is surfaced rather than claimed or dropped.
func TestFindZombieAgents_NativeScanReportsCandidateWithoutGoal(t *testing.T) {
	hideProcfs(t)
	stubCandidatePIDs(t, []int{1234}, nil)
	stubNativeCommandLine(t, []string{"liza", "agent", "coder"}, nil)

	result, err := FindZombieAgents(ZombieScanOptions{
		ProjectRoot: t.TempDir(),
		GoalID:      "goal-1",
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 {
		t.Errorf("zombies = %+v, want none claimed without a goal to prove it", result.Zombies)
	}
	if len(result.UnknownScope) != 1 {
		t.Fatalf("unknown scope count = %d, want 1: %+v", len(result.UnknownScope), result)
	}
	if result.UnknownScope[0].Reason != ScopeReasonCWDUnreadable {
		t.Errorf("reason = %q, want %q", result.UnknownScope[0].Reason, ScopeReasonCWDUnreadable)
	}
}

func TestFindZombieAgents_NativeScanIgnoresAnotherGoal(t *testing.T) {
	hideProcfs(t)
	stubCandidatePIDs(t, []int{1234}, nil)
	stubNativeCommandLine(t, []string{"liza", "agent", "coder", "--goal-id", "goal-2"}, nil)

	result, err := FindZombieAgents(ZombieScanOptions{GoalID: "goal-1"})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 || len(result.UnknownScope) != 0 {
		t.Fatalf("result = %+v, want nothing for another run's goal", result)
	}
}

func TestFindZombieAgents_NativeScanSkipsRegisteredPID(t *testing.T) {
	hideProcfs(t)
	stubCandidatePIDs(t, []int{1234}, nil)
	calls := stubNativeCommandLine(t, []string{"liza", "agent", "coder", "--goal-id", "goal-1"}, nil)

	result, err := FindZombieAgents(ZombieScanOptions{
		GoalID:         "goal-1",
		RegisteredPIDs: map[int]bool{1234: true},
	})
	if err != nil {
		t.Fatalf("FindZombieAgents() error = %v", err)
	}
	if len(result.Zombies) != 0 || len(result.UnknownScope) != 0 {
		t.Fatalf("result = %+v, want nothing for a registered pid", result)
	}
	if *calls != 0 {
		t.Errorf("native command line consulted %d times, want 0 for a registered pid", *calls)
	}
}

// TestFindZombieAgents_InjectedProcRootStaysOffTheHost mirrors the guard the
// per-PID status check already carries: naming a proc root describes a host, so
// the machine underneath must not be scanned instead. Without it, the fake-procfs
// fixtures in ops, agent and commands would start seeing this machine.
func TestFindZombieAgents_InjectedProcRootStaysOffTheHost(t *testing.T) {
	calls := stubCandidatePIDs(t, []int{1234}, nil)

	_, err := FindZombieAgents(ZombieScanOptions{
		GoalID:   "goal-1",
		ProcRoot: filepath.Join(t.TempDir(), "missing"),
	})

	if !errors.Is(err, ErrProcessScanUnavailable) {
		t.Fatalf("FindZombieAgents() error = %v, want ErrProcessScanUnavailable", err)
	}
	if *calls != 0 {
		t.Errorf("process table enumerated %d times, want 0 for an injected proc root", *calls)
	}
}

func TestIsLizaAgentArgv(t *testing.T) {
	binaryName := brand.RuntimeValues().BinaryName
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{name: "branded agent", argv: []string{filepath.Join("/usr/bin", binaryName), "agent", "coder"}, want: true},
		// filepath.Base only splits on backslashes when it runs on Windows, so
		// the path here stays separator-neutral and the suffix is what is
		// under test.
		{name: "windows executable suffix", argv: []string{binaryName + ".exe", "agent", "coder"}, want: true},
		// PATHEXT resolution decides the suffix case: launching a bare binary
		// through MSYS or wezterm can yield an uppercase suffix. Windows names
		// it the same file; POSIX does not, so the expectation follows the platform.
		{name: "uppercase suffix", argv: []string{binaryName + ".EXE", "agent", "coder"}, want: runtime.GOOS == "windows"},
		{name: "mixed case suffix", argv: []string{binaryName + ".Exe", "agent", "coder"}, want: runtime.GOOS == "windows"},
		{name: "too short", argv: []string{binaryName}, want: false},
		{name: "other branded command", argv: []string{binaryName, "status"}, want: false},
		{name: "other branded command on windows", argv: []string{binaryName + ".exe", "status"}, want: false},
		{name: "provider cli", argv: []string{"codex", "exec"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLizaAgentArgv(tt.argv); got != tt.want {
				t.Fatalf("IsLizaAgentArgv(%v) = %v, want %v", tt.argv, got, tt.want)
			}
		})
	}
}

func TestIsLizaAgentArgvRejectsAnotherBrand(t *testing.T) {
	originalBinaryName := brand.BinaryName
	brand.BinaryName = "acme-agent"
	t.Cleanup(func() {
		brand.BinaryName = originalBinaryName
	})

	if IsLizaAgentArgv([]string{"liza", "agent", "coder"}) {
		t.Fatal("default-brand executable matched a differently branded build")
	}
	if !IsLizaAgentArgv([]string{"acme-agent", "agent", "coder"}) {
		t.Fatal("runtime-brand executable did not match")
	}
}

func TestMatchesLizaAgentIdentity(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		role    string
		agentID string
		want    bool
	}{
		{
			name:    "exact match",
			argv:    []string{"/home/me/bin/liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"},
			role:    "coder",
			agentID: "coder-1",
			want:    true,
		},
		{
			name:    "auto assigned agent ID",
			argv:    []string{"/home/me/bin/liza", "agent", "coder", "--cli", "codex"},
			role:    "coder",
			agentID: "coder-1",
			want:    true,
		},
		{
			name:    "role wildcard",
			argv:    []string{"/home/me/bin/liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"},
			agentID: "coder-1",
			want:    true,
		},
		{
			name: "agent wildcard",
			argv: []string{"/home/me/bin/liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"},
			role: "coder",
			want: true,
		},
		{
			name:    "wrong role",
			argv:    []string{"/home/me/bin/liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"},
			role:    "code-reviewer",
			agentID: "coder-1",
			want:    false,
		},
		{
			name:    "wrong explicit agent",
			argv:    []string{"/home/me/bin/liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"},
			role:    "coder",
			agentID: "coder-2",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesLizaAgentIdentity(tt.argv, tt.role, tt.agentID)
			if got != tt.want {
				t.Fatalf("MatchesLizaAgentIdentity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentProcessStatusForPID_ProcfsIdentityStates(t *testing.T) {
	procRoot := t.TempDir()
	writeProcWithoutCWD(t, procRoot, 1234, []string{"liza", "agent", "coder", "--agent-id", "coder-1", "--cli", "codex"})
	writeProcWithoutCWD(t, procRoot, 5678, []string{"go", "test"})

	tests := []struct {
		name    string
		pid     int
		role    string
		agentID string
		want    AgentProcessState
		alive   bool
	}{
		{name: "matching liza agent", pid: 1234, role: "coder", agentID: "coder-1", want: AgentProcessLiveMatching, alive: true},
		{name: "pid reused by another process", pid: 5678, role: "coder", agentID: "coder-1", want: AgentProcessMismatched, alive: true},
		{name: "missing pid value", pid: 0, role: "coder", agentID: "coder-1", want: AgentProcessUnknown, alive: false},
		{name: "missing pid falls through to dead signal", pid: 987654321, role: "coder", agentID: "coder-1", want: AgentProcessDead, alive: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AgentProcessStatusForPID(tt.pid, tt.role, tt.agentID, procRoot)
			if got.State != tt.want || got.Alive != tt.alive {
				t.Fatalf("AgentProcessStatusForPID() = %+v, want state=%s alive=%v", got, tt.want, tt.alive)
			}
		})
	}
}

func TestAgentProcessStatusForPID_ProcfsUnavailableKeepsAliveUnknown(t *testing.T) {
	got := AgentProcessStatusForPID(os.Getpid(), "coder", "coder-1", filepath.Join(t.TempDir(), "missing-proc"))
	if got.State != AgentProcessUnknown || !got.Alive {
		t.Fatalf("AgentProcessStatusForPID() = %+v, want unknown alive", got)
	}
}

func TestFindExplicitAgentIdentityPIDs(t *testing.T) {
	procRoot := t.TempDir()
	writeProcWithoutCWD(t, procRoot, 42, []string{"liza", "agent", "coder", "--agent-id", "coder-1"})
	writeProcWithoutCWD(t, procRoot, 7, []string{"liza", "agent", "coder", "--agent-id=coder-1"})
	writeProcWithoutCWD(t, procRoot, 8, []string{"liza", "agent", "coder", "--agent-id", "coder-2"})
	writeProcWithoutCWD(t, procRoot, 9, []string{"liza", "agent", "coder"})
	writeProcWithoutCWD(t, procRoot, 10, []string{"liza", "agent", "orchestrator", "--agent-id", "coder-1"})

	got := FindExplicitAgentIdentityPIDs("coder", "coder-1", procRoot)
	want := []int{7, 42}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindExplicitAgentIdentityPIDs() = %v, want %v", got, want)
	}
}

func writeProc(t *testing.T, procRoot string, pid int, cwd string, argv []string) {
	t.Helper()
	procDir := writeProcCmdline(t, procRoot, pid, argv)
	if err := os.Symlink(cwd, filepath.Join(procDir, "cwd")); err != nil {
		// Creating a symlink requires Developer Mode or Administrator on Windows
		// (ERROR_PRIVILEGE_NOT_HELD). These tests simulate the Linux /proc
		// filesystem, which is a no-op target on Windows anyway, so skip rather
		// than fail when the host cannot create the required symlink.
		t.Skipf("cannot create cwd symlink (procfs simulation unavailable without symlink support): %v", err)
	}
}

func writeProcWithoutCWD(t *testing.T, procRoot string, pid int, argv []string) {
	t.Helper()
	writeProcCmdline(t, procRoot, pid, argv)
}

func writeProcWithUnreadableCWD(t *testing.T, procRoot string, pid int, argv []string) {
	t.Helper()
	procDir := writeProcCmdline(t, procRoot, pid, argv)
	if err := os.Mkdir(filepath.Join(procDir, "cwd"), 0755); err != nil {
		t.Fatal(err)
	}
}

func writeProcCmdline(t *testing.T, procRoot string, pid int, argv []string) string {
	t.Helper()
	procDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmdline := ""
	for _, arg := range argv {
		cmdline += arg + "\x00"
	}
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(cmdline), 0644); err != nil {
		t.Fatal(err)
	}
	return procDir
}
