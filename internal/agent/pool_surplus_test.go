package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const surplusTestGeneration = "test-generation"

// liveSurplusAgent is a registration that occupies its role: unexpired lease,
// fresh heartbeat, and this process's PID.
func liveSurplusAgent(role string, now time.Time) models.Agent {
	lease := now.Add(30 * time.Minute)
	return models.Agent{
		Role:         role,
		Status:       models.AgentStatusIdle,
		Generation:   surplusTestGeneration,
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: &lease,
		Provider:     "claude",
		PID:          os.Getpid(),
	}
}

func surplusAuthority(agentID string) models.AgentAuthority {
	return models.AgentAuthority{ID: agentID, Generation: surplusTestGeneration}
}

// writeSurplusState persists a state with the given project cap, agents and
// tasks, and returns its blackboard.
func writeSurplusState(t *testing.T, maxInstances int, agents map[string]models.Agent, tasks []models.Task) *db.Blackboard {
	t.Helper()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.MaxInstances = maxInstances
	for id, agent := range agents {
		state.Agents[id] = agent
	}
	state.Tasks = tasks
	return testhelpers.WriteInitialState(t, statePath, state)
}

func threeCoders(now time.Time) map[string]models.Agent {
	return map[string]models.Agent{
		"coder-1": liveSurplusAgent("coder", now),
		"coder-2": liveSurplusAgent("coder", now),
		"coder-3": liveSurplusAgent("coder", now),
	}
}

func readSurplusState(t *testing.T, bb *db.Blackboard) *models.State {
	t.Helper()
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

// A coder that submitted normally keeps assigned_to on the submitted task with
// an empty current_task. That retained attribution must not pin capacity:
// with the cap lowered and other work claimable, the coder retires without
// claiming, and the submitted task keeps its status, worktree, review commit
// and history, gaining one claim_released entry exactly as an idle exit would.
func TestReleaseIfSurplusRetiresSubmittedDoerWithoutClaiming(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	submitted := testhelpers.BuildTaskByStatus("submitted", models.TaskStatusReadyForReview, now)
	submitted.History = []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventSubmittedForReview, Agent: submitted.AssignedTo}}
	next := testhelpers.BuildTaskByStatus("next", models.TaskStatusReady, now)
	agents := threeCoders(now)
	waiting := agents["coder-1"]
	waiting.Status = models.AgentStatusWaiting
	agents["coder-1"] = waiting
	bb := writeSurplusState(t, 2, agents, []models.Task{submitted, next})

	released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), testResolver(t))

	if err != nil || !released {
		t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (true, nil)", released, err)
	}
	state := readSurplusState(t, bb)
	if _, exists := state.Agents["coder-1"]; exists {
		t.Fatal("coder-1 is still registered after retiring")
	}
	if _, ok := state.Agents["coder-2"]; !ok {
		t.Fatal("coder-2 was removed")
	}
	if _, ok := state.Agents["coder-3"]; !ok {
		t.Fatal("coder-3 was removed")
	}
	got := state.FindTask("submitted")
	if got.Status != models.TaskStatusReadyForReview || got.AssignedTo != nil {
		t.Fatalf("submitted task status=%s assigned=%v, want %s and no assignee", got.Status, got.AssignedTo, models.TaskStatusReadyForReview)
	}
	if got.Worktree == nil || *got.Worktree != *submitted.Worktree || got.ReviewCommit == nil || *got.ReviewCommit != *submitted.ReviewCommit {
		t.Fatalf("submitted task lost its work: worktree=%v review_commit=%v", got.Worktree, got.ReviewCommit)
	}
	if len(got.History) != 2 || got.History[0].Event != models.TaskEventSubmittedForReview || got.History[1].Event != models.TaskEventClaimReleased {
		t.Fatalf("submitted task history = %+v, want the submission then one claim_released", got.History)
	}
	if claimable := state.FindTask("next"); claimable.Status != models.TaskStatusReady || claimable.AssignedTo != nil || len(claimable.History) != 0 {
		t.Fatalf("claimable task was touched: status=%s assigned=%v history=%d", claimable.Status, claimable.AssignedTo, len(claimable.History))
	}
}

