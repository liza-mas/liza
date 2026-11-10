package commands

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// ADR-0245 composition: watcher ticks, the real state file, and the real
// registration and retirement transactions, with process starts faked.

type runCapHarness struct {
	*poolRepairHarness
	bb       *db.Blackboard
	resolver *pipeline.Resolver
	roles    []string // role of each spawned process, in start order
}

func newRunCapHarness(t *testing.T, state *models.State) *runCapHarness {
	t.Helper()
	isolateOrchestratorProcfs(t)
	base := newPoolRepairHarness(t)
	rewriteOrchestratorRepairState(t, base.root, state)
	cfg, err := pipeline.LoadFrozen(base.root)
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}
	h := &runCapHarness{poolRepairHarness: base, bb: db.For(paths.New(base.root).StatePath()), resolver: pipeline.NewResolver(cfg)}
	spawn := repairAgentPoolSpawn
	repairAgentPoolSpawn = func(root, role, cli, agentID string, fromConfig bool, item int) (int, error) {
		h.roles = append(h.roles, role)
		return spawn(root, role, cli, agentID, fromConfig, item)
	}
	t.Cleanup(func() { repairAgentPoolSpawn = spawn })
	return h
}

// tick runs one watcher pass over the state file as it is, with backoff
// elapsed for roles that started.
func (h *runCapHarness) tick() AutoRepairAgentPoolOutcome {
	h.t.Helper()
	for key := range h.cache {
		if strings.HasPrefix(key, autoRepairAgentPoolCachePrefix) {
			h.cache[key] = time.Now().UTC().Add(-2 * AutoRepairAgentPoolBackoff)
		}
	}
	return RunAutoRepairAgentPool(context.Background(), h.state(), WatchConfig{ProjectRoot: h.root, StateCache: h.cache, WarnWriter: io.Discard})
}

func (h *runCapHarness) state() *models.State {
	h.t.Helper()
	state, err := h.bb.Read()
	if err != nil {
		h.t.Fatalf("read state: %v", err)
	}
	return state
}

func (h *runCapHarness) reservations() []string {
	var roles []string
	for _, reservation := range models.UnexpiredPoolReservations(h.state(), time.Now().UTC()) {
		roles = append(roles, reservation.Role)
	}
	return roles
}

// register admits id through registration; pid makes it the process the
// watcher started, so its pending start settles.
func (h *runCapHarness) register(role, id string, pid int) error {
	h.t.Helper()
	_, err := ops.RegisterAgent(h.bb, ops.RegisterAgentInput{AgentID: id, Role: role, Terminal: "t", Provider: "claude", LeaseDurationSeconds: 600, Resolver: h.resolver})
	if err != nil {
		return err
	}
	return h.bb.Modify(func(s *models.State) error {
		agent := s.Agents[id]
		agent.PID = pid
		s.Agents[id] = agent
		return nil
	})
}

func (h *runCapHarness) retire(id string) bool {
	h.t.Helper()
	released, err := ops.RetireSurplusAgent(h.bb, h.state().Agents[id].Authority(id), h.resolver)
	if err != nil {
		h.t.Fatalf("RetireSurplusAgent(%s): %v", id, err)
	}
	return released
}

func (h *runCapHarness) surplus(id string) bool {
	return ops.PoolSurplus(h.state(), h.resolver, id, time.Now().UTC())
}

// act records a history entry by id now, as a claim, verdict or merge would.
func (h *runCapHarness) act(id, taskID string, event models.TaskEventName, mutate func(*models.Task)) {
	h.t.Helper()
	if err := h.bb.Modify(func(s *models.State) error {
		task := s.FindTask(taskID)
		task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: event, Agent: testhelpers.StringPtr(id)})
		if mutate != nil {
			mutate(task)
		}
		return nil
	}); err != nil {
		h.t.Fatalf("record action: %v", err)
	}
}

func runCapAgent(role string, now time.Time) models.Agent {
	agent := occupiedAgent(role, now.Add(-time.Minute), deadPID)
	agent.RegisteredAt = now.Add(-time.Minute)
	agent.Generation = testhelpers.TestAgentGeneration
	agent.Provider = "claude"
	return agent
}

func runCapState(maxAgents int, agents map[string]string, tasks ...models.Task) *models.State {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxAgents = maxAgents
	state.Config.MaxInstances = 5
	state.Agents = map[string]models.Agent{"orchestrator-1": occupiedAgent(models.RoleOrchestrator, now, 987654321)}
	for id, role := range agents {
		state.Agents[id] = runCapAgent(role, now)
	}
	state.Tasks = tasks
	return state
}

