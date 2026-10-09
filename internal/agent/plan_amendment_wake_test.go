package agent

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPlanAmendmentWakeMustDisposeTerminalCorrection(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	for _, status := range []models.TaskStatus{models.TaskStatusMerged, models.TaskStatusAbandoned} {
		original := handoffPlanTask("original")
		original.PlanAmendment = &models.PlanAmendment{Pending: "correction", OriginalOutput: original.Output, Corrections: []string{"correction"}}
		correction := models.Task{ID: "correction", RolePair: original.RolePair, AmendsPlan: original.ID, Status: status, Output: original.Output}
		before := handoffState(original, correction)
		if err := verifyPlanningCompleteTurn(root, before, before); err == nil || !strings.Contains(err.Error(), "reviewed amendment") {
			t.Fatalf("unhandled amendment accepted: %v", err)
		}
		after := handoffState(original, correction)
		copyRecord := *original.PlanAmendment
		copyRecord.Pending = "fresh-correction"
		after.FindTask(original.ID).PlanAmendment = &copyRecord
		if err := verifyPlanningCompleteTurn(root, before, after); err != nil {
			t.Fatalf("fresh review did not dispose old wake: %v", err)
		}
	}
}