// Attribution kept on a merged task does not pin pool capacity either.
func TestReleaseIfSurplusRetainedMergedAttributionDoesNotPin(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	merged := testhelpers.BuildTaskByStatus("merged", models.TaskStatusMerged, now)
	self := "coder-1"
	merged.AssignedTo = &self
	bb := writeSurplusState(t, 2, threeCoders(now), []models.Task{merged})

	released, err := ops.RetireSurplusAgent(bb, surplusAuthority(self), testResolver(t))

	if err != nil || !released {
		t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (true, nil)", released, err)
	}
	state := readSurplusState(t, bb)
	if _, exists := state.Agents[self]; exists {
		t.Fatal("coder-1 is still registered")
	}
	if got := state.FindTask("merged"); got.Status != models.TaskStatusMerged || got.MergeCommit == nil {
		t.Fatalf("merged task changed: status=%s merge_commit=%v", got.Status, got.MergeCommit)
	}
}

// In-flight ownership keeps a surplus agent in the pool with state untouched;
// an expired rejected lease is no longer reserved and does not.
func TestReleaseIfSurplusInFlightOwnershipPins(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	tests := []struct {
		name         string
		self         string
		build        func(agents map[string]models.Agent) []models.Task
		wantReleased bool
	}{
		{
			name: "current_task",
			self: "coder-1",
			build: func(agents map[string]models.Agent) []models.Task {
				task := testhelpers.BuildTaskByStatus("working", models.TaskStatusImplementing, now)
				agent := agents["coder-1"]
				agent.Status = models.AgentStatusWorking
				agent.CurrentTask = &task.ID
				agents["coder-1"] = agent
				return []models.Task{task}
			},
		},
		{
			name: "executing assignment without current_task",
			self: "coder-1",
			build: func(map[string]models.Agent) []models.Task {
				return []models.Task{testhelpers.BuildTaskByStatus("executing", models.TaskStatusImplementing, now)}
			},
		},
		{
			name: "handoff pending",
			self: "coder-1",
			build: func(map[string]models.Agent) []models.Task {
				task := testhelpers.BuildTaskByStatus("handoff", models.TaskStatusImplementing, now)
				task.HandoffPending = true
				return []models.Task{task}
			},
		},
		{
			name: "rejected rework under unexpired lease",
			self: "coder-1",
			build: func(map[string]models.Agent) []models.Task {
				return []models.Task{testhelpers.BuildTaskByStatus("rework", models.TaskStatusRejected, now)}
			},
		},
		{
			name: "rejected rework whose lease expired",
			self: "coder-1",
			build: func(map[string]models.Agent) []models.Task {
				task := testhelpers.BuildTaskByStatus("rework", models.TaskStatusRejected, now)
				task.LeaseExpires = &expired
				return []models.Task{task}
			},
			wantReleased: true,
		},
		{
			name: "review claim",
			self: "code-reviewer-1",
			build: func(map[string]models.Agent) []models.Task {
				return []models.Task{testhelpers.BuildTaskByStatus("reviewing", models.TaskStatusReviewing, now)}
			},
		},
		{
			name: "owned pending merge",
			self: "code-reviewer-1",
			build: func(map[string]models.Agent) []models.Task {
				task := testhelpers.BuildTaskByStatus("approved", models.TaskStatusApproved, now)
				task.Approvals = []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}}
				return []models.Task{task}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
			role := strings.TrimSuffix(tc.self, "-1")
			agents := map[string]models.Agent{
				role + "-1": liveSurplusAgent(role, now),
				role + "-2": liveSurplusAgent(role, now),
				role + "-3": liveSurplusAgent(role, now),
			}
			tasks := tc.build(agents)
			bb := writeSurplusState(t, 2, agents, tasks)
			before := readSurplusState(t, bb)

			released, err := ops.RetireSurplusAgent(bb, surplusAuthority(tc.self), testResolver(t))

			if err != nil || released != tc.wantReleased {
				t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (%v, nil)", released, err, tc.wantReleased)
			}
			after := readSurplusState(t, bb)
			if _, exists := after.Agents[tc.self]; exists == tc.wantReleased {
				t.Fatalf("%s registered = %v after released = %v", tc.self, exists, released)
			}
			if tc.wantReleased {
				return
			}
			for i := range before.Tasks {
				was, got := before.Tasks[i], after.FindTask(before.Tasks[i].ID)
				if got.Status != was.Status || !equalStringPtr(got.AssignedTo, was.AssignedTo) || !equalStringPtr(got.ReviewingBy, was.ReviewingBy) || len(got.History) != len(was.History) {
					t.Fatalf("task %s changed although the agent stayed: %+v -> %+v", was.ID, was, *got)
				}
			}
		})
	}
}

func equalStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// The cap is the boundary: an agent retires only when its live peers alone
// already fill it.
func TestReleaseIfSurplusCapBoundary(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	bb := writeSurplusState(t, 3, threeCoders(now), nil)

	released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), testResolver(t))

	if err != nil || released {
		t.Fatalf("ops.RetireSurplusAgent() at the cap = (%v, %v), want (false, nil)", released, err)
	}
	if _, ok := readSurplusState(t, bb).Agents["coder-1"]; !ok {
		t.Fatal("coder-1 removed while the role was at, not over, its cap")
	}
}

// A role's own pipeline max-instances wins over config.max_instances.
func TestReleaseIfSurplusRoleMaxInstancesOverridesProjectCap(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	coderRole := "    coder:\n      type: doer\n"
	if !strings.Contains(string(testPipelineYAML), coderRole) {
		t.Fatal("fixture: coder role definition not found in the test pipeline")
	}
	cfg, err := pipeline.LoadFromBytes([]byte(strings.Replace(string(testPipelineYAML), coderRole, coderRole+"      max-instances: 3\n", 1)))
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}
	bb := writeSurplusState(t, 1, threeCoders(now), nil)

	released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), pipeline.NewResolver(cfg))

	if err != nil || released {
		t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want the role cap of 3 to keep coder-1", released, err)
	}
}

// Orchestrators are never retired by pool sizing.
func TestReleaseIfSurplusNeverRetiresOrchestrator(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	bb := writeSurplusState(t, 1, map[string]models.Agent{
		"orchestrator-1": liveSurplusAgent("orchestrator", now),
		"orchestrator-2": liveSurplusAgent("orchestrator", now),
	}, nil)

	released, err := ops.RetireSurplusAgent(bb, surplusAuthority("orchestrator-1"), testResolver(t))

	if err != nil || released {
		t.Fatalf("ops.RetireSurplusAgent(orchestrator) = (%v, %v), want (false, nil)", released, err)
	}
}

// Occupancy is lease-first, as at registration: a peer whose process is dead
// but whose lease is unexpired still holds its slot; once the lease and
// heartbeat lapse it does not.
func TestReleaseIfSurplusCountsLeasedPeerWithDeadProcess(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name         string
		leaseExpires time.Time
		wantReleased bool
	}{
		{"unexpired lease occupies", now.Add(30 * time.Minute), true},
		{"expired lease frees the slot", now.Add(-time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An existing, empty proc root: PID evidence for the peer is "dead".
			t.Cleanup(ops.SetAgentProcessProcRootForTest(t.TempDir()))
			agents := threeCoders(now)
			dead := agents["coder-3"]
			dead.PID = 999999
			dead.Heartbeat = now.Add(-time.Hour)
			dead.LeaseExpires = &tc.leaseExpires
			agents["coder-3"] = dead
			bb := writeSurplusState(t, 2, agents, nil)

			released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), testResolver(t))

			if err != nil || released != tc.wantReleased {
				t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (%v, nil)", released, err, tc.wantReleased)
			}
		})
	}
}

// Concurrent retirements cannot take a role below its cap: three idle coders
// at cap 2 race, exactly one leaves.
func TestReleaseIfSurplusConcurrentRetirementsStopAtCap(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	bb := writeSurplusState(t, 2, threeCoders(now), nil)
	resolver := testResolver(t)

	var wg sync.WaitGroup
	results := make(chan bool, 3)
	start := make(chan struct{})
	for _, id := range []string{"coder-1", "coder-2", "coder-3"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			released, err := ops.RetireSurplusAgent(bb, surplusAuthority(id), resolver)
			if err != nil {
				t.Errorf("ops.RetireSurplusAgent(%s) error = %v", id, err)
			}
			results <- released
		}(id)
	}
	close(start)
	wg.Wait()
	close(results)

	retired := 0
	for released := range results {
		if released {
			retired++
		}
	}
	if retired != 1 {
		t.Fatalf("%d agents retired, want exactly 1", retired)
	}
	if got := len(readSurplusState(t, bb).Agents); got != 2 {
		t.Fatalf("%d agents registered after concurrent retirement, want 2", got)
	}
}

