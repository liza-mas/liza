package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

// These physical fixtures exercise the approved wire format without depending
// on a new production writer. The inline task is the logical reference value.
type coldTerminalTaskFixture struct {
	bb         *Blackboard
	task       models.Task
	stateBytes []byte
	objectPath string
	digest     string
}

func newColdTerminalTaskFixture(t *testing.T, status models.TaskStatus, archived bool) coldTerminalTaskFixture {
	t.Helper()
	root := t.TempDir()
	p := paths.New(root)
	created := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	reviewCommit := strings.Repeat("a", 40)
	reason := "review evidence survives physical archival"
	task := models.Task{
		ID: "terminal-evidence", Type: models.TaskTypeCoding, RolePair: "coding-pair",
		Status: status, Created: created, Description: "retain a complete logical task",
		DoneWhen: "the downstream output and audit evidence remain readable",
		Scope:    "artifact and history", SpecRef: "specs/evidence.md", ReviewCommit: &reviewCommit,
		History: []models.TaskHistoryEntry{
			{Time: created, Event: models.TaskEventCreated},
			{Time: created.Add(time.Minute), Event: models.TaskEventSubmittedForReview, Reason: &reason, Commit: &reviewCommit},
			{Time: created.Add(2 * time.Minute), Event: string(status)},
		},
		Output:              []models.OutputEntry{{Desc: "downstream child", DoneWhen: "child is verified", Scope: "child", SpecRef: "specs/child.md"}},
		TransitionsExecuted: map[string]bool{"coding-to-integration": true},
		Lifecycle: &models.TaskLifecycle{
			Revision: 7, CompletionSequence: 1,
			Receipts: []models.LifecycleReceipt{{
				LifecycleIdentity: models.LifecycleIdentity{
					Operation: "wt-merge", Actor: "code-reviewer-1", RequestID: "retained-completion",
					ExpectedTransition: strings.Repeat("b", 64), PayloadDigest: strings.Repeat("c", 64),
				},
				Sequence: 1, TransitionID: strings.Repeat("d", 64),
				Projection: models.LifecycleProjection{MergeCommit: strings.Repeat("e", 40)},
			}},
		},
		Extra: map[string]any{"audit_number": "001", "audit_flag": "false"},
	}
	taskYAML, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var physical any = task
	fixture := coldTerminalTaskFixture{bb: New(p.StatePath()), task: task}
	if archived {
		object, err := json.Marshal(map[string]any{
			"format_version": 2, "task_id": task.ID, "field": "terminal_task", "value_yaml": string(taskYAML),
		})
		if err != nil {
			t.Fatal(err)
		}
		object = append(object, '\n')
		sum := sha256.Sum256(object)
		fixture.digest = hex.EncodeToString(sum[:])
		fixture.objectPath = filepath.Join(p.ArchiveDir(), "objects", fixture.digest[:2], fixture.digest+".json")
		if err := os.MkdirAll(filepath.Dir(fixture.objectPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture.objectPath, object, 0o644); err != nil {
			t.Fatal(err)
		}
		physical = map[string]any{
			"id": task.ID, "status": task.Status, "created": task.Created,
			"terminal_archive": map[string]any{"sha256": fixture.digest, "archived_at": created.Add(3 * time.Minute)},
		}
	}
	fixture.stateBytes, err = yaml.Marshal(map[string]any{
		"version": 1, "goal": models.Goal{ID: "archive-goal"}, "agents": map[string]any{},
		"tasks": []any{physical}, "config": map[string]any{"terminal_task_archival": archived},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.StatePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.StatePath(), fixture.stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertColdTerminalTaskRestored(t *testing.T, state *models.State, want models.Task) {
	t.Helper()
	got := state.FindTask(want.ID)
	if got == nil {
		t.Fatal("archival removed the task identity from the graph")
	}
	if !reflect.DeepEqual(got.History, want.History) || !reflect.DeepEqual(got.Output, want.Output) || !reflect.DeepEqual(got.Lifecycle, want.Lifecycle) {
		t.Fatalf("terminal logical evidence was lost: history=%d output=%d lifecycle=%v", len(got.History), len(got.Output), got.Lifecycle != nil)
	}
	if got.Description != want.Description || got.DoneWhen != want.DoneWhen || got.Scope != want.Scope || got.Extra["audit_number"] != "001" || got.Extra["audit_flag"] != "false" {
		t.Fatal("terminal task contents or inline YAML scalar types changed")
	}
	if gotID, wantID := models.TaskTransitionID(got), models.TaskTransitionID(&want); gotID != wantID {
		t.Fatalf("physical archival changed the transition identity: got %s, want %s", gotID, wantID)
	}
}

func TestTerminalArchiveReadsRestoreLogicalEvidence(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusAbandoned, models.TaskStatusSuperseded} {
		for _, archived := range []bool{false, true} {
			layout := "inline"
			if archived {
				layout = "archived"
			}
			t.Run(string(status)+"/"+layout, func(t *testing.T) {
				fixture := newColdTerminalTaskFixture(t, status, archived)
				readers := map[string]func() (*models.State, error){
					"locked": fixture.bb.Read, "snapshot": fixture.bb.ReadSnapshot, "cached": fixture.bb.ReadCached,
				}
				for name, read := range readers {
					t.Run(name, func(t *testing.T) {
						state, err := read()
						if err != nil {
							t.Fatal(err)
						}
						assertColdTerminalTaskRestored(t, state, fixture.task)
					})
				}
			})
		}
	}
}

func TestTerminalArchiveCachedTasksAreIndependent(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
	first, err := fixture.bb.ReadCached()
	if err != nil {
		t.Fatal(err)
	}
	assertColdTerminalTaskRestored(t, first, fixture.task)
	first.Tasks[0].History[0].Event = "caller changed history"
	first.Tasks[0].Output[0].Desc = "caller changed output"
	first.Tasks[0].Lifecycle.Receipts[0].RequestID = "caller changed replay receipt"
	second, err := fixture.bb.ReadCached()
	if err != nil {
		t.Fatal(err)
	}
	assertColdTerminalTaskRestored(t, second, fixture.task)
}

func TestTerminalArchiveMissingOrCorruptEvidenceFailsIncludingWarmCache(t *testing.T) {
	for _, reader := range []string{"locked", "snapshot", "cached"} {
		for _, damage := range []string{"missing", "corrupt"} {
			for _, warm := range []bool{false, true} {
				t.Run(reader+"/"+damage+"/warm="+map[bool]string{false: "false", true: "true"}[warm], func(t *testing.T) {
					fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
					read := fixture.bb.ReadSnapshot
					if reader == "locked" {
						read = fixture.bb.Read
					} else if reader == "cached" {
						read = fixture.bb.ReadCached
					}
					if warm {
						if _, err := read(); err != nil {
							t.Fatalf("warm valid object: %v", err)
						}
					}
					if damage == "missing" {
						if err := os.Remove(fixture.objectPath); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(fixture.objectPath, []byte("corrupt archive evidence\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					_, err := read()
					if err == nil {
						t.Fatal("unreadable archive evidence was silently accepted")
					}
					if !strings.Contains(err.Error(), fixture.task.ID) || !strings.Contains(err.Error(), fixture.digest) || !strings.Contains(err.Error(), fixture.objectPath) {
						t.Fatalf("archive error does not locate the lost evidence: %v", err)
					}
				})
			}
		}
	}
}

func TestTerminalArchiveMutationPreservesColdStateAndOldSnapshot(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
	note := "terminal repair recorded after archival"
	err := fixture.bb.Modify(func(state *models.State) error {
		assertColdTerminalTaskRestored(t, state, fixture.task)
		task := state.FindTask(fixture.task.ID)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: fixture.task.Created.Add(4 * time.Minute), Event: models.TaskEventAcceptanceCommitsRemapped, Note: &note,
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	physical, err := os.ReadFile(fixture.bb.GetStatePath())
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := yaml.Unmarshal(physical, &state); err != nil {
		t.Fatal(err)
	}
	row := state["tasks"].([]any)[0].(map[string]any)
	if _, exists := row["history"]; exists {
		t.Fatal("terminal mutation expanded history back into live YAML")
	}
	ref, ok := row["terminal_archive"].(map[string]any)
	if !ok || ref["sha256"] == fixture.digest {
		t.Fatal("changed terminal evidence did not publish a new object reference")
	}
	fresh, err := fixture.bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := fixture.task
	want.History = append(want.History, models.TaskHistoryEntry{
		Time: fixture.task.Created.Add(4 * time.Minute), Event: models.TaskEventAcceptanceCommitsRemapped, Note: &note,
	})
	assertColdTerminalTaskRestored(t, fresh, want)
	// Restore the old published bytes under the same runtime root. Its immutable
	// object must still be available after the newer payload was published.
	if err := os.WriteFile(fixture.bb.GetStatePath(), fixture.stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := New(fixture.bb.GetStatePath()).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	assertColdTerminalTaskRestored(t, old, fixture.task)
}
