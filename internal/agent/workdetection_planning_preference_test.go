package agent

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D45: a BLOCKED_TASKS turn may not checkpoint and cannot create a reserved
// transition child, so a blocker waiting on unconsumed planning output must
// wake PLANNING_COMPLETE instead of spending a turn that cannot act.

var planningPreferenceMergedAt = time.Date(2026, time.September, 24, 9, 14, 33, 0, time.UTC)

func mergedPlanner(id string) models.Task {
	planner := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, planningPreferenceMergedAt.Add(-time.Hour))
	planner.RolePair = "code-planning-pair"
	planner.Output = []models.OutputEntry{{Desc: "Implement provider", DoneWhen: "Provider contract passes", Scope: "provider"}}
	planner.History = append(planner.History, models.TaskHistoryEntry{Time: planningPreferenceMergedAt, Event: models.TaskEventMerged})
	return planner
}

func blockedConsumer(id string, dependsOn ...string) models.Task {
	consumer := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, planningPreferenceMergedAt.Add(-2*time.Hour))
	consumer.DependsOn = dependsOn
	return consumer
}

func planningPreferenceState(tasks ...models.Task) *models.State {
	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	state.Sprint.Scope.Planned = nil
	for i := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, tasks[i].ID)
	}
	return state
}

// GIVEN a never-assessed blocked task that depends directly on a merged
// planner whose output has not been transitioned (2026-09-24 07:06Z shape)
// WHEN the orchestrator wake is selected
// THEN PLANNING_COMPLETE wins, so the checkpoint can materialize the children.
func TestDetectOrchestratorWakeTriggers_BlockedOnDirectPlannerPrefersPlanningComplete(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete || result.Count != 1 {
		t.Fatalf("wake = %s/%d, want PLANNING_COMPLETE/1: the BLOCKED_TASKS turn cannot checkpoint", result.Trigger, result.Count)
	}
}

// GIVEN a blocked task whose assessment awaits a planner (not a dependency)
// WHEN that planner merges with untransitioned output (09:14Z shape)
// THEN the now-actionable blocker yields to PLANNING_COMPLETE.
func TestDetectOrchestratorWakeTriggers_BlockedAwaitingPlannerPrefersPlanningComplete(t *testing.T) {
	planner := mergedPlanner("amendment")
	pending := planner
	pending.Status = models.TaskStatusImplementing
	pending.History = nil
	consumer := blockedConsumer("consumer")
	state := planningPreferenceState(pending, consumer)
	candidate := ops.AssessmentFingerprintCandidate{
		Reason: *consumer.BlockedReason, Questions: consumer.BlockedQuestions, Awaited: []string{planner.ID},
	}
	orchestrator := "orchestrator-1"
	state.Tasks[1].History = append(state.Tasks[1].History, models.TaskHistoryEntry{
		Time:  planningPreferenceMergedAt.Add(-30 * time.Minute),
		Event: models.TaskEventOrchestratorAssessment,
		Agent: &orchestrator,
		Extra: map[string]any{
			ops.AssessmentFingerprintExtraKey: ops.BuildAssessmentFingerprint(state, &state.Tasks[1], candidate),
			ops.AwaitedTasksExtraKey:          []string{planner.ID},
		},
	})
	if before := DetectOrchestratorWakeTriggers(state, nil, nil, nil); before.Trigger == WakeTriggerBlocked {
		t.Fatalf("precondition: the awaiting assessment must hold while the planner is pending, got %s", before.Trigger)
	}

	state.Tasks[0] = planner
	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete || result.Count != 1 {
		t.Fatalf("wake = %s/%d, want PLANNING_COMPLETE/1 once the awaited planner merged", result.Trigger, result.Count)
	}
}

// GIVEN a blocker awaiting a superseded planner whose replacement is pending
// WHEN the replacement merges with untransitioned output
// THEN the awaited supersession path is followed, as for dependencies.
func TestDetectOrchestratorWakeTriggers_AwaitedSupersededPlannerPrefersPlanningComplete(t *testing.T) {
	old := testhelpers.BuildTaskByStatus("old-plan", models.TaskStatusSuperseded, planningPreferenceMergedAt.Add(-3*time.Hour))
	old.SupersededBy = []string{"provider-plan"}
	replacement := mergedPlanner("provider-plan")
	pending := replacement
	pending.Status = models.TaskStatusImplementing
	pending.History = nil
	consumer := blockedConsumer("consumer")
	state := planningPreferenceState(old, pending, consumer)
	candidate := ops.AssessmentFingerprintCandidate{
		Reason: *consumer.BlockedReason, Questions: consumer.BlockedQuestions, Awaited: []string{old.ID},
	}
	orchestrator := "orchestrator-1"
	state.Tasks[2].History = append(state.Tasks[2].History, models.TaskHistoryEntry{
		Time:  planningPreferenceMergedAt.Add(-30 * time.Minute),
		Event: models.TaskEventOrchestratorAssessment,
		Agent: &orchestrator,
		Extra: map[string]any{
			ops.AssessmentFingerprintExtraKey: ops.BuildAssessmentFingerprint(state, &state.Tasks[2], candidate),
			ops.AwaitedTasksExtraKey:          []string{old.ID},
		},
	})
	if before := DetectOrchestratorWakeTriggers(state, nil, nil, nil); before.Trigger == WakeTriggerBlocked {
		t.Fatalf("precondition: the awaiting assessment must hold while the replacement is pending, got %s", before.Trigger)
	}

	state.Tasks[1] = replacement
	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete || result.Count != 1 {
		t.Fatalf("wake = %s/%d, want PLANNING_COMPLETE/1 once the awaited replacement merged", result.Trigger, result.Count)
	}
}