// The locked recheck decides, not the caller's snapshot: when a peer leaves or
// the cap rises after the snapshot said surplus, nothing changes.
func TestReleaseIfSurplusLockedRecheckDeclines(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.State)
	}{
		{"peer left", func(s *models.State) { delete(s.Agents, "coder-3") }},
		{"cap raised", func(s *models.State) { s.Config.MaxInstances = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
			now := time.Now().UTC()
			bb := writeSurplusState(t, 2, threeCoders(now), nil)
			resolver := testResolver(t)
			if !ops.PoolSurplus(readSurplusState(t, bb), resolver, "coder-1", now) {
				t.Fatal("fixture: snapshot does not report coder-1 as surplus")
			}
			if err := bb.Modify(func(s *models.State) error { tc.change(s); return nil }); err != nil {
				t.Fatal(err)
			}

			released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), resolver)

			if err != nil || released {
				t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (false, nil)", released, err)
			}
			if _, ok := readSurplusState(t, bb).Agents["coder-1"]; !ok {
				t.Fatal("coder-1 removed although the locked state no longer had it surplus")
			}
		})
	}
}

// A stale caller (registration replaced) is refused by the generation fence
// and changes nothing.
func TestReleaseIfSurplusRefusesStaleAuthority(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	bb := writeSurplusState(t, 2, threeCoders(now), nil)

	released, err := ops.RetireSurplusAgent(bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, testResolver(t))

	if released || !ops.IsAgentAuthorityError(err) {
		t.Fatalf("ops.RetireSurplusAgent(stale) = (%v, %v), want an authority error and no release", released, err)
	}
	if _, ok := readSurplusState(t, bb).Agents["coder-1"]; !ok {
		t.Fatal("stale caller removed the current registration")
	}
}

// A parked doer or reviewer wait returns errPoolSurplus as soon as a state
// write makes the agent surplus, ahead of any work check.
func TestWaitForWorkReturnsPoolSurplusOnStateChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		polling bool
	}{
		{"doer event-driven", "coder", false},
		{"doer polling", "coder", true},
		{"reviewer event-driven", "code-reviewer", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Config.MaxInstances = 3
			state.Tasks = []models.Task{}
			testhelpers.WriteInitialState(t, statePath, state)
			bb := db.New(statePath)
			withAbortTickInterval(t, 200*time.Millisecond)
			// Watcher setup follows the wait's initial check, so it marks the
			// wait as parked before the cap changes.
			parked := make(chan struct{})
			var parkedOnce sync.Once
			previous := newStateWatcher
			newStateWatcher = func(bb *db.Blackboard) (stateWatcher, error) {
				parkedOnce.Do(func() { close(parked) })
				if tc.polling {
					return nil, errors.New("force polling")
				}
				return previous(bb)
			}
			t.Cleanup(func() { newStateWatcher = previous })
			strategy, err := NewRoleStrategy(tc.role, testResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			config := SupervisorConfig{StatePath: statePath, ProjectRoot: tmpDir, AgentID: tc.role + "-1", Role: tc.role}
			config.poolSurplus = func(s *models.State) bool { return s.Config.MaxInstances == 2 }
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errCh := make(chan error, 1)
			go func() {
				_, err := strategy.WaitForWork(ctx, bb, config, 20*time.Millisecond, 5*time.Second)
				errCh <- err
			}()
			select {
			case <-parked:
			case err := <-errCh:
				t.Fatalf("wait returned before parking: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("wait did not park")
			}

			if err := bb.Modify(func(s *models.State) error { s.Config.MaxInstances = 2; return nil }); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-errCh:
				if !errors.Is(err, errPoolSurplus) {
					t.Fatalf("WaitForWork() error = %v, want errPoolSurplus", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("parked wait did not observe the lowered cap")
			}
		})
	}
}

// Without a predicate (orchestrator, or before registration) the wait keeps
// its prior behavior: no surplus exit, only work or the deadline end it.
func TestWaitForWorkWithoutSurplusPredicateIgnoresCap(t *testing.T) {
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.MaxInstances = 1
	state.Tasks = []models.Task{}
	testhelpers.WriteInitialState(t, statePath, state)
	bb := db.New(statePath)

	hasWork, err := waitForWorkPolling(context.Background(), bb, tmpDir, 10*time.Millisecond, 100*time.Millisecond,
		func(*models.State) (bool, string) { return false, "" }, nil)

	if hasWork || err != nil {
		t.Fatalf("waitForWorkPolling(nil predicate) = (%v, %v), want (false, nil) at the deadline", hasWork, err)
	}
}

// lockedBuffer is a log sink shared by the supervisor's goroutines and the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type surplusSupervisorRun struct {
	bb       *db.Blackboard
	logs     *lockedBuffer
	done     chan error
	executed chan struct{}
	watcher  *silentWatcher
	parked   chan struct{}
}

