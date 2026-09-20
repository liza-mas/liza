package models

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestTaskEventNameConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant TaskEventName
		want     string
	}{
		// Constants defined by coding-3
		{"Planning", TaskEventPlanning, "planning"},
		{"PreExecutionCheckpoint", TaskEventPreExecutionCheckpoint, "pre_execution_checkpoint"},
		{"SubmittedForReview", TaskEventSubmittedForReview, "submitted_for_review"},
		{"Approved", TaskEventApproved, "approved"},
		{"Rejected", TaskEventRejected, "rejected"},
		{"Blocked", TaskEventBlocked, "blocked"},
		{"Merged", TaskEventMerged, "merged"},
		{"Superseded", TaskEventSuperseded, "superseded"},
		{"IntegrationFailed", TaskEventIntegrationFailed, "integration_failed"},
		{"HandoffInitiated", TaskEventHandoffInitiated, "handoff_initiated"},
		{"HandoffResumed", TaskEventHandoffResumed, "handoff_resumed"},
		{"OwnedTaskResumed", TaskEventOwnedTaskResumed, "owned_task_resumed"},
		{"TransitionExecuted", TaskEventTransitionExecuted, "transition_executed"},
		{"TransitionCrashRecov", TaskEventTransitionCrashRecov, "transition_crash_recovery"},
		{"ReviewVerdictApproved", TaskEventReviewVerdictApproved, "review_verdict_approved"},
		{"ReviewVerdictRejected", TaskEventReviewVerdictRejected, "review_verdict_rejected"},
		// Constants added by cp4-remaining-constants
		{"Initialization", TaskEventInitialization, "initialization"},
		{"Created", TaskEventCreated, "created"},
		{"Claimed", TaskEventClaimed, "claimed"},
		{"Abandoned", TaskEventAbandoned, "abandoned"},
		{"ClaimedForIntegrationFix", TaskEventClaimedForIntegrationFix, "claimed_for_integration_fix"},
		{"ClaimReleased", TaskEventClaimReleased, "claim_released"},
		{"ReclaimedAfterRejection", TaskEventReclaimedAfterRejection, "reclaimed_after_rejection"},
		{"ReassignedAfterRejection", TaskEventReassignedAfterRejection, "reassigned_after_rejection"},
		{"WorktreeRecovered", TaskEventWorktreeRecovered, "worktree_recovered"},
		{"DoerClaimReleased", TaskEventDoerClaimReleased, "doer_claim_released"},
		{"ReviewClaimReleased", TaskEventReviewClaimReleased, "review_claim_released"},
		{"OrchestratorAssessment", TaskEventOrchestratorAssessment, "orchestrator_assessment"},
		{"Replanned", TaskEventReplanned, "replanned"},
		{"TransitionCycleBlocked", TaskEventTransitionCycleBlocked, "transition_cycle_blocked"},
		{"AcceptanceCommitsRemapped", TaskEventAcceptanceCommitsRemapped, "acceptance_commits_remapped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("TaskEvent%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestHandoffTriggerConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant HandoffTrigger
		want     string
	}{
		{"ContextExhaustion", HandoffTriggerContextExhaustion, "context_exhaustion"},
		{"Submission", HandoffTriggerSubmission, "submission"},
		{"Completion", HandoffTriggerCompletion, "completion"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.constant) != tt.want {
				t.Errorf("HandoffTrigger%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestHandoffEvent_YAMLRoundTrip(t *testing.T) {
	ts := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)

	t.Run("full event round-trips", func(t *testing.T) {
		original := HandoffEvent{
			Timestamp:  ts,
			Agent:      "coder-1",
			Trigger:    HandoffTriggerContextExhaustion,
			Succeeded:  []string{"implemented parser", "added tests"},
			Failed:     []string{"ORM integration failed due to schema mismatch"},
			Hypothesis: "schema needs migration before ORM works",
			NextStep:   "run migration script then retry ORM integration",
			KeyFiles:   []string{"internal/parser.go", "internal/parser_test.go"},
			DeadEnds:   []string{"tried manual SQL — too brittle"},
		}

		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded HandoffEvent
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !decoded.Timestamp.Equal(original.Timestamp) {
			t.Errorf("Timestamp = %v, want %v", decoded.Timestamp, original.Timestamp)
		}
		if decoded.Agent != original.Agent {
			t.Errorf("Agent = %q, want %q", decoded.Agent, original.Agent)
		}
		if decoded.Trigger != original.Trigger {
			t.Errorf("Trigger = %q, want %q", decoded.Trigger, original.Trigger)
		}
		if len(decoded.Succeeded) != 2 || decoded.Succeeded[0] != "implemented parser" {
			t.Errorf("Succeeded = %v, want %v", decoded.Succeeded, original.Succeeded)
		}
		if len(decoded.Failed) != 1 || decoded.Failed[0] != original.Failed[0] {
			t.Errorf("Failed = %v, want %v", decoded.Failed, original.Failed)
		}
		if decoded.Hypothesis != original.Hypothesis {
			t.Errorf("Hypothesis = %q, want %q", decoded.Hypothesis, original.Hypothesis)
		}
		if decoded.NextStep != original.NextStep {
			t.Errorf("NextStep = %q, want %q", decoded.NextStep, original.NextStep)
		}
		if len(decoded.KeyFiles) != 2 || decoded.KeyFiles[0] != "internal/parser.go" {
			t.Errorf("KeyFiles = %v, want %v", decoded.KeyFiles, original.KeyFiles)
		}
		if len(decoded.DeadEnds) != 1 || decoded.DeadEnds[0] != original.DeadEnds[0] {
			t.Errorf("DeadEnds = %v, want %v", decoded.DeadEnds, original.DeadEnds)
		}
	})

	t.Run("minimal event omits optional fields", func(t *testing.T) {
		original := HandoffEvent{
			Timestamp: ts,
			Agent:     "reviewer-1",
			Trigger:   HandoffTriggerCompletion,
		}

		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		yamlStr := string(data)
		// Optional fields should not appear in output
		for _, field := range []string{"succeeded", "failed", "hypothesis", "next_step", "key_files", "dead_ends"} {
			if strings.Contains(yamlStr, field+":") {
				t.Errorf("minimal event should omit %q, got:\n%s", field, yamlStr)
			}
		}

		var decoded HandoffEvent
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.Agent != "reviewer-1" {
			t.Errorf("Agent = %q, want %q", decoded.Agent, "reviewer-1")
		}
		if decoded.Trigger != HandoffTriggerCompletion {
			t.Errorf("Trigger = %q, want %q", decoded.Trigger, HandoffTriggerCompletion)
		}
	})

	t.Run("task with handoff events round-trips", func(t *testing.T) {
		task := Task{
			ID:     "test-task",
			Status: TaskStatusImplementing,
			HandoffEvents: []HandoffEvent{
				{
					Timestamp: ts,
					Agent:     "coder-1",
					Trigger:   HandoffTriggerContextExhaustion,
					Succeeded: []string{"partial implementation"},
					NextStep:  "continue from parser.go",
				},
				{
					Timestamp: ts.Add(time.Hour),
					Agent:     "coder-2",
					Trigger:   HandoffTriggerSubmission,
				},
			},
			Created: ts,
		}

		data, err := yaml.Marshal(task)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded Task
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(decoded.HandoffEvents) != 2 {
			t.Fatalf("HandoffEvents count = %d, want 2", len(decoded.HandoffEvents))
		}
		if decoded.HandoffEvents[0].Agent != "coder-1" {
			t.Errorf("HandoffEvents[0].Agent = %q, want %q", decoded.HandoffEvents[0].Agent, "coder-1")
		}
		if decoded.HandoffEvents[1].Trigger != HandoffTriggerSubmission {
			t.Errorf("HandoffEvents[1].Trigger = %q, want %q", decoded.HandoffEvents[1].Trigger, HandoffTriggerSubmission)
		}
	})

	t.Run("task without handoff events omits field", func(t *testing.T) {
		task := Task{
			ID:      "no-handoff-task",
			Status:  TaskStatusDraft,
			Created: ts,
		}

		data, err := yaml.Marshal(task)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if strings.Contains(string(data), "handoff_events") {
			t.Errorf("task without handoff events should omit field, got:\n%s", string(data))
		}
	})
}

func TestCircuitBreakerResponseYAMLRoundTripAndLegacyCompatibility(t *testing.T) {
	timestamp := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	haltTimestamp := timestamp.Add(time.Minute)
	pattern := "provider_audit_degradation"
	original := CircuitBreaker{
		Status: "TRIGGERED",
		CurrentTrigger: &CircuitBreakerTrigger{
			Timestamp: haltTimestamp, Pattern: pattern, Severity: "OBSERVABILITY_DEGRADED",
			ReportFile: ".liza/circuit_breaker_report.md",
		},
		CurrentResponse: &CircuitBreakerResponse{
			Timestamp: haltTimestamp, Pattern: pattern, Severity: "OBSERVABILITY_DEGRADED",
			Response:       CircuitBreakerResponseHalt,
			Classification: CircuitBreakerEvidenceContinuing,
			Explanation:    "all exact current provider epochs are degraded",
			ReportFile:     ".liza/circuit_breaker_report.md",
		},
		History: []CircuitBreakerHistory{
			{
				Timestamp: timestamp, Pattern: &pattern, Result: "CHECKPOINT",
				Response:             CircuitBreakerResponseCheckpoint,
				Classification:       CircuitBreakerEvidenceContinuing,
				Explanation:          "current provider health is unknown",
				SupersededByResponse: CircuitBreakerResponseHalt,
			},
			{
				Timestamp: haltTimestamp, Pattern: &pattern, Result: "TRIGGERED",
				Response:       CircuitBreakerResponseHalt,
				Classification: CircuitBreakerEvidenceContinuing,
				Explanation:    "all exact current provider epochs are degraded",
			},
		},
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded CircuitBreaker
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Errorf("round trip = %#v, want %#v", decoded, original)
	}
	if !strings.Contains(string(data), "superseded_by_response: HALT") {
		t.Fatalf("marshal missing superseded_by_response:\n%s", data)
	}

	legacyYAML := `status: OK
history:
  - timestamp: 2026-08-21T10:00:00Z
    pattern: retry_cluster
    severity: ARCHITECTURE_FLAW
    result: TRIGGERED
`
	var legacy CircuitBreaker
	if err := yaml.Unmarshal([]byte(legacyYAML), &legacy); err != nil {
		t.Fatalf("unmarshal legacy circuit breaker: %v", err)
	}
	if legacy.CurrentResponse != nil {
		t.Errorf("legacy CurrentResponse = %#v, want nil", legacy.CurrentResponse)
	}
	if len(legacy.History) != 1 {
		t.Fatalf("legacy History length = %d, want 1", len(legacy.History))
	}
	if legacy.History[0].Response != "" || legacy.History[0].Classification != "" || legacy.History[0].Explanation != "" || legacy.History[0].SupersededByResponse != "" {
		t.Errorf("legacy typed fields = %#v, want zero values", legacy.History[0])
	}
}
