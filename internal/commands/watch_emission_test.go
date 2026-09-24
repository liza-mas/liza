package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/testhelpers/perm"
)

// Alert emission policy: each condition is written once per episode of its
// semantic identity; a condition that resolves and recurs alerts again.

func newEmissionWatchConfig(t *testing.T) WatchConfig {
	t.Helper()
	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	return WatchConfig{
		ProjectRoot: tmpDir,
		StateCache:  make(map[string]time.Time),
		WarnWriter:  io.Discard,
	}
}

func stateWithTask(task models.Task) *models.State {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	return state
}

func emittedCount(snapshot AlertSnapshot, category string) int {
	return countAlertsByCategory(snapshot.Alerts, category)
}

func TestAlertEmission_ApproachingLimitOncePerCount(t *testing.T) {
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.Iteration = 8

	emitted := 0
	for range 3 {
		emitted += emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "APPROACHING LIMIT")
	}
	if emitted != 1 {
		t.Fatalf("APPROACHING LIMIT lines over three unchanged checks = %d, want 1", emitted)
	}

	task.Iteration = 9
	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "APPROACHING LIMIT"); got != 1 {
		t.Fatalf("APPROACHING LIMIT lines after iteration 8->9 = %d, want 1 (new count is a new condition)", got)
	}
}

func TestAlertEmission_LeaseExpiredIdentityIncludesLease(t *testing.T) {
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	state := stateWithTask(testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now))
	expiredAt := now.Add(-models.LeaseExpiryGracePeriod - 5*time.Minute)
	state.Agents["coder-1"] = models.Agent{
		Role:         models.RoleCoder,
		Status:       models.AgentStatusWorking,
		CurrentTask:  testhelpers.StringPtr("task-1"),
		LeaseExpires: &expiredAt,
	}

	emitted := emittedCount(RunChecksWithStateSnapshot(state, config), "LEASE EXPIRED")
	emitted += emittedCount(RunChecksWithStateSnapshot(state, config), "LEASE EXPIRED")
	if emitted != 1 {
		t.Fatalf("LEASE EXPIRED lines over two unchanged checks = %d, want 1", emitted)
	}

	laterExpiry := expiredAt.Add(time.Minute)
	agent := state.Agents["coder-1"]
	agent.LeaseExpires = &laterExpiry
	state.Agents["coder-1"] = agent
	if got := emittedCount(RunChecksWithStateSnapshot(state, config), "LEASE EXPIRED"); got != 1 {
		t.Fatalf("LEASE EXPIRED lines for a different expired lease = %d, want 1", got)
	}
}

func TestAlertEmission_HypothesisExhaustionRealertsWhenFailedBySetGrows(t *testing.T) {
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
	task.FailedBy = []string{"coder-1", "coder-2"}

	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "HYPOTHESIS EXHAUSTION"); got != 1 {
		t.Fatalf("first HYPOTHESIS EXHAUSTION lines = %d, want 1", got)
	}
	task.FailedBy = append(task.FailedBy, "coder-3")
	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "HYPOTHESIS EXHAUSTION"); got != 1 {
		t.Fatalf("HYPOTHESIS EXHAUSTION lines after failed_by grew = %d, want 1", got)
	}
}

func TestAlertEmission_StaleSentinelOncePerEpisode(t *testing.T) {
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.AssignedTo = testhelpers.StringPtr("$transitioning")
	config.StateCache["sentinel:task-1"] = now.Add(-StaleSentinelThreshold - time.Minute)

	emitted := emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "STALE SENTINEL")
	emitted += emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "STALE SENTINEL")
	if emitted != 1 {
		t.Fatalf("STALE SENTINEL lines over two checks = %d, want 1", emitted)
	}
}

func TestAlertEmission_StalledEscalatesByDoublingAndResetsOnProgress(t *testing.T) {
	t0 := time.Now().UTC()
	originalNow := watchNow
	t.Cleanup(func() { watchNow = originalNow })
	config := newEmissionWatchConfig(t)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, t0)
	task.History = []models.TaskHistoryEntry{{Time: t0.Add(-35 * time.Minute), Event: models.TaskEventClaimed}}

	stalledAt := func(at time.Time) int {
		watchNow = func() time.Time { return at }
		return emittedCount(RunChecksWithStateSnapshot(stateWithTask(task), config), "STALLED")
	}

	steps := []struct {
		offset time.Duration
		want   int
		why    string
	}{
		{0, 1, "age 35m crosses the 30m step"},
		{6 * time.Minute, 0, "age 41m is still in the 30m step"},
		{20 * time.Minute, 0, "age 55m is still in the 30m step"},
		{26 * time.Minute, 1, "age 61m crosses the 60m step"},
		{60 * time.Minute, 0, "age 95m is still in the 60m step"},
		{86 * time.Minute, 1, "age 121m crosses the 120m step"},
	}
	for _, step := range steps {
		if got := stalledAt(t0.Add(step.offset)); got != step.want {
			t.Fatalf("STALLED lines at t0+%s = %d, want %d (%s)", step.offset, got, step.want, step.why)
		}
	}

	task.History = append(task.History, models.TaskHistoryEntry{Time: t0.Add(87 * time.Minute), Event: models.TaskEventSubmittedForReview})
	if got := stalledAt(t0.Add(88 * time.Minute)); got != 0 {
		t.Fatalf("STALLED lines right after progress = %d, want 0", got)
	}
	if got := stalledAt(t0.Add(118 * time.Minute)); got != 1 {
		t.Fatalf("STALLED lines 31m after new progress = %d, want 1 (cadence restarts at 30m)", got)
	}
}