// GIVEN the related blocker AND a checkpoint that attempted no transition
// after the merge (breaker or trigger-clobbered checkpoint, J63 shape)
// THEN the preference still applies: only a transition attempt consumes it.
func TestDetectOrchestratorWakeTriggers_UnrelatedCheckpointKeepsPlanningPreference(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))
	checkpointAt := planningPreferenceMergedAt.Add(10 * time.Minute)
	state.Sprint.Timeline.CheckpointAt = &checkpointAt
	state.Sprint.CheckpointTrigger = ""

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("wake = %s, want PLANNING_COMPLETE: a checkpoint without a transition attempt must not consume the preference", result.Trigger)
	}
}

// Retry bound: once a transition pass started after the planner merged, its
// output staying unconsumed means the attempt failed (e.g. D46); blocked triage
// regains its normal priority instead of looping checkpoint → resume → fail.
func TestDetectOrchestratorWakeTriggers_FailedAttemptYieldsToBlockedTriage(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))
	attemptedAt := planningPreferenceMergedAt.Add(5 * time.Minute)
	state.Sprint.Timeline.TransitionsAttemptedAt = &attemptedAt

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerBlocked {
		t.Fatalf("wake = %s, want BLOCKED_TASKS after an attempted, unconsumed transition", result.Trigger)
	}
}

// A planner merging after the last attempt (including one that merged while
// that pass ran, since the stamp is taken before it) was never attempted.
func TestDetectOrchestratorWakeTriggers_MergeAfterAttemptKeepsPreference(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))
	attemptedAt := planningPreferenceMergedAt.Add(-time.Second)
	state.Sprint.Timeline.TransitionsAttemptedAt = &attemptedAt

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("wake = %s, want PLANNING_COMPLETE for a planner merged after the last attempt", result.Trigger)
	}
}

// A dependency on a superseded task resolves to its replacement planner.
func TestDetectOrchestratorWakeTriggers_SupersededDependencyReachesPlanner(t *testing.T) {
	old := testhelpers.BuildTaskByStatus("old-plan", models.TaskStatusSuperseded, planningPreferenceMergedAt.Add(-3*time.Hour))
	old.SupersededBy = []string{"provider-plan"}
	state := planningPreferenceState(old, mergedPlanner("provider-plan"), blockedConsumer("consumer", "old-plan"))

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerPlanningComplete {
		t.Fatalf("wake = %s, want PLANNING_COMPLETE through the supersession path", result.Trigger)
	}
}

// Counterfactual: a blocker with no relation to the ready planner keeps
// BLOCKED_TASKS priority; PLANNING_COMPLETE could not help it.
func TestDetectOrchestratorWakeTriggers_UnrelatedBlockerKeepsBlockedPriority(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer"))

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger != WakeTriggerBlocked {
		t.Fatalf("wake = %s, want BLOCKED_TASKS for a blocker unrelated to the planner", result.Trigger)
	}
}

// Counterfactual: a sprint at CHECKPOINT suppresses planning wakes; the
// preference must not bypass that gate.
func TestDetectOrchestratorWakeTriggers_PlanningPreferenceRespectsCheckpointGate(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))
	state.Sprint.Status = models.SprintStatusCheckpoint

	result := DetectOrchestratorWakeTriggers(state, nil, nil, nil)

	if result.Trigger == WakeTriggerPlanningComplete {
		t.Fatal("a CHECKPOINT sprint must not wake PLANNING_COMPLETE")
	}
}

// A selected BLOCKED_TASKS invocation keeps its own predicate on
// revalidation; the preference applies to fresh selection only.
func TestRevalidateOrchestratorWake_SelectedBlockedIsRetained(t *testing.T) {
	state := planningPreferenceState(mergedPlanner("provider-plan"), blockedConsumer("consumer", "provider-plan"))
	selected := OrchestratorWakeResult{Trigger: WakeTriggerBlocked, Count: 1}

	result, _ := revalidateOrchestratorWake(state, selected, nil, nil, nil, nil)

	if result.Trigger != WakeTriggerBlocked {
		t.Fatalf("revalidated wake = %s, want the selected BLOCKED_TASKS while its predicate holds", result.Trigger)
	}
}
