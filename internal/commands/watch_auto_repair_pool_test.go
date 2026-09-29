package commands

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// poolRepairHarness drives watcher auto-repair ticks with distinct fake PIDs
// and scripted process observations.
type poolRepairHarness struct {
	t       *testing.T
	root    string
	cache   map[string]time.Time
	nextPID int
	spawned []int
	status  map[int]procscan.AgentProcessState
}

func newPoolRepairHarness(t *testing.T) *poolRepairHarness {
	t.Helper()
	unsetAutoRepairAgentPoolEnv(t)
	h := &poolRepairHarness{
		t:       t,
		root:    writeRepairAgentPoolState(t, testhelpers.CreateValidState()),
		cache:   make(map[string]time.Time),
		nextPID: 40000,
		status:  make(map[int]procscan.AgentProcessState),
	}
	originalSpawn := repairAgentPoolSpawn
	repairAgentPoolSpawn = func(_, _, _, _ string) (int, error) {
		h.nextPID++
		h.spawned = append(h.spawned, h.nextPID)
		h.status[h.nextPID] = procscan.AgentProcessLiveMatching
		return h.nextPID, nil
	}
	originalStatus := autoRepairPendingProcessStatus
	autoRepairPendingProcessStatus = func(pid int, _ string) procscan.AgentProcessStatus {
		state, ok := h.status[pid]
		if !ok {
			state = procscan.AgentProcessDead
		}
		return procscan.AgentProcessStatus{State: state, Alive: state != procscan.AgentProcessDead}
	}
	t.Cleanup(func() {
		repairAgentPoolSpawn = originalSpawn
		autoRepairPendingProcessStatus = originalStatus
	})
	return h
}

// tick runs one watcher pass over state, with the per-role backoff elapsed.
func (h *poolRepairHarness) tick(state *models.State) AutoRepairAgentPoolOutcome {
	h.t.Helper()
	for key := range h.cache {
		if strings.HasPrefix(key, autoRepairAgentPoolCachePrefix) {
			h.cache[key] = time.Now().UTC().Add(-2 * AutoRepairAgentPoolBackoff)
		}
	}
	written := *state
	written.Agents = withLiveOrchestrator(state.Agents, time.Now().UTC())
	rewriteOrchestratorRepairState(h.t, h.root, &written)
	return RunAutoRepairAgentPool(context.Background(), &written, WatchConfig{
		ProjectRoot: h.root,
		StateCache:  h.cache,
		WarnWriter:  io.Discard,
	})
}

func (h *poolRepairHarness) pendingKeys() int {
	count := 0
	for key := range h.cache {
		if strings.HasPrefix(key, autoRepairAgentPoolPendingPrefix) {
			count++
		}
	}
	return count
}

func coderDemand(n int) *models.State {
	state := testhelpers.CreateValidState()
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, "task-"+string(rune('a'+i)))
	}
	state.Tasks = readyTasks(time.Now().UTC(), ids...)
	state.Agents = map[string]models.Agent{}
	return state
}

func TestRunAutoRepairAgentPool_PendingSpawnsSurviveDemandChanges(t *testing.T) {
	// GIVEN three coders started at the cap, none registered yet; one is
	// unobservable
	h := newPoolRepairHarness(t)
	h.tick(coderDemand(3))
	if len(h.spawned) != 3 {
		t.Fatalf("spawned = %v, want three coders", h.spawned)
	}
	h.status[h.spawned[2]] = procscan.AgentProcessUnknown

	// WHEN demand disappears, then returns
	h.tick(coderDemand(0))
	h.tick(coderDemand(3))

	// THEN the live and unobservable processes keep covering it
	if len(h.spawned) != 3 {
		t.Fatalf("spawned = %v, want no replacement while starts are pending", h.spawned)
	}
	if got := h.pendingKeys(); got != 3 {
		t.Fatalf("pending entries = %d, want 3 retained", got)
	}
}

func TestRunAutoRepairAgentPool_InFlightSpawnsDoNotSuppress(t *testing.T) {
	// GIVEN three coders just started
	h := newPoolRepairHarness(t)
	h.tick(coderDemand(3))

	// WHEN the next tick sees them still bootstrapping
	outcome := h.tick(coderDemand(3))

	// THEN no failed start is counted and nothing is suppressed
	if got := autoRepairStartCount(h.cache, "coder"); got != 0 {
		t.Fatalf("failed starts = %d, want 0 while pending", got)
	}
	for _, alert := range outcome.Alerts {
		if strings.Contains(alert.Message, "auto repair suppressed") {
			t.Fatalf("alert %q, want no suppression for in-flight starts", alert.Message)
		}
	}
}