func rejectedAfterVerdict(taskID string, status models.TaskStatus, assignee string, verdictAt time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(taskID, status, verdictAt)
	task.AssignedTo = testhelpers.StringPtr(assignee)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  verdictAt,
		Event: models.TaskEventRejected,
		Agent: testhelpers.StringPtr("reviewer-1"),
	})
	return task
}

func TestAlertEmission_OrphanedRejectedAnchorsGraceOnVerdictAndIgnoresStatusFlips(t *testing.T) {
	now := time.Now().UTC()

	t.Run("one alert per verdict across WAITING and IDLE flips", func(t *testing.T) {
		config := newEmissionWatchConfig(t)
		state := stateWithTask(rejectedAfterVerdict("task-1", models.TaskStatusRejected, "coder-1", now.Add(-5*time.Minute)))

		emitted := 0
		for _, status := range []models.AgentStatus{models.AgentStatusWaiting, models.AgentStatusIdle, models.AgentStatusWaiting} {
			state.Agents["coder-1"] = models.Agent{Role: models.RoleCoder, Status: status}
			emitted += emittedCount(RunChecksWithStateSnapshot(state, config), "ORPHANED REJECTED")
		}
		if emitted != 1 {
			t.Fatalf("ORPHANED REJECTED lines = %d, want 1 for one verdict whose grace has passed", emitted)
		}
	})

	t.Run("no alert within the verdict handoff grace", func(t *testing.T) {
		config := newEmissionWatchConfig(t)
		state := stateWithTask(rejectedAfterVerdict("task-1", models.TaskStatusRejected, "coder-1", now.Add(-30*time.Second)))
		state.Agents["coder-1"] = models.Agent{Role: models.RoleCoder, Status: models.AgentStatusWaiting}

		if got := emittedCount(RunChecksWithStateSnapshot(state, config), "ORPHANED REJECTED"); got != 0 {
			t.Fatalf("ORPHANED REJECTED lines within grace = %d, want 0", got)
		}
	})

	t.Run("non-coding role pair rejected status is covered", func(t *testing.T) {
		config := newEmissionWatchConfig(t)
		state := stateWithTask(rejectedAfterVerdict("plan-1", models.TaskStatusCodingPlanRejected, "code-planner-1", now.Add(-5*time.Minute)))

		if got := emittedCount(RunChecksWithStateSnapshot(state, config), "ORPHANED REJECTED"); got != 1 {
			t.Fatalf("ORPHANED REJECTED lines for CODING_PLAN_REJECTED with missing doer = %d, want 1", got)
		}
	})

	t.Run("missing verdict time is not an open-ended grace", func(t *testing.T) {
		config := newEmissionWatchConfig(t)
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
		task.History = nil
		state := stateWithTask(task)

		if got := emittedCount(RunChecksWithStateSnapshot(state, config), "ORPHANED REJECTED"); got != 1 {
			t.Fatalf("ORPHANED REJECTED lines without verdict history = %d, want 1", got)
		}
	})
}

func TestAlertEmission_MaskedValidationErrorIsNotResolution(t *testing.T) {
	config := newEmissionWatchConfig(t)
	statePath := paths.New(config.ProjectRoot).StatePath()
	now := time.Now().UTC()
	checkState := stateWithTask(testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now))

	zombie := true
	originalFind := findZombieAgents
	findZombieAgents = func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
		if !zombie {
			return procscan.ZombieScanResult{}, nil
		}
		return procscan.ZombieScanResult{Zombies: []procscan.ZombieProcess{{PID: 4242, Role: "coder", Reason: "not_registered_in_state"}}}, nil
	}
	t.Cleanup(func() { findZombieAgents = originalFind })

	validFile := testhelpers.CreateValidState()
	duplicateIDs := testhelpers.CreateValidState()
	duplicateIDs.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("dup-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("dup-1", models.TaskStatusReady, now),
	}

	tick := func(file *models.State) []Alert {
		testhelpers.WriteInitialState(t, statePath, file)
		snapshot := RunChecksWithStateSnapshot(checkState, config)
		var invalid []Alert
		for _, alert := range snapshot.Alerts {
			if alert.Category == "INVALID STATE" {
				invalid = append(invalid, alert)
			}
		}
		return invalid
	}

	if got := tick(validFile); len(got) != 1 || !strings.Contains(got[0].Message, "zombie") {
		t.Fatalf("tick 1 INVALID STATE = %v, want the zombie error once", got)
	}
	if got := tick(duplicateIDs); len(got) != 1 || strings.Contains(got[0].Message, "zombie") {
		t.Fatalf("tick 2 INVALID STATE = %v, want the schema error once (zombie masked, not resolved)", got)
	}
	if got := tick(validFile); len(got) != 0 {
		t.Fatalf("tick 3 INVALID STATE = %v, want none: the zombie error was masked, never observed resolved", got)
	}
	zombie = false
	if got := tick(validFile); len(got) != 0 {
		t.Fatalf("tick 4 INVALID STATE = %v, want none on a clean validation", got)
	}
	zombie = true
	if got := tick(validFile); len(got) != 1 {
		t.Fatalf("tick 5 INVALID STATE = %v, want the zombie error again after a clean validation", got)
	}
}

