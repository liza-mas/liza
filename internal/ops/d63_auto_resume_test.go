package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestD63ResumeStampsPlanningAttempt(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "expanded", true: "refused"}[invalid], func(t *testing.T) {
			plan := withPlanCheck(handoffPlan("plan", "code-planning-pair"), models.PlanCheckPassed, "")
			if invalid {
				plan.Output[0].Desc = ""
			}
			root, statePath := setupPlanCheckTest(t, plan)
			bb := db.For(statePath)
			if err := bb.Modify(func(s *models.State) error {
				s.Sprint.Status = models.SprintStatusCheckpoint
				s.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			started := time.Now().UTC()
			result, err := AutoResume(root, "auto-resume")
			if err != nil {
				t.Fatal(err)
			}
			if (result.TransitionError != "") != invalid || (result.TransitionsExecuted > 0) == invalid {
				t.Fatalf("unexpected resume outcome: %+v", result)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if attempted := after.Sprint.Timeline.TransitionsAttemptedAt; attempted == nil || attempted.Before(started) || attempted.After(time.Now().UTC()) {
				t.Fatalf("resume lost planning attempt time: %v", attempted)
			}
		})
	}
}

func TestD63ProviderRefusalRequiresRepair(t *testing.T) {
	plan := withPlanCheck(handoffPlan("plan", "architecture-pair"), models.PlanCheckPassed, "")
	plan.Output = []models.OutputEntry{replacingOutput("original")}
	original := testhelpers.BuildTaskByStatus("original", models.TaskStatusDraftCodingPlan, time.Now().UTC())
	original.RolePair = "code-planning-pair"
	original.Output = []models.OutputEntry{providerOpsOutput()}
	consumer := codingTask("consumer", models.TaskStatusReady)
	consumer.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "original", Transition: "code-plan-to-coding", Outputs: []int{0}}}
	root, statePath := setupPlanCheckTest(t, plan, original, consumer)
	bb := db.For(statePath)
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "consumer has a live provider_dependencies declaration") {
		t.Fatalf("expected genuine live-holder refusal: %+v, %v", report, err)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.FindTask("original").Status != models.TaskStatusDraftCodingPlan || after.FindTask("plan-cp-0") != nil || len(after.FindTask("plan").TransitionsExecuted) != 0 {
		t.Fatal("refusal partially expanded or retired the plan")
	}
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	if handoffFailureCount(after.FindTask("plan")) != 1 || len(domain.Failures(after, after.FindTask("plan"))) != 1 || domain.PlanningCompleteEligible(after, after.FindTask("plan")) {
		t.Fatal("provider refusal was not retained as repair-required evidence; plan can re-wake")
	}
	if err := bb.Modify(func(s *models.State) error {
		s.FindTask("consumer").History = append(s.FindTask("consumer").History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 0 || len(report.Failures) != 0 {
		t.Fatalf("unchanged provider refusal retried: %+v, %v", report, err)
	}
	if err := bb.Modify(func(s *models.State) error { s.FindTask("consumer").ProviderDependencies = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 1 || len(report.Failures) != 0 {
		t.Fatalf("repair did not re-admit plan: %+v, %v", report, err)
	}
	after, err = bb.Read()
	if err != nil || after.FindTask("original").Status != models.TaskStatusSuperseded || after.FindTask("plan-cp-0") == nil {
		t.Fatalf("repaired replacement not persisted: %+v, %v", after, err)
	}
}

func TestD63SkippedPlanningPassDoesNotStamp(t *testing.T) {
	for _, verdict := range []models.PlanCheckVerdict{"", models.PlanCheckHeld} {
		t.Run(string(verdict), func(t *testing.T) {
			plan := handoffPlan("plan", "code-planning-pair")
			if verdict != "" {
				plan = withPlanCheck(plan, verdict, "human prerequisite")
			}
			root, statePath := setupPlanCheckTest(t, plan)
			report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
			if err != nil || len(report.Results) != 0 || len(report.Failures) != 0 {
				t.Fatalf("skipped pass = %+v, %v", report, err)
			}
			after, err := db.For(statePath).Read()
			if err != nil || after.Sprint.Timeline.TransitionsAttemptedAt != nil {
				t.Fatalf("skipped pass stamped attempt: %+v, %v", after, err)
			}
		})
	}
}

func TestD63SelectedProviderChildRefusalRepairs(t *testing.T) {
	provider := handoffPlan("provider", "architecture-pair")
	provider.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	original := testhelpers.BuildTaskByStatus("provider-cp-0", models.TaskStatusDraftCodingPlan, time.Now().UTC())
	original.RolePair = "code-planning-pair"
	original.Output = []models.OutputEntry{providerOpsOutput()}
	plan := withPlanCheck(handoffPlan("plan", "architecture-pair"), models.PlanCheckPassed, "")
	plan.Output = []models.OutputEntry{replacingOutput(original.ID)}
	consumer := codingTask("consumer", models.TaskStatusReady)
	consumer.ProviderDependencies = providerOpsDependency(provider.ID, 0)
	root, statePath := setupPlanCheckTest(t, provider, original, plan, consumer)
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "consumer has a live provider_dependencies") {
		t.Fatalf("selected-child refusal = %+v, %v", report, err)
	}
	bb := db.For(statePath)
	after, err := bb.Read()
	if err != nil || handoffFailureCount(after.FindTask(plan.ID)) != 1 {
		t.Fatalf("missing selected-child failure evidence: %+v, %v", after, err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 0 || len(report.Failures) != 0 {
		t.Fatalf("selected-child refusal retried: %+v, %v", report, err)
	}
	if err := bb.Modify(func(s *models.State) error { s.FindTask(consumer.ID).ProviderDependencies = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 1 || len(report.Failures) != 0 {
		t.Fatalf("selected-child release did not re-admit: %+v, %v", report, err)
	}
}

func TestD63CheckpointCleanupPreservesNewerCheckpoint(t *testing.T) {
	for _, newer := range []string{"same", "checkpoint", "resumed checkpoint", "sprint"} {
		t.Run(newer, func(t *testing.T) {
			root, statePath := setupPlanCheckTest(t)
			bb := db.For(statePath)
			at := time.Now().UTC()
			if err := bb.Modify(func(s *models.State) error {
				s.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
				s.Sprint.Timeline.CheckpointAt = &at
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if err := bb.Modify(func(s *models.State) error {
				switch newer {
				case "checkpoint":
					s.Sprint.Status = models.SprintStatusCheckpoint
				case "resumed checkpoint":
					later := at.Add(time.Second)
					s.Sprint.Timeline.CheckpointAt = &later
				case "sprint":
					s.Sprint.Number++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := clearTransitionCheckpointTrigger(root, before.Sprint); err != nil {
				t.Fatal(err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			want := models.CheckpointTriggerPlanningComplete
			if newer == "same" {
				want = ""
			}
			if after.Sprint.CheckpointTrigger != want {
				t.Fatalf("cleanup trigger = %q, want %q", after.Sprint.CheckpointTrigger, want)
			}
		})
	}
}
