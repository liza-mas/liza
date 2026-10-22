package ops

import (
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-167: a correcting plan whose output supersedes a selected provider child
// hands the consumers' slot to the child it generates, as replace-task does
// (D-70), instead of blocking them or refusing its own hand-off. A split, or a
// declaration naming the child directly, stays a permanent retirement.

const correctedChild = "provider-cp-0"

// correctionSuccessor is the child fixPlan generates for its output index.
func correctionSuccessor(index int) string {
	return perSubtaskChildID("fix", "cp", index)
}

func requireValidState(t *testing.T, root, statePath string) {
	t.Helper()
	if err := statevalidate.ValidateState(readClaimStateForTest(t, statePath), root, true, io.Discard); err != nil {
		t.Fatalf("state after the correction is invalid: %v", err)
	}
}

func TestPlanReplacementOfSelectedChildKeepsConsumerSlot_D167(t *testing.T) {
	for name, consumers := range map[string]func() []models.Task{
		"unstarted consumer": func() []models.Task { return []models.Task{unstartedDraftConsumer("provider")} },
		"started consumer":   func() []models.Task { return []models.Task{claimedOnce(unstartedDraftConsumer("provider"))} },
		// B-402: the consumer is the generated child of an expanded plan.
		"generated consumer": func() []models.Task { return generatedDraftConsumer("provider") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// GIVEN a consumer selecting the provider's output 0 and a passed
			// correction superseding that child
			provider, child := transitionedLineagePlan("provider")
			tasks := append([]models.Task{provider, child, fixPlan(correctedChild)}, consumers()...)
			consumerID := tasks[len(tasks)-1].ID
			root, statePath, _ := setupProviderOpsTest(t, tasks...)

			// WHEN the correction hands off
			if err := admitFixPlan(root); err != nil {
				t.Fatalf("correction hand-off refused: %v", err)
			}

			// THEN the child is retired by the correction's child
			state := readClaimStateForTest(t, statePath)
			if got := state.FindTask(correctedChild); got.Status != models.TaskStatusSuperseded || !slices.Equal(got.SupersededBy, []string{correctionSuccessor(0)}) {
				t.Fatalf("%s: status %s superseded_by %v", correctedChild, got.Status, got.SupersededBy)
			}
			// AND the consumer is neither blocked nor rewritten
			want := tasks[len(tasks)-1]
			got := state.FindTask(consumerID)
			if got.Status != want.Status {
				t.Fatalf("consumer status = %s (history %+v), want %s", got.Status, got.History, want.Status)
			}
			if !reflect.DeepEqual(got.ProviderDependencies, want.ProviderDependencies) {
				t.Fatalf("correction rewrote the declaration: %+v", got.ProviderDependencies)
			}
			// AND its slot waits for the correction's child
			requireSlotPendingOnSuccessor(t, root, statePath, consumerID, correctionSuccessor(0))
			requireValidState(t, root, statePath)
		})
	}
}