func taskWithHistory(id string, status models.TaskStatus, at time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, at)
	task.History = []models.TaskHistoryEntry{{Time: at, Event: models.TaskEventName("created")}}
	return task
}

func hasRunCapAlert(outcome AutoRepairAgentPoolOutcome, role string) bool {
	return slices.ContainsFunc(outcome.Alerts, func(a Alert) bool {
		return a.Category == alerts.CategoryRunCap.String() && strings.Contains(a.Message, "role "+role+" ")
	})
}

// S1: with max_agents 2 the single non-orchestrator slot moves from the coder
// to waiting review work and back, one task boundary at a time.
func TestRunCap_S1_HandoffAtTwoAgents(t *testing.T) {
	now := time.Now().UTC()
	h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"},
		taskWithHistory("task-review", models.TaskStatusReadyForReview, now.Add(-time.Hour)),
		taskWithHistory("task-code", models.TaskStatusReady, now.Add(-30*time.Minute))))
	h.act("coder-1", "task-code", models.TaskEventName("claimed"), nil)

	// 1. The reviewer role is reserved; nothing can start yet.
	outcome := h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{"code-reviewer"}) {
		t.Fatalf("reservations = %v, want [code-reviewer]", got)
	}
	if len(h.roles) != 0 || !hasRunCapAlert(outcome, "code-reviewer") {
		t.Fatalf("started %v, alerts %v; want no start and a RUN CAP alert", h.roles, outcome.Alerts)
	}
	// 2. The coder gives up its slot at its boundary.
	if !h.retire("coder-1") {
		t.Fatal("coder-1 did not give up its slot for the reserved role")
	}
	// 3. The freed slot goes to the reserved role.
	h.tick()
	if !slices.Equal(h.roles, []string{"code-reviewer"}) {
		t.Fatalf("started %v, want one code-reviewer", h.roles)
	}
	// 4. While that start is pending, a coder cannot take the slot.
	if err := h.register("coder", "coder-2", 1); err == nil || !strings.Contains(err.Error(), "held for role(s) code-reviewer") {
		t.Fatalf("coder registration = %v, want a refusal naming the reservation", err)
	}
	// 5. The pending start keeps the reservation across ticks.
	h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{"code-reviewer"}) || len(h.roles) != 1 {
		t.Fatalf("reservations = %v, started %v; want the reservation kept and no new start", got, h.roles)
	}
	// 6. The reviewer registers into its slot.
	if err := h.register("code-reviewer", "code-reviewer-1", h.spawned[0]); err != nil {
		t.Fatalf("reserved reviewer registration: %v", err)
	}
	// 7. Its reservation ends; the coder's waiting work is reserved next.
	h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{"coder"}) {
		t.Fatalf("reservations = %v, want [coder]", got)
	}
	// 8. The new reviewer keeps its slot until it has acted, then gives it up.
	if h.surplus("code-reviewer-1") {
		t.Fatal("reviewer gives up its slot before doing any work")
	}
	h.act("code-reviewer-1", "task-review", models.TaskEventName("review_claimed"), nil)
	if !h.retire("code-reviewer-1") {
		t.Fatal("reviewer kept its slot after acting while coding work is reserved")
	}
}

// S1-merge: a reviewer that only merges an ownerless approved task, without a
// review claim, hands the slot back once the merge unblocks coding.
func TestRunCap_S1Merge_MergeOnlyHandoff(t *testing.T) {
	now := time.Now().UTC()
	approved := approvedByDeparted("task-approved", now.Add(-time.Hour))
	dependent := taskWithHistory("task-code", models.TaskStatusReady, now.Add(-time.Hour))
	dependent.DependsOn = []string{approved.ID}
	h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"}, approved, dependent))
	h.act("coder-1", "task-approved", models.TaskEventName("note"), nil)

	h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{"code-reviewer"}) {
		t.Fatalf("reservations = %v, want the merge reserved for code-reviewer", got)
	}
	if !h.retire("coder-1") {
		t.Fatal("coder-1 did not give up its slot for the merge")
	}
	h.tick()
	if !slices.Equal(h.roles, []string{"code-reviewer"}) {
		t.Fatalf("started %v, want one code-reviewer", h.roles)
	}
	if err := h.register("code-reviewer", "code-reviewer-2", h.spawned[0]); err != nil {
		t.Fatalf("reviewer registration: %v", err)
	}
	h.act("code-reviewer-2", "task-approved", models.TaskEventMerged, func(task *models.Task) { task.Status = models.TaskStatusMerged })

	h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{"coder"}) {
		t.Fatalf("reservations = %v, want [coder] once the merge unblocked coding", got)
	}
	if !h.retire("code-reviewer-2") {
		t.Fatal("merge-only reviewer kept the slot")
	}
	h.tick()
	if !slices.Equal(h.roles, []string{"code-reviewer", "coder"}) {
		t.Fatalf("started %v, want the coder started after the handoff", h.roles)
	}
}

