package ops

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-70: replace-task of a selected provider child hands the consumer's slot to
// the same-pair successor, as replan does (ADR-0184), instead of retiring it.

// requireSlotPendingOnSuccessor asserts that the consumer's declaration is
// valid and waits for successor, not for the retired child.
func requireSlotPendingOnSuccessor(t *testing.T, root, statePath, consumerID, successor string) {
	t.Helper()
	state := readClaimStateForTest(t, statePath)
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
		t.Fatalf("declaration on the replaced child's slot is invalid: %v", err)
	}
	unmet := models.UnmetProviderDependencies(state.FindTask(consumerID), state.Tasks, resolver)
	if len(unmet) != 1 || unmet[0].Kind != models.DependencyUnsatisfiedPending || !slices.Equal(unmet[0].BlockingIDs, []string{successor}) {
		t.Fatalf("consumer readiness = %+v, want pending on %s", unmet, successor)
	}
}

func TestReplaceTaskOfSelectedChildKeepsConsumerSlot_D70(t *testing.T) {
	for name, started := range map[string]bool{"unstarted consumer": false, "started consumer": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// GIVEN a consumer selecting the provider's output 0, generated as provider-cp-0
			provider, child := transitionedLineagePlan("provider")
			consumer := unstartedDraftConsumer("provider")
			if started {
				consumer = claimedOnce(consumer)
			}
			root, statePath, _ := setupProviderOpsTest(t, provider, child, consumer)

			// WHEN the selected child is replaced by a same-pair successor
			if err := replaceCodePlanForTest(root, statePath, "provider-cp-0", "provider-cp-0-r1", ""); err != nil {
				t.Fatalf("replace-task of the selected child: %v", err)
			}

			// THEN the consumer is neither blocked nor rewritten
			got := mustReadTask(t, statePath, "consumer")
			if got.Status != models.TaskStatusDraftCodingPlan {
				t.Fatalf("consumer status = %s (history %+v), want DRAFT_CODING_PLAN", got.Status, got.History)
			}
			if !reflect.DeepEqual(got.ProviderDependencies, consumer.ProviderDependencies) {
				t.Fatalf("replacement rewrote the declaration: %+v", got.ProviderDependencies)
			}
			// AND its slot now waits for the successor
			requireSlotPendingOnSuccessor(t, root, statePath, "consumer", "provider-cp-0-r1")
		})
	}
}

// The notes' probe: a BLOCKED consumer re-authored by replace-task re-states
// its typed selection after the selected child was replaced.
func TestReplaceTaskReauthorsConsumerWithTypedSelection_D70(t *testing.T) {
	t.Parallel()
	// GIVEN a BLOCKED consumer and its selected child replaced
	provider, child := transitionedLineagePlan("provider")
	consumer := unstartedDraftConsumer("provider")
	consumer.Status = models.TaskStatusBlocked
	consumer.BlockedReason = testhelpers.StringPtr("scope needs re-authoring")
	root, statePath, _ := setupProviderOpsTest(t, provider, child, consumer)
	if err := replaceCodePlanForTest(root, statePath, "provider-cp-0", "provider-cp-0-r1", ""); err != nil {
		t.Fatalf("replace-task of the selected child: %v", err)
	}

	// WHEN the consumer is re-authored with the same typed selection
	replacement := codePlanReplacementInput("consumer-r1")
	replacement.ProviderDependencies = providerOpsDependency("provider", 0)
	if err := replaceTaskForTest(root, statePath, "consumer", replacement, "scope re-authored"); err != nil {
		t.Fatalf("re-stated typed selection refused: %v", err)
	}

	// THEN the re-authored consumer keeps the selection and waits for the successor
	if got := mustReadTask(t, statePath, "consumer-r1").ProviderDependencies; !reflect.DeepEqual(got, replacement.ProviderDependencies) {
		t.Fatalf("re-authored consumer declaration = %+v", got)
	}
	requireSlotPendingOnSuccessor(t, root, statePath, "consumer-r1", "provider-cp-0-r1")
}

// A consumer blocked by a replacement committed before D-70 (no supersession
// evidence, only the completion entry) is released by its slot following.
func TestLegacyReplacedChildReleasesBlockedConsumer_D70(t *testing.T) {
	t.Parallel()
	// GIVEN the pre-fix state: provider-cp-0 replaced by provider-cp-0-r1, and
	// the unstarted consumer blocked for re-authoring by that replacement
	provider, child := transitionedLineagePlan("provider")
	child.Status = models.TaskStatusSuperseded
	child.SupersededBy = []string{"provider-cp-0-r1"}
	child.History = append(child.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventReplacementCommitted,
		Extra: map[string]any{"source_task_id": child.ID, "replacement_task_id": "provider-cp-0-r1"}})
	successor := providerOpsTask("provider-cp-0-r1", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	successor.ParentTasks = []string{"provider"}
	consumer := unstartedDraftConsumer("provider")
	consumer.Status = models.TaskStatusBlocked
	consumer.BlockedReason = testhelpers.StringPtr("provider_dependencies[0] declares provider provider whose selected child provider-cp-0 was retired")
	root, statePath, _ := setupProviderOpsTest(t, provider, child, successor, consumer)

	// WHEN the orchestrator unblocks it
	if _, err := UnblockTask(root, "consumer", "", "selected child replaced by provider-cp-0-r1", "orchestrator-1"); err != nil {
		t.Fatalf("unblock refused: %v", err)
	}

	// THEN it is back in its initial status, waiting for the successor
	if got := mustReadTask(t, statePath, "consumer").Status; got != models.TaskStatusDraftCodingPlan {
		t.Fatalf("consumer status = %s, want DRAFT_CODING_PLAN", got)
	}
	requireSlotPendingOnSuccessor(t, root, statePath, "consumer", "provider-cp-0-r1")
}

// Following the slot adds the edge consumer -> successor, so a successor that
// depends back on that consumer closes a cycle: the replacement is refused.
func TestReplaceTaskOfSelectedChildRefusesCycleThroughConsumer_D70(t *testing.T) {
	t.Parallel()
	// GIVEN a consumer selecting the provider's output 0
	provider, child := transitionedLineagePlan("provider")
	root, statePath, _ := setupProviderOpsTest(t, provider, child, unstartedDraftConsumer("provider"))
	before := replacementBytes(t, statePath)

	// WHEN the selected child is replaced by a successor depending on the consumer
	err := replaceCodePlanForTest(root, statePath, "provider-cp-0", "provider-cp-0-r1", "", "consumer")

	// THEN the replacement is refused atomically as a cycle
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cycle", "consumer", "provider-cp-0-r1")
}
