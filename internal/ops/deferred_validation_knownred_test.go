package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// knownRedFixture is a command A that failed once at failedAt, attributed to
// culprit task-2 with its discovery recorded, beside a command B that passes.
type knownRedFixture struct {
	*dvFixture
	a, b         models.DeferredValidationCommand
	runsA, runsB func() int
	failedAt     string
}

func newKnownRedFixture(t *testing.T) *knownRedFixture {
	t.Helper()
	f := &knownRedFixture{dvFixture: newDVFixture(t, map[string]string{"value.txt": "ok\n"})}
	f.a, f.runsA = dvCounted(t, dvCheckValue)
	f.b, f.runsB = dvCounted(t, "true")
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, f.a, f.b)
	f.failedAt = f.merge("task-2", map[string]string{"value.txt": "broken\n"})
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchFailed || len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-2" {
		t.Fatalf("setup batch = %#v, want A failed with culprit task-2", batch)
	}
	return f
}

// addTask records a task in status, superseded by successors when given.
func (f *knownRedFixture) addTask(id string, status models.TaskStatus, successors ...string) {
	f.t.Helper()
	f.modify(func(state *models.State) {
		task := testhelpers.BuildTaskByStatus(id, status, time.Now().UTC())
		task.SupersededBy = successors
		state.Tasks = append(state.Tasks, task)
	})
}

// convert sets the disposition of culprit's discovery for A.
func (f *knownRedFixture) convert(culprit, disposition string) {
	f.t.Helper()
	f.modify(func(state *models.State) {
		for i := range state.Discovered {
			if state.Discovered[i].Extra["culprit_task"] == culprit && state.Discovered[i].Extra["command_sha256"] == f.a.CommandSHA256 {
				state.Discovered[i].ConvertedToTask = &disposition
				return
			}
		}
		f.t.Fatalf("no discovery for culprit %s", culprit)
	})
}

// requireKnownRed asserts batch skipped A as known red owned by owner, from
// the failure at failedAt, and still executed B.
func (f *knownRedFixture) requireKnownRed(batch models.DeferredValidationBatch, owner string, runsA int) {
	f.t.Helper()
	got := batch.Results[0]
	if got.Status != models.DeferredValidationResultKnownRed || got.OwnedBy != owner || got.CarriedFrom != f.failedAt {
		f.t.Fatalf("A = %#v, want known_red owned by %s from %s", got, owner, f.failedAt)
	}
	requireResult(f.t, batch, 1, models.DeferredValidationResultPassed, "")
	if batch.Status != models.DeferredValidationBatchFailed || len(batch.Culprits) != 0 {
		f.t.Fatalf("batch = %#v, want failed with no new culprit", batch)
	}
	if f.runsA() != runsA {
		f.t.Fatalf("a known-red command executed or was bisected: %d runs, want %d", f.runsA(), runsA)
	}
}

// requireExecuted asserts batch ran and bisected A again.
func (f *knownRedFixture) requireExecuted(batch models.DeferredValidationBatch, runsA int) {
	f.t.Helper()
	if got := batch.Results[0].Status; got != models.DeferredValidationResultFailed {
		f.t.Fatalf("A = %s, want executed and failed (batch %#v)", got, batch)
	}
	if f.runsA() <= runsA+1 {
		f.t.Fatalf("A was not run and bisected: %d runs after %d", f.runsA(), runsA)
	}
}

// A command whose attributed failure has a fix in flight is neither run nor
// bisected at later HEADs; the batch still fails and the gate names the fix
// without counting it as a culprit (D-181).
func TestDeferredValidationKnownRedWithLiveFixIsNotRun(t *testing.T) {
	t.Parallel()
	f := newKnownRedFixture(t)
	f.addTask("fix-1", models.TaskStatusReady)
	f.convert("task-2", "fix-1")
	runsA := f.runsA()

	head := f.merge("task-3", map[string]string{"c.txt": "c\n"})
	f.requireKnownRed(onlyBatch(t, f.run(false)), "fix-1", runsA)
	state, _ := f.bb.Read()
	if len(state.Discovered) != 1 {
		t.Fatalf("a known-red skip recorded a discovery: %#v", state.Discovered)
	}
	_, blocked := deferredValidationGate(state, head)
	if blocked == nil || blocked.Code != string(integrationProgressBlockedDeferredFailed) || len(blocked.TaskIDs) != 0 || !strings.Contains(blocked.Guidance, "fix-1") {
		t.Fatalf("gate = %#v, want failed naming fix-1 only in guidance", blocked)
	}
	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "failed_at_head" {
		t.Fatalf("same HEAD after a known-red batch: %#v", again)
	}

	// The same attribution, deduplicated to the same discovery, still holds
	// at the next HEAD.
	f.merge("task-4", map[string]string{"d.txt": "d\n"})
	f.requireKnownRed(onlyBatch(t, f.run(false)), "fix-1", runsA)
}

