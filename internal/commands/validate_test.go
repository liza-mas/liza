package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateCommand_RequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		setupState  func() *models.State
		wantErr     bool
		errContains string
	}{
		{
			name: "valid complete state",
			setupState: func() *models.State {
				return testhelpers.CreateValidState()
			},
			wantErr: false,
		},
		{
			name: "missing version",
			setupState: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Version = 0
				return state
			},
			wantErr:     true,
			errContains: "version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := tt.setupState()
			testhelpers.WriteInitialState(t, statePath, state)

			// Skip spec file checks for most tests
			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommandWithOptions_FailsOnZombieAgent(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	findZombieAgents = func(opts procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		if opts.GoalID != "goal-1" {
			t.Fatalf("GoalID = %q, want goal-1", opts.GoalID)
		}
		return procscan.ZombieScanResult{Zombies: []procscan.ZombieProcess{{
			PID:    222,
			Role:   "coder",
			Reason: "not_registered_in_state",
		}}}, nil
	}

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Goal.ID = "goal-1"
	state.Sprint.GoalRef = "goal-1"
	testhelpers.WriteInitialState(t, statePath, state)

	err := ValidateCommandWithOptions(statePath, ValidateOptions{SkipSpecFileCheck: true})
	if err == nil {
		t.Fatal("ValidateCommandWithOptions() error = nil, want zombie validation failure")
	}
	testhelpers.AssertErrorContains(t, err, "zombie "+brand.BinaryName+" agent process detected")
	testhelpers.AssertErrorContains(t, err, "pid 222 role coder")
	testhelpers.AssertErrorContains(t, err, brand.BinaryName+" get agents --zombies")
}

func TestValidateCommandWithOptions_RecentlySpawnedPIDDoesNotSuppressOtherZombies(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		return procscan.ZombieScanResult{Zombies: []procscan.ZombieProcess{
			{PID: 222, Role: "coder", Reason: "not_registered_in_state"},
			{PID: 333, Role: "reviewer", Reason: "not_registered_in_state"},
		}}, nil
	}

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck:        true,
		RecentlySpawnedAgentPIDs: []int{222},
	})
	if err == nil {
		t.Fatal("ValidateCommandWithOptions() error = nil, want non-recent zombie validation failure")
	}
	testhelpers.AssertErrorContains(t, err, "pid 333 role reviewer")
	if strings.Contains(err.Error(), "pid 222 role coder") {
		t.Fatalf("recently spawned PID appeared in validation error: %v", err)
	}
}

func TestValidateCommandWithOptions_UnknownScopeWarnsWithoutFailing(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		return procscan.ZombieScanResult{UnknownScope: []procscan.AgentProcess{{
			PID:    222,
			Role:   "coder",
			Reason: procscan.ScopeReasonCWDUnreadable,
		}}}, nil
	}

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

	var warnings bytes.Buffer
	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck: true,
		WarnWriter:        &warnings,
	})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() error = %v, want nil", err)
	}
	for _, want := range []string{"process scan partial", "pid 222 role coder", "cwd_unreadable", "not classified as zombies"} {
		if !strings.Contains(warnings.String(), want) {
			t.Fatalf("warning = %q, want %q", warnings.String(), want)
		}
	}
}

func TestWriteUnknownScopeWarningUsesConfiguredBrand(t *testing.T) {
	originalBinaryName := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = originalBinaryName })

	var warnings bytes.Buffer
	writeUnknownScopeWarning(&warnings, []procscan.AgentProcess{{
		PID:    222,
		Role:   "coder",
		Reason: procscan.ScopeReasonCWDUnreadable,
	}})

	if !strings.Contains(warnings.String(), "Live acme agent process scan partial") {
		t.Fatalf("warning = %q, want configured binary name", warnings.String())
	}
}

func TestValidateCommandWithOptions_RecentlySpawnedPIDSuppressesUnknownScopeWarning(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		return procscan.ZombieScanResult{UnknownScope: []procscan.AgentProcess{{
			PID:    222,
			Role:   "coder",
			Reason: procscan.ScopeReasonCWDUnreadable,
		}}}, nil
	}

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

	var warnings bytes.Buffer
	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck:        true,
		WarnWriter:               &warnings,
		RecentlySpawnedAgentPIDs: []int{222},
	})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() error = %v, want nil", err)
	}
	if warnings.Len() != 0 {
		t.Fatalf("warning = %q, want none for recently spawned process", warnings.String())
	}
}

func TestValidateCommandWithOptions_SkipProcessChecks(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	called := false
	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		called = true
		return procscan.ZombieScanResult{Zombies: []procscan.ZombieProcess{{PID: 222, Role: "coder"}}}, nil
	}

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck: true,
		SkipProcessChecks: true,
	})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() error = %v, want nil", err)
	}
	if called {
		t.Fatal("process scanner called despite SkipProcessChecks")
	}
}