// D-172 item (2) for a unique successor: a reservation selecting the
// corrected child waits for its successor instead of stranding the writer.
func TestPlanReplacementOfSelectedChildCarriesReservation_D167(t *testing.T) {
	t.Parallel()
	// GIVEN a writer reserved behind every child the provider generates
	provider, child := transitionedLineagePlan("provider")
	holder := providerOpsTask("later", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	holder.ProviderReservations = []models.ProviderReservation{{ProviderTask: "provider", Transition: providerOpsTransition}}
	root, statePath, _ := setupProviderOpsTest(t, provider, child, fixPlan(correctedChild), holder)

	// WHEN the correction replaces the reserved child
	if err := admitFixPlan(root); err != nil {
		t.Fatalf("correction hand-off refused: %v", err)
	}

	// THEN the reservation is unchanged and waits for the successor
	state := readClaimStateForTest(t, statePath)
	if got := state.FindTask("later").ProviderReservations; !reflect.DeepEqual(got, holder.ProviderReservations) {
		t.Fatalf("reservation rewritten: %+v", got)
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	result := models.ResolveReservation(holder.ProviderReservations[0], state, resolver)
	if result.Kind != models.DependencyUnsatisfiedPending || !slices.Equal(result.BlockingIDs, []string{correctionSuccessor(0)}) {
		t.Fatalf("reservation = %+v, want pending on %s", result, correctionSuccessor(0))
	}
	requireValidState(t, root, statePath)
}

func TestPlanReplacementSplitOfSelectedChildStaysPermanent_D167(t *testing.T) {
	t.Run("unstarted consumer is blocked for re-authoring", func(t *testing.T) {
		t.Parallel()
		provider, child := transitionedLineagePlan("provider")
		root, statePath, _ := setupProviderOpsTest(t, provider, child, splitFixPlan(correctedChild), unstartedDraftConsumer("provider"))
		if err := admitFixPlan(root); err != nil {
			t.Fatalf("split refused: %v", err)
		}
		got := mustReadTask(t, statePath, "consumer")
		if got.Status != models.TaskStatusBlocked || got.BlockedReason == nil || !strings.Contains(*got.BlockedReason, "selected child "+correctedChild+" was retired") {
			t.Fatalf("consumer status %s reason %v, want BLOCKED naming the retired child", got.Status, got.BlockedReason)
		}
	})
	t.Run("started consumer refuses the hand-off", func(t *testing.T) {
		t.Parallel()
		provider, child := transitionedLineagePlan("provider")
		root, statePath, _ := setupProviderOpsTest(t, provider, child, splitFixPlan(correctedChild), claimedOnce(unstartedDraftConsumer("provider")))
		before := readClaimStateForTest(t, statePath)
		err := admitFixPlan(root)
		if err == nil || !strings.Contains(err.Error(), "consumer") {
			t.Fatalf("split hand-off error = %v, want a refusal naming the consumer", err)
		}
		after := readClaimStateForTest(t, statePath)
		if after.FindTask(correctionSuccessor(0)) != nil || after.FindTask(correctedChild).Status != before.FindTask(correctedChild).Status {
			t.Fatal("refused split committed generation or retirement")
		}
	})
}

// A direct declaration never follows lineage (ADR-0190): it still holds.
func TestPlanReplacementDirectDeclarationOfOriginalStillHolds_D167(t *testing.T) {
	t.Parallel()
	provider, child := transitionedLineagePlan("provider")
	direct := claimedOnce(providerOpsTask("direct", "coding-pair", models.TaskStatusReady))
	direct.ProviderDependencies = []models.ProviderDependency{{ProviderTask: correctedChild, Transition: "code-plan-to-coding", Outputs: []int{0}}}
	root, statePath, _ := setupProviderOpsTest(t, provider, child, fixPlan(correctedChild), direct)
	err := admitFixPlan(root)
	if err == nil || !strings.Contains(err.Error(), "direct") {
		t.Fatalf("hand-off error = %v, want a refusal naming the direct holder", err)
	}
	if readClaimStateForTest(t, statePath).FindTask(correctionSuccessor(0)) != nil {
		t.Fatal("refused hand-off generated the successor")
	}
}

// Following the slot adds consumer -> successor, so a successor that depends
// on that consumer closes a cycle: the hand-off is refused atomically.
func TestPlanReplacementCycleThroughConsumerRefused_D167(t *testing.T) {
	t.Parallel()
	provider, child := transitionedLineagePlan("provider")
	fix := fixPlan(correctedChild)
	fix.Output[0].TaskDependsOn = []string{"consumer"}
	root, statePath, _ := setupProviderOpsTest(t, provider, child, fix, unstartedDraftConsumer("provider"))
	before := readClaimStateForTest(t, statePath)
	err := admitFixPlan(root)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("hand-off error = %v, want a cycle refusal", err)
	}
	requireNothingGenerated(t, before, readClaimStateForTest(t, statePath))
}

// Missing-child recovery regenerates the successor; the slot follows it.
func TestPlanReplacementRecoveryRestoresSlotSuccessor_D167(t *testing.T) {
	t.Parallel()
	// GIVEN a crash after the correction retired the child but before its
	// successor was written
	provider, child := transitionedLineagePlan("provider")
	child.Status = models.TaskStatusSuperseded
	child.SupersededBy = []string{correctionSuccessor(0)}
	child.RescopeReason = testhelpers.StringPtr("replaced by plan fix")
	child.History = append(child.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventSuperseded})
	fix := fixPlan(correctedChild)
	fix.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	root, statePath, _ := setupProviderOpsTest(t, provider, child, fix, unstartedDraftConsumer("provider"))

	// WHEN transitions recover
	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range report.Failures {
		t.Fatalf("recovery failed: %+v", failure)
	}

	// THEN the successor exists and the consumer's slot waits for it
	requireSlotPendingOnSuccessor(t, root, statePath, "consumer", correctionSuccessor(0))
	if got := mustReadTask(t, statePath, "consumer").Status; got != models.TaskStatusDraftCodingPlan {
		t.Fatalf("consumer status = %s, want DRAFT_CODING_PLAN", got)
	}
}

