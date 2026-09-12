package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestAgentGeneration(t *testing.T) {
	var legacy models.Agent
	if err := yaml.Unmarshal([]byte("role: coder\nstatus: IDLE\nheartbeat: 2026-08-25T00:00:00Z\nterminal: terminal-1\n"), &legacy); err != nil {
		t.Fatalf("decode legacy agent: %v", err)
	}
	if legacy.Generation != "" {
		t.Fatalf("legacy generation = %q, want empty", legacy.Generation)
	}

	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	resolver := testResolver(t)

	authorityA, err := registerAgentWithAuthority(bb, projectRoot, "coder-1", "coder", "terminal-a", 1800, "codex", resolver)
	if err != nil {
		t.Fatalf("register generation A: %v", err)
	}
	if authorityA.ID != "coder-1" || authorityA.Generation == "" {
		t.Fatalf("authority A = %#v, want matching ID and non-empty generation", authorityA)
	}
	registeredA, err := bb.Read()
	if err != nil {
		t.Fatalf("read generation A: %v", err)
	}
	if registeredA.Agents[authorityA.ID].Generation != authorityA.Generation {
		t.Fatalf("persisted generation A = %q, want %q", registeredA.Agents[authorityA.ID].Generation, authorityA.Generation)
	}

	if err := bb.Modify(func(state *models.State) error {
		agent := state.Agents[authorityA.ID]
		expired := time.Now().UTC().Add(-time.Minute)
		agent.LeaseExpires = &expired
		state.Agents[authorityA.ID] = agent
		return nil
	}); err != nil {
		t.Fatalf("expire generation A: %v", err)
	}

	authorityB, err := registerAgentWithAuthority(bb, projectRoot, "coder-1", "coder", "terminal-b", 1800, "claude", resolver)
	if err != nil {
		t.Fatalf("register takeover generation B: %v", err)
	}
	if authorityB.Generation == "" || authorityB.Generation == authorityA.Generation {
		t.Fatalf("authority B generation = %q, want non-empty and distinct from %q", authorityB.Generation, authorityA.Generation)
	}
	registeredB, err := bb.Read()
	if err != nil {
		t.Fatalf("read generation B: %v", err)
	}
	if registeredB.Agents[authorityB.ID].Generation != authorityB.Generation {
		t.Fatalf("persisted generation B = %q, want %q", registeredB.Agents[authorityB.ID].Generation, authorityB.Generation)
	}
}

func testAgentAuthority(t *testing.T, bb *db.Blackboard, agentID string) models.AgentAuthority {
	t.Helper()
	const generation = "test-generation"
	if err := bb.Modify(func(state *models.State) error {
		agent, exists := state.Agents[agentID]
		if !exists {
			return fmt.Errorf("agent %s not found", agentID)
		}
		agent.Generation = generation
		state.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("install test authority for %s: %v", agentID, err)
	}
	return models.AgentAuthority{ID: agentID, Generation: generation}
}

// TestValidateIdentity tests agent ID format validation
func TestValidateIdentity(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		role    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid coder ID",
			agentID: "coder-1",
			role:    "coder",
			wantErr: false,
		},
		{
			name:    "valid reviewer ID",
			agentID: "code-reviewer-1",
			role:    "code-reviewer",
			wantErr: false,
		},
		{
			name:    "valid orchestrator ID",
			agentID: "orchestrator-1",
			role:    "orchestrator",
			wantErr: false,
		},
		{
			name:    "valid multi-digit number",
			agentID: "coder-42",
			role:    "coder",
			wantErr: false,
		},
		{
			name:    "empty agent ID",
			agentID: "",
			role:    "coder",
			wantErr: true,
			errMsg:  "agent ID required",
		},
		{
			name:    "missing number",
			agentID: "coder",
			role:    "coder",
			wantErr: true,
			errMsg:  "format",
		},
		{
			name:    "non-numeric suffix",
			agentID: "coder-abc",
			role:    "coder",
			wantErr: true,
			errMsg:  "numeric",
		},
		{
			name:    "role mismatch",
			agentID: "coder-1",
			role:    "orchestrator",
			wantErr: true,
			errMsg:  "mismatch",
		},
		{
			name:    "invalid prefix",
			agentID: "invalid-1",
			role:    "coder",
			wantErr: true,
			errMsg:  "mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIdentity(tt.agentID, tt.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIdentity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Error should contain %q, got %v", tt.errMsg, err)
			}
		})
	}
}