func TestValidateCommandWithOptions_ProcessScanUnavailableWarns(t *testing.T) {
	originalFind := findZombieAgents
	t.Cleanup(func() { findZombieAgents = originalFind })

	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		return procscan.ZombieScanResult{}, procscan.ErrProcessScanUnavailable
	}

	var warnBuf bytes.Buffer
	SetWarnWriter(&warnBuf)
	t.Cleanup(func() { SetWarnWriter(os.Stderr) })

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

	err := ValidateCommandWithOptions(statePath, ValidateOptions{SkipSpecFileCheck: true})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() error = %v, want nil warning", err)
	}
	if !strings.Contains(warnBuf.String(), "process scan skipped") {
		t.Fatalf("warning = %q, want process scan skipped", warnBuf.String())
	}
}

func TestValidateCommandWithOptions_RepairInvalidReviewOwnership(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		status     models.TaskStatus
		wantStatus models.TaskStatus
	}{
		{
			name:       "first review reverts to submitted",
			status:     models.TaskStatusReviewing,
			wantStatus: models.TaskStatusReadyForReview,
		},
		{
			name:       "second review reverts to partially approved",
			status:     models.TaskStatus("REVIEWING_CODE_2"),
			wantStatus: models.TaskStatus("CODE_PARTIALLY_APPROVED"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
			task.Status = tt.status

			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task}
			state.Agents = map[string]models.Agent{
				"code-reviewer-1": {
					Role:         "code-reviewer",
					Status:       models.AgentStatusIdle,
					LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
					Heartbeat:    now,
					Provider:     "anthropic",
					PID:          os.Getpid(),
				},
			}
			bb := testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommandWithOptions(statePath, ValidateOptions{
				SkipSpecFileCheck: true,
				SkipProcessChecks: true,
			})
			if err == nil {
				t.Fatal("ValidateCommandWithOptions() error = nil, want invalid active review ownership")
			}
			testhelpers.AssertErrorContains(t, err, "agent status IDLE, want REVIEWING")

			var warnBuf bytes.Buffer
			warnWriter = &warnBuf
			t.Cleanup(func() { warnWriter = os.Stderr })

			err = ValidateCommandWithOptions(statePath, ValidateOptions{
				SkipSpecFileCheck: true,
				SkipProcessChecks: true,
				Repair:            true,
			})
			if err != nil {
				t.Fatalf("ValidateCommandWithOptions() repair error = %v", err)
			}
			if !strings.Contains(warnBuf.String(), "REPAIRED: invalid active review ownership cleared for 1 task(s)") {
				t.Fatalf("repair warning = %q, want repair count", warnBuf.String())
			}

			repaired, err := bb.Read()
			if err != nil {
				t.Fatalf("read repaired state: %v", err)
			}
			gotTask := repaired.FindTask("task-1")
			if gotTask == nil {
				t.Fatal("task-1 missing after repair")
			}
			if gotTask.Status != tt.wantStatus {
				t.Fatalf("task status = %s, want %s", gotTask.Status, tt.wantStatus)
			}
			if gotTask.ReviewingBy != nil {
				t.Fatalf("reviewing_by = %q, want nil", *gotTask.ReviewingBy)
			}
			if gotTask.ReviewLeaseExpires != nil {
				t.Fatalf("review_lease_expires = %s, want nil", gotTask.ReviewLeaseExpires.Format(time.RFC3339))
			}
		})
	}
}

func TestValidateCommandWithOptions_RepairInvalidDoerOwnershipDeadOrMissingPID(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name string
		pid  int
	}{
		{name: "missing pid", pid: 0},
		{name: "dead pid with future lease", pid: deadPIDForTest(t)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			state := invalidDoerOwnershipState(t, tmpDir, now)
			agent := state.Agents["coder-1"]
			agent.PID = tt.pid
			state.Agents["coder-1"] = agent
			bb := testhelpers.WriteInitialState(t, statePath, state)

			var warnBuf bytes.Buffer
			SetWarnWriter(&warnBuf)
			t.Cleanup(func() { SetWarnWriter(os.Stderr) })

			err := ValidateCommandWithOptions(statePath, ValidateOptions{
				SkipSpecFileCheck: true,
				Repair:            true,
			})
			if err != nil {
				t.Fatalf("ValidateCommandWithOptions() repair error = %v", err)
			}
			if !strings.Contains(warnBuf.String(), "REPAIRED: invalid active doer ownership cleared for 1 task(s)") {
				t.Fatalf("repair warning = %q, want doer repair count", warnBuf.String())
			}

			repaired, err := bb.Read()
			if err != nil {
				t.Fatalf("read repaired state: %v", err)
			}
			task := repaired.FindTask("task-1")
			if task == nil {
				t.Fatal("task-1 missing after repair")
			}
			if task.Status != models.TaskStatusReady {
				t.Fatalf("task status = %s, want READY", task.Status)
			}
			if task.AssignedTo != nil || task.LeaseExpires != nil || task.Worktree != nil || task.BaseCommit != nil {
				t.Fatalf("doer claim fields not cleared: assigned=%v lease=%v worktree=%v base=%v", task.AssignedTo, task.LeaseExpires, task.Worktree, task.BaseCommit)
			}
			if task.Iteration != 0 {
				t.Fatalf("task iteration = %d, want 0", task.Iteration)
			}
		})
	}
}

