package db_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func terminalArchiveTransactionState(tasks int) *models.State {
	state := testhelpers.CreateValidState()
	created := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	reason := strings.Repeat("reviewed terminal evidence ", 40)
	commit := strings.Repeat("a", 40)
	for i := range tasks {
		task := models.Task{
			ID: fmt.Sprintf("fixture-%03d", i), Type: models.TaskTypeCoding, RolePair: "coding-pair",
			Status: models.TaskStatusMerged, Created: created, ReviewCommit: &commit,
			Description: "same logical payload in inline and cold fixtures", Scope: "fixture",
			DoneWhen: "fixture preserves logical evidence", SpecRef: "specs/vision.md",
			Lifecycle: &models.TaskLifecycle{Revision: 3, CompletionSequence: 4},
		}
		for h := range 32 {
			task.History = append(task.History, models.TaskHistoryEntry{
				Time: created.Add(time.Duration(h) * time.Minute), Event: models.TaskEventOrchestratorAssessment,
				Reason: &reason, Note: &reason,
			})
		}
		for output := range 4 {
			task.Output = append(task.Output, models.OutputEntry{Desc: fmt.Sprintf("child-%d", output), DoneWhen: "child verified", Scope: "child", SpecRef: "specs/vision.md"})
		}
		for j, operation := range []string{"write-checkpoint", "submit-for-review", "submit-verdict", "wt-merge"} {
			task.Lifecycle.Receipts = append(task.Lifecycle.Receipts, models.LifecycleReceipt{
				LifecycleIdentity: models.LifecycleIdentity{Operation: operation, Actor: "code-reviewer-1", RequestID: fmt.Sprintf("receipt-%d", j), ExpectedTransition: strings.Repeat("b", 64), PayloadDigest: strings.Repeat("c", 64)},
				Sequence:          uint64(j + 1), TransitionID: strings.Repeat("d", 64),
			})
		}
		if i >= tasks*3/4 {
			task.Status = models.TaskStatusReady
			task.History = task.History[:1]
			task.Lifecycle = nil
			task.Output = nil
		}
		state.Tasks = append(state.Tasks, task)
	}
	return state
}

func writeTerminalArchiveTransactionFixture(tb testing.TB, root string, original *models.State, archived bool) string {
	tb.Helper()
	p := paths.New(root)
	if _, err := os.Stat(p.StatePath()); !os.IsNotExist(err) {
		tb.Fatalf("refuse to overwrite existing benchmark state: %s (%v)", p.StatePath(), err)
	}
	if err := os.MkdirAll(filepath.Dir(p.StatePath()), 0o755); err != nil {
		tb.Fatal(err)
	}
	state := db.CloneState(original)
	bb := db.New(p.StatePath())
	if err := bb.Write(state); err != nil {
		tb.Fatal(err)
	}
	if archived {
		if err := bb.Modify(func(state *models.State) error {
			state.Config.TerminalTaskArchival = true
			for i := range state.Tasks {
				if state.Tasks[i].Status.IsTerminal() {
					if _, err := bb.ArchiveTerminalTask(&state.Tasks[i], state.Tasks[i].Created.Add(time.Hour)); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			tb.Fatal(err)
		}
	}
	return p.StatePath()
}

// TestTerminalArchiveBenchmarkFixtures exports identical logical fixtures for
// an actual built-CLI cold-process measurement. It is explicit test harness
// setup, never a production benchmark command or a live-run operation.
func TestTerminalArchiveBenchmarkFixtures(t *testing.T) {
	root := os.Getenv("D41C_BENCH_FIXTURE_ROOT")
	if root == "" {
		t.Skip("set D41C_BENCH_FIXTURE_ROOT to provision actual CLI benchmark fixtures")
	}
	original := terminalArchiveTransactionState(64)
	for _, layout := range []string{"inline", "cold"} {
		path := writeTerminalArchiveTransactionFixture(t, filepath.Join(root, layout), original, layout == "cold")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s fixture: %s (%d YAML bytes)", layout, path, len(data))
	}
}

// BenchmarkTerminalArchiveTransaction measures the complete read-modify-fsync-
// publish transaction, not a serializer or state-size proxy. Cold instances
// include object parsing; warm instances retain only the bounded object cache.
// The 256-task layout's archived objects cost more than the former 16 MiB
// cache limit, which every warm read used to re-decode in full.
// This in-process benchmark is supplemented by the actual CLI fixtures above.
func BenchmarkTerminalArchiveTransaction(b *testing.B) {
	layouts := []struct {
		name     string
		tasks    int
		archived bool
	}{{"inline", 64, false}, {"archived", 64, true}, {"archived-256", 256, true}}
	for _, layout := range layouts {
		original := terminalArchiveTransactionState(layout.tasks)
		for _, cache := range []string{"cold-instance", "warm"} {
			b.Run(layout.name+"/"+cache, func(b *testing.B) {
				path := writeTerminalArchiveTransactionFixture(b, b.TempDir(), original, layout.archived)
				data, err := os.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				warm := db.New(path)
				if _, err := warm.Read(); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					bb := warm
					if cache == "cold-instance" {
						bb = db.New(path)
					}
					state, err := bb.Read()
					if err != nil || len(state.Tasks[0].History) != 32 {
						b.Fatalf("read lost logical evidence: %v", err)
					}
					if err := bb.Modify(func(state *models.State) error {
						// Every sample must actually publish, including after a
						// preceding sample populated all caches and state fields.
						if state.Goal.Description == "complete transaction benchmark A" {
							state.Goal.Description = "complete transaction benchmark B"
						} else {
							state.Goal.Description = "complete transaction benchmark A"
						}
						return nil
					}); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(len(data)), "yaml_bytes")
			})
		}
	}
}
