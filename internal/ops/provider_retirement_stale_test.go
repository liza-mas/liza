package ops

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-61: a MERGED plan that has generated nothing does not hold the provider
// its output names directly. The declaration goes stale, stays unchanged for
// audit, blocks the plan's hand-off and is cleared by replanning the plan.

const staleProviderBlocker = "output[0] declares retired provider provider"

// staleConsumerPlan is a MERGED, unexpanded architecture plan whose output[0]
// declares the provider directly and which has no ordinary depends_on edge.
func staleConsumerPlan(passed bool) models.Task {
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("provider", 0)
	consumer.Output = []models.OutputEntry{output}
	if passed {
		consumer.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: time.Now().UTC()}
	}
	return consumer
}

func mergedProviderPlan() models.Task {
	provider := providerOpsTask("provider", "architecture-pair", models.TaskStatusMerged)
	provider.Output = []models.OutputEntry{providerOpsOutput()}
	return provider
}

type staleRetirement struct {
	name    string
	tasks   func() []models.Task
	retire  func(root string) error
	retired func(*models.Task) bool
}

func staleRetirements() []staleRetirement {
	draftProvider := func() models.Task {
		return providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	}
	return []staleRetirement{
		{
			name:  "replan provider",
			tasks: func() []models.Task { return []models.Task{mergedProviderPlan()} },
			retire: func(root string) error {
				_, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"})
				return err
			},
			retired: func(p *models.Task) bool { return p.TransitionsExecuted["replanned"] },
		},
		{
			name:  "cancel provider",
			tasks: func() []models.Task { return []models.Task{draftProvider()} },
			retire: func(root string) error {
				_, err := CancelTask(root, "provider", "reviewed retirement", "orchestrator-1")
				return err
			},
			retired: func(p *models.Task) bool { return p.Status == models.TaskStatusAbandoned },
		},
		{
			name: "supersede provider",
			tasks: func() []models.Task {
				return []models.Task{draftProvider(), providerOpsTask("replacement", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))}
			},
			retire: func(root string) error {
				_, err := SupersedeTask(root, "provider", []string{"replacement"}, "reviewed replacement", "orchestrator-1")
				return err
			},
			retired: func(p *models.Task) bool { return p.Status == models.TaskStatusSuperseded },
		},
		{
			name: "plan-check replace provider",
			tasks: func() []models.Task {
				correction := providerOpsTask("correction", "architecture-pair", models.TaskStatusMerged)
				correction.Output = []models.OutputEntry{providerOpsOutput()}
				return []models.Task{mergedProviderPlan(), correction}
			},
			retire: func(root string) error {
				_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "provider", Action: PlanCheckActionReplace, ReplacedBy: "correction", ChangedBy: "human"})
				return err
			},
			retired: func(p *models.Task) bool { return p.PlanHandoffRetired() },
		},
	}
}

func classifyForTest(t *testing.T, root, statePath, taskID string) (PlanHandoffClass, string) {
	t.Helper()
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := readClaimStateForTest(t, statePath)
	return domain.Classify(state, state.FindTask(taskID))
}

