package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func recoveryTransitionFixture(t *testing.T) (*models.State, *models.State) {
	t.Helper()
	old := testhelpers.CreateValidState()
	old.Config.Mode = models.SystemModePaused
	old.Tasks = []models.Task{{ID: "integration-global-1", Status: models.TaskStatusReadyForReview, ReviewCommit: testhelpers.StringPtr("report"), IntegrationAnalysis: &models.IntegrationAnalysisMetadata{Key: "global:1", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 1, SourceCommit: "source"}}}
	old.Goal.Integration = &models.IntegrationLifecycle{ContributingSet: &models.IntegrationContributingSet{}}
	next := cloneIntegrationState(t, old)
	next.Tasks[0].Status = models.TaskStatusAbandoned
	next.Tasks[0].ReviewCommit = nil
	next.Goal.Integration.ContributingSet = nil
	next.Goal.Integration.PrematureRecovery = &models.IntegrationPrematureRecovery{At: time.Now().UTC(), Reason: "premature freeze", AnalysisTaskID: "integration-global-1", SourceCommit: "source", ReportCommit: "report", PreservationRef: "refs/integration-recovery/integration-global-1"}
	return old, next
}

func TestIntegrationRecoveryTransitionIsNarrow(t *testing.T) {
	old, next := recoveryTransitionFixture(t)
	if err := ValidateIntegrationLifecycleTransition(old, next); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*models.State, *models.State)
	}{
		{"running", func(o, n *models.State) { o.Config.Mode = models.SystemModeRunning }},
		{"nonempty", func(o, n *models.State) {
			o.Goal.Integration.ContributingSet.Scopes = []models.IntegrationScopeSnapshot{{PlanTaskID: "plan", RootTaskIDs: []string{"root"}}}
		}},
		{"accepted evidence", func(o, n *models.State) {
			o.Goal.Integration.GlobalGenerations = []models.IntegrationGlobalGeneration{{Generation: 1}}
		}},
		{"metadata removal", func(o, n *models.State) { n.Tasks[0].IntegrationAnalysis = nil }},
		{"report replacement", func(o, n *models.State) { n.Goal.Integration.PrematureRecovery.ReportCommit = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, n := recoveryTransitionFixture(t)
			tc.mutate(o, n)
			if err := ValidateIntegrationLifecycleTransition(o, n); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
		})
	}
	changed := cloneIntegrationState(t, next)
	changed.Goal.Integration.PrematureRecovery.Reason = "rewritten"
	if err := ValidateIntegrationLifecycleTransition(next, changed); err == nil || !strings.Contains(err.Error(), "receipt cannot change") {
		t.Fatalf("recovery receipt rewrite accepted: %v", err)
	}
}

func TestIntegrationRecoveryGlobalTwoCanCloseWithoutLosingOldEvidence(t *testing.T) {
	_, state := recoveryTransitionFixture(t)
	report := "valid-report"
	state.Tasks = append(state.Tasks, models.Task{ID: "integration-global-2", Status: models.TaskStatusMerged, ReviewCommit: &report, IntegrationAnalysis: &models.IntegrationAnalysisMetadata{Key: "global:2", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 2, SourceCommit: "valid-source"}})
	l := state.Goal.Integration
	l.ContributingSet = &models.IntegrationContributingSet{}
	l.GlobalGenerations = []models.IntegrationGlobalGeneration{{Generation: 2, AnalysisTaskID: "integration-global-2", AnalysisKey: "global:2", Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: "valid-source", ReportCommit: report}}
	l.Closure = &models.IntegrationClosure{Status: models.IntegrationClosureStatusClean, Generation: 2, AnalysisKey: "global:2", SourceCommit: "valid-source"}
	if err := validateIntegrationLifecycle(state, "", true); err != nil {
		t.Fatalf("valid recovered closure refused: %v", err)
	}
	previous := cloneIntegrationState(t, state)
	state.Goal.Integration.GlobalGenerations = nil
	if err := ValidateIntegrationLifecycleTransition(previous, state); err == nil {
		t.Fatal("accepted clean evidence could be erased")
	}
	state = cloneIntegrationState(t, previous)
	state.Tasks[0].IntegrationAnalysis = nil
	if err := ValidateIntegrationLifecycleTransition(previous, state); err == nil {
		t.Fatal("retired metadata could be erased")
	}
}