// S2: two waiting roles are reserved in wait-age order and funded one
// boundary at a time; no third coder gives up a slot.
func TestRunCap_S2_MultipleReservations(t *testing.T) {
	now := time.Now().UTC()
	coders := map[string]string{"coder-1": "coder", "coder-2": "coder", "coder-3": "coder", "coder-4": "coder"}
	h := newRunCapHarness(t, runCapState(5, coders,
		taskWithHistory("task-review", models.TaskStatusReadyForReview, now.Add(-time.Hour)),
		taskWithHistory("task-plan", models.TaskStatusDraftCodingPlan, now.Add(-2*time.Hour))))
	planner, err := h.resolver.DoerRole("code-planning-pair")
	if err != nil {
		t.Fatalf("planner role: %v", err)
	}
	for id := range coders {
		h.act(id, "task-review", models.TaskEventName("note"), nil)
	}

	h.tick()
	if got := h.reservations(); !slices.Equal(got, []string{planner, "code-reviewer"}) {
		t.Fatalf("reservations = %v, want [%s code-reviewer] by wait age", got, planner)
	}
	retired := 0
	for _, id := range []string{"coder-1", "coder-2", "coder-3", "coder-4"} {
		if h.retire(id) {
			retired++
		}
	}
	if retired != 2 {
		t.Fatalf("%d coders gave up a slot, want 2", retired)
	}
	h.tick()
	if !slices.Equal(h.roles, []string{planner, "code-reviewer"}) {
		t.Fatalf("started %v, want %s then code-reviewer", h.roles, planner)
	}
}

// S5: a full run starts nothing through manual repair or its dry run, though
// demand exists before the run budget.
func TestRunCap_S5_ManualRepairRespectsFullRun(t *testing.T) {
	now := time.Now().UTC()
	h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"},
		taskWithHistory("task-review", models.TaskStatusReadyForReview, now)))
	for _, dryRun := range []bool{true, false} {
		result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: h.root, DryRun: dryRun})
		if err != nil {
			t.Fatalf("RepairAgentPool(dry-run %v): %v", dryRun, err)
		}
		if len(result.Missing) != 0 || len(result.Commands) != 0 || len(h.roles) != 0 {
			t.Fatalf("dry-run %v: missing %v, commands %v, started %v; want nothing", dryRun, result.Missing, result.Commands, h.roles)
		}
		if result.RunBudget.Budget != 1 || result.RunBudget.Occupied != 1 || !slices.Equal(result.RunBudget.HeldRoles, []string{"code-reviewer"}) {
			t.Fatalf("run budget = %+v, want budget 1, occupied 1, code-reviewer held", result.RunBudget)
		}
		human := captureStdout(t, func() { printRepairAgentPoolResult(result) })
		if strings.Contains(human, "No role needs more agents") ||
			!strings.Contains(human, "Held by the run-wide cap (max_agents 2: 1 of 1 non-orchestrator slot(s) occupied, 0 pending start(s)):\n  code-reviewer\n") {
			t.Fatalf("human output does not explain the held role:\n%s", human)
		}
	}
}