// TestRegisterAgent tests agent registration
func TestRegisterAgent(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	tests := []struct {
		name           string
		agentID        string
		role           string
		provider       string
		existingAgent  *models.Agent
		expectRegister bool
		wantErr        bool
		errMsg         string
	}{
		{
			name:           "new agent registration",
			agentID:        "coder-1",
			role:           "coder",
			provider:       "claude",
			existingAgent:  nil,
			expectRegister: true,
			wantErr:        false,
		},
		{
			name:     "collision with valid lease",
			agentID:  "coder-1",
			role:     "coder",
			provider: "claude",
			existingAgent: &models.Agent{
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
				Heartbeat:    time.Now().UTC(),
				PID:          os.Getpid(),
			},
			expectRegister: false,
			wantErr:        true,
			errMsg:         "collision",
		},
		{
			name:     "takeover expired lease",
			agentID:  "coder-1",
			role:     "coder",
			provider: "codex",
			existingAgent: &models.Agent{
				Role:         "coder",
				Status:       models.AgentStatusWorking,
				LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(-10 * time.Minute)),
				Heartbeat:    time.Now().UTC().Add(-10 * time.Minute),
			},
			expectRegister: true,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			if tt.existingAgent != nil {
				state.Agents[tt.agentID] = *tt.existingAgent
			}

			bb := testhelpers.WriteInitialState(t, statePath, state)

			resolver := testResolver(t)
			err := registerAgent(bb, tmpDir, tt.agentID, tt.role, "terminal-1", 1800, tt.provider, resolver)

			if (err != nil) != tt.wantErr {
				t.Errorf("registerAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Error should contain %q, got %v", tt.errMsg, err)
			}

			if tt.expectRegister {
				// Verify agent was registered with correct status
				state, err := bb.Read()
				if err != nil {
					t.Fatalf("Failed to read state: %v", err)
				}

				agent, exists := state.Agents[tt.agentID]
				if !exists {
					t.Errorf("Agent %s not registered", tt.agentID)
					return
				}

				if agent.Status != models.AgentStatusIdle {
					t.Errorf("Expected status IDLE, got %s", agent.Status)
				}

				if agent.Role != tt.role {
					t.Errorf("Expected role %s, got %s", tt.role, agent.Role)
				}

				// Verify provider is stored
				if agent.Provider != tt.provider {
					t.Errorf("Expected provider %q, got %q", tt.provider, agent.Provider)
				}

				// Verify PID is stored
				if agent.PID == 0 {
					t.Error("Expected PID to be set (non-zero)")
				}
			}
		})
	}
}

func TestRegisterAgentWaitsForProjectCleanupExclusion(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)
	resolver := testResolver(t)

	exclusiveAcquired := make(chan struct{})
	releaseExclusive := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseExclusive) }) }
	t.Cleanup(release)
	exclusiveDone := make(chan error, 1)
	go func() {
		exclusiveDone <- ops.WithProjectLifecycleExclusiveLock(projectRoot, "test-cleanup", func() error {
			close(exclusiveAcquired)
			<-releaseExclusive
			return nil
		})
	}()
	select {
	case <-exclusiveAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup exclusion was not acquired")
	}

	registrationStarted := make(chan struct{})
	registrationDone := make(chan error, 1)
	go func() {
		close(registrationStarted)
		registrationDone <- registerAgent(bb, projectRoot, "coder-1", "coder", "terminal-1", 1800, "codex", resolver)
	}()
	<-registrationStarted
	select {
	case err := <-registrationDone:
		t.Fatalf("registerAgent() completed during cleanup exclusion: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	current, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := current.Agents["coder-1"]; exists {
		t.Fatal("agent registered while cleanup exclusion was held")
	}

	release()
	if err := <-exclusiveDone; err != nil {
		t.Fatalf("cleanup exclusion error = %v", err)
	}
	if err := <-registrationDone; err != nil {
		t.Fatalf("registerAgent() error after cleanup exclusion = %v", err)
	}
	current, err = bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := current.Agents["coder-1"]; !exists {
		t.Fatal("agent did not register after cleanup exclusion released")
	}
}