func TestDeferredValidationKnownRedRunsWithoutALiveFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*knownRedFixture)
		force bool
	}{
		{name: "fix merged", setup: func(f *knownRedFixture) { f.addTask("fix-1", models.TaskStatusMerged); f.convert("task-2", "fix-1") }},
		{name: "fix abandoned", setup: func(f *knownRedFixture) { f.addTask("fix-1", models.TaskStatusAbandoned); f.convert("task-2", "fix-1") }},
		{name: "deferred disposition", setup: func(f *knownRedFixture) { f.convert("task-2", "deferred") }},
		{name: "dismissed disposition", setup: func(f *knownRedFixture) { f.convert("task-2", "dismissed") }},
		{name: "no disposition", setup: func(*knownRedFixture) {}},
		{name: "forced", setup: func(f *knownRedFixture) { f.addTask("fix-1", models.TaskStatusReady); f.convert("task-2", "fix-1") }, force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newKnownRedFixture(t)
			tc.setup(f)
			runsA := f.runsA()
			f.merge("task-3", map[string]string{"c.txt": "c\n"})
			f.requireExecuted(onlyBatch(t, f.run(tc.force)), runsA)
		})
	}
}

// A superseded fix hands ownership to its live replacement.
func TestDeferredValidationKnownRedFollowsSupersededFix(t *testing.T) {
	t.Parallel()
	f := newKnownRedFixture(t)
	f.addTask("fix-2", models.TaskStatusReady)
	f.addTask("fix-1", models.TaskStatusSuperseded, "fix-2")
	f.convert("task-2", "fix-1")
	runsA := f.runsA()
	f.merge("task-3", map[string]string{"c.txt": "c\n"})
	f.requireKnownRed(onlyBatch(t, f.run(false)), "fix-2", runsA)
}

// Ownership binds to the culprit of the newest executed failure: a live fix
// for an older culprit never suppresses a newer, differently attributed one.
func TestDeferredValidationKnownRedBindsToTheNewestCulprit(t *testing.T) {
	t.Parallel()
	f := newKnownRedFixture(t)
	f.addTask("fix-1", models.TaskStatusReady)
	f.convert("task-2", "fix-1")

	// Another merge repairs A (a forced batch observes the pass), then a new
	// culprit breaks it again: fail (task-2), pass, fail (task-4).
	f.merge("task-3", map[string]string{"value.txt": "ok\n"})
	if batch := onlyBatch(t, f.run(true)); batch.Results[0].Status != models.DeferredValidationResultPassed {
		t.Fatalf("forced batch = %#v, want A passed", batch)
	}
	f.merge("task-4", map[string]string{"value.txt": "broken\n"})
	batch := onlyBatch(t, f.run(false))
	if batch.Results[0].Status != models.DeferredValidationResultFailed || len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-4" {
		t.Fatalf("batch = %#v, want A run and attributed to task-4 while fix-1 is live", batch)
	}
	runsA := f.runsA()
	f.merge("task-5", map[string]string{"c.txt": "c\n"})
	f.requireExecuted(onlyBatch(t, f.run(false)), runsA)
}

// A newest failure that is unattributed, or attributed ambiguously, runs.
func TestDeferredValidationKnownRedRequiresOneAttributedCulprit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		culprits []models.DeferredValidationCulprit
	}{
		{name: "unattributed", culprits: []models.DeferredValidationCulprit{{Unattributed: true, Reason: "fails_at_baseline"}}},
		{name: "unbisected", culprits: nil},
		{name: "ambiguous", culprits: []models.DeferredValidationCulprit{{TaskID: "task-2"}, {TaskID: "task-9"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newKnownRedFixture(t)
			f.addTask("fix-1", models.TaskStatusReady)
			f.convert("task-2", "fix-1")
			newer := f.merge("task-3", map[string]string{"c.txt": "c\n"})
			f.modify(func(state *models.State) {
				now := time.Now().UTC()
				culprits := make([]models.DeferredValidationCulprit, len(tc.culprits))
				for i, culprit := range tc.culprits {
					culprit.CommandSHA256 = f.a.CommandSHA256
					culprits[i] = culprit
				}
				state.Goal.Integration.DeferredValidation.AppendBatch(models.DeferredValidationBatch{
					Commit: newer, StartedAt: now, FinishedAt: now, Status: models.DeferredValidationBatchFailed, Culprits: culprits,
					Results: []models.DeferredValidationResult{
						{Command: f.a.Command, CommandSHA256: f.a.CommandSHA256, Status: models.DeferredValidationResultFailed, ExitCode: 1},
						{Command: f.b.Command, CommandSHA256: f.b.CommandSHA256, Status: models.DeferredValidationResultPassed},
					},
				})
			})
			runsA := f.runsA()
			f.merge("task-5", map[string]string{"d.txt": "d\n"})
			f.requireExecuted(onlyBatch(t, f.run(false)), runsA)
		})
	}
}
