package db_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// The persisted vocabulary types are defined strings; the actual blackboard
// writer must emit them as the plain scalars the string-typed fields produced,
// and read them back unchanged, including values no constant names (AC2).
func TestWritePersistsVocabularyAsPlainScalars(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	state := &models.State{
		Goal: models.Goal{Description: "vocabulary persistence"},
		Tasks: []models.Task{{
			ID: "task-1",
			History: []models.TaskHistoryEntry{
				{Time: at, Event: models.TaskEventBlockedRecoveryReleased},
				{Time: at, Event: "legacy_event_no_constant_names"},
				{Time: at},
			},
			Output: []models.OutputEntry{
				{Desc: "child", InheritInputs: &models.InheritInputs{
					Mode:       models.InheritModeSelected,
					Selections: []models.InputSelection{{UpstreamTask: "plan-1", Outputs: []int{0}}},
				}},
				{Desc: "unknown mode", InheritInputs: &models.InheritInputs{Mode: "not-a-mode"}},
			},
			RejectionRCA: &models.RejectionRCARecord{
				SchemaVersion: models.RejectionRCASchemaVersion,
				Summary:       "causes",
				Contributions: []models.RejectionRCAContribution{
					{RejectionIndex: 1, Categories: []models.RejectionCause{models.RejectionCauseProductDefect, "toolchain_drift"}},
					{RejectionIndex: 2, Categories: nil},
					{RejectionIndex: 3, Categories: []models.RejectionCause{}},
				},
				Disposition: &models.RejectionRCADisposition{
					RecoveryPath: models.RecoveryCapabilityReroute,
					RestoreMode:  models.RestoreModeAssign,
					Actor:        "orchestrator-1",
					DecidedAt:    at,
				},
			},
		}},
		Anomalies: []models.Anomaly{
			{Timestamp: at, Task: "task-1", Reporter: "coder-1", Type: models.AnomalyTypeRuntimeInputUnavailable, Details: map[string]any{}},
			{Timestamp: at, Task: "task-1", Reporter: "coder-1", Type: "legacy_anomaly_type", Details: map[string]any{}},
		},
		Discovered: []models.Discovery{
			{ID: "d-1", Severity: models.DiscoverySeverityCritical, Urgency: models.DiscoveryUrgencyImmediate, Created: at},
			{ID: "d-2", Severity: "invalid-severity", Urgency: "", Created: at},
		},
	}

	path := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(path)
	if err := bb.Write(state); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data := string(readBytes(t, path))

	for _, want := range []string{
		"event: blocked_recovery_released\n",
		"event: legacy_event_no_constant_names\n",
		"event: \"\"\n",
		"mode: selected\n",
		"mode: not-a-mode\n",
		"categories:\n                - product_defect\n                - toolchain_drift\n",
		"categories: []\n",
		"recovery_path: capability_reroute\n",
		"restore_mode: assign\n",
		"type: runtime_input_unavailable\n",
		"type: legacy_anomaly_type\n",
		"severity: critical\n",
		"urgency: immediate\n",
		"severity: invalid-severity\n",
		"urgency: \"\"\n",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("state.yaml missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(data, "!!") {
		t.Errorf("state.yaml carries a YAML tag, so a vocabulary value was not written as a plain scalar:\n%s", data)
	}

	read, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	task := read.Tasks[0]
	if got := []models.TaskEventName{task.History[0].Event, task.History[1].Event, task.History[2].Event}; !reflect.DeepEqual(got,
		[]models.TaskEventName{models.TaskEventBlockedRecoveryReleased, "legacy_event_no_constant_names", ""}) {
		t.Errorf("events after round trip = %q", got)
	}
	if got := task.Output[0].InheritInputs.Mode; got != models.InheritModeSelected {
		t.Errorf("inherit mode = %q, want selected", got)
	}
	if got := task.RejectionRCA.Contributions[0].Categories; !reflect.DeepEqual(got, []models.RejectionCause{models.RejectionCauseProductDefect, "toolchain_drift"}) {
		t.Errorf("categories = %q, want the unknown cause retained", got)
	}
	if d := task.RejectionRCA.Disposition; d.RecoveryPath != models.RecoveryCapabilityReroute || d.RestoreMode != models.RestoreModeAssign {
		t.Errorf("disposition = %+v", d)
	}
	if got := read.Anomalies[1].Type; got != "legacy_anomaly_type" || read.Anomalies[1].IsValidType() {
		t.Errorf("legacy anomaly type = %q, valid = %v, want it kept and refused by validation", got, read.Anomalies[1].IsValidType())
	}
	if got := read.Discovered[1]; got.IsValidSeverity() || got.IsValidUrgency() || got.Severity != "invalid-severity" {
		t.Errorf("invalid discovery = %+v, want it kept and refused by validation", got)
	}
}