func TestStaleProviderDeclarationOnUnexpandedPlanDoesNotBlockRetirement(t *testing.T) {
	for _, scenario := range staleRetirements() {
		for _, passed := range []bool{false, true} {
			name := scenario.name + "/unreviewed consumer"
			if passed {
				name = scenario.name + "/passed consumer"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// GIVEN an unexpanded consumer plan declaring the provider directly
				consumer := staleConsumerPlan(passed)
				root, statePath, _ := setupProviderOpsTest(t, append(scenario.tasks(), consumer)...)

				// WHEN the provider is retired
				if err := scenario.retire(root); err != nil {
					t.Fatalf("unexpanded consumer plan blocked provider retirement: %v", err)
				}

				// THEN the retirement persisted and the declaration is kept for audit
				after := readClaimStateForTest(t, statePath)
				if !scenario.retired(after.FindTask("provider")) {
					t.Fatalf("provider retirement not persisted: %+v", after.FindTask("provider"))
				}
				if got := after.FindTask(consumer.ID).Output; !reflect.DeepEqual(got, consumer.Output) {
					t.Fatalf("retirement rewrote the stale consumer output: %+v", got)
				}

				// AND the consumer is a visible reconcile blocker
				wantClass := PlanHandoffNeedsReview
				if passed {
					wantClass = PlanHandoffNeedsReconciliation
				}
				class, blocker := classifyForTest(t, root, statePath, consumer.ID)
				if class != wantClass || !strings.Contains(blocker, staleProviderBlocker) {
					t.Fatalf("Classify = (%q, %q), want (%q, containing %q)", class, blocker, wantClass, staleProviderBlocker)
				}

				// AND an orchestrator pass is refused without effects
				if !passed {
					before := replacementBytes(t, statePath)
					_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: consumer.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
					requireProviderOpsAtomicRefusal(t, statePath, before, err, "retired provider", "replan or hold")
				}

				// AND automatic reviewed admission generates nothing from it
				if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
					t.Fatal(err)
				}
				if child := readClaimStateForTest(t, statePath).FindTask("consumer-cp-0"); child != nil {
					t.Fatalf("reviewed admission generated a child from a stale declaration: %+v", child)
				}
			})
		}
	}
}

func TestStaleProviderDeclarationRefusesEveryGenerationPathAndReplanReauthors(t *testing.T) {
	t.Parallel()
	// GIVEN a passed consumer left stale by the provider's replan
	consumer := staleConsumerPlan(true)
	root, statePath, bb := setupProviderOpsTest(t, mergedProviderPlan(), consumer)
	providerReplan, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"})
	if err != nil {
		t.Fatalf("unexpanded consumer plan blocked provider replan: %v", err)
	}

	// WHEN an operator-admitted transition pass runs
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitOperator)
	if err != nil {
		t.Fatal(err)
	}

	// THEN nothing is generated and the refusal is recorded against the plan
	if readClaimStateForTest(t, statePath).FindTask("consumer-cp-0") != nil {
		t.Fatal("operator admission generated a child from a stale declaration")
	}
	refused := false
	for _, failure := range report.Failures {
		refused = refused || (failure.SourceTaskID == consumer.ID && strings.Contains(failure.Error, "retired provider provider"))
	}
	if !refused {
		t.Fatalf("operator pass did not report the stale declaration: %+v", report)
	}
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := readClaimStateForTest(t, statePath)
	if failures := domain.Failures(state, state.FindTask(consumer.ID)); len(failures) != 1 || failures[0].Class != handoffOutputRefusal {
		t.Fatalf("stale declaration refusal not persisted as an output_validation failure: %+v", failures)
	}

	// AND a manual proceed is refused without effects
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	before := replacementBytes(t, statePath)
	_, err = Proceed(root, consumer.ID, providerOpsTransition)
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "retired provider provider")

	// WHEN the stale consumer is replanned
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusInProgress; return nil }); err != nil {
		t.Fatal(err)
	}
	consumerReplan, err := Replan(root, &ReplanInput{TaskID: consumer.ID, ChangedBy: "orchestrator-1", Reason: "re-author against the replanned provider"})
	if err != nil {
		t.Fatalf("stale consumer could not be replanned: %v", err)
	}

	// THEN its replacement can declare the provider's replacement
	if _, err := ClaimTask(root, consumerReplan.NewTaskID, "architect-1"); err != nil {
		t.Fatalf("consumer replacement could not be claimed: %v", err)
	}
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency(providerReplan.NewTaskID, 0)
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: consumerReplan.NewTaskID, AgentID: "architect-1", Output: []models.OutputEntry{output}}); err != nil {
		t.Fatalf("re-authored declaration on the provider replacement refused: %v", err)
	}
	if got := mustReadTask(t, statePath, consumerReplan.NewTaskID).Output[0].ProviderDependencies; !reflect.DeepEqual(got, output.ProviderDependencies) {
		t.Fatalf("re-authored declaration not persisted: %+v", got)
	}
}

