package ops

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestDeleteAgent_Validation(t *testing.T) {
	_, err := DeleteAgent("/nonexistent", "", false, false, "reason")
	if err == nil {
		t.Fatal("Expected error for empty agent ID")
	}
	if !strings.Contains(err.Error(), "agent ID required") {
		t.Errorf("Error = %q, want to contain 'agent ID required'", err.Error())
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteAgent(tmpDir, "nonexistent", false, false, "reason")
	if err == nil {
		t.Fatal("Expected error for nonexistent agent")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestDeleteAgent_IdleAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteAgent(tmpDir, "coder-1", false, false, "no longer needed")
	if err != nil {
		t.Fatalf("DeleteAgent() error: %v", err)
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}

	// Verify agent removed
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Error("Agent should be removed from state")
	}

	// Verify human note added
	if len(readState.HumanNotes) == 0 {
		t.Fatal("Expected human note to be added")
	}
	lastNote := readState.HumanNotes[len(readState.HumanNotes)-1]
	if !strings.Contains(lastNote.Message, "coder-1") {
		t.Errorf("Note message = %q, want to contain agent ID", lastNote.Message)
	}
}

func TestDeleteAgent_ActiveLease_NoForce(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: &leaseExpires,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteAgent(tmpDir, "coder-1", false, false, "reason")
	if err == nil {
		t.Fatal("Expected error for active lease without force")
	}
	if !strings.Contains(err.Error(), "active lease") {
		t.Errorf("Error = %q, want to contain 'active lease'", err.Error())
	}
}

func TestDeleteAgent_ActiveLease_Force(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: &leaseExpires,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteAgent(tmpDir, "coder-1", true, false, "force remove")
	if err != nil {
		t.Fatalf("DeleteAgent() with force error: %v", err)
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}
}

func TestDeleteAgent_BusyWithTask_NoForce(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskRef := "task-1"
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := DeleteAgent(tmpDir, "coder-1", false, false, "reason")
	if err == nil {
		t.Fatal("Expected error for busy agent without force")
	}
	if !strings.Contains(err.Error(), "working on task") {
		t.Errorf("Error = %q, want to contain 'working on task'", err.Error())
	}
}

func TestDeleteAgent_AllowRunningPID_BypassesPIDOnly(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	taskRef := "task-1"
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		PID:          os.Getpid(), // alive PID
		LeaseExpires: &leaseExpires,
		CurrentTask:  &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// allowRunningPID=true should still refuse due to active lease
	_, err := DeleteAgent(tmpDir, "coder-1", false, true, "pid confirmed")
	if err == nil {
		t.Fatal("Expected error: allowRunningPID should not bypass lease check")
	}
	if !strings.Contains(err.Error(), "active lease") {
		t.Errorf("Error = %q, want to contain 'active lease'", err.Error())
	}
}

func TestDeleteAgent_AllowRunningPID_BypassesPIDWithNoLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    os.Getpid(), // alive PID
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// allowRunningPID=true with no lease/task should succeed
	result, err := DeleteAgent(tmpDir, "coder-1", false, true, "pid confirmed")
	if err != nil {
		t.Fatalf("DeleteAgent() error: %v", err)
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Error("Agent should be removed from state")
	}
}

func TestIsAgentProcessRunning_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, _, err := IsAgentProcessRunning(tmpDir, "nonexistent")
	if err == nil {
		t.Fatal("Expected error for nonexistent agent")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestDeleteAgent_BusyWithTask_Force(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskRef := "task-1"
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskRef,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteAgent(tmpDir, "coder-1", true, false, "force remove")
	if err != nil {
		t.Fatalf("DeleteAgent() with force error: %v", err)
	}
	if result.AgentID != "coder-1" {
		t.Errorf("AgentID = %q, want %q", result.AgentID, "coder-1")
	}
}

func TestMatchLizaAgentCmdline(t *testing.T) {
	tests := []struct {
		name     string
		cmdline  string
		expected bool
	}{
		{"exact match", "liza\x00agent\x00coder\x00--cli\x00claude\x00", true},
		{"full path", "/home/user/.local/bin/liza\x00agent\x00code-reviewer\x00", true},
		{"wrong binary", "codex\x00agent\x00coder\x00", false},
		{"wrong subcommand", "liza\x00status\x00", false},
		{"empty cmdline", "", false},
		{"single arg", "liza\x00", false},
		{"go test runner", "go\x00test\x00./internal/ops/...\x00", false},
		{"liza without agent", "liza\x00validate\x00", false},
		{"agent without liza", "other\x00agent\x00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := procscan.IsLizaAgentArgv(procscan.ParseCmdlineBytes([]byte(tt.cmdline)))
			if got != tt.expected {
				t.Errorf("IsLizaAgentArgv(ParseCmdlineBytes(%q)) = %v, want %v", tt.cmdline, got, tt.expected)
			}
		})
	}
}