func TestRegisterAgentNamespaceSafeSingularity(t *testing.T) {
	tests := []struct {
		name              string
		pid               int
		argv              []string
		candidatePID      int
		wantCorrelation   string
		allowTakeoverWith string
	}{
		{
			name:              "procfs mismatch",
			pid:               1234,
			argv:              []string{"go", "test"},
			candidatePID:      4321,
			wantCorrelation:   "observer-visible matching pid 4321",
			allowTakeoverWith: "expiry",
		},
		{
			name:              "pid not found",
			pid:               987654321,
			wantCorrelation:   "correlation unavailable",
			allowTakeoverWith: "removal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			procRoot := t.TempDir()
			t.Cleanup(ops.SetAgentProcessProcRootForTest(procRoot))
			if len(tt.argv) > 0 {
				writeRegistrationProcCmdline(t, procRoot, tt.pid, tt.argv)
			}
			if tt.candidatePID != 0 {
				writeRegistrationProcCmdline(t, procRoot, tt.candidatePID, []string{
					"liza", "agent", "orchestrator", "--agent-id", "orchestrator-1",
				})
			}

			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Agents["orchestrator-1"] = models.Agent{
				Role:         "orchestrator",
				Status:       models.AgentStatusPlanning,
				LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
				Heartbeat:    time.Now().UTC(),
				PID:          tt.pid,
			}
			bb := testhelpers.WriteInitialState(t, statePath, state)

			err := registerAgent(bb, tmpDir, "orchestrator-2", "orchestrator", "terminal-2", 1800, "codex", testResolver(t))
			if err == nil {
				t.Fatal("registerAgent() error = nil, want fresh unexpired orchestrator to preserve singularity")
			}
			for _, want := range []string{
				"unknown/degraded",
				fmt.Sprintf("recorded namespace-local pid %d", tt.pid),
				tt.wantCorrelation,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("registerAgent() error = %q, want %q", err, want)
				}
			}

			if err := bb.Modify(func(state *models.State) error {
				switch tt.allowTakeoverWith {
				case "expiry":
					expired := time.Now().UTC().Add(-time.Minute)
					existing := state.Agents["orchestrator-1"]
					existing.LeaseExpires = &expired
					state.Agents["orchestrator-1"] = existing
				case "removal":
					delete(state.Agents, "orchestrator-1")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			if err := registerAgent(bb, tmpDir, "orchestrator-2", "orchestrator", "terminal-2", 1800, "codex", testResolver(t)); err != nil {
				t.Fatalf("registerAgent() after explicit %s = %v, want takeover", tt.allowTakeoverWith, err)
			}
		})
	}
}

func TestRegisterAgentKeepsUnknownActiveLeaseConservative(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := registerAgent(bb, tmpDir, "coder-1", "coder", "terminal-1", 1800, "codex", testResolver(t))
	if err == nil {
		t.Fatal("registerAgent() error = nil, want collision for unknown live process identity")
	}
}

// TestRegisterAgentCollisionIsTyped verifies collision returns AgentCollisionError
func TestRegisterAgentCollisionIsTyped(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	resolver := testResolver(t)
	err := registerAgent(bb, tmpDir, "coder-1", "coder", "terminal-1", 1800, "claude", resolver)
	if err == nil {
		t.Fatal("Expected collision error, got nil")
	}
	if !errors.IsAgentCollision(err) {
		t.Errorf("Expected AgentCollisionError, got %T: %v", err, err)
	}
}

// TestAutoAssignAgentID_RetryOnCollision verifies the retry loop picks the next
// available ID when the first candidate collides with a concurrent registration.
func TestAutoAssignAgentID_RetryOnCollision(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Seed state: coder-1 has a valid lease, so NextAvailableID picks coder-2.
	state := testhelpers.CreateValidState()
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// tryFn simulates a collision on the first call, then succeeds.
	// This mimics another process registering coder-2 between Read and Register.
	resolver := testResolver(t)
	calls := 0
	assignedID, err := AutoAssignAgentID(bb, "coder", 5, func(candidateID string) error {
		calls++
		if calls == 1 {
			// Simulate concurrent registration: register the candidate under lock
			// so the next retry sees it as taken.
			regErr := registerAgent(bb, tmpDir, candidateID, "coder", "terminal-1", 1800, "claude", resolver)
			if regErr != nil {
				t.Fatalf("Setup: registerAgent failed: %v", regErr)
			}
			// Return collision as if our own registration attempt lost the race.
			return &errors.AgentCollisionError{AgentID: candidateID}
		}
		// Second call: just succeed.
		return nil
	})

	if err != nil {
		t.Fatalf("AutoAssignAgentID returned error: %v", err)
	}
	if calls != 2 {
		t.Errorf("Expected 2 calls to tryFn, got %d", calls)
	}
	// First call got coder-2, collision. Second call should get coder-3
	// (coder-1 has valid lease, coder-2 was registered in the collision setup).
	if assignedID != "coder-3" {
		t.Errorf("Expected coder-3, got %s", assignedID)
	}
}

// TestAutoAssignAgentID_NoCollision verifies the happy path.
func TestAutoAssignAgentID_NoCollision(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	calls := 0
	assignedID, err := AutoAssignAgentID(bb, "coder", 5, func(candidateID string) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("AutoAssignAgentID returned error: %v", err)
	}
	if calls != 1 {
		t.Errorf("Expected 1 call, got %d", calls)
	}
	if assignedID != "coder-1" {
		t.Errorf("Expected coder-1, got %s", assignedID)
	}
}

// TestAutoAssignAgentID_ExhaustsRetries verifies that persistent collisions
// return the last collision error.
func TestAutoAssignAgentID_ExhaustsRetries(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	calls := 0
	_, err := AutoAssignAgentID(bb, "coder", 3, func(candidateID string) error {
		calls++
		return &errors.AgentCollisionError{AgentID: candidateID}
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.IsAgentCollision(err) {
		t.Errorf("Expected AgentCollisionError, got %T: %v", err, err)
	}
	if calls != 3 {
		t.Errorf("Expected 3 calls (exhausted retries), got %d", calls)
	}
}

// TestRegisterAgentConcurrent tests concurrent registration race condition
func TestRegisterAgentConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	agentID := "coder-1"
	role := "coder"
	numGoroutines := 5

	// Track success/failure counts
	successes := make(chan bool, numGoroutines)
	errors := make(chan error, numGoroutines)

	resolver := testResolver(t)

	// Launch multiple goroutines trying to register the same agent ID
	for range numGoroutines {
		go func() {
			err := registerAgent(bb, tmpDir, agentID, role, "terminal-1", 1800, "claude", resolver)
			if err != nil {
				errors <- err
			} else {
				successes <- true
			}
		}()
	}

	// Collect results
	successCount := 0
	errorCount := 0
	for range numGoroutines {
		select {
		case <-successes:
			successCount++
		case err := <-errors:
			errorCount++
			// Verify error is about collision
			if !strings.Contains(err.Error(), "collision") && !strings.Contains(err.Error(), "already has") {
				t.Errorf("Expected collision error, got: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for registration results")
		}
	}

	// Exactly one should succeed, rest should fail with collision
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful registration, got %d", successCount)
	}

	if errorCount != numGoroutines-1 {
		t.Errorf("Expected %d collision errors, got %d", numGoroutines-1, errorCount)
	}

	// Verify only one agent exists in state
	finalState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read final state: %v", err)
	}

	agent, exists := finalState.Agents[agentID]
	if !exists {
		t.Fatal("Agent should be registered")
	}

	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Agent status should be IDLE, got %s", agent.Status)
	}
}

