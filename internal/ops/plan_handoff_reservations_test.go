package ops

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// legacyReservationFailure models the persisted pre-D89 wire contract. Its
// digest deliberately omits the placement-policy discriminator: upgrading must
// retry this observation without requiring an operator to edit the plan.
func legacyReservationFailure(t *testing.T, root string, state *models.State) models.TaskHistoryEntry {
	t.Helper()
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	plan := state.FindTask(replacementPlanID)
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
		t.Fatal("legacy fixture does not reproduce the reservation barrier")
	}
	material := map[string]any{
		"version": 1, "class": "provider_retirement", "index": 0,
		"transition": td, "output": plan.Output, "depends_on": nil,
		"resolved_target": resolved.targetRolePair, "target_status": resolved.targetStatus, "task_type": resolved.taskType,
		"upstreams": map[string]any{}, "incumbents": map[string]string{},
		"provider_retirement": map[string]any{"original": original.ID, "status": original.Status, "role_pair": original.RolePair, "barrier": barrier.Error()},
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	failure := PlanHandoffFailure{Version: 1, TaskID: plan.ID, Transition: providerOpsTransition, Class: "provider_retirement", OutputIndex: 0, Fingerprint: lifecycleDigest(encoded), Error: barrier.Error()}
	encoded, err = json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	var extra map[string]any
	if err := json.Unmarshal(encoded, &extra); err != nil {
		t.Fatal(err)
	}
	return models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventTransitionFailed, Extra: extra}
}

func TestPlanHandoffReservationLegacyFailureRetriesAfterUpgrade(t *testing.T) {
	t.Parallel()
	original, holder := d89Writer("original"), d89Writer("later")
	original.MaxOutputs = 3
	holder.ProviderReservations = d89Reservations(original.ID)
	root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID), original, holder)
	state := readReplacementState(t, statePath)
	state.FindTask(replacementPlanID).History = append(state.FindTask(replacementPlanID).History, legacyReservationFailure(t, root, state))
	testhelpers.WriteInitialState(t, statePath, state)
	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
		t.Fatalf("pre-upgrade reservation refusal still suppresses valid generation: err=%v report=%+v", err, report)
	}
	state = readReplacementState(t, statePath)
	requireRetiredBy(t, state, original.ID, d89Successor(0))
	if got := handoffFailureCount(state.FindTask(replacementPlanID)); got != 1 {
		t.Fatalf("upgrade rewrote historical failure observations: %d", got)
	}
}

func TestPlanHandoffReservationFreshRefusalIsSuppressed(t *testing.T) {
	t.Parallel()
	original, holder, typed := d89Writer("original"), d89Writer("later"), d89Writer("typed")
	original.MaxOutputs = 3
	holder.ProviderReservations = d89Reservations(original.ID)
	typed.DescendantDependencies = []models.DescendantDependency{{AtTransition: d89WriterTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: original.ID, Transition: d89WriterTransition, Outputs: []int{0, 1, 2}}}}}
	root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID), original, holder, typed)
	first, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, first, "typed descendant_dependencies")
	state := readReplacementState(t, statePath)
	if state.FindTask(d89Successor(0)) != nil || state.FindTask(original.ID).Status != original.Status || !models.HoldsReservation(state.FindTask(holder.ID), original.ID, d89WriterTransition) {
		t.Fatal("refusal committed candidate placement changes")
	}
	if got := handoffFailureCount(state.FindTask(replacementPlanID)); got != 1 {
		t.Fatalf("fresh typed refusal recorded %d observations, want one", got)
	}
	second, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(second.Results) != 0 || len(second.Failures) != 0 {
		t.Fatalf("unchanged typed refusal was retried: err=%v report=%+v", err, second)
	}
	requireNothingGenerated(t, state, readReplacementState(t, statePath))
}

func TestPlanHandoffReservationCandidateHolderDoesNotBecomeInputFailure(t *testing.T) {
	t.Parallel()
	original, holder := d89Writer("original"), d89Writer("later")
	original.MaxOutputs = 3
	holder.ProviderReservations = d89Reservations(original.ID)
	plan := d89Plan(original.ID)
	// This reviewed output generates a new typed holder of the provider being
	// retired. Unlike an existing holder, that is a candidate graph fault and
	// must not become a fingerprinted pure-input refusal.
	plan.Output[0].DescendantDependencies = []models.DescendantDependency{{AtTransition: d89WriterTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: original.ID, Transition: d89WriterTransition, Outputs: []int{0, 1, 2}}}}}
	root, statePath := setupPlanReplacementTest(t, plan, original, holder)
	before := readReplacementState(t, statePath)
	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, d89Successor(0)+" descendant_dependencies")
	state := readReplacementState(t, statePath)
	if got := handoffFailureCount(state.FindTask(replacementPlanID)); got != 0 {
		t.Fatalf("candidate-only barrier recorded %d input observations", got)
	}
	requireNothingGenerated(t, before, state)
}
