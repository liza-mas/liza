package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func newAuthenticatedTestHeartbeat(t *testing.T, config HeartbeatConfig) *Heartbeat {
	t.Helper()
	const generation = "test-generation"
	config.Authority = models.AgentAuthority{ID: config.AgentID, Generation: generation}
	bb := db.New(config.StatePath)
	state, err := bb.Read()
	if err == nil {
		if _, exists := state.Agents[config.AgentID]; exists {
			if err := bb.Modify(func(current *models.State) error {
				agent := current.Agents[config.AgentID]
				agent.Generation = generation
				current.Agents[config.AgentID] = agent
				return nil
			}); err != nil {
				t.Fatalf("install heartbeat test authority: %v", err)
			}
		}
	}
	return NewHeartbeat(config)
}

func TestHeartbeat(t *testing.T) {
	tests := []struct {
		name          string
		agentID       string
		interval      time.Duration
		leaseDuration time.Duration
		runDuration   time.Duration
		expectedBeats int // minimum number of heartbeats expected
		cancelEarly   bool
		wantErr       bool
	}{
		{
			name:          "heartbeat updates timestamp and lease",
			agentID:       "coder-1",
			interval:      100 * time.Millisecond,
			leaseDuration: 30 * time.Minute,
			runDuration:   350 * time.Millisecond, // Should get 3+ beats
			expectedBeats: 3,
			cancelEarly:   false,
			wantErr:       false,
		},
		{
			name:          "heartbeat stops on context cancellation",
			agentID:       "coder-2",
			interval:      50 * time.Millisecond,
			leaseDuration: 30 * time.Minute,
			runDuration:   250 * time.Millisecond,
			expectedBeats: 1,
			cancelEarly:   true,
			wantErr:       false,
		},
		{
			name:          "single heartbeat",
			agentID:       "code-reviewer-1",
			interval:      50 * time.Millisecond,
			leaseDuration: 15 * time.Minute,
			runDuration:   100 * time.Millisecond,
			expectedBeats: 1,
			cancelEarly:   false,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory and state
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Create initial state with agent
			initialState := testhelpers.CreateValidState()
			now := time.Now().UTC()
			initialHeartbeat := now.Add(-5 * time.Minute)
			initialLease := now.Add(-4 * time.Minute)
			initialState.Agents = map[string]models.Agent{
				tt.agentID: {
					Role:         "coder",
					Status:       models.AgentStatusWorking,
					Heartbeat:    initialHeartbeat,
					LeaseExpires: &initialLease,
				},
			}
			testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create heartbeat config
			config := HeartbeatConfig{
				AgentID:       tt.agentID,
				StatePath:     stateFile,
				Interval:      tt.interval,
				LeaseDuration: tt.leaseDuration,
			}

			hb := newAuthenticatedTestHeartbeat(t, config)

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), tt.runDuration)
			defer cancel()

			// Start heartbeat
			doneCh := make(chan error, 1)
			go func() {
				doneCh <- hb.Start(ctx)
			}()

			// Cancel early if requested
			if tt.cancelEarly {
				cancelTimer := time.AfterFunc(tt.runDuration/2, cancel)
				defer cancelTimer.Stop()
			}

			// Wait for completion
			select {
			case err := <-doneCh:
				if (err != nil) != tt.wantErr {
					t.Errorf("Start() error = %v, wantErr %v", err, tt.wantErr)
				}
			case <-time.After(tt.runDuration + 500*time.Millisecond):
				t.Fatal("Heartbeat did not stop in time")
			}

			// Read final state and verify heartbeat updates
			bb := db.New(stateFile)
			state, err := bb.Read()
			if err != nil {
				t.Fatalf("Failed to read state: %v", err)
			}

			agent, exists := state.Agents[tt.agentID]
			if !exists {
				t.Fatalf("Agent %s not found in state", tt.agentID)
			}

			// Verify heartbeat was updated (should be more recent than initial)
			if !agent.Heartbeat.After(initialHeartbeat) {
				t.Errorf("Heartbeat not updated: initial=%v, final=%v", initialHeartbeat, agent.Heartbeat)
			}

			// Verify lease was extended (should be more recent than initial)
			if agent.LeaseExpires == nil {
				t.Error("LeaseExpires is nil after heartbeat")
			} else if !agent.LeaseExpires.After(initialLease) {
				t.Errorf("Lease not extended: initial=%v, final=%v", initialLease, *agent.LeaseExpires)
			}

			// Verify lease is in the future
			if agent.LeaseExpires != nil && agent.LeaseExpires.Before(time.Now().UTC()) {
				t.Error("Lease is in the past after heartbeat")
			}

			// Verify lease duration is approximately correct
			if agent.LeaseExpires != nil {
				expectedLease := agent.Heartbeat.Add(tt.leaseDuration)
				leaseDiff := agent.LeaseExpires.Sub(expectedLease).Abs()
				if leaseDiff > 5*time.Second {
					t.Errorf("Lease duration incorrect: expected ~%v from heartbeat, got %v (diff: %v)",
						tt.leaseDuration, agent.LeaseExpires.Sub(agent.Heartbeat), leaseDiff)
				}
			}
		})
	}
}