// TestUnregisterAgent tests agent cleanup
func TestUnregisterAgent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	state.Agents[agentID] = models.Agent{
		Role:      "coder",
		Status:    models.AgentStatusWorking,
		Heartbeat: time.Now().UTC(),
	}

	bb := testhelpers.WriteInitialState(t, statePath, state)

	unregisterAgent(bb, testAgentAuthority(t, bb, agentID), tmpDir)

	// Verify agent was removed
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	if _, exists := state.Agents[agentID]; exists {
		t.Errorf("Agent %s should be unregistered", agentID)
	}
}

func TestUnregisterAgent_ReleasesTaskSideClaimWhenCurrentTaskMissing(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	state.Agents[agentID] = models.Agent{
		Status:       models.AgentStatusIdle,
		LeaseExpires: &leaseExpires,
	}
	state.Tasks = []models.Task{
		{
			ID:           "task-1",
			Status:       models.TaskStatusImplementing,
			RolePair:     "coding-pair",
			Priority:     1,
			AssignedTo:   &agentID,
			LeaseExpires: &leaseExpires,
			Created:      now,
			Description:  "orphaned claim",
			DoneWhen:     "done",
			Scope:        "scope",
			SpecRef:      "README.md",
		},
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	unregisterAgent(bb, testAgentAuthority(t, bb, agentID), tmpDir)

	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if _, exists := readState.Agents[agentID]; exists {
		t.Fatal("Agent should be removed")
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.AssignedTo != nil || task.LeaseExpires != nil {
		t.Fatalf("Task claim still present: assigned=%v lease=%v", task.AssignedTo, task.LeaseExpires)
	}
	if task.Status != models.TaskStatusReady {
		t.Fatalf("Task status = %s, want READY", task.Status)
	}
}

// TestRegisterAgentStoresPID tests that agent registration stores the process PID
func TestRegisterAgentStoresPID(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	bb := testhelpers.WriteInitialState(t, statePath, state)

	agentID := "coder-1"
	role := "coder"

	resolver := testResolver(t)
	err := registerAgent(bb, tmpDir, agentID, role, "terminal-1", 1800, "claude", resolver)
	if err != nil {
		t.Fatalf("registerAgent() error = %v", err)
	}

	// Verify PID is stored and matches current process
	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent, exists := state.Agents[agentID]
	if !exists {
		t.Fatal("Agent should be registered")
	}

	// Verify PID is non-zero
	if agent.PID == 0 {
		t.Error("PID should be set (non-zero)")
	}

	// Verify PID matches current process (in test, it will be the test process PID)
	currentPID := os.Getpid()
	if agent.PID != currentPID {
		t.Errorf("Expected PID to be %d (current process), got %d", currentPID, agent.PID)
	}
}

// TestOrchestratorStatusTransitions tests orchestrator agent status lifecycle
func TestOrchestratorStatusTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	// No tasks - triggers INITIAL_PLANNING work for orchestrator
	state.Tasks = []models.Task{}
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)

	// Register orchestrator agent
	agentID := "orchestrator-1"
	resolver := testResolver(t)
	err := registerAgent(bb, tmpDir, agentID, "orchestrator", "terminal-1", 1800, "claude", resolver)
	if err != nil {
		t.Fatalf("registerAgent() error = %v", err)
	}

	// Verify initial status is IDLE
	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Initial status = %s, want IDLE", agent.Status)
	}

	// Simulate orchestrator starting work - set PLANNING status
	err = setAgentToOrchestratingStatus(bb, testAgentAuthority(t, bb, agentID))
	if err != nil {
		t.Fatalf("setAgentToOrchestratingStatus() error = %v", err)
	}

	// Verify status is now PLANNING
	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent = state.Agents[agentID]
	if agent.Status != models.AgentStatusPlanning {
		t.Errorf("Status after setAgentToOrchestratingStatus = %s, want PLANNING", agent.Status)
	}

	// Verify heartbeat was updated
	if agent.Heartbeat.Before(time.Now().UTC().Add(-5 * time.Second)) {
		t.Error("Heartbeat should be updated when status changes to PLANNING")
	}

	// Simulate orchestrator completing work - reset to IDLE
	err = resetAgentToIdle(bb, testAgentAuthority(t, bb, agentID))
	if err != nil {
		t.Fatalf("resetAgentToIdle() error = %v", err)
	}

	// Verify status is back to IDLE
	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent = state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Status after resetAgentToIdle = %s, want IDLE", agent.Status)
	}
}

