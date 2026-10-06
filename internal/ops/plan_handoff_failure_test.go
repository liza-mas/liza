package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/taskkind"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func invalidHandoffPlan() models.Task {
	plan := withPlanCheck(handoffPlan("failed-plan", "code-planning-pair"), models.PlanCheckPassed, "")
	plan.Output[0].InheritInputs = &models.InheritInputs{
		Mode:       models.InheritModeSelected,
		Selections: []models.InputSelection{{UpstreamTask: "unavailable-plan", Outputs: []int{0}}},
	}
	return plan
}

func handoffFailureCount(task *models.Task) int {
	count := 0
	for _, event := range task.History {
		if event.Event == models.TaskEventTransitionFailed {
			count++
		}
	}
	return count
}

func TestPlanHandoffFailureRepairAndDiagnostics(t *testing.T) {
	root, stateFile := setupPlanCheckTest(t, invalidHandoffPlan())
	bb := db.For(stateFile)
	if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	// Explicit operator retries return the refusal but persist/alert only once.
	for range 2 {
		report, err := ExecuteTransitionsReportWith(root, "manual", AdmitOperator)
		if err != nil || len(report.Failures) != 1 {
			t.Fatalf("operator retry = %+v, %v", report, err)
		}
	}
	if err := bb.Modify(func(s *models.State) error {
		unrelated := testhelpers.BuildTaskByStatus("unrelated", models.TaskStatusMerged, time.Now().UTC())
		s.Tasks = append(s.Tasks, unrelated)
		s.FindTask("failed-plan").History = append(s.FindTask("failed-plan").History, models.TaskHistoryEntry{Event: models.TaskEventOrchestratorAssessment, Time: time.Now().UTC()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || len(report.Results) != 0 {
		t.Fatalf("unrelated change retried: %+v, %v", report, err)
	}
	state, _ := bb.Read()
	if handoffFailureCount(state.FindTask("failed-plan")) != 1 {
		t.Fatal("duplicated durable failure")
	}
	alertBytes, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil || strings.Count(string(alertBytes), "PLAN HANDOFF FAILED") != 1 || !strings.Contains(string(alertBytes), "--replaced-by") {
		t.Fatalf("alerts = %q, %v", alertBytes, err)
	}
	// Empty-trigger manual checkpoints must not auto-select this failed handoff.
	if _, err := SprintCheckpoint(root, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = bb.Read()
	if state.Sprint.CheckpointTrigger != "" {
		t.Fatalf("checkpoint trigger = %s", state.Sprint.CheckpointTrigger)
	}
	if _, err := Resume(root, "operator"); err != nil {
		t.Fatal(err)
	}
	state, _ = bb.Read()
	if state.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("failed handoff completed sprint on resume: %s", state.Sprint.Status)
	}
	if handoffFailureCount(state.FindTask("failed-plan")) != 1 {
		t.Fatal("resume duplicated failure")
	}
	// Repairing this plan's selected-input declaration re-admits it.
	if err := bb.Modify(func(s *models.State) error { s.FindTask("failed-plan").Output[0].InheritInputs = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 1 || len(report.Failures) != 0 || len(report.Results[0].ChildTaskIDs) != 1 {
		t.Fatalf("repaired pass = %+v, %v", report, err)
	}
}

func TestPlanHandoffFailureConcurrentPasses(t *testing.T) {
	root, stateFile := setupPlanCheckTest(t, invalidHandoffPlan())
	type outcome struct {
		report TransitionReport
		err    error
	}
	finished := make(chan outcome, 2)
	for range 2 {
		go func() {
			report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
			finished <- outcome{report, err}
		}()
	}
	failures := 0
	for range 2 {
		result := <-finished
		if result.err != nil {
			t.Fatal(result.err)
		}
		failures += len(result.report.Failures)
	}
	state, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if failures != 1 || handoffFailureCount(state.FindTask("failed-plan")) != 1 {
		t.Fatalf("concurrent attempts = %d, history = %d", failures, handoffFailureCount(state.FindTask("failed-plan")))
	}
}

func TestPlanHandoffFailureUpstreamRepair(t *testing.T) {
	upstream := withPlanCheck(handoffPlan("upstream", "code-planning-pair"), models.PlanCheckHeld, "provision prerequisite")
	downstream := invalidHandoffPlan()
	downstream.DependsOn = []string{upstream.ID}
	downstream.Output[0].InheritInputs.Selections[0].UpstreamTask = upstream.ID
	root, stateFile := setupPlanCheckTest(t, upstream, downstream)
	// Explicit retry validates downstream input while the upstream is still held.
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitOperator)
	if err != nil || len(report.Failures) != 1 || report.Failures[0].SourceTaskID != downstream.ID || !strings.Contains(report.Failures[0].Error, "supplies no inherited children") {
		t.Fatalf("initial downstream refusal = %+v, %v", report, err)
	}
	bb := db.For(stateFile)
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: upstream.ID, Action: PlanCheckActionClear, ChangedBy: "operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: upstream.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
		t.Fatal(err)
	}
	// Upstream expands first; the next pass observes its real children.
	for range 2 {
		if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
			t.Fatal(err)
		}
	}
	state, _ := bb.Read()
	resolver, _, _ := loadResolver(root)
	td, _ := resolver.Transition(selTransition)
	child := state.FindTask(perSubtaskChildID(downstream.ID, td.TaskSlugOrName(), 0))
	upstreamChild := perSubtaskChildID(upstream.ID, td.TaskSlugOrName(), 0)
	if child == nil || !slices.Contains(child.DependsOn, upstreamChild) {
		t.Fatalf("repair did not create downstream child with selected prerequisite %s: %+v", upstreamChild, child)
	}
}

func TestPlanHandoffFailureStructuralRefusal(t *testing.T) {
	plan := withPlanCheck(handoffPlan("failed-plan", "code-planning-pair"), models.PlanCheckPassed, "")
	plan.Output[0].Desc = ""
	plan.Output[0].Kind = taskkind.PreCommitBootstrap
	root, stateFile := setupPlanCheckTest(t, plan)
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "desc") {
		t.Fatalf("structural refusal = %+v, %v", report, err)
	}
	bb := db.For(stateFile)
	if err := bb.Modify(func(s *models.State) error {
		incumbent := testhelpers.BuildTaskByStatus("incumbent", models.TaskStatusReady, time.Now().UTC())
		incumbent.Kind = taskkind.PreCommitBootstrap
		s.Tasks = append(s.Tasks, incumbent)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || len(report.Results) != 0 {
		t.Fatalf("incumbent cannot repair pre-dedup structural refusal: %+v, %v", report, err)
	}
	if err := bb.Modify(func(s *models.State) error { s.FindTask(plan.ID).Output[0].Desc = "fixed"; return nil }); err != nil {
		t.Fatal(err)
	}
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 1 || len(report.Failures) != 0 {
		t.Fatalf("repaired structure = %+v, %v", report, err)
	}
}

func TestPlanHandoffFailureObservationChanges(t *testing.T) {
	for _, change := range []string{"malformed", "null index", "missing index", "unknown version", "transition", "dedup"} {
		t.Run(change, func(t *testing.T) {
			plan := invalidHandoffPlan()
			plan.Output[0].Kind = taskkind.PreCommitBootstrap
			root, stateFile := setupPlanCheckTest(t, plan)
			if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
				t.Fatal(err)
			}
			bb := db.For(stateFile)
			if change == "transition" {
				configPath := filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml")
				config, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				config = []byte(strings.Replace(string(config), "name: code-plan-to-coding\n          task-slug: code", "name: code-plan-to-coding\n          task-slug: repaired-transition", 1))
				if err := os.WriteFile(configPath, config, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := bb.Modify(func(s *models.State) error {
				if change != "dedup" {
					task := s.FindTask(plan.ID)
					extra := task.History[len(task.History)-1].Extra
					switch change {
					case "malformed":
						extra["input_fingerprint"] = "broken"
					case "null index":
						extra["output_index"] = nil
					case "missing index":
						delete(extra, "output_index")
					case "unknown version":
						extra["version"] = 999
					}
				} else {
					incumbent := testhelpers.BuildTaskByStatus("incumbent", models.TaskStatusReady, time.Now().UTC())
					incumbent.Kind = taskkind.PreCommitBootstrap
					incumbent.RolePair = "coding-pair"
					s.Tasks = append(s.Tasks, incumbent)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
			if err != nil {
				t.Fatal(err)
			}
			if change == "dedup" {
				if len(report.Results) != 1 || len(report.Results[0].ChildTaskIDs) != 0 || len(report.Failures) != 0 {
					t.Fatalf("dedup repair = %+v", report)
				}
			} else if len(report.Failures) != 1 {
				t.Fatalf("changed observation did not retry: %+v", report)
			}
		})
	}
}

func TestPlanHandoffRetirementAdmissionAndLifecycle(t *testing.T) {
	original, correction := handoffPlan("original", "code-planning-pair"), handoffPlan("correction", "code-planning-pair", "original")
	root, stateFile := setupPlanCheckTest(t, original, correction)
	input := PlanCheckInput{TaskID: original.ID, Action: PlanCheckActionReplace, ReplacedBy: correction.ID, ChangedBy: "operator"}
	result, err := RecordPlanCheck(root, input)
	if err != nil || !result.Changed || result.Verdict != models.PlanCheckReplaced {
		t.Fatalf("retirement = %+v, %v", result, err)
	}
	result, err = RecordPlanCheck(root, input)
	if err != nil || result.Changed {
		t.Fatalf("replay = %+v, %v", result, err)
	}
	for _, action := range []PlanCheckAction{PlanCheckActionPass, PlanCheckActionHold, PlanCheckActionClear} {
		request := PlanCheckInput{TaskID: original.ID, Action: action, Ask: "wait", ChangedBy: "operator"}
		if action != PlanCheckActionClear {
			request.Authority = orchestratorAuthority()
		}
		if _, err := RecordPlanCheck(root, request); err == nil || !strings.Contains(err.Error(), "retired") {
			t.Fatalf("revive %s = %v", action, err)
		}
	}
	if _, err := Replan(root, &ReplanInput{TaskID: original.ID, ChangedBy: "operator"}); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("replan retired = %v", err)
	}
	domain, _ := LoadPlanHandoffDomain(root)
	state, _ := db.For(stateFile).Read()
	if domain.Pending(state.FindTask(original.ID)) || IsUnconsumedPlanningOutput(state.FindTask(original.ID), nil) {
		t.Fatal("retired handoff remains pending")
	}
	if carried := collectPendingPlanningHandoffs(state, domain); !reflect.DeepEqual(carried, []string{correction.ID}) {
		t.Fatalf("carry-forward = %v", carried)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: correction.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
		t.Fatal(err)
	}
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Results) != 1 || report.Results[0].SourceTaskID != correction.ID {
		t.Fatalf("correction expansion = %+v, %v", report, err)
	}
	if err := db.For(stateFile).Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := Proceed(root, original.ID, selTransition); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("proceed retired = %v", err)
	}
}

func TestPlanHandoffRetirementRefusals(t *testing.T) {
	for _, scenario := range []string{"held", "executed", "children", "selector", "dependent", "not merged", "different pair", "self", "agent"} {
		t.Run(scenario, func(t *testing.T) {
			original, correction := handoffPlan("original", "code-planning-pair"), handoffPlan("correction", "code-planning-pair")
			tasks := []models.Task{original, correction}
			input := PlanCheckInput{TaskID: original.ID, Action: PlanCheckActionReplace, ReplacedBy: correction.ID, ChangedBy: "operator"}
			switch scenario {
			case "held":
				tasks[0] = withPlanCheck(original, models.PlanCheckHeld, "human action")
			case "executed":
				tasks[0].TransitionsExecuted = map[string]bool{selTransition: true}
			case "children":
				child := testhelpers.BuildTaskByStatus("child", models.TaskStatusReady, time.Now().UTC())
				child.ParentTasks = []string{original.ID}
				tasks = append(tasks, child)
			case "selector":
				tasks[1].Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: original.ID, Outputs: []int{0}}}}
			case "dependent":
				tasks = append(tasks, handoffPlan("downstream", "code-planning-pair", original.ID))
			case "not merged":
				tasks[1].Status = models.TaskStatusCodePlanning
			case "different pair":
				tasks[1].RolePair = "architecture-pair"
			case "self":
				input.ReplacedBy = original.ID
			case "agent":
				input.Authority = orchestratorAuthority()
			}
			root, stateFile := setupPlanCheckTest(t, tasks...)
			before, _ := db.For(stateFile).Read()
			if _, err := RecordPlanCheck(root, input); err == nil {
				t.Fatal("unsafe retirement admitted")
			}
			after, _ := db.For(stateFile).Read()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("refused retirement changed state")
			}
		})
	}
}

func TestPlanHandoffRetirementSettlesIntegration(t *testing.T) {
	state := integrationProgressState(progressPlan("original", models.TaskStatusMerged, false), progressPlan("correction", models.TaskStatusMerged, true), progressCoding("coding", "correction"))
	available := pipeline.SlicedIntegrationCapability{Available: true}
	if decision := evaluateProgress(t, state, available, "head"); decision.PlanningSettled {
		t.Fatal("unused original settled integration")
	}
	state.FindTask("original").PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: "correction", By: "operator", At: time.Now().UTC()}
	decision := evaluateProgress(t, state, available, "head")
	if !decision.PlanningSettled || !reflect.DeepEqual(scopePlanIDs(decision.ContributingSet), []string{"correction"}) {
		t.Fatalf("retirement settlement = %+v", decision)
	}
}