// A stale consumer stays childless, so plans depending on it would inherit no
// barrier from it: the staleness must block them transitively.
func TestStaleProviderConsumerBlocksDependentPlans(t *testing.T) {
	t.Parallel()
	// GIVEN passed plans downstream -> consumer and further -> downstream, and an
	// unreviewed sibling depending on the consumer
	dependent := func(id, upstream string, passed bool) models.Task {
		plan := providerOpsTask(id, "architecture-pair", models.TaskStatusMerged)
		plan.Output = []models.OutputEntry{providerOpsOutput()}
		plan.DependsOn = []string{upstream}
		if passed {
			plan.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: time.Now().UTC()}
		}
		return plan
	}
	root, statePath, _ := setupProviderOpsTest(t, mergedProviderPlan(), staleConsumerPlan(true),
		dependent("downstream", "consumer", true), dependent("further", "downstream", true), dependent("sibling", "consumer", false))

	// WHEN the provider is replanned
	if _, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"}); err != nil {
		t.Fatalf("unexpanded consumer plan blocked provider replan: %v", err)
	}

	// THEN every dependent reports the stale upstream through the chain
	for id, want := range map[string]struct {
		class   PlanHandoffClass
		blocker string
	}{
		"downstream": {PlanHandoffNeedsReconciliation, "upstream consumer: " + staleProviderBlocker},
		"further":    {PlanHandoffNeedsReconciliation, "upstream downstream: upstream consumer: " + staleProviderBlocker},
		"sibling":    {PlanHandoffNeedsReview, "upstream consumer: " + staleProviderBlocker},
	} {
		if class, blocker := classifyForTest(t, root, statePath, id); class != want.class || blocker != want.blocker {
			t.Errorf("Classify(%s) = (%q, %q), want (%q, %q)", id, class, blocker, want.class, want.blocker)
		}
	}

	// AND an unreviewed dependent cannot pass
	before := replacementBytes(t, statePath)
	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "sibling", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "retired provider", "replan or hold")

	// AND reviewed admission generates nothing anywhere in the chain
	if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	after := readClaimStateForTest(t, statePath)
	for _, id := range []string{"consumer-cp-0", "downstream-cp-0", "further-cp-0", "sibling-cp-0"} {
		if child := after.FindTask(id); child != nil {
			t.Fatalf("reviewed admission generated %s behind a stale upstream: %+v", id, child)
		}
	}
}

func TestReplanWarnsAboutStaleProviderConsumers(t *testing.T) {
	t.Parallel()
	root, _, _ := setupProviderOpsTest(t, mergedProviderPlan(), staleConsumerPlan(false))
	result, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"})
	if err != nil {
		t.Fatalf("unexpanded consumer plan blocked provider replan: %v", err)
	}
	want := "task consumer output[0] declares replanned provider provider; replan consumer to re-author it"
	for _, warning := range result.Warnings {
		if warning == want {
			return
		}
	}
	t.Fatalf("replan warnings %q do not name the stale consumer (%q)", result.Warnings, want)
}

func TestHeldStaleConsumerKeepsItsHold(t *testing.T) {
	t.Parallel()
	// GIVEN an unexpanded consumer held for a human action
	consumer := staleConsumerPlan(false)
	consumer.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: "confirm contract", By: "orchestrator-1", At: time.Now().UTC()}
	root, statePath, _ := setupProviderOpsTest(t, mergedProviderPlan(), consumer)

	// WHEN the provider is replanned
	if _, err := Replan(root, &ReplanInput{TaskID: "provider", ChangedBy: "human"}); err != nil {
		t.Fatalf("held unexpanded consumer blocked provider replan: %v", err)
	}

	// THEN the hold still wins and only an operator clear releases the consumer
	if class, blocker := classifyForTest(t, root, statePath, consumer.ID); class != PlanHandoffHeld || blocker != "confirm contract" {
		t.Fatalf("Classify = (%q, %q), want held hold", class, blocker)
	}
	before := replacementBytes(t, statePath)
	_, err := Replan(root, &ReplanInput{TaskID: consumer.ID, ChangedBy: "orchestrator-1"})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "held for human action")
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: consumer.ID, Action: PlanCheckActionClear, ChangedBy: "human"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Replan(root, &ReplanInput{TaskID: consumer.ID, ChangedBy: "orchestrator-1"}); err != nil {
		t.Fatalf("cleared stale consumer could not be replanned: %v", err)
	}
}