func TestValidateCommandWithOptions_RepairInvalidDoerOwnershipSkipsSentinel(t *testing.T) {
	now := time.Now().UTC()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := invalidDoerOwnershipState(t, tmpDir, now)
	sentinel := "$transitioning"
	task := state.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 missing")
	}
	task.AssignedTo = &sentinel
	task.Iteration = 4
	delete(state.Agents, "coder-1")
	bb := testhelpers.WriteInitialState(t, statePath, state)

	var warnBuf bytes.Buffer
	SetWarnWriter(&warnBuf)
	t.Cleanup(func() { SetWarnWriter(os.Stderr) })

	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck: true,
		Repair:            true,
	})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() repair error = %v", err)
	}
	if strings.Contains(warnBuf.String(), "invalid active doer ownership cleared") {
		t.Fatalf("repair warning = %q, want no doer repair", warnBuf.String())
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state after repair: %v", readErr)
	}
	readTask := readState.FindTask("task-1")
	if readTask == nil {
		t.Fatal("task-1 missing after repair")
	}
	if readTask.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want IMPLEMENTING_CODE", readTask.Status)
	}
	if readTask.AssignedTo == nil || *readTask.AssignedTo != sentinel {
		t.Fatalf("assigned_to = %v, want sentinel %s", readTask.AssignedTo, sentinel)
	}
	if readTask.Worktree == nil || *readTask.Worktree == "" || readTask.BaseCommit == nil || *readTask.BaseCommit == "" || readTask.LeaseExpires == nil {
		t.Fatalf("sentinel task claim fields were cleared: worktree=%v base=%v lease=%v", readTask.Worktree, readTask.BaseCommit, readTask.LeaseExpires)
	}
	if readTask.Iteration != 4 {
		t.Fatalf("iteration = %d, want 4", readTask.Iteration)
	}
}

func TestValidateCommandWithOptions_RepairInvalidDoerOwnershipRepairsDeadAndReportsLive(t *testing.T) {
	now := time.Now().UTC()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := invalidDoerOwnershipState(t, tmpDir, now)
	deadAgent := state.Agents["coder-1"]
	deadAgent.PID = deadPIDForTest(t)
	state.Agents["coder-1"] = deadAgent

	liveDoerID := "coder-2"
	liveTask := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
	liveTask.AssignedTo = testhelpers.StringPtr(liveDoerID)
	liveTask.Iteration = 2
	if liveTask.Worktree == nil {
		t.Fatal("live test task missing worktree")
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, *liveTask.Worktree), 0o755); err != nil {
		t.Fatalf("create live task worktree: %v", err)
	}
	state.Tasks = append(state.Tasks, liveTask)
	state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, liveTask.ID)
	liveAgent := deadAgent
	liveAgent.PID = os.Getpid()
	liveAgent.CurrentTask = testhelpers.StringPtr(liveTask.ID)
	state.Agents[liveDoerID] = liveAgent

	bb := testhelpers.WriteInitialState(t, statePath, state)

	var warnBuf bytes.Buffer
	SetWarnWriter(&warnBuf)
	t.Cleanup(func() { SetWarnWriter(os.Stderr) })

	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck: true,
		Repair:            true,
	})
	if err == nil {
		t.Fatal("ValidateCommandWithOptions() repair error = nil, want validation failure after live PID refusal")
	}
	testhelpers.AssertErrorContains(t, err, "task-2")
	testhelpers.AssertErrorContains(t, err, "has agent status WAITING")
	if !strings.Contains(warnBuf.String(), "REPAIRED: invalid active doer ownership cleared for 1 task(s)") {
		t.Fatalf("repair warning = %q, want dead task repair count", warnBuf.String())
	}
	if !strings.Contains(warnBuf.String(), "assigned agent coder-2 has live PID") {
		t.Fatalf("repair warning = %q, want live PID refusal", warnBuf.String())
	}

	repaired, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read repaired state: %v", readErr)
	}
	deadTask := repaired.FindTask("task-1")
	if deadTask == nil {
		t.Fatal("task-1 missing after repair")
	}
	if deadTask.Status != models.TaskStatusReady || deadTask.AssignedTo != nil {
		t.Fatalf("dead task repair = status %s assigned %v, want READY/unassigned", deadTask.Status, deadTask.AssignedTo)
	}
	refusedTask := repaired.FindTask("task-2")
	if refusedTask == nil {
		t.Fatal("task-2 missing after refused repair")
	}
	if refusedTask.Status != models.TaskStatusImplementing || refusedTask.AssignedTo == nil || *refusedTask.AssignedTo != liveDoerID {
		t.Fatalf("live task changed despite refusal: status=%s assigned=%v", refusedTask.Status, refusedTask.AssignedTo)
	}
}

