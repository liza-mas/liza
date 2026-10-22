package ops

import (
	"errors"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-177: a BLOCKED task can wait on a passed plan's hand-off through a typed
// provider declaration instead of depends_on or --awaits. Those waits cannot
// be restated as --awaits once the provider merged, so the BLOCKED_TASKS
// preference must see them or the hand-off that unblocks the task starves.

const providerWaitTransition = "code-plan-to-coding"

func providerWaitDomain(t *testing.T) PlanHandoffDomain {
	t.Helper()
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	return NewPlanHandoffDomain(pipeline.NewResolver(cfg))
}

// providerWaitState holds a passed, merged plan "ref-cp-1" and a never-assessed
// BLOCKED consumer shaped by declare, all in sprint scope.
func providerWaitState(declare func(*models.Task), others ...models.Task) *models.State {
	plan := withPlanCheck(handoffPlan("ref-cp-1", "code-planning-pair"), models.PlanCheckPassed, "")
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusBlocked, time.Now().UTC().Add(-time.Hour))
	declare(&consumer)
	state := testhelpers.CreateValidState()
	state.Tasks = append([]models.Task{plan, consumer}, others...)
	state.Sprint.Scope.Planned = nil
	for i := range state.Tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, state.Tasks[i].ID)
	}
	return state
}

func reserves(provider string) func(*models.Task) {
	return func(task *models.Task) {
		task.ProviderReservations = []models.ProviderReservation{{ProviderTask: provider, Transition: providerWaitTransition}}
	}
}

// GIVEN an actionable BLOCKED task whose only edge to a passed, unattempted
// plan is a provider reservation, a task-level provider dependency, or a
// descendant dependency (the uw-12 and service-role planner shapes)
// WHEN the blocked-wake preference is evaluated in the reviewed domain
// THEN it reports the planning hand-off, so PLANNING_COMPLETE takes the rank.
func TestBlockedTasksAwaitPlanningHandoff_ProviderWaits(t *testing.T) {
	domain := providerWaitDomain(t)
	selectOutput := []models.ProviderDependency{{ProviderTask: "ref-cp-1", Transition: providerWaitTransition, Outputs: []int{0}}}
	cases := map[string]func(*models.Task){
		"provider reservation": reserves("ref-cp-1"),
		"provider dependency": func(task *models.Task) {
			task.ProviderDependencies = selectOutput
		},
		"descendant dependency": func(task *models.Task) {
			task.DescendantDependencies = []models.DescendantDependency{{AtTransition: providerWaitTransition, ProviderDependencies: selectOutput}}
		},
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			state := providerWaitState(declare)

			if !BlockedTasksAwaitPlanningHandoff(state, domain) {
				t.Fatalf("%s on a passed plan must yield BLOCKED_TASKS to PLANNING_COMPLETE", name)
			}
		})
	}
}

// GIVEN a reservation naming a replanned original whose same-pair successor is
// the passed plan
// WHEN the preference is evaluated
// THEN the reservation follows the successor readiness uses, as dependencies
// follow supersession.
func TestBlockedTasksAwaitPlanningHandoff_ReservationFollowsReplanSuccessor(t *testing.T) {
	domain := providerWaitDomain(t)
	original := replannedPlan(handoffPlan("cp-1", "code-planning-pair"))
	state := providerWaitState(reserves(original.ID), original)
	state.FindTask("ref-cp-1").Supersedes = &original.ID
	if got := models.EffectiveReservationProvider(state, original.ID); got != "ref-cp-1" {
		t.Fatalf("precondition: reservation provider resolves to %q, want ref-cp-1", got)
	}

	if !BlockedTasksAwaitPlanningHandoff(state, domain) {
		t.Fatal("a reservation on a replanned original must reach its passed successor")
	}
}

// Counterfactuals: the provider-typed edges add candidates only; every
// existing bound on the preference still applies.
func TestBlockedTasksAwaitPlanningHandoff_ProviderWaitBounds(t *testing.T) {
	domain := providerWaitDomain(t)
	cases := map[string]func(t *testing.T) *models.State{
		"held plan": func(*testing.T) *models.State {
			state := providerWaitState(reserves("ref-cp-1"))
			plan := state.FindTask("ref-cp-1")
			*plan = withPlanCheck(*plan, models.PlanCheckHeld, "provision credentials")
			return state
		},
		"recorded refusal": func(t *testing.T) *models.State {
			state := providerWaitState(reserves("ref-cp-1"))
			plan := state.FindTask("ref-cp-1")
			refusal := &handoffInputError{class: handoffOutputRefusal, index: 0, err: errors.New("output 0: desc is required")}
			if domain.recordFailure(state, plan, providerWaitTransition, refusal, time.Now().UTC()) == nil {
				t.Fatal("precondition: refusal was not recorded")
			}
			return state
		},
		"provider still planning": func(*testing.T) *models.State {
			// A merged, unreviewed plan would itself be a pending hand-off.
			planning := handoffPlan("other-plan", "code-planning-pair")
			planning.Status = models.TaskStatusCodePlanning
			return providerWaitState(reserves(planning.ID), planning)
		},
		"attempted after the plan's last event": func(*testing.T) *models.State {
			state := providerWaitState(reserves("ref-cp-1"))
			plan := state.FindTask("ref-cp-1")
			mergedAt := time.Now().UTC().Add(-10 * time.Minute)
			plan.History = append(plan.History, models.TaskHistoryEntry{Time: mergedAt, Event: models.TaskEventMerged})
			attemptedAt := mergedAt.Add(time.Minute)
			state.Sprint.Timeline.TransitionsAttemptedAt = &attemptedAt
			return state
		},
		"assessed and unchanged": func(*testing.T) *models.State {
			state := providerWaitState(reserves("ref-cp-1"))
			consumer := state.FindTask("consumer")
			candidate := AssessmentFingerprintCandidate{Reason: *consumer.BlockedReason, Questions: consumer.BlockedQuestions}
			orchestrator := "orchestrator-1"
			consumer.History = append(consumer.History, models.TaskHistoryEntry{
				Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator,
				Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(state, consumer, candidate)},
			})
			return state
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			state := build(t)

			if BlockedTasksAwaitPlanningHandoff(state, domain) {
				t.Fatalf("%s: the provider-typed edge must not bypass the preference's bound", name)
			}
		})
	}
}