// Guards: the exception covers only a MERGED plan that provably generated
// nothing, and a selected child slot only when no replan lineage follows it.
func TestStaleProviderExceptionKeepsOtherDeclarationsLive(t *testing.T) {
	draftProvider := providerOpsTask("provider", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	cancelProvider := func(root string) error {
		_, err := CancelTask(root, "provider", "reviewed retirement", "orchestrator-1")
		return err
	}
	for _, tc := range []struct {
		name   string
		tasks  func() []models.Task
		retire func(root string) error
		holder string
	}{
		{
			name: "markerless plan with an existing child",
			tasks: func() []models.Task {
				child := providerOpsTask("consumer-cp-0", "code-planning-pair", models.TaskStatusMerged)
				child.ParentTasks = []string{"consumer"}
				return []models.Task{draftProvider, staleConsumerPlan(false), child}
			},
			retire: cancelProvider, holder: "consumer output[0]",
		},
		{
			name: "executed marker with a missing child",
			tasks: func() []models.Task {
				consumer := staleConsumerPlan(false)
				consumer.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
				return []models.Task{draftProvider, consumer}
			},
			retire: cancelProvider, holder: "consumer output[0]",
		},
		{
			name: "in-flight producer output",
			tasks: func() []models.Task {
				consumer := staleConsumerPlan(false)
				consumer.Status = models.TaskStatus("ARCHITECTING")
				consumer.AssignedTo = testhelpers.StringPtr("architect-1")
				return []models.Task{draftProvider, consumer}
			},
			retire: cancelProvider, holder: "consumer output[0]",
		},
		{
			name: "non-terminal task-level declaration",
			tasks: func() []models.Task {
				consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
				consumer.ProviderDependencies = providerOpsDependency("provider", 0)
				return []models.Task{draftProvider, consumer}
			},
			retire: cancelProvider, holder: "consumer",
		},
		{
			// D-64: replan lineage would resolve the slot to the successor.
			name: "selected child slot retired by replan",
			tasks: func() []models.Task {
				provider, child := transitionedLineagePlan("provider")
				child.Status = models.TaskStatusMerged
				child.Output = []models.OutputEntry{providerOpsOutput()}
				return []models.Task{provider, child, staleConsumerPlan(false)}
			},
			retire: func(root string) error {
				_, err := Replan(root, &ReplanInput{TaskID: "provider-cp-0", ChangedBy: "human"})
				return err
			},
			holder: "consumer output[0]",
		},
		{
			name: "selected child slot under an expanded plan",
			tasks: func() []models.Task {
				provider, child := transitionedLineagePlan("provider")
				consumerChild := providerOpsTask("consumer-cp-0", "code-planning-pair", models.TaskStatusMerged)
				consumerChild.ParentTasks = []string{"consumer"}
				return []models.Task{provider, child, staleConsumerPlan(false), consumerChild}
			},
			retire: func(root string) error {
				_, err := CancelTask(root, "provider-cp-0", "reviewed retirement", "orchestrator-1")
				return err
			},
			holder: "consumer output[0]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, statePath, _ := setupProviderOpsTest(t, tc.tasks()...)
			before := replacementBytes(t, statePath)
			requireProviderOpsAtomicRefusal(t, statePath, before, tc.retire(root), "live provider_dependencies", tc.holder)
		})
	}
}

// D-64: retiring the selected child an unexpanded plan's output declares by
// anything but replan is permanent. No replan lineage can resolve the slot to
// a successor, so the declaration goes stale instead of holding the child and
// classification and generation keep seeing the retired child.