func TestValidateCommandWithOptions_RepairInvalidDoerOwnershipLivePIDRefuses(t *testing.T) {
	now := time.Now().UTC()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := invalidDoerOwnershipState(t, tmpDir, now)
	agent := state.Agents["coder-1"]
	agent.PID = os.Getpid()
	state.Agents["coder-1"] = agent
	bb := testhelpers.WriteInitialState(t, statePath, state)

	var warnBuf bytes.Buffer
	SetWarnWriter(&warnBuf)
	t.Cleanup(func() { SetWarnWriter(os.Stderr) })

	err := ValidateCommandWithOptions(statePath, ValidateOptions{
		SkipSpecFileCheck: true,
		Repair:            true,
	})
	if err == nil {
		t.Fatal("ValidateCommandWithOptions() repair error = nil, want validation failure after live PID refusal")
	}
	testhelpers.AssertErrorContains(t, err, "has agent status WAITING")
	if !strings.Contains(warnBuf.String(), "assigned agent coder-1 has live PID") {
		t.Fatalf("repair warning = %q, want live PID refusal", warnBuf.String())
	}
	if !strings.Contains(warnBuf.String(), "use "+brand.BinaryName+" recover-agent coder-1 --force or "+brand.BinaryName+" release-claim task-1 --role doer --force") {
		t.Fatalf("repair warning = %q, want recovery commands", warnBuf.String())
	}

	readState, readErr := bb.Read()
	if readErr != nil {
		t.Fatalf("read state after refused repair: %v", readErr)
	}
	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 missing after refused repair")
	}
	if task.Status != models.TaskStatusImplementing || task.AssignedTo == nil || *task.AssignedTo != "coder-1" {
		t.Fatalf("task ownership changed despite live PID refusal: status=%s assigned=%v", task.Status, task.AssignedTo)
	}
}

func deadPIDForTest(t *testing.T) int {
	t.Helper()

	for pid := os.Getpid() + 100000; pid < os.Getpid()+101000; pid++ {
		if !ops.IsProcessAlive(pid) {
			return pid
		}
	}
	t.Fatal("could not find a dead PID for test")
	return 0
}

func TestValidateCommandWithOptions_RepairUsesExactStatePath(t *testing.T) {
	now := time.Now().UTC()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	alternateStatePath := filepath.Join(filepath.Dir(statePath), "state-copy.yaml")

	canonicalState := invalidReviewOwnershipState(now)
	canonicalBB := testhelpers.WriteInitialState(t, statePath, canonicalState)

	alternateState := invalidReviewOwnershipState(now)
	alternateBB := testhelpers.WriteInitialState(t, alternateStatePath, alternateState)

	var warnBuf bytes.Buffer
	warnWriter = &warnBuf
	t.Cleanup(func() { warnWriter = os.Stderr })

	err := ValidateCommandWithOptions(alternateStatePath, ValidateOptions{
		SkipSpecFileCheck: true,
		SkipProcessChecks: true,
		Repair:            true,
	})
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() repair error = %v", err)
	}

	repairedAlternate, err := alternateBB.Read()
	if err != nil {
		t.Fatalf("read alternate state: %v", err)
	}
	alternateTask := repairedAlternate.FindTask("task-1")
	if alternateTask == nil {
		t.Fatal("alternate task-1 missing")
	}
	if alternateTask.ReviewingBy != nil {
		t.Fatalf("alternate reviewing_by = %q, want nil", *alternateTask.ReviewingBy)
	}

	canonicalRead, err := canonicalBB.Read()
	if err != nil {
		t.Fatalf("read canonical state: %v", err)
	}
	canonicalTask := canonicalRead.FindTask("task-1")
	if canonicalTask == nil {
		t.Fatal("canonical task-1 missing")
	}
	if canonicalTask.ReviewingBy == nil {
		t.Fatal("canonical reviewing_by was cleared; repair mutated the wrong state file")
	}
}

func invalidReviewOwnershipState(now time.Time) *models.State {
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReviewing, now)
	state.Tasks = []models.Task{task}
	state.Agents = map[string]models.Agent{
		"code-reviewer-1": {
			Role:         "code-reviewer",
			Status:       models.AgentStatusIdle,
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
			Heartbeat:    now,
			Provider:     "anthropic",
			PID:          os.Getpid(),
		},
	}
	return state
}

func invalidDoerOwnershipState(t *testing.T, projectRoot string, now time.Time) *models.State {
	t.Helper()

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.Iteration = 3
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{task.ID}
	if task.Worktree == nil {
		t.Fatal("test task missing worktree")
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, *task.Worktree), 0o755); err != nil {
		t.Fatalf("create task worktree: %v", err)
	}

	state.Agents = map[string]models.Agent{
		"coder-1": {
			Role:         "coder",
			Status:       models.AgentStatusWaiting,
			CurrentTask:  testhelpers.StringPtr(task.ID),
			LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
			Heartbeat:    now,
			Provider:     "anthropic",
		},
	}
	return state
}

