package db_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// These tests pin the ledger invariants of ADR-0169 at the write boundary: a
// consumed or invalidated instance never changes, instances are never removed,
// and new instances start available.

func ledgerInstance(state string) models.RuntimeInputInstance {
	instance := models.RuntimeInputInstance{
		Recipe: "project.fixture", InputID: "w03", Consumption: models.RuntimeInputSingleUse,
		KeyID: "k1", Envelope: "/operator/w03.env", Names: []string{"W03_FIXTURE"},
		Tasks: []string{"task-1"}, State: state,
		RegisteredAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
	}
	switch state {
	case models.RuntimeInputConsumed:
		instance.Consumed = &models.RuntimeInputConsumption{Task: "task-1", RunID: "run-1", At: instance.RegisteredAt}
	case models.RuntimeInputInvalidated:
		instance.Invalidated = &models.RuntimeInputInvalidation{Reason: models.RuntimeInputInvalidatedArtifactChanged, At: instance.RegisteredAt}
	}
	return instance
}

func newLedgerFixture(t *testing.T, instances map[string]models.RuntimeInputInstance) (*db.Blackboard, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(path)
	if err := bb.Write(&models.State{Goal: models.Goal{Description: "ledger"}, RuntimeInputs: instances}); err != nil {
		t.Fatal(err)
	}
	return bb, path
}

func requireLedgerRefused(t *testing.T, bb *db.Blackboard, path string, fn func(*models.State) error) {
	t.Helper()
	before := readBytes(t, path)
	err := bb.Modify(fn)
	if err == nil || !strings.Contains(err.Error(), "runtime-input write refused") {
		t.Fatalf("Modify() error = %v, want a runtime-input write refusal", err)
	}
	if string(readBytes(t, path)) != string(before) {
		t.Fatal("refused transaction changed the state file")
	}
}

func TestModifyRefusesChangingATerminalInstance(t *testing.T) {
	for _, state := range []string{models.RuntimeInputConsumed, models.RuntimeInputInvalidated} {
		t.Run(state, func(t *testing.T) {
			bb, path := newLedgerFixture(t, map[string]models.RuntimeInputInstance{"a": ledgerInstance(state)})
			requireLedgerRefused(t, bb, path, func(s *models.State) error {
				instance := s.RuntimeInputs["a"]
				instance.State = models.RuntimeInputAvailable
				instance.Consumed, instance.Invalidated = nil, nil
				s.RuntimeInputs["a"] = instance
				return nil
			})
			requireLedgerRefused(t, bb, path, func(s *models.State) error {
				instance := s.RuntimeInputs["a"]
				instance.Tasks = append(instance.Tasks, "task-2")
				s.RuntimeInputs["a"] = instance
				return nil
			})
		})
	}
}

func TestModifyRefusesRemovingAnInstance(t *testing.T) {
	bb, path := newLedgerFixture(t, map[string]models.RuntimeInputInstance{"a": ledgerInstance(models.RuntimeInputAvailable)})
	requireLedgerRefused(t, bb, path, func(s *models.State) error {
		s.RuntimeInputs = nil
		return nil
	})
}

func TestModifyRefusesChangingAnAvailableInstanceIdentityFields(t *testing.T) {
	bb, path := newLedgerFixture(t, map[string]models.RuntimeInputInstance{"a": ledgerInstance(models.RuntimeInputAvailable)})
	requireLedgerRefused(t, bb, path, func(s *models.State) error {
		instance := s.RuntimeInputs["a"]
		instance.Envelope = "/elsewhere.env"
		s.RuntimeInputs["a"] = instance
		return nil
	})
}

func TestModifyRefusesRecordingATerminalInstance(t *testing.T) {
	bb, path := newLedgerFixture(t, nil)
	requireLedgerRefused(t, bb, path, func(s *models.State) error {
		s.RuntimeInputs = map[string]models.RuntimeInputInstance{"a": ledgerInstance(models.RuntimeInputConsumed)}
		return nil
	})
}

func TestModifyAllowsConsumingRebindingAndRecording(t *testing.T) {
	bb, _ := newLedgerFixture(t, map[string]models.RuntimeInputInstance{"a": ledgerInstance(models.RuntimeInputAvailable)})
	if err := bb.Modify(func(s *models.State) error {
		instance := s.RuntimeInputs["a"]
		instance.Tasks = []string{"task-2"}
		s.RuntimeInputs["a"] = instance
		s.RuntimeInputs["b"] = ledgerInstance(models.RuntimeInputAvailable)
		return nil
	}); err != nil {
		t.Fatalf("rebind and record: %v", err)
	}
	if err := bb.Modify(func(s *models.State) error {
		instance := s.RuntimeInputs["a"]
		instance.State = models.RuntimeInputConsumed
		instance.Consumed = &models.RuntimeInputConsumption{Task: "task-2", RunID: "run", At: time.Now().UTC()}
		s.RuntimeInputs["a"] = instance
		return nil
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}
}

func TestModifyRefusesIntroducingARuntimeInputNameCollision(t *testing.T) {
	bb, path := newLedgerFixture(t, map[string]models.RuntimeInputInstance{"id-1": ledgerInstance(models.RuntimeInputAvailable)})
	prerequisite := []models.ValidationPrerequisite{{Command: "make canary", Env: []string{"W03_FIXTURE"}}}
	requireLedgerRefused(t, bb, path, func(state *models.State) error {
		state.Tasks = append(state.Tasks, models.Task{ID: "needs", Status: models.TaskStatusReady, Validation: []string{"make canary"}, ValidationPrerequisites: prerequisite})
		return nil
	})

	// A collision already in the pre-image does not block unrelated writes.
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	state.Tasks = append(state.Tasks, models.Task{ID: "needs", Status: models.TaskStatusReady, Validation: []string{"make canary"}, ValidationPrerequisites: prerequisite})
	if err := bb.Write(state); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(state *models.State) error {
		state.Goal.Description = "unrelated"
		return nil
	}); err != nil {
		t.Fatalf("legacy collision blocked an unrelated write: %v", err)
	}
}
