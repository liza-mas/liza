package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-49: a plan an orchestrator turn left undecided is shown as awaiting a
// disposition, never as ready.
func TestPhaseHandoffUndecidedPlanVisibleButNotReady(t *testing.T) {
	// GIVEN a needs_review plan with a recorded undecided observation
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	build := func() *models.State {
		plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusMerged, time.Now().UTC())
		plan.RolePair = "code-planning-pair"
		plan.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "tests pass", Scope: "pkg/", SpecRef: "README.md"}}
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{plan}
		state.Sprint.Status = models.SprintStatusInProgress
		state.Sprint.Scope.Planned = []string{plan.ID}
		return state
	}
	domain, err := ops.LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := build()
	if domain.RecordUndecided(build(), state, "plan", time.Now().UTC()) == nil {
		t.Fatal("precondition: observation not recorded")
	}

	// WHEN status is built
	handoff := buildPhaseHandoffStatus(state, root)

	// THEN it is listed for a disposition, not as ready, with the remedies
	if handoff == nil || handoff.State != "DISPOSITION_REQUIRED" || len(handoff.ReadyPlanningTasks) != 0 ||
		len(handoff.UndecidedPlanningTasks) != 1 || handoff.UndecidedPlanningTasks[0].TaskID != "plan" {
		t.Fatalf("handoff status = %+v", handoff)
	}
	if !strings.Contains(handoff.Explanation, "add-human-note") || !strings.Contains(handoff.Explanation, "proceed") {
		t.Fatalf("explanation lacks the remedies: %s", handoff.Explanation)
	}
	var text strings.Builder
	writePhaseHandoffSection(&text, handoff)
	if strings.Contains(text.String(), "Ready planning tasks:") || !strings.Contains(text.String(), "plan (needs_review)") {
		t.Fatalf("status text = %s", text.String())
	}
}
