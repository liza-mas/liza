package procscan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
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

func TestIsLizaAgentArgv(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{name: "liza agent", argv: []string{"/usr/bin/liza", "agent", "coder"}, want: true},
		// filepath.Base only splits on backslashes when it runs on Windows, so
		// the path here stays separator-neutral and the suffix is what is
		// under test.
		{name: "windows executable suffix", argv: []string{"liza.exe", "agent", "coder"}, want: true},
		{name: "too short", argv: []string{"liza"}, want: false},
		{name: "other liza command", argv: []string{"liza", "status"}, want: false},
		{name: "other liza command on windows", argv: []string{"liza.exe", "status"}, want: false},
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
