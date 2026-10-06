package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/archiveobject"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func TestTerminalArchiveRejectsIncompleteWriteAndAllowsInlineRollback(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
	var incomplete models.State
	if err := yaml.Unmarshal(fixture.stateBytes, &incomplete); err != nil {
		t.Fatal(err)
	}
	if err := fixture.bb.Write(&incomplete); err == nil || !strings.Contains(err.Error(), "not a restored terminal task") {
		t.Fatalf("raw physical stub write = %v", err)
	}
	if got, err := os.ReadFile(fixture.bb.GetStatePath()); err != nil || !bytes.Equal(got, fixture.stateBytes) {
		t.Fatalf("incomplete write changed state: %v", err)
	}
	if err := fixture.bb.Modify(func(state *models.State) error {
		assertColdTerminalTaskRestored(t, state, fixture.task)
		state.Config.TerminalTaskArchival = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixture.bb.GetStatePath())
	if err != nil {
		t.Fatal(err)
	}
	var restored models.State
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	assertColdTerminalTaskRestored(t, &restored, fixture.task)
	if restored.Config.TerminalTaskArchival || restored.Tasks[0].TerminalArchive != nil {
		t.Fatal("inline rollback retained archival mode or physical reference")
	}
	if _, err := os.Stat(fixture.objectPath); err != nil {
		t.Fatalf("rollback removed old evidence: %v", err)
	}
}