// TestSetAgentToPlanningStatusNonExistent tests error handling for non-existent agent
func TestSetAgentToPlanningStatusNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)

	// Try to set status for non-existent agent
	err := setAgentToOrchestratingStatus(bb, models.AgentAuthority{ID: "orchestrator-999", Generation: "test-generation"})
	if err == nil {
		t.Error("setAgentToOrchestratingStatus() should return error for non-existent agent")
	}

	if !strings.Contains(err.Error(), "current generation fingerprint <missing>") {
		t.Errorf("expected missing current-generation diagnostic, got %T: %v", err, err)
	}
}

// TestResetAgentToIdle_NotFound tests error handling for non-existent agent
func TestResetAgentToIdle_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)

	err := resetAgentToIdle(bb, models.AgentAuthority{ID: "nonexistent", Generation: "test-generation"})
	if err == nil {
		t.Fatal("Expected error for nonexistent agent")
	}
	if !strings.Contains(err.Error(), "current generation fingerprint <missing>") {
		t.Errorf("expected missing current-generation diagnostic, got %T: %v", err, err)
	}
}

// TestResetAgentAfterExit_WaitingWithoutCurrentTask tests that a WAITING agent
// with no CurrentTask gets reset to IDLE (not preserved as WAITING).
func TestResetAgentAfterExit_WaitingWithoutCurrentTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	state.Agents[agentID] = models.Agent{
		Role:      "coder",
		Status:    models.AgentStatusWaiting,
		Heartbeat: time.Now().UTC(),
		// CurrentTask is nil — simulates post-submit state
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}
}

// TestResetAgentAfterExit_DoerWaitingReleasesClaim tests that a WAITING doer
// with CurrentTask set is reset and its claim released. A session never resumes
// after exit, so preserving the claim would only pin the task to a departed agent.
func TestResetAgentAfterExit_DoerWaitingReleasesClaim(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	taskID := "task-1"
	state.Agents[agentID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskID,
		Heartbeat:   time.Now().UTC(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}
}

func TestResetAgentAfterExit_ReviewerWaitingReleasesPassiveOwnership(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "code-reviewer-1"
	taskID := "task-1"
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:                 taskID,
			Description:        "Rejected task awaiting resubmission",
			Status:             models.TaskStatusRejected,
			Priority:           1,
			Created:            now,
			SpecRef:            "README.md",
			DoneWhen:           "Done",
			Scope:              "Test",
			RolePair:           "coding-pair",
			ReviewingBy:        testhelpers.StringPtr(agentID),
			ReviewLeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		},
	}
	state.Agents[agentID] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskID,
		Heartbeat:   now,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	if err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir); err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected agent status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusRejected {
		t.Errorf("Expected task status CODE_REJECTED, got %s", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("Expected ReviewingBy nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("Expected ReviewLeaseExpires nil")
	}
}

func TestResetAgentAfterExit_ReviewerWaitingDoesNotClearOtherReviewerClaim(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "code-reviewer-1"
	otherReviewer := "code-reviewer-2"
	taskID := "task-1"
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		{
			ID:                 taskID,
			Description:        "Rejected task claimed by another reviewer",
			Status:             models.TaskStatusRejected,
			Priority:           1,
			Created:            now,
			SpecRef:            "README.md",
			DoneWhen:           "Done",
			Scope:              "Test",
			RolePair:           "coding-pair",
			ReviewingBy:        testhelpers.StringPtr(otherReviewer),
			ReviewLeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		},
	}
	state.Agents[agentID] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWaiting,
		CurrentTask: &taskID,
		Heartbeat:   now,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	if err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir); err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected agent status IDLE, got %s", agent.Status)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewingBy == nil || *task.ReviewingBy != otherReviewer {
		t.Fatalf("ReviewingBy = %v, want %s", task.ReviewingBy, otherReviewer)
	}
	if task.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires should remain set")
	}
}

// TestResetAgentAfterExit_NotFound tests error handling for non-existent agent
func TestResetAgentAfterExit_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, statePath, state)

	bb := db.New(statePath)

	err := resetAgentAfterExit(bb, models.AgentAuthority{ID: "nonexistent", Generation: "test-generation"}, tmpDir)
	if err == nil {
		t.Fatal("Expected error for nonexistent agent")
	}
	if !strings.Contains(err.Error(), "current generation fingerprint <missing>") {
		t.Errorf("expected missing current-generation diagnostic, got %T: %v", err, err)
	}
}

