package ops

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReportResumeTransitions_ExecutionErrorIsReportedWithoutCount(t *testing.T) {
	t.Parallel()
	// GIVEN a project whose pipeline pass cannot run at all
	report := reportResumeTransitions(t.TempDir(), resumeOriginOperator, models.Sprint{})

	// THEN the error text is the failure and nothing counts as executed
	if report.failure == "" || report.executed != 0 {
		t.Fatalf("report = %+v, want a failure with no executed transitions", report)
	}
}

func TestReportResumeTransitions_JoinsPassFailuresWithSeparator(t *testing.T) {
	t.Parallel()
	// GIVEN a completed sprint whose replaced story lost its cohort lineage
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted
	for _, task := range supersededCohort("epic-plan-1", false) {
		task.SpecRef = "README.md"
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// WHEN the post-commit pass reports with no checkpoint to clean
	report := reportResumeTransitions(tmpDir, resumeOriginOperator, models.Sprint{})

	// THEN every pass failure is reported, joined by "; ", and none executed
	if report.executed != 0 || strings.Count(report.failure, "; task ") != 1 {
		t.Fatalf("report = %+v, want two failures joined by \"; \"", report)
	}
}
