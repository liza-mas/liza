package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestGlobalAnalysisParentsAfterPrematureRecovery(t *testing.T) {
	state := &models.State{}
	state.Goal.Integration = &models.IntegrationLifecycle{
		PrematureRecovery: &models.IntegrationPrematureRecovery{AnalysisTaskID: "integration-global-1"},
	}
	decision := IntegrationProgressDecision{ContributingSet: &models.IntegrationContributingSet{}}

	parents, err := globalAnalysisParents(state, decision, 2)
	if err != nil || len(parents) != 0 {
		t.Fatalf("first valid global analysis must not inherit the discarded report: parents=%v error=%v", parents, err)
	}
	if _, err := globalAnalysisParents(state, decision, 3); err == nil || !strings.Contains(err.Error(), "lacks generation 2 provenance") {
		t.Fatalf("rescan without prior reviewed evidence must fail: %v", err)
	}
	state.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{
		{Generation: 2, AnalysisTaskID: "integration-global-2"},
	}
	parents, err = globalAnalysisParents(state, decision, 3)
	if err != nil || !reflect.DeepEqual(parents, []string{"integration-global-2"}) {
		t.Fatalf("rescan must inherit the preceding valid analysis: parents=%v error=%v", parents, err)
	}
}