// S6: a free budget of two across three due roles starts exactly two, by wait
// age; the held role is not put in backoff and starts on the tick a slot
// frees. Every role already holds a busy slot, so no reservation is involved.
func TestRunCap_S6_PartialBudgetAndNoBackoffForHeldRoles(t *testing.T) {
	now := time.Now().UTC()
	h := newRunCapHarness(t, runCapState(6, map[string]string{"coder-9": "coder", "code-reviewer-9": "code-reviewer", "code-planner-9": "code-planner"},
		taskWithHistory("task-code-a", models.TaskStatusReady, now.Add(-3*time.Hour)),
		taskWithHistory("task-code-b", models.TaskStatusReady, now.Add(-3*time.Hour)),
		taskWithHistory("task-review", models.TaskStatusReadyForReview, now.Add(-2*time.Hour)),
		taskWithHistory("task-plan", models.TaskStatusDraftCodingPlan, now.Add(-time.Hour))))
	if planner, _ := h.resolver.DoerRole("code-planning-pair"); planner != "code-planner" {
		t.Fatalf("pipeline planner role is %q, want code-planner", planner)
	}
	if err := h.bb.Modify(func(s *models.State) error {
		for _, id := range []string{"coder-9", "code-reviewer-9", "code-planner-9"} {
			agent := s.Agents[id]
			agent.CurrentTask = testhelpers.StringPtr("elsewhere")
			s.Agents[id] = agent
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	h.tick()
	if !slices.Equal(h.roles, []string{"coder", "coder"}) {
		t.Fatalf("started %v, want the two coders of the oldest work and nothing else", h.roles)
	}
	if got := h.reservations(); len(got) != 0 {
		t.Fatalf("reservations = %v, want none while every role holds a slot", got)
	}
	if _, stamped := h.cache[autoRepairAgentPoolCachePrefix+"code-reviewer"]; stamped {
		t.Fatal("a role the run budget held back was put in backoff")
	}
	// A slot frees (the cap rises by one); the pending coders cover their
	// work, so the reviewer, next by wait age, starts on the next tick.
	if err := h.bb.Modify(func(s *models.State) error { s.Config.MaxAgents = 7; return nil }); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if !slices.Equal(h.roles, []string{"coder", "coder", "code-reviewer"}) {
		t.Fatalf("started %v, want code-reviewer third and no planner", h.roles)
	}
}

// S4: a reservation ends when its role is suppressed or its demand goes, and
// an unchanged list is not rewritten; a refresh happens only past half-life.
func TestRunCap_S4_ReservationReleaseAndRefresh(t *testing.T) {
	now := time.Now().UTC()
	review := taskWithHistory("task-review", models.TaskStatusReadyForReview, now)

	t.Run("suppression releases and frees admission", func(t *testing.T) {
		h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"}, review))
		h.tick()
		if got := h.reservations(); !slices.Equal(got, []string{"code-reviewer"}) {
			t.Fatalf("reservations = %v, want [code-reviewer]", got)
		}
		for range AutoRepairAgentPoolMaxStarts {
			recordAutoRepairFailedStart(h.cache, "code-reviewer", now)
		}
		h.tick()
		if got := h.reservations(); len(got) != 0 {
			t.Fatalf("reservations = %v, want none once the role is suppressed", got)
		}
		if h.surplus("coder-1") {
			t.Fatal("coder still asked to give up its slot for a suppressed role")
		}
	})
	t.Run("demand gone releases", func(t *testing.T) {
		h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"}, review))
		h.tick()
		if err := h.bb.Modify(func(s *models.State) error { s.Tasks = nil; return nil }); err != nil {
			t.Fatal(err)
		}
		h.tick()
		if got := h.reservations(); len(got) != 0 {
			t.Fatalf("reservations = %v, want none once the demand is gone", got)
		}
	})
	t.Run("written on change, refreshed past half-life", func(t *testing.T) {
		h := newRunCapHarness(t, runCapState(2, map[string]string{"coder-1": "coder"}, review))
		h.tick()
		first, sequence := h.state().PoolReservations, h.state().MutationSequence
		h.tick()
		if again := h.state(); again.MutationSequence != sequence || !slices.Equal(again.PoolReservations, first) {
			t.Fatalf("an unchanged reservation was rewritten: sequence %d -> %d, %v -> %v", sequence, again.MutationSequence, first, again.PoolReservations)
		}
		if err := h.bb.Modify(func(s *models.State) error {
			s.PoolReservations[0].Until = time.Now().UTC().Add(PoolReservationTTL/2 - time.Second)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		h.tick()
		refreshed := h.state().PoolReservations
		if len(refreshed) != 1 || refreshed[0].Until.Sub(time.Now().UTC()) < PoolReservationTTL-time.Minute || !refreshed[0].SelectedAt.Equal(first[0].SelectedAt) {
			t.Fatalf("reservation = %+v, want its expiry refreshed and its selection time kept", refreshed)
		}
	})
}
