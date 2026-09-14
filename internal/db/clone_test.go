package db

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// buildNestedState returns a state exercising every aliasing-capable shape the
// model contains: slices, maps, pointers, nested slices inside slice elements,
// and inline Extra maps holding arbitrary decoded YAML.
func buildNestedState(t testing.TB) *models.State {
	t.Helper()
	now := time.Now().UTC()
	agent := "orchestrator-1"
	note := "assessment body"

	return &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:               "goal-1",
			Status:           models.GoalStatusInProgress,
			Created:          now,
			AlignmentHistory: []models.AlignmentHistory{{Timestamp: now, Event: "created", Summary: "initial"}},
			Extra:            map[string]any{"nested": map[string]any{"list": []any{"a", "b"}}},
		},
		Tasks: []models.Task{{
			ID:               "task-1",
			Status:           models.TaskStatusBlocked,
			AssignedTo:       &agent,
			DependsOn:        []string{"task-0"},
			BlockedQuestions: []string{"why?"},
			History: []models.TaskHistoryEntry{{
				Time:  now,
				Event: models.TaskEventOrchestratorAssessment,
				Agent: &agent,
				Note:  &note,
				Extra: map[string]any{"snapshot": []any{map[string]any{"id": "task-0"}}},
			}},
		}},
		Agents:     map[string]models.Agent{"orchestrator-1": {Role: "orchestrator", Generation: "g1"}},
		HumanNotes: []models.HumanNote{{For: "task-1", Message: "look", Timestamp: now}},
		Config:     models.Config{IntegrationBranch: "main"},
		Extra:      map[string]any{"top": map[string]any{"inner": "value"}},
	}
}

func marshalState(t testing.TB, state *models.State) string {
	t.Helper()
	data, err := yaml.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return string(data)
}

// mutateEverything writes through every reference-bearing field a caller can
// reach, so any surviving alias between clone and source is observable.
func mutateEverything(state *models.State) {
	state.Version = 99
	state.Goal.AlignmentHistory[0].Event = "mutated"
	state.Goal.Extra["nested"].(map[string]any)["list"].([]any)[0] = "mutated"
	task := &state.Tasks[0]
	task.Status = models.TaskStatusMerged
	*task.AssignedTo = "mutated"
	task.DependsOn[0] = "mutated"
	task.BlockedQuestions[0] = "mutated"
	entry := &task.History[0]
	*entry.Note = "mutated"
	entry.Extra["snapshot"].([]any)[0].(map[string]any)["id"] = "mutated"
	delete(entry.Extra, "snapshot")
	state.Agents["orchestrator-1"] = models.Agent{Role: "mutated"}
	state.HumanNotes[0].Message = "mutated"
	state.Extra["top"].(map[string]any)["inner"] = "mutated"
}

// TestCloneStateIsolation verifies a cloned state shares no mutable memory
// with its source. A miss here means one caller's edits would corrupt the
// parsed cache for every later reader in the process.
func TestCloneStateIsolation(t *testing.T) {
	source := buildNestedState(t)
	before := marshalState(t, source)

	clone := cloneState(source)
	if got := marshalState(t, clone); got != before {
		t.Fatalf("clone differs from source:\ngot:\n%s\nwant:\n%s", got, before)
	}

	mutateEverything(clone)

	if after := marshalState(t, source); after != before {
		t.Errorf("mutating the clone changed the source:\ngot:\n%s\nwant:\n%s", after, before)
	}
}

// TestReadCachedMutationIsolation verifies the ReadCached contract end to end:
// mutating a returned state must not affect what the next call returns.
func TestReadCachedMutationIsolation(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	bb := New(statePath)
	if err := bb.Write(buildNestedState(t)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	first, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("first ReadCached failed: %v", err)
	}
	baseline := marshalState(t, first)

	mutateEverything(first)

	second, err := bb.ReadCached()
	if err != nil {
		t.Fatalf("second ReadCached failed: %v", err)
	}
	if got := marshalState(t, second); got != baseline {
		t.Errorf("cached read reflected a caller mutation:\ngot:\n%s\nwant:\n%s", got, baseline)
	}
}

