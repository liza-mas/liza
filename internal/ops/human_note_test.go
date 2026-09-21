package ops

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupHumanNoteTest(t *testing.T) (string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	now := time.Now().UTC().Add(-time.Hour)
	state.Tasks = nil
	for _, id := range []string{"target", "unrelated"} {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, now)
		task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventOrchestratorAssessment, Agent: testhelpers.StringPtr("orchestrator-1")})
		state.Tasks = append(state.Tasks, task)
	}
	state.HumanNotes = []models.HumanNote{{Timestamp: now.Add(-time.Hour), For: "all", Message: "Prior input"}}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		lastOrchestratorAssessment(task).Extra = map[string]any{
			AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(state, task, AssessmentFingerprintCandidate{
				Reason: *task.BlockedReason, Questions: task.BlockedQuestions,
			}),
		}
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return root, db.For(statePath)
}

func TestAddHumanNoteWakesOnlyTargetsAndPreservesState(t *testing.T) {
	for _, target := range []string{"target", "all"} {
		t.Run(target, func(t *testing.T) {
			root, bb := setupHumanNoteTest(t)
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if CountActionableBlockedTasks(before) != 0 {
				t.Fatal("fixture must be assessed")
			}
			message := "Reviewed recovery evidence is available at docs/recovery.md.\n"
			result, err := AddHumanNote(root, target, message)
			if err != nil {
				t.Fatal(err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if target == "all" {
				wantCount = 2
			}
			if got := CountActionableBlockedTasks(after); got != wantCount {
				t.Fatalf("actionable = %d, want %d", got, wantCount)
			}
			if target != "all" && isTaskActionableSinceAssessment(after.FindTask("unrelated"), after) {
				t.Fatal("unrelated task woke")
			}
			if len(after.HumanNotes) != len(before.HumanNotes)+1 {
				t.Fatal("existing notes replaced")
			}
			note := after.HumanNotes[len(after.HumanNotes)-1]
			if note.Message != message || note.For != target || !note.Timestamp.Equal(result.Timestamp) || result.Bytes != len(message) || len(result.Warnings) != 0 {
				t.Fatal("note/result mismatch")
			}
			if note.Extra["source"] != "operator_cli" || note.Extra["operation"] != "add-human-note" {
				t.Fatal("missing operator provenance")
			}
			after.HumanNotes = after.HumanNotes[:len(after.HumanNotes)-1]
			if !reflect.DeepEqual(before, after) {
				t.Fatal("note changed unrelated state, task metadata or prior notes")
			}
			entries, err := log.New(paths.New(root).LogPath()).Read()
			if err != nil || len(entries) != 1 {
				t.Fatalf("activity log: %v, entries=%d", err, len(entries))
			}
			if entries[0].Action != "human_note_added" || entries[0].Agent != "operator" || strings.Contains(entries[0].Detail, message) {
				t.Fatal("incorrect or content-bearing activity event")
			}
		})
	}
}

func TestAddHumanNoteInvalidInputLeavesStateUnchanged(t *testing.T) {
	for _, tc := range []struct{ name, target, note, want string }{
		{"empty target", " ", "note", "target is required"},
		{"missing target", "missing", "note", "not found"},
		{"empty note", "target", " \n\t", "must not be empty"},
		{"oversize", "target", strings.Repeat("x", statehygiene.MaxStateTextBytes+1), "exceeds"},
		{"invalid utf8", "target", string([]byte{0xff}), "UTF-8"},
		{"transcript", "target", `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"raw"}}`, "transcript"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := setupHumanNoteTest(t)
			lp := paths.New(root)
			before, err := os.ReadFile(lp.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			result, err := AddHumanNote(root, tc.target, tc.note)
			if err == nil || result != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("result=%v err=%v; want %s", result, err, tc.want)
			}
			after, err := os.ReadFile(lp.StatePath())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("invalid input changed state")
			}
			if _, err := os.Stat(lp.LogPath()); !os.IsNotExist(err) {
				t.Fatal("invalid input wrote activity log")
			}
		})
	}
}

func TestAddHumanNoteConcurrentAppendPreservesNotes(t *testing.T) {
	root, bb := setupHumanNoteTest(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AddHumanNote(root, "target", "Independent operator input"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.HumanNotes) != 5 {
		t.Fatalf("lost append: got %d notes", len(state.HumanNotes))
	}
}

func TestAddHumanNoteLogFailureRemainsCommitted(t *testing.T) {
	root, bb := setupHumanNoteTest(t)
	if err := os.Mkdir(paths.New(root).LogPath(), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := AddHumanNote(root, "target", strings.Repeat("x", statehygiene.MaxStateTextBytes))
	if err != nil || result == nil || len(result.Warnings) != 1 {
		t.Fatalf("result=%v error=%v", result, err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.HumanNotes) != 2 || len(state.HumanNotes[1].Message) != statehygiene.MaxStateTextBytes {
		t.Fatal("committed note missing")
	}
}