// preD167ProviderFailure is a provider-retirement observation persisted before
// D-167: its digest carries the D-89 placement policy but not the slot-lineage
// policy, so upgrading must retry it once.
func preD167ProviderFailure(t *testing.T, root string, state *models.State) models.TaskHistoryEntry {
	t.Helper()
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	plan := state.FindTask("fix")
	td, err := domain.resolver.Transition(providerOpsTransition)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := buildTransitionDefFromPipeline(domain.resolver, providerOpsTransition)
	if err != nil {
		t.Fatal(err)
	}
	original := state.FindTask(plan.Output[0].Supersedes)
	barrier := rejectReferencedProviderRetirement(state, domain.resolver, original.ID, retirePermanently)
	if barrier == nil {
		t.Fatal("fixture does not reproduce the pre-fix barrier")
	}
	material := map[string]any{
		"version": 1, "class": handoffProviderRefusal, "index": 0,
		"transition": td, "output": plan.Output, "depends_on": nil,
		"resolved_target": resolved.targetRolePair, "target_status": resolved.targetStatus, "task_type": resolved.taskType,
		"upstreams": map[string]any{}, "incumbents": map[string]string{},
		"provider_retirement": map[string]any{"original": original.ID, "status": original.Status, "role_pair": original.RolePair,
			"barrier": barrier.Error(), "reservation_transfer_policy": 1},
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	failure := PlanHandoffFailure{Version: 1, TaskID: plan.ID, Transition: providerOpsTransition, Class: handoffProviderRefusal, OutputIndex: 0, Fingerprint: lifecycleDigest(encoded), Error: barrier.Error()}
	if encoded, err = json.Marshal(failure); err != nil {
		t.Fatal(err)
	}
	var extra map[string]any
	if err := json.Unmarshal(encoded, &extra); err != nil {
		t.Fatal(err)
	}
	return models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventTransitionFailed, Extra: extra}
}

func TestPlanReplacementPreFixProviderRefusalRetries_D167(t *testing.T) {
	t.Parallel()
	// GIVEN a correction refused before D-167 because a started consumer held
	// the child it supersedes
	provider, child := transitionedLineagePlan("provider")
	root, statePath, _ := setupProviderOpsTest(t, provider, child, fixPlan(correctedChild), claimedOnce(unstartedDraftConsumer("provider")))
	state := readClaimStateForTest(t, statePath)
	state.FindTask("fix").History = append(state.FindTask("fix").History, preD167ProviderFailure(t, root, state))
	testhelpers.WriteInitialState(t, statePath, state)

	// WHEN reviewed admission runs after the upgrade
	if err := admitFixPlan(root); err != nil {
		t.Fatalf("retry refused: %v", err)
	}

	// THEN the correction handed off and the consumer follows the successor
	if readClaimStateForTest(t, statePath).FindTask(correctionSuccessor(0)) == nil {
		t.Fatal("pre-fix refusal still suppresses the correction's hand-off")
	}
	requireSlotPendingOnSuccessor(t, root, statePath, "consumer", correctionSuccessor(0))
}