// buildLargeState returns a state in the size range observed in long runs,
// where re-parsing per cached read is the cost being avoided.
func buildLargeState(t testing.TB, tasks, historyPerTask int) *models.State {
	t.Helper()
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Status: models.GoalStatusInProgress, Created: now},
		Agents:  map[string]models.Agent{},
		Config:  models.Config{IntegrationBranch: "main"},
	}
	body := ""
	for i := 0; i < 40; i++ {
		body += "assessment narrative line with enough text to resemble a real note\n"
	}
	for i := 0; i < tasks; i++ {
		agent := fmt.Sprintf("coder-%d", i%4)
		task := models.Task{
			ID:        fmt.Sprintf("task-%d", i),
			Status:    models.TaskStatusMerged,
			DependsOn: []string{fmt.Sprintf("task-%d", (i+tasks-1)%tasks)},
		}
		for j := 0; j < historyPerTask; j++ {
			note := body
			task.History = append(task.History, models.TaskHistoryEntry{
				Time:  now.Add(time.Duration(j) * time.Minute),
				Event: models.TaskEventOrchestratorAssessment,
				Agent: &agent,
				Note:  &note,
			})
		}
		state.Tasks = append(state.Tasks, task)
	}
	return state
}

func benchStatePath(b *testing.B) string {
	b.Helper()
	statePath := filepath.Join(b.TempDir(), "state.yaml")
	bb := New(statePath)
	if err := bb.Write(buildLargeState(b, 120, 6)); err != nil {
		b.Fatalf("Write failed: %v", err)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		b.Fatalf("stat state: %v", err)
	}
	b.Logf("fixture size: %d bytes", info.Size())
	return statePath
}

// BenchmarkReadCachedHit measures the cached path: stat plus deep copy.
func BenchmarkReadCachedHit(b *testing.B) {
	bb := New(benchStatePath(b))
	if _, err := bb.ReadCached(); err != nil {
		b.Fatalf("warm-up ReadCached failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := bb.ReadCached(); err != nil {
			b.Fatalf("ReadCached failed: %v", err)
		}
	}
}

// BenchmarkStateParse measures what the cached path used to pay per call.
func BenchmarkStateParse(b *testing.B) {
	data, err := os.ReadFile(benchStatePath(b))
	if err != nil {
		b.Fatalf("read state: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var state models.State
		if err := yaml.Unmarshal(data, &state); err != nil {
			b.Fatalf("unmarshal failed: %v", err)
		}
		normalizeAgentRoles(&state)
		normalizeTaskAttempts(&state)
	}
}

// TestStateModelShapeIsCloneable guards the one assumption cloneState makes
// about the model: a struct carrying unexported fields is copied by assignment,
// which is only sound while such structs hold no caller-mutable references.
// time.Time is the sanctioned case. A new model type with unexported fields
// alongside exported pointers, slices or maps would alias silently, so it fails
// here instead.
func TestStateModelShapeIsCloneable(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(t2 reflect.Type, path string) {
		if seen[t2] {
			return
		}
		seen[t2] = true

		switch t2.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			walk(t2.Elem(), path+"[]")
		case reflect.Map:
			walk(t2.Key(), path+".key")
			walk(t2.Elem(), path+".value")
		case reflect.Struct:
			if t2 == reflect.TypeOf(time.Time{}) {
				return
			}
			if hasUnexportedFields(t2) {
				for i := 0; i < t2.NumField(); i++ {
					field := t2.Field(i)
					if field.IsExported() && needsDeepCopy(field.Type) {
						t.Errorf("%s (%s) has unexported fields and reference-bearing field %s: "+
							"cloneState would copy it shallowly and alias the cache",
							path, t2, field.Name)
					}
				}
				return
			}
			for i := 0; i < t2.NumField(); i++ {
				walk(t2.Field(i).Type, path+"."+t2.Field(i).Name)
			}
		}
	}
	walk(reflect.TypeOf(models.State{}), "State")
}