// startSurplusSupervisor registers coder-1 as the third coder (project cap 3)
// beside two live peers and runs the real supervisor loop. onExecute, when
// set, runs inside the provider session.
func startSurplusSupervisor(t *testing.T, tasks []models.Task, onExecute func(bb *db.Blackboard)) *surplusSupervisorRun {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxInstances = 3
	state.Config.CoderPollInterval = 1
	state.Config.DoerMaxWait = 30
	state.Agents["coder-2"] = liveSurplusAgent("coder", now)
	state.Agents["coder-3"] = liveSurplusAgent("coder", now)
	state.Tasks = tasks
	bb := testhelpers.WriteInitialState(t, statePath, state)

	run := &surplusSupervisorRun{
		bb: bb, logs: &lockedBuffer{}, done: make(chan error, 1),
		executed: make(chan struct{}, 8), watcher: newSilentWatcher(), parked: make(chan struct{}),
	}
	var parkedOnce sync.Once
	previousWatcher := newStateWatcher
	newStateWatcher = func(*db.Blackboard) (stateWatcher, error) {
		parkedOnce.Do(func() { close(run.parked) })
		return run.watcher, nil
	}
	restoreLogger := UseLoggerOutput(run.logs)
	provider := &MockLLMAgent{OnExecute: func(context.Context, string, string, string, string, []string) error {
		run.executed <- struct{}{}
		if onExecute != nil {
			onExecute(bb)
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		err := RunSupervisor(ctx, SupervisorConfig{
			AgentID: "coder-1", Role: "coder",
			ProjectRoot: root, StatePath: statePath,
			LogPath: paths.New(root).LogPath(), SpecsDir: filepath.Join(root, "specs"),
			CLIName: "codex", LLMAgent: provider, ExecutionTimeout: 30 * time.Second,
		})
		close(finished)
		run.done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("supervisor did not exit after cancellation")
		}
		restoreLogger()
		newStateWatcher = previousWatcher
	})
	return run
}

func (r *surplusSupervisorRun) waitExit(t *testing.T) {
	t.Helper()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("RunSupervisor() error = %v, want a clean surplus exit\n%s", err, r.logs.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("supervisor did not exit after becoming surplus\n%s", r.logs.String())
	}
	logs := r.logs.String()
	if !strings.Contains(logs, "Role over max-instances, supervisor exiting") {
		t.Fatalf("surplus exit not logged:\n%s", logs)
	}
	if strings.Contains(logs, "Failed to unregister agent") {
		t.Fatalf("deferred unregister ran after retirement:\n%s", logs)
	}
}

