package db

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func TestPlanningChangeMetricsRetainArchivedLogicalRecords(t *testing.T) {
	p := paths.New(t.TempDir())
	original := coldTerminalTask(models.TaskStatusMerged)
	original.ID, original.Type, original.RolePair = "original-architecture", models.TaskTypeArchitecture, "architecture-pair"
	correction := coldTerminalTask(models.TaskStatusMerged)
	correction.ID = "commissioned-correction"
	correction.PlanningChange = models.NewPlanningChange(models.PlanningChangeCorrection, "consumer-gap", original.ID)
	want := (&models.State{Tasks: []models.Task{original, correction}}).ComputePlanningChanges()
	_, _, archivedOriginal := writeColdTerminalTaskObject(t, p, original)
	_, _, archivedCorrection := writeColdTerminalTaskObject(t, p, correction)
	writeColdTerminalTaskState(t, p, []any{archivedOriginal, archivedCorrection}, true)
	logical, err := New(p.StatePath()).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := logical.ComputePlanningChanges(); !reflect.DeepEqual(got, want) || len(got.Daily) != 1 || !got.Daily[0].Architecture {
		t.Fatalf("archive lost planning-change attribution: %#v, want %#v", got, want)
	}
}
