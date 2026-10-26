package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// dvCounted wraps check so that every execution appends a line to a counter
// outside the repository: a carried-forward result leaves the count unchanged.
func dvCounted(t *testing.T, check string) (models.DeferredValidationCommand, func() int) {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "runs")
	runs := func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}
	return dvCommand(fmt.Sprintf("echo run >> %q; %s", counter, check), 60), runs
}

func (f *dvFixture) deferredConfig(config *models.DeferredValidationConfig) {
	f.t.Helper()
	f.modify(func(state *models.State) { state.Config.DeferredValidation = config })
}

func requireResult(t *testing.T, batch models.DeferredValidationBatch, i int, status models.DeferredValidationResultStatus, carriedFrom string) {
	t.Helper()
	if len(batch.Results) <= i || batch.Results[i].Status != status || batch.Results[i].CarriedFrom != carriedFrom {
		t.Fatalf("result %d: want %s carried from %q, got batch %#v", i, status, carriedFrom, batch)
	}
}

func TestDeferredValidationInertMoveCarriesPassForward(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		config  *models.DeferredValidationConfig
		carried bool
	}{
		{name: "inert path carries", config: &models.DeferredValidationConfig{InertPaths: []string{"docs/**"}}, carried: true},
		{name: "no policy executes", config: nil, carried: false},
		{name: "pathspec magic is refused and executes", config: &models.DeferredValidationConfig{InertPaths: []string{":(top)docs"}}, carried: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
			f.deferredConfig(tc.config)
			check, runs := dvCounted(t, dvCheckValue)
			pass := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
			requireResult(t, onlyBatch(t, f.run(false)), 0, models.DeferredValidationResultPassed, "")

			head := f.merge("task-2", map[string]string{"docs/notes/guide.md": "words\n"})
			batch := onlyBatch(t, f.run(false))
			if batch.Status != models.DeferredValidationBatchPassed || batch.Commit != head {
				t.Fatalf("batch = %#v", batch)
			}
			if tc.carried {
				requireResult(t, batch, 0, models.DeferredValidationResultCarriedForward, pass)
				if runs() != 1 {
					t.Fatalf("a carried command executed: %d runs", runs())
				}
			} else {
				requireResult(t, batch, 0, models.DeferredValidationResultPassed, "")
				if runs() != 2 {
					t.Fatalf("command must execute again: %d runs", runs())
				}
			}
			state, _ := f.bb.Read()
			if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
				t.Fatalf("gate at the new HEAD: waiting=%v blocked=%v", waiting, blocked)
			}
		})
	}
}

func TestDeferredValidationScopeCarriesOnlyUnchangedCommands(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"a/value.txt": "ok\n", "b/value.txt": "ok\n"})
	checkA, runsA := dvCounted(t, `test "$(cat a/value.txt)" = ok`)
	checkB, runsB := dvCounted(t, `test "$(cat b/value.txt)" = ok`)
	f.deferredConfig(&models.DeferredValidationConfig{Scopes: []models.DeferredValidationScope{
		{Command: checkA.Command, Paths: []string{"a/**"}},
		{Command: checkB.Command, Paths: []string{"b/**"}},
	}})
	pass := f.merge("task-1", map[string]string{"c.txt": "c\n"}, checkA, checkB)
	onlyBatch(t, f.run(false))

	f.merge("task-2", map[string]string{"b/value.txt": "broken\n"})
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchFailed {
		t.Fatalf("batch = %#v", batch)
	}
	requireResult(t, batch, 0, models.DeferredValidationResultCarriedForward, pass)
	requireResult(t, batch, 1, models.DeferredValidationResultFailed, "")
	if runsA() != 1 || runsB() < 2 {
		t.Fatalf("runs: a=%d (want 1) b=%d (want >=2)", runsA(), runsB())
	}
}

func TestDeferredValidationRenameOutOfScopeExecutes(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"lib/value.txt": "ok\n"})
	check, runs := dvCounted(t, "true")
	f.deferredConfig(&models.DeferredValidationConfig{Scopes: []models.DeferredValidationScope{{Command: check.Command, Paths: []string{"lib/**"}}}})
	f.merge("task-1", map[string]string{"c.txt": "c\n"}, check)
	onlyBatch(t, f.run(false))

	testhelpers.MustGit(t, f.root, "mv", "lib/value.txt", "moved.txt")
	f.merge("task-2", map[string]string{})
	requireResult(t, onlyBatch(t, f.run(false)), 0, models.DeferredValidationResultPassed, "")
	if runs() != 2 {
		t.Fatalf("a move out of scope must execute the command: %d runs", runs())
	}
}