func TestTerminateProcess_GracefulExit(t *testing.T) {
	original := agentProcesses
	signalCalls := 0
	killCalls := 0
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return false },
		signalTree: func(pid int) error {
			signalCalls++
			return nil
		},
		killTree: func(pid int) error {
			killCalls++
			return nil
		},
		waitForExit: func(pid int, grace time.Duration) bool { return true },
	}
	t.Cleanup(func() { agentProcesses = original })

	result, err := terminateProcess(123, time.Second)
	if err != nil {
		t.Fatalf("terminateProcess() error = %v", err)
	}
	if signalCalls != 1 {
		t.Fatalf("signal calls = %d, want 1", signalCalls)
	}
	if killCalls != 0 {
		t.Fatalf("kill calls = %d, want 0", killCalls)
	}
	if !result.Signaled || !result.Exited || result.Killed {
		t.Fatalf("result = %+v, want signaled/exited without killed", result)
	}
}

func TestTerminateProcess_EscalatesToKill(t *testing.T) {
	original := agentProcesses
	waitCalls := 0
	killCalls := 0
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return true },
		signalTree:  func(pid int) error { return nil },
		killTree: func(pid int) error {
			killCalls++
			return nil
		},
		waitForExit: func(pid int, grace time.Duration) bool {
			waitCalls++
			return waitCalls > 1
		},
	}
	t.Cleanup(func() { agentProcesses = original })

	result, err := terminateProcess(123, time.Second)
	if err != nil {
		t.Fatalf("terminateProcess() error = %v", err)
	}
	if killCalls != 1 {
		t.Fatalf("kill calls = %d, want 1", killCalls)
	}
	if !result.Signaled || !result.Killed || !result.Exited {
		t.Fatalf("result = %+v, want signaled/killed/exited", result)
	}
}

func TestTerminateProcess_ReportsStillRunningAfterKill(t *testing.T) {
	original := agentProcesses
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return true },
		signalTree:  func(pid int) error { return nil },
		killTree:    func(pid int) error { return nil },
		waitForExit: func(pid int, grace time.Duration) bool { return false },
	}
	t.Cleanup(func() { agentProcesses = original })

	result, err := terminateProcess(123, time.Second)
	if err == nil {
		t.Fatal("terminateProcess() error = nil, want still-running error")
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Fatalf("error = %q, want still running", err.Error())
	}
	if !result.Signaled || !result.Killed || result.Exited {
		t.Fatalf("result = %+v, want signaled/killed without exited", result)
	}
}

func TestTerminateAgent_DoesNotDeleteStateWhenProcessSurvives(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    123,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	original := agentProcesses
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return true },
		signalTree:  func(pid int) error { return nil },
		killTree:    func(pid int) error { return nil },
		waitForExit: func(pid int, grace time.Duration) bool { return false },
	}
	t.Cleanup(func() { agentProcesses = original })

	_, err := TerminateAgent(tmpDir, "coder-1", true, true, "test", time.Second)
	if err == nil {
		t.Fatal("TerminateAgent() error = nil, want still-running error")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; !exists {
		t.Fatal("agent should remain registered when process termination fails")
	}
}

func TestTerminateAgent_DeletesAfterProcessExit(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    123,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	original := agentProcesses
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return true },
		signalTree:  func(pid int) error { return nil },
		killTree:    func(pid int) error { return nil },
		waitForExit: func(pid int, grace time.Duration) bool {
			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("read state during wait: %v", err)
			}
			if _, exists := readState.Agents["coder-1"]; !exists {
				t.Fatal("agent was deleted before process exit")
			}
			return true
		},
	}
	t.Cleanup(func() { agentProcesses = original })

	result, err := TerminateAgent(tmpDir, "coder-1", true, true, "test", time.Second)
	if err != nil {
		t.Fatalf("TerminateAgent() error = %v", err)
	}
	if !result.StateDeleted {
		t.Fatal("StateDeleted = false, want true")
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Fatal("agent should be deleted after process exit")
	}
}

func TestTerminateAgent_SucceedsWhenSupervisorUnregistersDuringShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    123,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	original := agentProcesses
	agentProcesses = agentProcessOps{
		isLizaAgent: func(pid int) bool { return pid == 123 },
		isAlive:     func(int) bool { return true },
		signalTree:  func(pid int) error { return nil },
		killTree:    func(pid int) error { return nil },
		waitForExit: func(pid int, grace time.Duration) bool {
			err := db.New(stateFile).Modify(func(state *models.State) error {
				delete(state.Agents, "coder-1")
				return nil
			})
			if err != nil {
				t.Fatalf("self-unregister setup failed: %v", err)
			}
			return true
		},
	}
	t.Cleanup(func() { agentProcesses = original })

	result, err := TerminateAgent(tmpDir, "coder-1", true, true, "test", time.Second)
	if err != nil {
		t.Fatalf("TerminateAgent() error = %v", err)
	}
	if !result.StateDeleted {
		t.Fatal("StateDeleted = false, want true")
	}
}

func TestDeleteAgent_ReturnsPID(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    12345,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := DeleteAgent(tmpDir, "coder-1", false, true, "test")
	if err != nil {
		t.Fatalf("DeleteAgent() error: %v", err)
	}
	if result.PID != 12345 {
		t.Errorf("PID = %d, want 12345", result.PID)
	}
}