// TestResetAgentAfterExit_ReviewerReleasesTask tests that a reviewer exiting
// without submitting a verdict releases the task claim (REVIEWING → READY_FOR_REVIEW).
func TestResetAgentAfterExit_ReviewerReleasesTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "code-reviewer-1"
	taskID := "task-1"
	now := time.Now().UTC()

	state.Tasks = append(state.Tasks, models.Task{
		ID:                 taskID,
		Status:             models.TaskStatusReviewing,
		ReviewingBy:        testhelpers.StringPtr(agentID),
		ReviewLeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		Created:            now,
		Priority:           1,
		Iteration:          1,
		Type:               models.TaskTypeCoding,
		RolePair:           "coding-pair",
	})
	state.Agents[agentID] = models.Agent{
		Role:        "code-reviewer",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskID,
		Heartbeat:   now,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Agent should be IDLE with no CurrentTask
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected agent status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}

	// Task should be back to READY_FOR_REVIEW with claim fields cleared
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReadyForReview {
		t.Errorf("Expected task status READY_FOR_REVIEW, got %s", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("Expected ReviewingBy nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("Expected ReviewLeaseExpires nil")
	}
}

func TestResetAgentAfterExit_ReviewerReleasesReviewing2Task(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	writeValidationSpecRefs(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "code-reviewer-2"
	taskID := "task-1"
	now := time.Now().UTC()
	reviewCommit := "review123"
	reviewLease := now.Add(30 * time.Minute)
	agentLease := now.Add(30 * time.Minute)
	worktree := ".worktrees/task-1"

	state.Tasks = append(state.Tasks, models.Task{
		ID:                 taskID,
		Status:             "REVIEWING_CODE_2",
		ReviewCommit:       &reviewCommit,
		ReviewingBy:        testhelpers.StringPtr(agentID),
		ReviewLeaseExpires: &reviewLease,
		Created:            now,
		Priority:           1,
		Iteration:          1,
		Type:               models.TaskTypeCoding,
		RolePair:           "coding-pair",
		SpecRef:            "README.md",
		DoneWhen:           "Task is complete",
		Scope:              "Test scope",
		Worktree:           &worktree,
		HandoffEvents: []models.HandoffEvent{
			{Timestamp: now.Add(-2 * time.Minute), Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
		},
		Approvals: []models.Approval{
			{Agent: "code-reviewer-1", Provider: "codex", Timestamp: now.Add(-time.Minute)},
		},
	})
	state.Agents[agentID] = models.Agent{
		Role:         "code-reviewer",
		Provider:     "codex",
		Status:       models.AgentStatusReviewing,
		CurrentTask:  &taskID,
		LeaseExpires: &agentLease,
		Heartbeat:    now,
		PID:          12345,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected agent status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}

	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != "CODE_PARTIALLY_APPROVED" {
		t.Errorf("Expected task status CODE_PARTIALLY_APPROVED, got %s", task.Status)
	}
	if task.ReviewingBy != nil {
		t.Errorf("Expected ReviewingBy nil, got %v", *task.ReviewingBy)
	}
	if task.ReviewLeaseExpires != nil {
		t.Error("Expected ReviewLeaseExpires nil")
	}
	if err := statevalidate.ValidateState(state, tmpDir, false, io.Discard); err != nil {
		t.Fatalf("state should validate after resetAgentAfterExit: %v", err)
	}
}

func writeValidationSpecRefs(t *testing.T, tmpDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(tmpDir, "specs"), 0755); err != nil {
		t.Fatalf("create specs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "specs", "vision.md"), []byte("test goal\n"), 0644); err != nil {
		t.Fatalf("write goal spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("test task spec\n"), 0644); err != nil {
		t.Fatalf("write task spec: %v", err)
	}
}

// TestResetAgentAfterExit_CoderReleasesTask tests that a coder exiting
// without submitting for review releases the task claim (IMPLEMENTING → READY).
func TestResetAgentAfterExit_CoderReleasesTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	taskID := "task-1"
	now := time.Now().UTC()

	state.Tasks = append(state.Tasks, models.Task{
		ID:         taskID,
		Status:     models.TaskStatusImplementing,
		AssignedTo: testhelpers.StringPtr(agentID),
		Created:    now,
		Priority:   1,
		Iteration:  1,
		Type:       models.TaskTypeCoding,
		RolePair:   "coding-pair",
	})
	state.Agents[agentID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusWorking,
		CurrentTask: &taskID,
		Heartbeat:   now,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Agent should be IDLE with no CurrentTask
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected agent status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}

	// Task should be back to READY with claim fields cleared
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusReady {
		t.Errorf("Expected task status READY, got %s", task.Status)
	}
	if task.AssignedTo != nil {
		t.Errorf("Expected AssignedTo nil, got %v", *task.AssignedTo)
	}
}

// TestResetAgentAfterExit_HandoffPreservesTask tests that a HANDOFF agent
// with CurrentTask set preserves the task claim (existing behavior).
func TestResetAgentAfterExit_HandoffPreservesTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	taskID := "task-1"
	now := time.Now().UTC()

	state.Tasks = append(state.Tasks, models.Task{
		ID:         taskID,
		Status:     models.TaskStatusImplementing,
		AssignedTo: testhelpers.StringPtr(agentID),
		Created:    now,
		Priority:   1,
		Iteration:  1,
		Type:       models.TaskTypeCoding,
	})
	state.Agents[agentID] = models.Agent{
		Role:        "coder",
		Status:      models.AgentStatusHandoff,
		CurrentTask: &taskID,
		Heartbeat:   now,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	// Agent should still be HANDOFF with CurrentTask preserved
	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusHandoff {
		t.Errorf("Expected agent status HANDOFF, got %s", agent.Status)
	}
	if agent.CurrentTask == nil || *agent.CurrentTask != taskID {
		t.Errorf("Expected CurrentTask %q, got %v", taskID, agent.CurrentTask)
	}

	// Task should still be IMPLEMENTING with assignment preserved
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.Status != models.TaskStatusImplementing {
		t.Errorf("Expected task status IMPLEMENTING, got %s", task.Status)
	}
	if task.AssignedTo == nil || *task.AssignedTo != agentID {
		t.Errorf("Expected AssignedTo %q, got %v", agentID, task.AssignedTo)
	}
}

// TestResetAgentAfterExit_NoCurrentTask tests that an agent with no CurrentTask
// is a no-op for task release (no panic, no error).
func TestResetAgentAfterExit_NoCurrentTask(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	agentID := "coder-1"
	state.Agents[agentID] = models.Agent{
		Role:      "coder",
		Status:    models.AgentStatusWorking,
		Heartbeat: time.Now().UTC(),
		// CurrentTask is nil
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	err := resetAgentAfterExit(bb, testAgentAuthority(t, bb, agentID), tmpDir)
	if err != nil {
		t.Fatalf("resetAgentAfterExit() error = %v", err)
	}

	state, err = bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	agent := state.Agents[agentID]
	if agent.Status != models.AgentStatusIdle {
		t.Errorf("Expected status IDLE, got %s", agent.Status)
	}
	if agent.CurrentTask != nil {
		t.Errorf("Expected CurrentTask nil, got %v", *agent.CurrentTask)
	}
}

// TestRegisterSecondOrchestratorBlocked tests that registering a second
// orchestrator with an active lease is rejected.
func TestRegisterSecondOrchestratorBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{
		Role:         "orchestrator",
		Status:       models.AgentStatusPlanning,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	resolver := testResolver(t)
	err := registerAgent(bb, tmpDir, "orchestrator-2", "orchestrator", "terminal-2", 1800, "claude", resolver)
	if err == nil {
		t.Fatal("expected error registering second orchestrator, got nil")
	}
	if !strings.Contains(err.Error(), "already has") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRegisterOrchestratorTakeoverExpired tests that a new orchestrator can
// register when the existing orchestrator's lease has expired.
func TestRegisterOrchestratorTakeoverExpired(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{
		Role:         "orchestrator",
		Status:       models.AgentStatusPlanning,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(-10 * time.Minute)),
		Heartbeat:    time.Now().UTC().Add(-10 * time.Minute),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	resolver2 := testResolver(t)
	err := registerAgent(bb, tmpDir, "orchestrator-2", "orchestrator", "terminal-2", 1800, "claude", resolver2)
	if err != nil {
		t.Fatalf("expected registration to succeed with expired peer, got: %v", err)
	}

	// Verify orchestrator-2 is registered
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if _, ok := readState.Agents["orchestrator-2"]; !ok {
		t.Error("orchestrator-2 should be registered")
	}
}

// TestRegisterOrchestratorSingularityAcrossRoleKeys verifies that orchestrator
// singularity is enforced by resolved type, not role key. Two different YAML role
// keys that both resolve to type: orchestrator must not coexist.
func TestRegisterOrchestratorSingularityAcrossRoleKeys(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Pipeline with two distinct orchestrator role keys.
	twoOrchYAML := []byte(`
pipeline:
  roles:
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
    lead-orchestrator:
      type: orchestrator
      display-name: "Lead Orchestrator"
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := pipeline.LoadFromBytes(twoOrchYAML)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)

	state := testhelpers.CreateValidState()
	// Register first orchestrator with role key "orchestrator".
	state.Agents["orchestrator-1"] = models.Agent{
		Role:         "orchestrator",
		Status:       models.AgentStatusPlanning,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// Attempt to register a second orchestrator with a DIFFERENT role key.
	// Must be rejected because the resolved type is still "orchestrator".
	err = registerAgent(bb, tmpDir, "lead-orchestrator-1", "lead-orchestrator", "terminal-2", 1800, "claude", resolver)
	if err == nil {
		t.Fatal("expected error registering second orchestrator with different role key, got nil")
	}
	if !strings.Contains(err.Error(), "orchestrator") {
		t.Errorf("error should mention orchestrator, got: %v", err)
	}
}

// TestRegisterNonOrchestratorMaxInstancesUnaffected verifies that per-role-key
// max-instances enforcement for non-orchestrator roles is unchanged.
func TestRegisterNonOrchestratorMaxInstancesUnaffected(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Pipeline with two doer roles that have max-instances: 1.
	yaml := []byte(`
pipeline:
  roles:
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
    coder:
      type: doer
      max-instances: 1
      display-name: "Coder"
    data-engineer:
      type: doer
      max-instances: 1
      display-name: "Data Engineer"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := pipeline.LoadFromBytes(yaml)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)

	state := testhelpers.CreateValidState()
	// Register a coder (doer, max-instances: 1).
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// A second coder should be rejected (same role key, max-instances: 1).
	err = registerAgent(bb, tmpDir, "coder-2", "coder", "terminal-2", 1800, "claude", resolver)
	if err == nil {
		t.Fatal("expected error registering second coder, got nil")
	}

	// A data-engineer (different doer role key, max-instances: 1) should succeed
	// because non-orchestrator roles are checked per-role-key, not per-type.
	err = registerAgent(bb, tmpDir, "data-engineer-1", "data-engineer", "terminal-3", 1800, "claude", resolver)
	if err != nil {
		t.Fatalf("data-engineer registration should succeed (different role key), got: %v", err)
	}
}

func TestRegisterMaxInstancesUsesLeaseFirstCapacity(t *testing.T) {
	const maxInstances = 2

	tests := []struct {
		name         string
		firstPID     int
		argv         []string
		wantRawState string
		release      string
	}{
		{name: "pid not found then lease expiry", firstPID: 987654320, wantRawState: "dead", release: "expiry"},
		{name: "pid not found then removal", firstPID: 987654320, wantRawState: "dead", release: "removal"},
		{name: "mismatched pid then lease expiry", firstPID: 1234, argv: []string{"go", "test"}, wantRawState: "mismatched", release: "expiry"},
		{name: "mismatched pid then removal", firstPID: 1234, argv: []string{"go", "test"}, wantRawState: "mismatched", release: "removal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			procRoot := t.TempDir()
			t.Cleanup(ops.SetAgentProcessProcRootForTest(procRoot))
			if tt.argv != nil {
				for i := range maxInstances {
					writeRegistrationProcCmdline(t, procRoot, tt.firstPID+i, tt.argv)
				}
			}
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

			cfg, err := pipeline.LoadFromBytes([]byte(`
pipeline:
  roles:
    coder:
      type: doer
      max-instances: 2
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`))
			if err != nil {
				t.Fatalf("LoadFromBytes: %v", err)
			}
			resolver := pipeline.NewResolver(cfg)

			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			for i := range maxInstances {
				id := fmt.Sprintf("coder-%d", i+1)
				state.Agents[id] = models.Agent{
					Role:         "coder",
					Status:       models.AgentStatusWorking,
					LeaseExpires: testhelpers.TimePtr(now.Add(10 * time.Minute)),
					Heartbeat:    now,
					PID:          tt.firstPID + i,
				}
			}
			bb := testhelpers.WriteInitialState(t, statePath, state)

			err = registerAgent(bb, tmpDir, "coder-3", "coder", "terminal-3", 1800, "claude", resolver)
			if err == nil {
				t.Fatal("registerAgent() error = nil, want lease-first role capacity rejection")
			}
			for _, want := range []string{
				"role coder already has 2 live agent(s) (max 2)",
				"effective ownership unknown/degraded",
				"raw state " + tt.wantRawState,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("registerAgent() error = %q, want %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "effective ownership dead") {
				t.Fatalf("registerAgent() error = %q, effective ownership must remain lease-first", err)
			}

			if err := bb.Modify(func(state *models.State) error {
				switch tt.release {
				case "expiry":
					expired := now.Add(-time.Minute)
					agent := state.Agents["coder-1"]
					agent.LeaseExpires = &expired
					state.Agents["coder-1"] = agent
				case "removal":
					delete(state.Agents, "coder-1")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			if err := registerAgent(bb, tmpDir, "coder-3", "coder", "terminal-3", 1800, "claude", resolver); err != nil {
				t.Fatalf("registerAgent() after %s = %v, want available capacity", tt.release, err)
			}
		})
	}
}

// TestRegisterSecondOrchestratorBlockedDespiteMisconfiguredMaxInstances verifies
// that orchestrator singularity holds even when pipeline YAML sets max-instances: 2.
// This is the spec invariant: orchestrator is always singular regardless of YAML.
func TestRegisterSecondOrchestratorBlockedDespiteMisconfiguredMaxInstances(t *testing.T) {
	tmpDir := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)

	// Build a resolver from YAML that misconfigures orchestrator with max-instances: 2.
	misconfiguredYAML := []byte(`
pipeline:
  roles:
    orchestrator:
      type: orchestrator
      max-instances: 2
      display-name: "Orchestrator"
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := pipeline.LoadFromBytes(misconfiguredYAML)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)

	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{
		Role:         "orchestrator",
		Status:       models.AgentStatusPlanning,
		LeaseExpires: testhelpers.TimePtr(time.Now().UTC().Add(10 * time.Minute)),
		Heartbeat:    time.Now().UTC(),
		PID:          os.Getpid(),
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// Even though YAML says max-instances: 2, resolver coerces to 1 for orchestrator.
	// A second live orchestrator must be rejected.
	err = registerAgent(bb, tmpDir, "orchestrator-2", "orchestrator", "terminal-2", 1800, "claude", resolver)
	if err == nil {
		t.Fatal("expected error registering second orchestrator with misconfigured max-instances: 2, got nil")
	}
	if !strings.Contains(err.Error(), "already has") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func writeRegistrationProcCmdline(t *testing.T, procRoot string, pid int, argv []string) {
	t.Helper()
	procDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmdline := strings.Join(argv, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(cmdline), 0644); err != nil {
		t.Fatal(err)
	}
}