// Carry-forward never reuses a pass over a newer executed failure, nor a pass
// from a batch whose processes could not be shown to have ended.
func TestDeferredValidationCarryRequiresCertifiedLatestExecution(t *testing.T) {
	t.Parallel()
	t.Run("newer failure", func(t *testing.T) {
		t.Parallel()
		flag := filepath.Join(t.TempDir(), "fail")
		f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
		f.deferredConfig(&models.DeferredValidationConfig{InertPaths: []string{"docs/**"}})
		check, runs := dvCounted(t, fmt.Sprintf("test ! -f %q", flag))
		f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
		onlyBatch(t, f.run(false))
		if err := os.WriteFile(flag, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		f.merge("task-2", map[string]string{"docs/one.md": "1\n"})
		requireResult(t, onlyBatch(t, f.run(true)), 0, models.DeferredValidationResultFailed, "")
		if err := os.Remove(flag); err != nil {
			t.Fatal(err)
		}
		before := runs()
		f.merge("task-3", map[string]string{"docs/two.md": "2\n"})
		requireResult(t, onlyBatch(t, f.run(false)), 0, models.DeferredValidationResultPassed, "")
		if runs() != before+1 {
			t.Fatalf("the command must execute after a newer failure: %d runs, want %d", runs(), before+1)
		}
	})
	t.Run("uncertified pass", func(t *testing.T) {
		t.Parallel()
		f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
		f.deferredConfig(&models.DeferredValidationConfig{InertPaths: []string{"docs/**"}})
		check, runs := dvCounted(t, dvCheckValue)
		pass := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
		onlyBatch(t, f.run(false))
		f.modify(func(state *models.State) {
			now := time.Now().UTC()
			state.Goal.Integration.DeferredValidation.AppendBatch(models.DeferredValidationBatch{
				Commit: pass, StartedAt: now, FinishedAt: now, Status: models.DeferredValidationBatchAborted, Reason: string(deferredValidationReasonUnstoppable),
				Results: []models.DeferredValidationResult{{Command: check.Command, CommandSHA256: check.CommandSHA256, Status: models.DeferredValidationResultPassed}},
			})
		})
		f.merge("task-2", map[string]string{"docs/one.md": "1\n"})
		requireResult(t, onlyBatch(t, f.run(false)), 0, models.DeferredValidationResultPassed, "")
		if runs() != 2 {
			t.Fatalf("a pass from an unstoppable batch must not be carried: %d runs", runs())
		}
	})
}

// D-223 item 3: a later batch at the same HEAD re-runs only what has not
// passed there.
func TestDeferredValidationSameHEADRetryRunsOnlyUnpassedCommands(t *testing.T) {
	t.Parallel()
	once := filepath.Join(t.TempDir(), "dirtied")
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check, runs := dvCounted(t, dvCheckValue)
	dirtyOnce := dvCommand(fmt.Sprintf("if [ ! -f %q ]; then touch %q; echo x >> value.txt; fi", once, once), 60)
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check, dirtyOnce)

	first := onlyBatch(t, f.run(false))
	if first.Status != models.DeferredValidationBatchInfraFailed {
		t.Fatalf("first batch = %#v", first)
	}
	second := onlyBatch(t, f.run(false))
	if second.Status != models.DeferredValidationBatchPassed || second.Commit != head {
		t.Fatalf("second batch = %#v", second)
	}
	requireResult(t, second, 0, models.DeferredValidationResultCarriedForward, head)
	requireResult(t, second, 1, models.DeferredValidationResultPassed, "")
	if runs() != 1 {
		t.Fatalf("a command passed at this HEAD ran again: %d runs", runs())
	}
}

// An over-narrow scope admits a regression; editing the scope alone does not
// reopen the recorded batch, and run --force re-executes and closes the gate.
func TestDeferredValidationForceRecoversFromWrongScope(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n", "lib/x.txt": "x\n"})
	check, runs := dvCounted(t, dvCheckValue)
	f.deferredConfig(&models.DeferredValidationConfig{Scopes: []models.DeferredValidationScope{{Command: check.Command, Paths: []string{"lib/**"}}}})
	pass := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
	onlyBatch(t, f.run(false))

	head := f.merge("task-2", map[string]string{"value.txt": "broken\n"})
	requireResult(t, onlyBatch(t, f.run(false)), 0, models.DeferredValidationResultCarriedForward, pass)
	state, _ := f.bb.Read()
	if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
		t.Fatalf("carried HEAD gate: waiting=%v blocked=%v", waiting, blocked)
	}

	f.deferredConfig(nil)
	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "covered" {
		t.Fatalf("a config edit alone must not reopen the recorded batch, got %#v", again)
	}
	forced := onlyBatch(t, f.run(true))
	if !forced.Forced || forced.Status != models.DeferredValidationBatchFailed {
		t.Fatalf("forced batch = %#v", forced)
	}
	requireResult(t, forced, 0, models.DeferredValidationResultFailed, "")
	if runs() < 2 {
		t.Fatalf("the forced batch did not execute the command: %d runs", runs())
	}
	state, _ = f.bb.Read()
	if _, blocked := deferredValidationGate(state, head); blocked == nil || blocked.Code != integrationProgressBlockedDeferredFailed {
		t.Fatalf("gate after the forced failure = %#v", blocked)
	}
	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "failed_at_head" {
		t.Fatalf("after a forced failure, got %#v", again)
	}
}