func blockedEpisodes(taskID, reason string, starts ...time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusBlocked, starts[0])
	task.BlockedReason = &reason
	for i, start := range starts {
		if i > 0 {
			task.History = append(task.History, models.TaskHistoryEntry{Time: start.Add(-time.Second), Event: models.TaskEventUnblocked})
		}
		task.History = append(task.History, models.TaskHistoryEntry{Time: start, Event: models.TaskEventBlocked, Reason: &reason})
	}
	return task
}

func TestAlertEmission_NewBlockedEpisodeAlertsWithoutObservedUnblock(t *testing.T) {
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	first := now.Add(-10 * time.Minute)
	second := now.Add(-time.Minute)

	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(blockedEpisodes("task-1", "needs spec", first)), config), "BLOCKED"); got != 1 {
		t.Fatalf("first episode BLOCKED lines = %d, want 1", got)
	}
	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(blockedEpisodes("task-1", "needs spec", first)), config), "BLOCKED"); got != 0 {
		t.Fatalf("same episode BLOCKED lines = %d, want 0", got)
	}
	if got := emittedCount(RunChecksWithStateSnapshot(stateWithTask(blockedEpisodes("task-1", "needs spec", first, second)), config), "BLOCKED"); got != 1 {
		t.Fatalf("second episode BLOCKED lines = %d, want 1 even though no unblocked poll was observed", got)
	}
}

func TestAlertEmission_MarkBlockedAndWatcherWriteOneLinePerEpisode(t *testing.T) {
	t.Setenv(EnvAutoRepairAgentPool, "no")
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	now := time.Now().UTC()
	testhelpers.WriteInitialState(t, statePath, stateWithTask(testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)))

	if _, err := ops.MarkBlocked(tmpDir, "task-1", "Waiting on API spec", []string{"Which version?"}, "coder-1"); err != nil {
		t.Fatalf("MarkBlocked() error = %v", err)
	}
	alertsLog := paths.New(tmpDir).AlertsLogPath()
	config := WatchConfig{ProjectRoot: tmpDir, AlertsLog: alertsLog, StateCache: make(map[string]time.Time), WarnWriter: io.Discard}
	if err := runChecks(context.Background(), config); err != nil {
		t.Fatalf("runChecks() error = %v", err)
	}

	data, err := os.ReadFile(alertsLog)
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	if got := strings.Count(string(data), "BLOCKED: task-1 — Waiting on API spec"); got != 1 {
		t.Fatalf("BLOCKED lines for one episode = %d, want 1:\n%s", got, data)
	}
}

func TestAlertEmission_LedgerFailureDoesNotDropLaterAlertsOfTheBatch(t *testing.T) {
	t.Setenv(EnvAutoRepairAgentPool, "no")
	now := time.Now().UTC()
	config := newEmissionWatchConfig(t)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		blockedEpisodes("blocked-first", "needs spec", now.Add(-2*time.Minute)),
		blockedEpisodes("blocked-second", "needs spec", now.Add(-time.Minute)),
	}
	testhelpers.WriteInitialState(t, paths.New(config.ProjectRoot).StatePath(), state)

	// The alerts log and the ledger lock exist and stay writable; the ledger
	// itself cannot be created.
	alertsDir := t.TempDir()
	config.AlertsLog = filepath.Join(alertsDir, "alerts.log")
	for _, path := range []string{config.AlertsLog, config.AlertsLog + ".once.lock"} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("pre-create %s: %v", path, err)
		}
	}
	restore, err := perm.DenyWrites(alertsDir)
	if err != nil {
		t.Fatalf("DenyWrites() error = %v", err)
	}
	t.Cleanup(func() { _ = restore() })

	if err := runChecks(context.Background(), config); err != nil {
		t.Fatalf("runChecks() error = %v, want nil: a ledger failure is a warning", err)
	}
	data, err := os.ReadFile(config.AlertsLog)
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	for _, task := range []string{"blocked-first", "blocked-second"} {
		if !strings.Contains(string(data), "BLOCKED: "+task+" — needs spec") {
			t.Fatalf("alerts log missing %s:\n%s", task, data)
		}
	}
}