func TestPlanHandoffFailureCarriesDespiteOtherTransition(t *testing.T) {
	root, stateFile := setupPlanCheckTest(t, invalidHandoffPlan())
	config, err := os.ReadFile(filepath.Join(testhelpers.FindRepoRoot(t), "internal/pipeline/testdata/valid-coding-subpipeline.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	config = []byte(strings.Replace(string(config), "trigger: auto\n          cardinality: per-subtask", "trigger: auto\n          cardinality: one-to-one", 1))
	if err := os.WriteFile(filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil || len(report.Failures) != 1 {
		t.Fatalf("manual refusal = %+v, %v", report, err)
	}
	if report, err := ExecuteTransitionsReportWith(root, "auto", AdmitReviewed); err != nil || len(report.Results) != 1 {
		t.Fatalf("other outgoing transition = %+v, %v", report, err)
	}
	domain, _ := LoadPlanHandoffDomain(root)
	state, _ := db.For(stateFile).Read()
	if !domain.Pending(state.FindTask("failed-plan")) || domain.PlanningCompleteEligible(state, state.FindTask("failed-plan")) {
		t.Fatal("outstanding failed manual handoff was consumed or made actionable by another transition")
	}
	if !reflect.DeepEqual(collectPendingPlanningHandoffs(state, domain), []string{"failed-plan"}) {
		t.Fatal("carry-forward lost failed source")
	}
}

func TestPlanHandoffFailurePersistsWithoutPartialMutation(t *testing.T) {
	root, stateFile := setupPlanCheckTest(t, invalidHandoffPlan())
	before, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 1 || !strings.Contains(report.Failures[0].Error, "supplies no inherited children") {
		t.Fatalf("transition report = %+v, err = %v; want selective-inheritance refusal", report, err)
	}
	after, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	publishedSequence := after.MutationSequence
	if publishedSequence != before.MutationSequence+1 {
		t.Fatalf("failure observation published sequence %d, want %d", publishedSequence, before.MutationSequence+1)
	}
	after.MutationSequence = before.MutationSequence
	plan := after.FindTask("failed-plan")
	growth := len(plan.History) - len(before.FindTask(plan.ID).History)
	if growth == 1 {
		event := plan.History[len(plan.History)-1]
		if string(event.Event) != "transition_failed" {
			t.Fatalf("event = %s, want transition_failed", event.Event)
		}
		plan.History = plan.History[:len(plan.History)-1]
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("refused transition changed state beyond the failure event")
	}
	if growth != 1 {
		t.Fatalf("history grew by %d, want one persisted failure event", growth)
	}

	// Reload from disk: another automatic pass must neither retry nor duplicate history.
	report, err = ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || len(report.Results) != 0 {
		t.Fatalf("unchanged automatic pass = %+v, %v; want no attempt", report, err)
	}
	reloaded, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MutationSequence != publishedSequence {
		t.Fatalf("unchanged pass published sequence %d, want %d", reloaded.MutationSequence, publishedSequence)
	}
	if len(reloaded.FindTask(plan.ID).History) != len(before.FindTask(plan.ID).History)+1 {
		t.Fatal("unchanged pass duplicated failure history")
	}
}
