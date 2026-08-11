//go:build windows

package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestTerminateAgent_StopsSpawnedTreeBeforeRemovingState exercises the real
// production path — no agentProcesses substitution — because the defect it
// covers lived entirely in the parts the unit tests replace: identity was read
// from procfs, so on Windows no agent was ever recognised, nothing was
// signalled, and the state row was removed while the agent and its provider
// kept running.
func TestTerminateAgent_StopsSpawnedTreeBeforeRemovingState(t *testing.T) {
	useProductionAgentProcesses(t)

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	agent := startFakeAgentWithChild(t)
	t.Cleanup(func() { _ = killAgentProcessTree(agent.Pid) })

	// Captured before termination: afterwards the parent links are gone, so
	// there would be nothing left to check the provider against.
	tree, err := processTreeDeepestFirst(uint32(agent.Pid))
	if err != nil {
		t.Fatalf("read process tree: %v", err)
	}
	if len(tree) < 3 {
		t.Fatalf("fake agent tree = %v (%d processes), want the agent, its shell and the child it spawned", tree, len(tree))
	}

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    agent.Pid,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := TerminateAgent(tmpDir, "coder-1", true, true, "test", 10*time.Second)
	if err != nil {
		t.Fatalf("TerminateAgent() error = %v", err)
	}
	if !result.Process.Signaled {
		t.Fatal("TerminateAgent() did not signal the recorded process; the identity check did not recognise the agent")
	}
	if result.Process.IdentityUnverified {
		t.Fatal("TerminateAgent() reported the agent's own PID as unverifiable")
	}

	for _, pid := range tree {
		if alive, _, _ := procscan.ProcessAlive(int(pid)); alive {
			t.Fatalf("process %d survived termination of agent tree %v", pid, tree)
		}
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if _, exists := readState.Agents["coder-1"]; exists {
		t.Fatal("agent row survived a successful termination")
	}
}

// TestTerminateAgent_LeavesUnrelatedPIDAloneAndSaysSo covers the other half of
// the identity check: a recorded PID that now belongs to something else must
// not be killed, and the caller must be able to tell that something is still
// running under it.
func TestTerminateAgent_LeavesUnrelatedPIDAloneAndSaysSo(t *testing.T) {
	useProductionAgentProcesses(t)

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	unrelated := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := unrelated.Start(); err != nil {
		t.Fatalf("start unrelated process: %v", err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:   "coder",
		Status: models.AgentStatusIdle,
		PID:    unrelated.Process.Pid,
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := TerminateAgent(tmpDir, "coder-1", true, true, "test", time.Second)
	if err != nil {
		t.Fatalf("TerminateAgent() error = %v", err)
	}
	if result.Process.Signaled || result.Process.Killed {
		t.Fatal("TerminateAgent() signalled a process it could not identify as the agent")
	}
	if !result.Process.IdentityUnverified {
		t.Fatal("TerminateAgent() dropped the agent row without reporting that its PID was still in use")
	}
	if alive, _, _ := procscan.ProcessAlive(unrelated.Process.Pid); !alive {
		t.Fatal("the unrelated process was killed")
	}
}

// useProductionAgentProcesses pins agentProcesses to the real implementations
// for the duration of a test, and restores whatever was there before.
func useProductionAgentProcesses(t *testing.T) {
	t.Helper()
	original := agentProcesses
	agentProcesses = agentProcessOps{
		isLizaAgent: isLizaAgentProcess,
		isAlive:     IsProcessAlive,
		signalTree:  signalAgentProcessTree,
		killTree:    killAgentProcessTree,
		waitForExit: waitForAgentProcessExit,
	}
	t.Cleanup(func() { agentProcesses = original })
}

// startFakeAgentWithChild starts a process the identity check accepts —
// argv[0] resolves to the binary name and argv[1] is "agent" — which then
// spawns a long-lived child of its own, standing in for the provider CLI.
func startFakeAgentWithChild(t *testing.T) *os.Process {
	t.Helper()

	// The stubs stay out of t.TempDir: a running executable cannot be deleted
	// on Windows, and a cleanup failure there would fail a test whose
	// assertions passed.
	binDir, err := os.MkdirTemp("", "fake-agent-*")
	if err != nil {
		t.Fatalf("create stub directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(binDir) })

	stub := testhelpers.WriteShellStub(t, filepath.Join(binDir, "liza"), `#!/bin/sh
ping -n 60 127.0.0.1 >/dev/null 2>&1 &
wait
`)

	cmd := exec.Command(stub, "agent", "coder")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake agent: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })

	// The tree is only complete once the shell has spawned its child.
	deadline := time.Now().Add(30 * time.Second)
	for {
		tree, err := processTreeDeepestFirst(uint32(cmd.Process.Pid))
		if err == nil && len(tree) >= 3 {
			return cmd.Process
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake agent never spawned its child (tree: %v, err: %v)", tree, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