func TestValidateCommand_TaskStateInvariants(t *testing.T) {
	tests := []struct {
		name        string
		setupTask   func() models.Task
		wantErr     bool
		errContains string
	}{
		{
			name: "DRAFT_CODE with assigned_to",
			setupTask: func() models.Task {
				agent := "coder-1"
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusReady,
					RolePair:    "coding-pair",
					AssignedTo:  &agent,
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "DRAFT_CODE task with assigned_to",
		},
		{
			name: "IMPLEMENTING without assigned_to",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusImplementing,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "IMPLEMENTING_CODE task without assigned_to",
		},
		{
			name: "IMPLEMENTING without worktree",
			setupTask: func() models.Task {
				agent := "coder-1"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				baseCommit := "abc123"
				return models.Task{
					ID:           "task-1",
					Description:  "Test",
					Status:       models.TaskStatusImplementing,
					RolePair:     "coding-pair",
					AssignedTo:   &agent,
					LeaseExpires: &leaseExpires,
					BaseCommit:   &baseCommit,
					Created:      time.Now().UTC(),
					SpecRef:      "specs/test.md",
					DoneWhen:     "Complete",
					History:      []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "IMPLEMENTING_CODE task without worktree",
		},
		{
			name: "TO_REVIEW without review_commit",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusReadyForReview,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "CODE_TO_REVIEW task without review_commit",
		},
		{
			name: "APPROVED without review_commit",
			setupTask: func() models.Task {
				approvedBy := "code-reviewer-1"
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusApproved,
					RolePair:    "coding-pair",
					ApprovedBy:  &approvedBy,
					// ReviewCommit intentionally nil
					Created:  time.Now().UTC(),
					SpecRef:  "specs/test.md",
					DoneWhen: "Complete",
					History:  []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "APPROVED task without review_commit",
		},
		{
			name: "BLOCKED without blocked_reason",
			setupTask: func() models.Task {
				return models.Task{
					ID:               "task-1",
					Description:      "Test",
					Status:           models.TaskStatusBlocked,
					RolePair:         "coding-pair",
					Created:          time.Now().UTC(),
					SpecRef:          "specs/test.md",
					DoneWhen:         "Complete",
					BlockedQuestions: []string{"How to proceed?"},
					History:          []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "BLOCKED task without blocked_reason",
		},
		{
			name: "REJECTED without rejection_reason",
			setupTask: func() models.Task {
				return models.Task{
					ID:          "task-1",
					Description: "Test",
					Status:      models.TaskStatusRejected,
					RolePair:    "coding-pair",
					Created:     time.Now().UTC(),
					SpecRef:     "specs/test.md",
					DoneWhen:    "Complete",
					History:     []models.TaskHistoryEntry{},
				}
			},
			wantErr:     true,
			errContains: "REJECTED task without rejection_reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{tt.setupTask()}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true) // Skip spec file check
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_Dependencies(t *testing.T) {
	tests := []struct {
		name        string
		setupTasks  func() []models.Task
		wantErr     bool
		errContains string
	}{
		{
			name: "depends_on references non-existent task",
			setupTasks: func() []models.Task {
				return []models.Task{
					{
						ID:          "task-1",
						Description: "Test",
						Status:      models.TaskStatusReady,
						RolePair:    "coding-pair",
						DependsOn:   []string{"task-nonexistent"},
						Created:     time.Now().UTC(),
						SpecRef:     "specs/test.md",
						DoneWhen:    "Complete",
						History:     []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "non-existent task",
		},
		{
			name: "IMPLEMENTING task with unmet dependencies",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree := "wt-task-1"
				baseCommit := "abc123"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				return []models.Task{
					{
						ID:          "task-2",
						Description: "Dependency",
						Status:      models.TaskStatusReady, // Not MERGED
						RolePair:    "coding-pair",
						Created:     time.Now().UTC(),
						SpecRef:     "specs/test.md",
						DoneWhen:    "Complete",
						History:     []models.TaskHistoryEntry{},
					},
					{
						ID:           "task-1",
						Description:  "Test",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						DependsOn:    []string{"task-2"},
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "unmet dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.setupTasks()

			// Create worktree directories if tasks have them
			for _, task := range state.Tasks {
				if task.Worktree != nil {
					wtPath := filepath.Join(tmpDir, *task.Worktree)
					if err := os.MkdirAll(wtPath, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_AgentInvariants(t *testing.T) {
	tests := []struct {
		name        string
		setupAgent  func() map[string]models.Agent
		wantErr     bool
		errContains string
	}{
		{
			name: "WORKING agent without current_task",
			setupAgent: func() map[string]models.Agent {
				return map[string]models.Agent{
					"coder-1": {
						Role:      "coder",
						Status:    models.AgentStatusWorking,
						Heartbeat: time.Now().UTC(),
						Terminal:  "term-1",
					},
				}
			},
			wantErr:     true,
			errContains: "WORKING but no current_task",
		},
		{
			name: "WORKING agent with empty current_task",
			setupAgent: func() map[string]models.Agent {
				return map[string]models.Agent{
					"coder-1": {
						Role:        "coder",
						Status:      models.AgentStatusWorking,
						CurrentTask: testhelpers.StringPtr(""),
						Heartbeat:   time.Now().UTC(),
						Terminal:    "term-1",
					},
				}
			},
			wantErr:     true,
			errContains: "WORKING but no current_task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Agents = tt.setupAgent()

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateAgentInvariants_LeaseExpiryGracePeriod(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		leaseExpiry time.Time
		wantWarning bool
	}{
		{
			name:        "within grace period",
			leaseExpiry: now.Add(-(models.LeaseExpiryGracePeriod - 30*time.Second)),
			wantWarning: false,
		},
		{
			// Lease expired exactly at the grace boundary should not warn.
			// Before() is strict <, so equal-to-deadline is not "before" it.
			// 100ms buffer accounts for wall-clock drift between test and function
			// (1ms was insufficient on CI under load).
			name:        "exactly at grace period boundary",
			leaseExpiry: now.Add(-models.LeaseExpiryGracePeriod + 100*time.Millisecond),
			wantWarning: false,
		},
		{
			name:        "past grace period",
			leaseExpiry: now.Add(-(models.LeaseExpiryGracePeriod + 30*time.Second)),
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentTask := "task-1"
			state := &models.State{
				Agents: map[string]models.Agent{
					"coder-1": {
						Role:         "coder",
						Status:       models.AgentStatusWorking,
						CurrentTask:  &currentTask,
						LeaseExpires: &tt.leaseExpiry,
						Heartbeat:    now,
						Terminal:     "term-1",
					},
				},
			}

			var buf bytes.Buffer
			warnWriter = &buf
			defer func() { warnWriter = os.Stderr }()

			validateErr := validateAgentInvariants(state, "", true)
			if validateErr != nil {
				t.Fatalf("validateAgentInvariants() error = %v", validateErr)
			}

			hasWarning := strings.Contains(buf.String(), "lease expired")
			if hasWarning != tt.wantWarning {
				t.Errorf("warning present = %v, want %v; output=%q", hasWarning, tt.wantWarning, buf.String())
			}
		})
	}
}

func TestValidateCommand_DuplicateAssignments(t *testing.T) {
	tests := []struct {
		name        string
		setupTasks  func() []models.Task
		setupState  func(*models.State)
		wantErr     bool
		errContains string
	}{
		{
			name: "agent with multiple IMPLEMENTING tasks fails",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree1 := "wt-task-1"
				worktree2 := "wt-task-2"
				baseCommit := "abc123"
				leaseExpires := time.Now().UTC().Add(30 * time.Minute)
				return []models.Task{
					{
						ID:           "task-1",
						Description:  "Test 1",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree1,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
					{
						ID:           "task-2",
						Description:  "Test 2",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree2,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			wantErr:     true,
			errContains: "assigned to multiple active tasks",
		},
		{
			name: "agent with REJECTED and IMPLEMENTING tasks passes",
			setupTasks: func() []models.Task {
				agent := "coder-1"
				worktree := "wt-task-2"
				baseCommit := "abc123"
				now := time.Now().UTC()
				leaseExpires := now.Add(30 * time.Minute)
				rejectionReason := "Not good enough"
				return []models.Task{
					{
						ID:              "task-1",
						Description:     "Rejected task",
						Status:          models.TaskStatusRejected,
						RolePair:        "coding-pair",
						AssignedTo:      &agent,
						LeaseExpires:    &leaseExpires,
						RejectionReason: &rejectionReason,
						Created:         now,
						SpecRef:         "specs/test.md",
						DoneWhen:        "Complete",
						History:         []models.TaskHistoryEntry{},
						HandoffEvents: []models.HandoffEvent{
							{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
						},
					},
					{
						ID:           "task-2",
						Description:  "Active task",
						Status:       models.TaskStatusImplementing,
						RolePair:     "coding-pair",
						AssignedTo:   &agent,
						Worktree:     &worktree,
						BaseCommit:   &baseCommit,
						LeaseExpires: &leaseExpires,
						Created:      time.Now().UTC(),
						SpecRef:      "specs/test.md",
						DoneWhen:     "Complete",
						History:      []models.TaskHistoryEntry{},
					},
				}
			},
			setupState: func(state *models.State) {
				now := time.Now().UTC()
				state.Agents["coder-1"] = models.Agent{
					Role:         models.RoleCoder,
					Status:       models.AgentStatusWorking,
					CurrentTask:  testhelpers.StringPtr("task-2"),
					LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
					Heartbeat:    now,
					Terminal:     "test",
					Provider:     "test",
					PID:          os.Getpid(),
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)

			state := testhelpers.CreateValidState()
			state.Tasks = tt.setupTasks()
			if tt.setupState != nil {
				tt.setupState(state)
			}

			// Create worktree directories if tasks have them
			for _, task := range state.Tasks {
				if task.Worktree != nil {
					wtPath := filepath.Join(tmpDir, *task.Worktree)
					if err := os.MkdirAll(wtPath, 0755); err != nil {
						t.Fatal(err)
					}
				}
			}

			testhelpers.WriteInitialState(t, statePath, state)

			err := ValidateCommand(statePath, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				testhelpers.AssertErrorContains(t, err, tt.errContains)
			}
		})
	}
}

func TestValidateCommand_SpecFileValidation(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	// Create a spec file
	specFile := testhelpers.CreateSpecFile(t, tmpDir, "test.md", "# Test Spec\n")

	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "specs/test.md"

	testhelpers.WriteInitialState(t, statePath, state)

	// Should pass with spec file check
	if err := ValidateCommand(statePath, false); err != nil {
		t.Errorf("ValidateCommand() with existing spec file error = %v", err)
	}

	// Remove spec file
	os.Remove(specFile)

	// Should fail without skip flag
	if err := ValidateCommand(statePath, false); err == nil {
		t.Error("ValidateCommand() should fail for missing spec file")
	}

	// Should pass with skip flag
	if err := ValidateCommand(statePath, true); err != nil {
		t.Errorf("ValidateCommand() with skip spec check error = %v", err)
	}
}

func TestValidateAnomalies_RequiredDetailsByType(t *testing.T) {
	tests := []struct {
		name        string
		anomaly     models.Anomaly
		errContains string
	}{
		{
			name: "retry_loop missing required details fails",
			anomaly: models.Anomaly{
				Type:    "retry_loop",
				Details: map[string]any{"count": 3},
			},
			errContains: "retry_loop anomaly",
		},
		{
			name: "trade_off missing required details fails",
			anomaly: models.Anomaly{
				Type:    "trade_off",
				Details: map[string]any{"what": "faster claim path", "why": "reduce lock contention"},
			},
			errContains: "trade_off anomaly",
		},
		{
			name: "external_blocker missing required details fails",
			anomaly: models.Anomaly{
				Type:    "external_blocker",
				Details: map[string]any{"note": "service unavailable"},
			},
			errContains: "external_blocker anomaly",
		},
		{
			name: "assumption_violated missing required details fails",
			anomaly: models.Anomaly{
				Type:    "assumption_violated",
				Details: map[string]any{"assumption": "state file always present"},
			},
			errContains: "assumption_violated anomaly",
		},
		{
			name: "provider_audit_degraded missing required details fails",
			anomaly: models.Anomaly{
				Type:    "provider_audit_degraded",
				Details: map[string]any{"provider": "codex", "agent_id": "coder-1"},
			},
			errContains: "provider_audit_degraded anomaly",
		},
		{
			name: "agent_degraded missing required details fails",
			anomaly: models.Anomaly{
				Type:    "agent_degraded",
				Details: map[string]any{"agent_id": "coder-1", "role": "coder", "reason": "claim_worktree_create_failed"},
			},
			errContains: "agent_degraded anomaly",
		},
		{
			name: "stale_verdict missing required details fails",
			anomaly: models.Anomaly{
				Type:    "stale_verdict",
				Details: map[string]any{"attempted_verdict": "REJECTED"},
			},
			errContains: "stale_verdict anomaly",
		},
		{
			name: "submit_verdict_failed missing required details fails",
			anomaly: models.Anomaly{
				Type:    "submit_verdict_failed",
				Details: map[string]any{"verdict": "REJECTED"},
			},
			errContains: "submit_verdict_failed anomaly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{tt.anomaly}

			err := validateAnomalies(state, t.TempDir(), true)
			if err == nil {
				t.Fatalf("validateAnomalies() error = nil, want error containing %q", tt.errContains)
			}
			testhelpers.AssertErrorContains(t, err, tt.errContains)
		})
	}
}

func TestValidateAnomalies_RequestedTypeBranchesPassWithValidDetails(t *testing.T) {
	tests := []struct {
		name    string
		anomaly models.Anomaly
	}{
		{
			name: "retry_loop branch",
			anomaly: models.Anomaly{
				Type:    "retry_loop",
				Details: map[string]any{"count": 3, "error_pattern": "timeout"},
			},
		},
		{
			name: "trade_off branch",
			anomaly: models.Anomaly{
				Type: "trade_off",
				Details: map[string]any{
					"what":         "skip cache warmup",
					"why":          "reduce startup time",
					"debt_created": "slower first request",
				},
			},
		},
		{
			name: "spec_ambiguity branch",
			anomaly: models.Anomaly{
				Type:    "spec_ambiguity",
				Details: map[string]any{},
			},
		},
		{
			name: "external_blocker branch",
			anomaly: models.Anomaly{
				Type:    "external_blocker",
				Details: map[string]any{"blocker_service": "github"},
			},
		},
		{
			name: "assumption_violated branch",
			anomaly: models.Anomaly{
				Type:    "assumption_violated",
				Details: map[string]any{"assumption": "single reviewer", "reality": "reviewer unavailable"},
			},
		},
		{
			name: "provider_audit_degraded branch",
			anomaly: models.Anomaly{
				Type:    "provider_audit_degraded",
				Details: map[string]any{"provider": "codex", "agent_id": "coder-1", "message": "failed to record rollout items"},
			},
		},
		{
			name: "agent_degraded branch",
			anomaly: models.Anomaly{
				Type: "agent_degraded",
				Details: map[string]any{
					"agent_id":   "coder-1",
					"role":       "coder",
					"reason":     "claim_worktree_create_failed",
					"last_error": "cannot lock ref",
				},
			},
		},
		{
			name: "stale_verdict branch",
			anomaly: models.Anomaly{
				Type:    "stale_verdict",
				Details: map[string]any{"attempted_verdict": "REJECTED", "current_status": "IMPLEMENTING_CODE"},
			},
		},
		{
			name: "submit_verdict_failed branch",
			anomaly: models.Anomaly{
				Type:    "submit_verdict_failed",
				Details: map[string]any{"verdict": "REJECTED", "error": "failed to submit verdict: sentinel replaced"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{tt.anomaly}

			if err := validateAnomalies(state, t.TempDir(), true); err != nil {
				t.Fatalf("validateAnomalies() error = %v, want nil", err)
			}
		})
	}
}

func TestValidateStateRejectsRawProviderTranscriptMessage(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{
		{
			Type: "provider_audit_degraded",
			Details: map[string]any{
				"provider": "codex",
				"agent_id": "orchestrator-1",
				"message":  `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"raw output"}}`,
			},
		},
	}

	err := statevalidate.ValidateState(state, t.TempDir(), true, nil)
	if err == nil {
		t.Fatal("ValidateState() error = nil, want raw transcript rejection")
	}
	testhelpers.AssertErrorContains(t, err, "raw provider transcript payload")
}

func TestSetWarnWriter(t *testing.T) {
	// Save and restore original writer
	original := warnWriter
	defer func() { warnWriter = original }()

	var buf bytes.Buffer
	SetWarnWriter(&buf)

	if warnWriter != &buf {
		t.Fatal("SetWarnWriter did not update warnWriter")
	}

	// Restore to stderr
	SetWarnWriter(os.Stderr)
	if warnWriter != os.Stderr {
		t.Fatal("SetWarnWriter did not restore warnWriter to os.Stderr")
	}
}

func TestValidateCommandWithOptions_WarnsUnresolvedRefFragments(t *testing.T) {
	// GIVEN a strict carrier on integration and refs into it from live and pending work
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.MustGit(t, tmpDir, "checkout", "-q", "integration")
	revision := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	carrier := "# Epic\n\n## Source References\n\nSource revision: \"" + revision + "\"\n\n### Direct References\n\n- \"source\": \"README.md#Test\"\n\n### Obligation Coverage\n\n- \"AC-one\" -> \"source\"\n\n## Capability One\n\nFirst capability.\n"
	if err := os.MkdirAll(filepath.Join(tmpDir, "specs/epics"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "specs/epics/ep-1.md"), []byte(carrier), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, tmpDir, "add", "specs/epics/ep-1.md")
	testhelpers.MustGit(t, tmpDir, "commit", "-q", "-m", "Add epic")

	const slug = "specs/epics/ep-1.md#capability-one"
	const exact = "specs/epics/ep-1.md#Capability One"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"
	live := func(id, epicRef string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, now)
		task.SpecRef = "README.md"
		task.EpicRef = epicRef
		return task
	}
	merged := func(id, epicRef string, transitionsExecuted map[string]bool) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		task.SpecRef = "README.md"
		task.Output = []models.OutputEntry{{Desc: "Story", DoneWhen: "Done", Scope: "Scope", SpecRef: "README.md", EpicRef: epicRef}}
		task.TransitionsExecuted = transitionsExecuted
		return task
	}
	state.Tasks = []models.Task{
		live("live-slug", slug),
		live("live-exact", exact),
		merged("pending-slug", slug, nil),
		merged("consumed-slug", slug, map[string]bool{"epic-to-us": true}),
	}
	testhelpers.WriteInitialState(t, statePath, state)

	// WHEN the operator validates
	var warnings bytes.Buffer
	err := ValidateCommandWithOptions(statePath, ValidateOptions{SkipProcessChecks: true, WarnWriter: &warnings})

	// THEN validation passes and warns only for refs prompt context would fail on
	if err != nil {
		t.Fatalf("ValidateCommandWithOptions() error = %v, want nil", err)
	}
	got := warnings.String()
	for _, want := range []string{
		`task live-slug epic_ref "` + slug + `" does not resolve at integration HEAD: eligible ATX heading "capability-one" is missing`,
		`task pending-slug output[0].epic_ref "` + slug + `" does not resolve`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("warnings = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"live-exact", "consumed-slug"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("warnings = %q, must not mention %s", got, unwanted)
		}
	}

	// WHEN offline validation skips file checks
	warnings.Reset()
	if err := ValidateCommandWithOptions(statePath, ValidateOptions{SkipSpecFileCheck: true, SkipProcessChecks: true, WarnWriter: &warnings}); err != nil {
		t.Fatal(err)
	}
	// THEN fragments are not resolved either
	if strings.Contains(warnings.String(), "does not resolve") {
		t.Fatalf("offline validation resolved fragments: %q", warnings.String())
	}
}