func TestTerminalArchiveRejectsMalformedEnvelopeAndMismatchedPayload(t *testing.T) {
	cases := map[string]func(map[string]any){
		"format":        func(o map[string]any) { o["format_version"] = 3 },
		"task":          func(o map[string]any) { o["task_id"] = "other-task" },
		"field":         func(o map[string]any) { o["field"] = "history" },
		"unknown key":   func(o map[string]any) { o["unsupported"] = true },
		"missing value": func(o map[string]any) { delete(o, "value_yaml") },
		"trailing YAML": func(o map[string]any) { o["value_yaml"] = o["value_yaml"].(string) + "\n---\nid: other-task\n" },
		"status": func(o map[string]any) {
			o["value_yaml"] = strings.Replace(o["value_yaml"].(string), "status: MERGED", "status: ABANDONED", 1)
		},
		"created": func(o map[string]any) {
			o["value_yaml"] = strings.Replace(o["value_yaml"].(string), "created: 2026-10-06", "created: 2026-10-07", 1)
		},
		"recursive reference": func(o map[string]any) {
			o["value_yaml"] = o["value_yaml"].(string) + "\nterminal_archive:\n  sha256: " + strings.Repeat("a", 64) + "\n  archived_at: 2026-10-06T08:03:00Z\n"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
			original, err := os.ReadFile(fixture.objectPath)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(original, &object); err != nil {
				t.Fatal(err)
			}
			mutate(object)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			digest := archiveobject.Digest(data)
			path, err := archiveobject.Path(fixture.bb.terminalArchiveDir(), digest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			physical := bytes.ReplaceAll(fixture.stateBytes, []byte(fixture.digest), []byte(digest))
			if err := os.WriteFile(fixture.bb.GetStatePath(), physical, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err = fixture.bb.ReadSnapshot()
			var archiveErr *TerminalArchiveError
			if !errors.As(err, &archiveErr) || archiveErr.TaskID != fixture.task.ID || archiveErr.SHA256 != digest || archiveErr.Path != path {
				t.Fatalf("malformed evidence error = %v", err)
			}
		})
	}
}

func TestTerminalArchiveFailedIdentityCheckCannotPoisonCacheForAnotherTask(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
	original, err := os.ReadFile(fixture.objectPath)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(original, &object); err != nil {
		t.Fatal(err)
	}
	otherID := "another-terminal-task"
	// The envelope belongs to the original task, but the full YAML payload
	// names another task. Both physical identities must refuse this object.
	object["value_yaml"] = strings.Replace(object["value_yaml"].(string), "id: "+fixture.task.ID, "id: "+otherID, 1)
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := archiveobject.Write(fixture.bb.terminalArchiveDir(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	physical := bytes.ReplaceAll(fixture.stateBytes, []byte(fixture.digest), []byte(digest))
	for _, taskID := range []string{fixture.task.ID, otherID} {
		current := bytes.Replace(physical, []byte("id: "+fixture.task.ID), []byte("id: "+taskID), 1)
		if err := os.WriteFile(fixture.bb.GetStatePath(), current, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := fixture.bb.ReadSnapshot() // Reuse the same decoded-object cache.
		var archiveErr *TerminalArchiveError
		if !errors.As(err, &archiveErr) || archiveErr.TaskID != taskID || archiveErr.SHA256 != digest {
			t.Fatalf("mismatched evidence was accepted for %s after the prior read: %v", taskID, err)
		}
	}
}

func TestTerminalArchiveDurabilityFailureLeavesStateAndRetryRepeatsBarrier(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, true)
	previous := terminalArchiveSyncDir
	t.Cleanup(func() { terminalArchiveSyncDir = previous })
	failure := errors.New("terminal archive directory sync failed")
	terminalArchiveSyncDir = func(string) error { return failure }
	// An unchanged published payload needs no new durability barrier.
	if err := fixture.bb.Modify(func(state *models.State) error { state.Goal.Description = "unrelated mutation"; return nil }); err != nil {
		t.Fatalf("unchanged object was reinstalled: %v", err)
	}
	before, err := os.ReadFile(fixture.bb.GetStatePath())
	if err != nil {
		t.Fatal(err)
	}
	change := func(state *models.State) error { state.Tasks[0].Description += " repaired"; return nil }
	if err := fixture.bb.Modify(change); !errors.Is(err, failure) {
		t.Fatalf("object barrier failure = %v", err)
	}
	if after, err := os.ReadFile(fixture.bb.GetStatePath()); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed barrier published changed state: %v", err)
	}
	barriers := 0
	terminalArchiveSyncDir = func(string) error { barriers++; return nil }
	if err := fixture.bb.Modify(change); err != nil {
		t.Fatal(err)
	}
	if barriers != 4 {
		t.Fatalf("retry durability barriers = %d, want complete directory chain", barriers)
	}
	state, err := fixture.bb.ReadSnapshot()
	if err != nil || state.Tasks[0].Description != fixture.task.Description+" repaired" {
		t.Fatalf("retry lost logical mutation: %v", err)
	}
}

func TestTerminalArchiveMutationSequenceBelongsToPublication(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, false)
	state, err := fixture.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	state.MutationSequence = 7
	if err := fixture.bb.Write(state); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.bb.GetStatePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, callback := range []func(*models.State) error{
		func(*models.State) error { return nil },
		func(state *models.State) error { state.MutationSequence = 0; return nil },
	} {
		if err := fixture.bb.Modify(callback); err != nil {
			t.Fatal(err)
		}
		if after, err := os.ReadFile(fixture.bb.GetStatePath()); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("no-op or counter-only callback manufactured a publication: %v", err)
		}
	}
	if err := fixture.bb.Modify(func(state *models.State) error {
		state.MutationSequence = 0
		state.Goal.Description = "actual logical mutation"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, err = fixture.bb.ReadSnapshot()
	if err != nil || state.MutationSequence != 8 {
		t.Fatalf("callback reset publication sequence: %v, %v", state, err)
	}
	failure := errors.New("aborted candidate")
	if err := fixture.bb.Modify(func(state *models.State) error { state.MutationSequence = 123; return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	state, err = fixture.bb.ReadSnapshot()
	if err != nil || state.MutationSequence != 8 {
		t.Fatalf("aborted candidate advanced sequence: %v, %v", state, err)
	}
	state.MutationSequence = math.MaxUint64
	if err := fixture.bb.Write(state); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := fixture.bb.Modify(func(*models.State) error { called = true; return nil }); err == nil || !strings.Contains(err.Error(), "sequence exhausted") || called {
		t.Fatalf("overflow did not fail before callback: err=%v called=%v", err, called)
	}
}

func TestTerminalArchiveNoOpStillPublishesPendingLivenessOnce(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	path, bb := writeLivenessState(t, base.Add(time.Minute), nil)
	beat, lease := base.Add(time.Second), base.Add(time.Hour)
	publishLiveness(t, path, LivenessRecord{Seq: 17, Heartbeat: beat, LeaseExpires: lease})
	if err := bb.Modify(func(*models.State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	stored := persistedState(t, path)
	if stored.MutationSequence != 1 || stored.Agents[livenessAgent].LivenessSeq != 17 || !stored.Agents[livenessAgent].Heartbeat.Equal(beat) {
		t.Fatal("no-op callback failed to publish its pending liveness fold exactly once")
	}
	assertTime(t, "folded task lease", stored.Tasks[0].LeaseExpires, lease)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(*models.State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("already-folded liveness produced another no-op publication: %v", err)
	}
}

func TestTerminalArchiveRuntimeInputPreimageRestoresTerminalOutput(t *testing.T) {
	fixture := newColdTerminalTaskFixture(t, models.TaskStatusMerged, false)
	state, err := fixture.bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	state.Config.TerminalTaskArchival = true
	state.Tasks[0].Output[0].RuntimeInputs = []models.RuntimeInput{{ID: "fixture-input", Env: []string{"BOUND_NAME"}}}
	state.Tasks = append(state.Tasks, models.Task{
		ID: "active-consumer", Status: models.TaskStatusReady,
		ValidationPrerequisites: []models.ValidationPrerequisite{{Command: "fixture", Env: []string{"BOUND_NAME"}}},
	})
	// Whole-state Write preserves an existing legacy collision; later Modify
	// refuses only names absent from its locked logical pre-image.
	if err := fixture.bb.Write(state); err != nil {
		t.Fatal(err)
	}
	if err := fixture.bb.Modify(func(state *models.State) error {
		_, err := fixture.bb.ArchiveTerminalTask(&state.Tasks[0], fixture.task.Created.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.bb.Modify(func(state *models.State) error { state.Goal.Description = "unrelated"; return nil }); err != nil {
		t.Fatalf("cold pre-image hid an existing terminal-output declaration: %v", err)
	}
	before, err := os.ReadFile(fixture.bb.GetStatePath())
	if err != nil {
		t.Fatal(err)
	}
	err = fixture.bb.Modify(func(state *models.State) error {
		state.Tasks[0].Output[0].RuntimeInputs[0].Env = append(state.Tasks[0].Output[0].RuntimeInputs[0].Env, "OTHER_BOUND_NAME")
		state.Tasks[1].ValidationPrerequisites[0].Env = append(state.Tasks[1].ValidationPrerequisites[0].Env, "OTHER_BOUND_NAME")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "OTHER_BOUND_NAME") {
		t.Fatalf("new collision was not refused: %v", err)
	}
	if after, err := os.ReadFile(fixture.bb.GetStatePath()); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused terminal-output collision changed publication: %v", err)
	}
}
