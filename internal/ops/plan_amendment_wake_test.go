package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPlanAmendmentTerminalReviewWakesWithoutReleasingFence(t *testing.T) {
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	domain := NewPlanHandoffDomain(pipeline.NewResolver(cfg))
	for _, status := range []models.TaskStatus{models.TaskStatusDraftCodingPlan, models.TaskStatusCodePlanning, models.TaskStatusCodingPlanToReview, models.TaskStatusReviewingCodingPlan, models.TaskStatusCodingPlanApproved, models.TaskStatusMerged, models.TaskStatusAbandoned} {
		t.Run(string(status), func(t *testing.T) {
			original := handoffPlan("original", "code-planning-pair")
			original.PlanAmendment = &models.PlanAmendment{Pending: "correction", OriginalOutput: original.Output, Corrections: []string{"correction"}}
			correction := models.Task{ID: "correction", RolePair: original.RolePair, AmendsPlan: original.ID, Status: status, Output: original.Output}
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{original, correction}
			state.Sprint.Scope.Planned = []string{original.ID, correction.ID}
			ready := status == models.TaskStatusMerged || status == models.TaskStatusAbandoned
			if domain.PlanningCompleteEligible(state, state.FindTask(original.ID)) != ready || domain.PlanningCompleteEligible(state, state.FindTask(correction.ID)) {
				t.Fatal("incorrect amendment wake eligibility")
			}
			if !state.FindTask(original.ID).PlanGenerationFenced() || !state.FindTask(correction.ID).PlanGenerationFenced() {
				t.Fatal("review readiness released generation fence")
			}
			if !ready {
				return
			}
			originalTask := state.FindTask(original.ID)
			originalTask.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld, By: "operator", At: time.Now().UTC(), Ask: "provision input"}
			if !domain.PlanningCompleteEligible(state, originalTask) {
				t.Fatal("hold incorrectly hides independent correction disposition")
			}
			if class, _ := domain.Classify(state, originalTask); class != PlanHandoffAmendmentReady {
				t.Fatal(class)
			}
			if observed := domain.RecordUndecided(state, state, original.ID, time.Now().UTC()); observed == nil {
				t.Fatal("unhandled terminal amendment was not observed")
			}
			if domain.PlanningCompleteEligible(state, originalTask) {
				t.Fatal("unchanged undecided adoption causes wake loop")
			}
			state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: original.ID, Timestamp: time.Now().UTC(), Message: "fresh adoption evidence"})
			if !domain.PlanningCompleteEligible(state, originalTask) {
				t.Fatal("fresh input did not re-admit disposition")
			}
			if originalTask.PlanCheckVerdictOf() != models.PlanCheckHeld {
				t.Fatal("adoption wake altered human hold")
			}
		})
	}
}