func TestRunAutoRepairAgentPool_StalePendingCountsOnceAndKeepsCapacity(t *testing.T) {
	// GIVEN one started coder that stays unregistered past the timeout
	h := newPoolRepairHarness(t)
	h.tick(coderDemand(1))
	key := autoRepairPendingKey("coder", h.spawned[0], "")
	h.cache[key] = time.Now().UTC().Add(-AutoRepairAgentPoolPendingTimeout - time.Second)

	// WHEN two ticks observe it still alive
	h.tick(coderDemand(1))
	h.tick(coderDemand(1))

	// THEN it counts once as a failed start and still covers the task
	if got := autoRepairStartCount(h.cache, "coder"); got != 1 {
		t.Fatalf("failed starts = %d, want 1", got)
	}
	if len(h.spawned) != 1 {
		t.Fatalf("spawned = %v, want no replacement for a live process", h.spawned)
	}

	// WHEN it finally exits
	h.status[h.spawned[0]] = procscan.AgentProcessDead
	h.tick(coderDemand(1))

	// THEN it is not counted again and its capacity is freed
	if got := autoRepairStartCount(h.cache, "coder"); got != 1 {
		t.Fatalf("failed starts = %d, want still 1", got)
	}
	if len(h.spawned) != 2 {
		t.Fatalf("spawned = %v, want a replacement after the exit", h.spawned)
	}
}

func TestRunAutoRepairAgentPool_ExitedPendingCountsAsFailedStart(t *testing.T) {
	for _, exit := range []procscan.AgentProcessState{procscan.AgentProcessDead, procscan.AgentProcessMismatched} {
		t.Run(string(exit), func(t *testing.T) {
			// GIVEN one started coder whose process is gone before registering
			h := newPoolRepairHarness(t)
			h.tick(coderDemand(1))
			h.status[h.spawned[0]] = exit

			// WHEN
			h.tick(coderDemand(1))

			// THEN it is a failed start and a new coder replaces it
			if got := autoRepairStartCount(h.cache, "coder"); got != 1 {
				t.Fatalf("failed starts = %d, want 1", got)
			}
			if len(h.spawned) != 2 {
				t.Fatalf("spawned = %v, want a replacement", h.spawned)
			}
		})
	}
}

func TestRunAutoRepairAgentPool_RegistrationResetsFailedStarts(t *testing.T) {
	// GIVEN two earlier failed starts and one pending coder; the pending PID
	// is this test process so the registered row passes claim admission
	h := newPoolRepairHarness(t)
	now := time.Now().UTC()
	pid := os.Getpid()
	h.cache[autoRepairPendingKey("coder", pid, "")] = now
	h.status[pid] = procscan.AgentProcessLiveMatching
	recordAutoRepairFailedStart(h.cache, "coder", now)
	recordAutoRepairFailedStart(h.cache, "coder", now)

	// WHEN the pending coder registers under its PID
	state := coderDemand(1)
	state.Agents["coder-1"] = poolDoer(now, pid, "")
	h.tick(state)

	// THEN the consecutive failure count restarts
	if got := autoRepairStartCount(h.cache, "coder"); got != 0 {
		t.Fatalf("failed starts = %d, want 0 after a registration", got)
	}
	if got := h.pendingKeys(); got != 0 || len(h.spawned) != 0 {
		t.Fatalf("pending entries = %d spawned = %v, want the registered coder to cover the task", got, h.spawned)
	}
}

func TestAutoRepairUnservableAlerts_OncePerEpisode(t *testing.T) {
	cache := make(map[string]time.Time)
	now := time.Now().UTC()
	work := []UnservableRoleWork{{Role: "code-reviewer", CLI: "codex", TaskIDs: []string{"t1"}}}

	first := autoRepairUnservableAlerts(work, cache, now)
	repeat := autoRepairUnservableAlerts(work, cache, now)
	autoRepairUnservableAlerts(nil, cache, now)
	again := autoRepairUnservableAlerts(work, cache, now)

	if len(first) != 1 || first[0].Category != "AUTO REPAIR UNSERVABLE" || !strings.Contains(first[0].Message, "--cli codex") {
		t.Fatalf("first = %+v, want one unservable warning naming the CLI", first)
	}
	if len(repeat) != 0 {
		t.Fatalf("repeat = %+v, want no duplicate within the episode", repeat)
	}
	if len(again) != 1 {
		t.Fatalf("again = %+v, want a new warning after the episode ended", again)
	}
}

func TestResolveAutoRepairPendingSpawns_ReservesExplicitIDs(t *testing.T) {
	// GIVEN one live reviewer start under an explicit ID and one live doer
	// start that auto-assigns
	h := newPoolRepairHarness(t)
	now := time.Now().UTC()
	h.cache[autoRepairPendingKey("code-reviewer", 50001, "code-reviewer-2")] = now
	h.cache[autoRepairPendingKey("coder", 50002, "")] = now
	h.status[50001] = procscan.AgentProcessLiveMatching
	h.status[50002] = procscan.AgentProcessLiveMatching

	// WHEN
	pending, ids := resolveAutoRepairPendingSpawns(testhelpers.CreateValidState(), h.cache, now)

	// THEN both cover demand and only the explicit ID is reserved
	if pending["code-reviewer"] != 1 || pending["coder"] != 1 {
		t.Fatalf("pending = %v, want one per role", pending)
	}
	if len(ids) != 1 || !ids["code-reviewer-2"] {
		t.Fatalf("reserved IDs = %v, want only code-reviewer-2", ids)
	}
}