// A parked idle coder retires when the cap drops, even though the same write
// makes work claimable: it neither claims nor launches the provider.
func TestRunSupervisorParkedSurplusRetiresInsteadOfClaiming(t *testing.T) {
	run := startSurplusSupervisor(t, []models.Task{}, nil)
	select {
	case <-run.parked:
	case err := <-run.done:
		t.Fatalf("supervisor exited before parking: %v\n%s", err, run.logs.String())
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor did not enter its work wait")
	}

	ready := testhelpers.BuildTaskByStatus("ready", models.TaskStatusReady, time.Now().UTC())
	if err := run.bb.Modify(func(s *models.State) error {
		s.Config.MaxInstances = 2
		s.Tasks = append(s.Tasks, ready)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case run.watcher.events <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("work wait did not consume its notification")
	}

	run.waitExit(t)
	if len(run.executed) != 0 {
		t.Fatal("provider launched although the agent was surplus")
	}
	state := readSurplusState(t, run.bb)
	if _, exists := state.Agents["coder-1"]; exists {
		t.Fatal("coder-1 still registered")
	}
	if len(state.Agents) != 2 {
		t.Fatalf("%d agents registered, want the two peers", len(state.Agents))
	}
	if task := state.FindTask("ready"); task.Status != models.TaskStatusReady || task.AssignedTo != nil {
		t.Fatalf("ready task claimed: status=%s assigned=%v", task.Status, task.AssignedTo)
	}
}

// Strict task boundary: a coder that finishes a task while the cap drops
// retires before claiming the next ready task, keeping its submitted work.
func TestRunSupervisorRetiresAtTaskBoundaryUnderBacklog(t *testing.T) {
	first := testhelpers.BuildTaskByStatus("first", models.TaskStatusReady, time.Now().UTC())
	run := startSurplusSupervisor(t, []models.Task{first}, func(bb *db.Blackboard) {
		// Stand-in for a normal submission: the task moves to review keeping
		// assigned_to, the agent's current task clears; meanwhile the cap
		// drops and more work arrives.
		if err := bb.Modify(func(s *models.State) error {
			task := s.FindTask("first")
			reviewCommit := "review123"
			task.Status = models.TaskStatusReadyForReview
			task.ReviewCommit = &reviewCommit
			agent := s.Agents["coder-1"]
			agent.Status = models.AgentStatusWaiting
			agent.CurrentTask = nil
			agent.LeaseExpires = nil
			s.Agents["coder-1"] = agent
			s.Config.MaxInstances = 2
			s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("second", models.TaskStatusReady, time.Now().UTC()))
			return nil
		}); err != nil {
			t.Errorf("simulate submission: %v", err)
		}
	})

	run.waitExit(t)
	if got := len(run.executed); got != 1 {
		t.Fatalf("provider ran %d times, want only the first task's session", got)
	}
	state := readSurplusState(t, run.bb)
	if _, exists := state.Agents["coder-1"]; exists {
		t.Fatal("coder-1 still registered")
	}
	if task := state.FindTask("second"); task.Status != models.TaskStatusReady || task.AssignedTo != nil {
		t.Fatalf("next task claimed at the boundary: status=%s assigned=%v", task.Status, task.AssignedTo)
	}
	if task := state.FindTask("first"); task.Status != models.TaskStatusReadyForReview || task.ReviewCommit == nil {
		t.Fatalf("submitted work changed: status=%s review_commit=%v", task.Status, task.ReviewCommit)
	}
}

// The locked recheck reads the clock after acquiring the lock: a peer whose
// lease expires while retirement waits for the lock no longer counts, so the
// healthy supervisor stays when current occupancy is at the cap.
func TestReleaseIfSurplusIgnoresPeerExpiredWhileWaitingForLock(t *testing.T) {
	t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	now := time.Now().UTC()
	agents := threeCoders(now)
	expiring := agents["coder-3"]
	leaseEnd := now.Add(300 * time.Millisecond)
	expiring.LeaseExpires = &leaseEnd
	agents["coder-3"] = expiring
	bb := writeSurplusState(t, 2, agents, nil)
	resolver := testResolver(t)
	if !ops.PoolSurplus(readSurplusState(t, bb), resolver, "coder-1", now) {
		t.Fatal("fixture: coder-1 is not surplus while coder-3's lease is live")
	}

	// Hold the state lock across coder-3's lease deadline while coder-1
	// tries to retire.
	locked, unlock := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- bb.Modify(func(*models.State) error {
			close(locked)
			<-unlock
			return nil
		})
	}()
	<-locked
	type outcome struct {
		released bool
		err      error
	}
	retire := make(chan outcome, 1)
	go func() {
		released, err := ops.RetireSurplusAgent(bb, surplusAuthority("coder-1"), resolver)
		retire <- outcome{released, err}
	}()
	<-time.After(time.Until(leaseEnd) + 100*time.Millisecond)
	close(unlock)
	if err := <-holderDone; err != nil {
		t.Fatalf("lock holder: %v", err)
	}

	var got outcome
	select {
	case got = <-retire:
	case <-time.After(5 * time.Second):
		t.Fatal("releaseIfSurplus did not return after the lock was released")
	}
	if got.err != nil || got.released {
		t.Fatalf("ops.RetireSurplusAgent() = (%v, %v), want (false, nil): only coder-2 still occupies beside coder-1", got.released, got.err)
	}
	if _, ok := readSurplusState(t, bb).Agents["coder-1"]; !ok {
		t.Fatal("coder-1 retired although its expired peer no longer held a slot")
	}
}