type staleSlotRetirement struct {
	name     string
	provider string // declared provider; its output 0 selects child
	child    string
	tasks    func(passed bool) []models.Task
	retire   func(root string) error
}

// fixPlan is a passed corrective plan whose output supersedes original: its
// reviewed hand-off retires original (plan-declared replacement, ADR-0161).
func fixPlan(original string) models.Task {
	fix := withPlanCheck(handoffPlan("fix", "architecture-pair"), models.PlanCheckPassed, "")
	fix.Output = []models.OutputEntry{replacingOutput(original)}
	return fix
}

// admitFixPlan runs reviewed admission and reports fixPlan's own failure.
func admitFixPlan(root string) error {
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed)
	if err != nil {
		return err
	}
	for _, failure := range report.Failures {
		if failure.SourceTaskID == "fix" {
			return errors.New(failure.Error)
		}
	}
	return nil
}

// staleSlotConsumer declares provider's output 0, i.e. its selected child
// slot. A passed consumer waits on an unreviewed upstream so that reviewed
// admission cannot expand it before the retirement under test.
func staleSlotConsumer(provider string, passed bool) []models.Task {
	consumer := staleConsumerPlan(passed)
	consumer.Output[0].ProviderDependencies = providerOpsDependency(provider, 0)
	if !passed {
		return []models.Task{consumer}
	}
	gate := providerOpsTask("gate", "architecture-pair", models.TaskStatusMerged)
	gate.Output = []models.OutputEntry{providerOpsOutput()}
	consumer.DependsOn = []string{gate.ID}
	return []models.Task{gate, consumer}
}

func staleSlotRetirements() []staleSlotRetirement {
	expanded := func(passed bool, extra ...models.Task) []models.Task {
		provider, child := transitionedLineagePlan("provider")
		return append(append([]models.Task{provider, child}, extra...), staleSlotConsumer("provider", passed)...)
	}
	return []staleSlotRetirement{
		{
			// I-409: a corrective plan's hand-off supersedes the child.
			name: "plan-declared replacement", provider: "provider", child: "provider-cp-0",
			tasks:  func(passed bool) []models.Task { return expanded(passed, fixPlan("provider-cp-0")) },
			retire: admitFixPlan,
		},
		{
			name: "cancel", provider: "provider", child: "provider-cp-0",
			tasks: func(passed bool) []models.Task { return expanded(passed) },
			retire: func(root string) error {
				_, err := CancelTask(root, "provider-cp-0", "reviewed retirement", "orchestrator-1")
				return err
			},
		},
		{
			name: "supersede-task", provider: "provider", child: "provider-cp-0",
			tasks: func(passed bool) []models.Task {
				replacement := providerOpsTask("replacement", "code-planning-pair", models.TaskStatusDraftCodingPlan)
				replacement.ParentTasks = []string{"provider"}
				return expanded(passed, replacement)
			},
			retire: func(root string) error {
				_, err := SupersedeTask(root, "provider-cp-0", []string{"replacement"}, "reviewed replacement", "orchestrator-1")
				return err
			},
		},
		{
			// The slot already resolves through lineage to its successor.
			name: "plan-declared replacement of the effective successor", provider: "arm", child: "arm-cp-0-replan-1",
			tasks: func(passed bool) []models.Task {
				tasks := append(replannedProviderChildTasks(models.TaskStatusDraftCodingPlan), fixPlan("arm-cp-0-replan-1"))
				return append(tasks, staleSlotConsumer("arm", passed)...)
			},
			retire: admitFixPlan,
		},
	}
}

