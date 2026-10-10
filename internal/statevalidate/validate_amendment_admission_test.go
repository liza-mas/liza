package statevalidate

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestPendingContractCorrectionPreservesExistingExecutingPrerequisites(t *testing.T) {
	for _, reservation := range []bool{false, true} {
		t.Run(map[bool]string{false: "provider_dependency", true: "provider_reservation"}[reservation], func(t *testing.T) {
			testPendingContractExecutingPrerequisites(t, reservation)
		})
	}
}

func testPendingContractExecutingPrerequisites(t *testing.T, reservation bool) {
	state, resolver, _ := providerValidationFixture(t)
	if reservation {
		state.Tasks[1].ProviderDependencies = nil
		state.Tasks[1].ProviderReservations = []models.ProviderReservation{{ProviderTask: "provider", Transition: "architecture-to-code-plan"}}
	}
	initial := state.Tasks[1].Status
	status, err := resolver.ExecutingStatus("architecture-pair")
	if err != nil {
		t.Fatal(err)
	}
	state.Tasks[1].Status = status
	state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	state.Tasks[0].PlanAmendment = &models.PlanAmendment{Pending: "correction", Corrections: []string{"correction"}, OriginalOutput: state.Tasks[0].Output}
	state.Tasks = append(state.Tasks,
		models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}},
		models.Task{ID: "correction", RolePair: "architecture-pair", Status: initial, AmendsPlan: "provider", AmendmentMode: models.PlanAmendmentContract},
	)
	if err := ValidateProviderDependencies(state, resolver); err != nil {
		t.Fatalf("temporary correction invalidated historical execution: %v", err)
	}
	consumer := state.FindTask("consumer")
	if unmet := models.UnmetProviderDependencies(consumer, state.Tasks, resolver); len(unmet) != 1 || unmet[0].DependencyID != "provider" || unmet[0].Kind != models.DependencyUnsatisfiedPending {
		t.Fatalf("future admission escaped pending correction: %+v", unmet)
	}
	state.FindTask("provider-cp-0").Status = models.TaskStatusDraftCodingPlan
	if err := ValidateProviderDependencies(state, resolver); err == nil || !strings.Contains(err.Error(), "executing task consumer") {
		t.Fatalf("genuinely unmet provider prerequisite escaped validation: %v", err)
	}
}