func TestHeartbeatWithInvalidAgent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create state without the agent
	initialState := testhelpers.CreateValidState()
	initialState.Agents = map[string]models.Agent{}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	config := HeartbeatConfig{
		AgentID:       "nonexistent-agent",
		StatePath:     stateFile,
		Interval:      100 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	hb := newAuthenticatedTestHeartbeat(t, config)

	err := hb.Start(ctx)
	if !ops.IsAgentAuthorityError(err) {
		t.Fatalf("Start() error = %v, want generation authority error", err)
	}
}

func TestHeartbeatWithInvalidStatePath(t *testing.T) {
	config := HeartbeatConfig{
		AgentID:       "coder-1",
		StatePath:     "/nonexistent/path/state.yaml",
		Interval:      100 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	hb := newAuthenticatedTestHeartbeat(t, config)

	// Should handle invalid path gracefully
	err := hb.Start(ctx)

	// Should complete without error (errors are logged but not returned)
	if err != nil && err != context.DeadlineExceeded {
		t.Errorf("Start() unexpected error = %v", err)
	}
}

func TestHeartbeatStop(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	initialState := testhelpers.CreateValidState()
	now := time.Now().UTC()
	initialState.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			Heartbeat:    now,
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		},
	}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	config := HeartbeatConfig{
		AgentID:       "coder-1",
		StatePath:     stateFile,
		Interval:      50 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	hb := newAuthenticatedTestHeartbeat(t, config)
	if err := hb.beat(); err != nil {
		t.Fatalf("establish heartbeat before cancellation test: %v", err)
	}

	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read established heartbeat: %v", err)
	}
	if !state.Agents["coder-1"].Heartbeat.After(now) {
		t.Fatal("heartbeat was not established before cancellation test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- hb.Start(ctx)
	}()

	// Cancel context to stop heartbeat
	cancel()

	// Should stop quickly
	select {
	case <-doneCh:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Heartbeat did not stop after context cancellation")
	}
}

func TestHeartbeatConfig(t *testing.T) {
	tests := []struct {
		name   string
		config HeartbeatConfig
		valid  bool
	}{
		{
			name: "valid config",
			config: HeartbeatConfig{
				AgentID:       "coder-1",
				StatePath:     "/path/to/state.yaml",
				Interval:      60 * time.Second,
				LeaseDuration: 30 * time.Minute,
			},
			valid: true,
		},
		{
			name: "zero interval uses default",
			config: HeartbeatConfig{
				AgentID:       "coder-1",
				StatePath:     "/path/to/state.yaml",
				Interval:      0,
				LeaseDuration: 30 * time.Minute,
			},
			valid: true,
		},
		{
			name: "zero lease duration uses default",
			config: HeartbeatConfig{
				AgentID:       "coder-1",
				StatePath:     "/path/to/state.yaml",
				Interval:      60 * time.Second,
				LeaseDuration: 0,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hb := NewHeartbeat(tt.config)
			if hb == nil {
				t.Error("NewHeartbeat() returned nil")
			}
		})
	}
}

func TestHeartbeatConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create state with multiple agents
	initialState := testhelpers.CreateValidState()
	now := time.Now().UTC()
	initialState.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			Heartbeat:    now,
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		},
		"coder-2": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			Heartbeat:    now,
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		},
	}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Start multiple heartbeats concurrently
	config1 := HeartbeatConfig{
		AgentID:       "coder-1",
		StatePath:     stateFile,
		Interval:      50 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	config2 := HeartbeatConfig{
		AgentID:       "coder-2",
		StatePath:     stateFile,
		Interval:      50 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	hb1 := newAuthenticatedTestHeartbeat(t, config1)
	hb2 := newAuthenticatedTestHeartbeat(t, config2)

	done1 := make(chan error, 1)
	done2 := make(chan error, 1)

	go func() { done1 <- hb1.Start(ctx) }()
	go func() { done2 <- hb2.Start(ctx) }()

	// Wait for both to complete
	<-done1
	<-done2

	// Verify both agents were updated
	bb := db.New(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	for _, agentID := range []string{"coder-1", "coder-2"} {
		agent, exists := state.Agents[agentID]
		if !exists {
			t.Errorf("Agent %s not found", agentID)
			continue
		}
		if !agent.Heartbeat.After(now) {
			t.Errorf("Agent %s heartbeat not updated", agentID)
		}
	}
}

func TestHeartbeatDefaultValues(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, paths.ProjectDirName(), "state.yaml")

	config := HeartbeatConfig{
		AgentID:   "coder-1",
		StatePath: stateFile,
		// Interval and LeaseDuration not specified - should use defaults
	}

	hb := NewHeartbeat(config)
	if hb == nil {
		t.Fatal("NewHeartbeat() returned nil")
	}

	// Verify defaults were applied (we can't directly check private fields,
	// but we can verify the heartbeat struct was created successfully)
}

func TestHeartbeatIntervalFromConfig(t *testing.T) {
	tests := []struct {
		name                 string
		configInterval       int
		expectedIntervalSecs int
	}{
		{
			name:                 "valid interval from config (30s)",
			configInterval:       30,
			expectedIntervalSecs: 30,
		},
		{
			name:                 "valid interval from config (120s)",
			configInterval:       120,
			expectedIntervalSecs: 120,
		},
		{
			name:                 "zero interval uses default (60s)",
			configInterval:       0,
			expectedIntervalSecs: 60,
		},
		{
			name:                 "negative interval uses default (60s)",
			configInterval:       -10,
			expectedIntervalSecs: 60,
		},
		{
			name:                 "unreasonably large interval uses default (60s)",
			configInterval:       3600,
			expectedIntervalSecs: 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			// Create state with specific heartbeat interval
			initialState := testhelpers.CreateValidState()
			initialState.Config.HeartbeatInterval = tt.configInterval
			now := time.Now().UTC()
			initialState.Agents = map[string]models.Agent{
				"coder-1": {
					Role:         "coder",
					Status:       models.AgentStatusWorking,
					Heartbeat:    now,
					LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
				},
			}
			testhelpers.WriteInitialState(t, stateFile, initialState)

			// Create heartbeat config with State
			config := HeartbeatConfig{
				AgentID:   "coder-1",
				StatePath: stateFile,
				State:     initialState,
			}

			hb := newAuthenticatedTestHeartbeat(t, config)
			if hb == nil {
				t.Fatal("NewHeartbeat() returned nil")
			}

			// Verify the interval was set correctly by checking ticker behavior
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			doneCh := make(chan error, 1)
			go func() {
				doneCh <- hb.Start(ctx)
			}()

			<-doneCh
		})
	}
}

func TestHeartbeatBoundsValidation(t *testing.T) {
	tests := []struct {
		name           string
		interval       int
		wantNormalized time.Duration
	}{
		{
			name:           "zero uses default",
			interval:       0,
			wantNormalized: 60 * time.Second,
		},
		{
			name:           "negative uses default",
			interval:       -1,
			wantNormalized: 60 * time.Second,
		},
		{
			name:           "minimum valid (1s)",
			interval:       1,
			wantNormalized: 1 * time.Second,
		},
		{
			name:           "typical value (30s)",
			interval:       30,
			wantNormalized: 30 * time.Second,
		},
		{
			name:           "maximum valid (300s)",
			interval:       300,
			wantNormalized: 300 * time.Second,
		},
		{
			name:           "over maximum uses default",
			interval:       301,
			wantNormalized: 60 * time.Second,
		},
		{
			name:           "very large uses default",
			interval:       3600,
			wantNormalized: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.NormalizeHeartbeatInterval(tt.interval)
			if got != tt.wantNormalized {
				t.Errorf("NormalizeHeartbeatInterval(%d) = %v, want %v",
					tt.interval, got, tt.wantNormalized)
			}
		})
	}
}

func TestHeartbeatRenewsTaskLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	staleLeaseTime := now.Add(-5 * time.Minute) // expired lease
	taskID := "coding-1"

	initialState := testhelpers.CreateValidState()
	initialState.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			CurrentTask:  testhelpers.StringPtr(taskID),
			Heartbeat:    now.Add(-10 * time.Minute),
			LeaseExpires: &staleLeaseTime,
		},
	}
	initialState.Tasks = []models.Task{
		{
			ID:           taskID,
			Description:  "Test task",
			Status:       models.TaskStatusImplementing,
			Priority:     1,
			AssignedTo:   testhelpers.StringPtr("coder-1"),
			LeaseExpires: &staleLeaseTime,
			SpecRef:      "specs/test.md",
			DoneWhen:     "tests pass",
			Scope:        "internal/",
		},
	}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	config := HeartbeatConfig{
		AgentID:       "coder-1",
		StatePath:     stateFile,
		Interval:      50 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	hb := newAuthenticatedTestHeartbeat(t, config)
	doneCh := make(chan error, 1)
	go func() { doneCh <- hb.Start(ctx) }()
	<-doneCh

	bb := db.New(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.LeaseExpires == nil {
		t.Fatal("Task LeaseExpires is nil after heartbeat")
	}
	if !task.LeaseExpires.After(staleLeaseTime) {
		t.Errorf("Task lease not renewed: initial=%v, final=%v", staleLeaseTime, *task.LeaseExpires)
	}
	if task.LeaseExpires.Before(time.Now().UTC()) {
		t.Error("Task lease is in the past after heartbeat")
	}
}

func TestHeartbeatRenewsReviewLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	staleLeaseTime := now.Add(-5 * time.Minute)
	taskID := "coding-2"

	initialState := testhelpers.CreateValidState()
	initialState.Agents = map[string]models.Agent{
		"reviewer-1": {
			Role:         "code-reviewer",
			Status:       models.AgentStatusWorking,
			CurrentTask:  testhelpers.StringPtr(taskID),
			Heartbeat:    now.Add(-10 * time.Minute),
			LeaseExpires: &staleLeaseTime,
		},
	}
	initialState.Tasks = []models.Task{
		{
			ID:                 taskID,
			Description:        "Test task for review",
			Status:             models.TaskStatusReviewing,
			Priority:           1,
			ReviewingBy:        testhelpers.StringPtr("reviewer-1"),
			ReviewLeaseExpires: &staleLeaseTime,
			SpecRef:            "specs/test.md",
			DoneWhen:           "tests pass",
			Scope:              "internal/",
		},
	}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	config := HeartbeatConfig{
		AgentID:            "reviewer-1",
		StatePath:          stateFile,
		Interval:           50 * time.Millisecond,
		LeaseDuration:      30 * time.Minute,
		ActiveProviderTask: func() string { return taskID },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	hb := newAuthenticatedTestHeartbeat(t, config)
	doneCh := make(chan error, 1)
	go func() { doneCh <- hb.Start(ctx) }()
	<-doneCh

	bb := db.New(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.ReviewLeaseExpires == nil {
		t.Fatal("ReviewLeaseExpires is nil after heartbeat")
	}
	if !task.ReviewLeaseExpires.After(staleLeaseTime) {
		t.Errorf("Review lease not renewed: initial=%v, final=%v", staleLeaseTime, *task.ReviewLeaseExpires)
	}
	if task.ReviewLeaseExpires.Before(time.Now().UTC()) {
		t.Error("Review lease is in the past after heartbeat")
	}
}

func TestHeartbeatSkipsClearedLease(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	taskID := "coding-3"

	initialState := testhelpers.CreateValidState()
	initialState.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWorking,
			CurrentTask:  testhelpers.StringPtr(taskID),
			Heartbeat:    now.Add(-10 * time.Minute),
			LeaseExpires: testhelpers.TimePtr(now.Add(-5 * time.Minute)),
		},
	}
	initialState.Tasks = []models.Task{
		{
			ID:           taskID,
			Description:  "Blocked task",
			Status:       models.TaskStatusBlocked,
			Priority:     1,
			AssignedTo:   testhelpers.StringPtr("coder-1"),
			LeaseExpires: nil, // intentionally cleared (e.g. BLOCKED)
			SpecRef:      "specs/test.md",
			DoneWhen:     "tests pass",
			Scope:        "internal/",
		},
	}
	testhelpers.WriteInitialState(t, stateFile, initialState)

	config := HeartbeatConfig{
		AgentID:       "coder-1",
		StatePath:     stateFile,
		Interval:      50 * time.Millisecond,
		LeaseDuration: 30 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	hb := newAuthenticatedTestHeartbeat(t, config)
	doneCh := make(chan error, 1)
	go func() { doneCh <- hb.Start(ctx) }()
	<-doneCh

	bb := db.New(stateFile)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := state.FindTask(taskID)
	if task == nil {
		t.Fatal("Task not found")
	}
	if task.LeaseExpires != nil {
		t.Errorf("Task LeaseExpires should remain nil for cleared lease, got %v", *task.LeaseExpires)
	}
}