func TestStaleSelectedChildOnUnexpandedPlanDoesNotBlockPermanentRetirement(t *testing.T) {
	for _, scenario := range staleSlotRetirements() {
		for _, passed := range []bool{false, true} {
			name := scenario.name + "/unreviewed consumer"
			if passed {
				name = scenario.name + "/passed consumer"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// GIVEN an unexpanded consumer plan declaring the child's slot
				tasks := scenario.tasks(passed)
				root, statePath, _ := setupProviderOpsTest(t, tasks...)
				var declared []models.OutputEntry
				for _, task := range tasks {
					if task.ID == "consumer" {
						declared = task.Output
					}
				}

				// WHEN the selected child is retired permanently
				if err := scenario.retire(root); err != nil {
					t.Fatalf("unexpanded consumer plan blocked the selected child's retirement: %v", err)
				}

				// THEN the retirement persisted and the declaration is kept for audit
				after := readClaimStateForTest(t, statePath)
				if child := after.FindTask(scenario.child); child == nil || !models.ProviderRetired(child) {
					t.Fatalf("selected child retirement not persisted: %+v", child)
				}
				if got := after.FindTask("consumer").Output; !reflect.DeepEqual(got, declared) {
					t.Fatalf("retirement rewrote the stale consumer output: %+v", got)
				}

				// AND the consumer is a visible reconcile blocker naming the child
				wantBlocker := fmt.Sprintf("output[0] declares provider %s whose selected child %s was retired", scenario.provider, scenario.child)
				wantClass := PlanHandoffNeedsReview
				if passed {
					wantClass = PlanHandoffNeedsReconciliation
				}
				class, blocker := classifyForTest(t, root, statePath, "consumer")
				if class != wantClass || !strings.Contains(blocker, wantBlocker) {
					t.Fatalf("Classify = (%q, %q), want (%q, containing %q)", class, blocker, wantClass, wantBlocker)
				}

				// AND an orchestrator pass is refused without effects
				if !passed {
					before := replacementBytes(t, statePath)
					_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "consumer", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
					requireProviderOpsAtomicRefusal(t, statePath, before, err, "selected child "+scenario.child+" was retired", "replan or hold")
				}

				// AND automatic reviewed admission generates nothing from it
				if _, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil {
					t.Fatal(err)
				}
				if child := readClaimStateForTest(t, statePath).FindTask("consumer-cp-0"); child != nil {
					t.Fatalf("reviewed admission generated a child from a stale selected child: %+v", child)
				}
			})
		}
	}
}

func TestStaleSelectedChildRefusesOperatorAndManualGeneration(t *testing.T) {
	t.Parallel()
	// GIVEN a passed consumer left stale by its selected child's cancellation
	provider, child := transitionedLineagePlan("provider")
	consumer := staleConsumerPlan(true)
	root, statePath, bb := setupProviderOpsTest(t, provider, child, consumer)
	if _, err := CancelTask(root, child.ID, "reviewed retirement", "orchestrator-1"); err != nil {
		t.Fatalf("unexpanded consumer plan blocked the selected child's cancellation: %v", err)
	}
	wantRefusal := "output[0] declares retired provider child provider-cp-0; replan consumer to re-author it"

	// WHEN an operator-admitted transition pass runs
	report, err := ExecuteTransitionsReportWith(root, "manual", AdmitOperator)
	if err != nil {
		t.Fatal(err)
	}

	// THEN nothing is generated and the refusal is recorded against the plan
	if readClaimStateForTest(t, statePath).FindTask("consumer-cp-0") != nil {
		t.Fatal("operator admission generated a child from a stale selected child")
	}
	refused := false
	for _, failure := range report.Failures {
		refused = refused || (failure.SourceTaskID == consumer.ID && strings.Contains(failure.Error, wantRefusal))
	}
	if !refused {
		t.Fatalf("operator pass did not report the stale selected child: %+v", report)
	}
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := readClaimStateForTest(t, statePath)
	if failures := domain.Failures(state, state.FindTask(consumer.ID)); len(failures) != 1 || failures[0].Class != handoffOutputRefusal {
		t.Fatalf("stale selected child refusal not persisted as an output_validation failure: %+v", failures)
	}

	// AND a manual proceed is refused without effects
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	before := replacementBytes(t, statePath)
	_, err = Proceed(root, consumer.ID, providerOpsTransition)
	requireProviderOpsAtomicRefusal(t, statePath, before, err, wantRefusal)
}
