package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The A4 spellings below are persisted, hashed, rendered in operator guidance or
// read by other areas, so each named value and typed set is pinned literal by
// literal: renaming a constant must not change what it spells.

func TestA4VocabularySpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		got, want []string
	}{
		{
			name: "discoveryDisposition",
			got:  []string{string(discoveryDispositionDeferred), string(discoveryDispositionDismissed)},
			want: []string{"deferred", "dismissed"},
		},
		{
			name: "operation names",
			got:  []string{addHumanNoteOperation, setTaskOutputOperation, addTaskOperation, proceedOperation},
			want: []string{"add-human-note", "set-task-output", "add-task", "proceed"},
		},
		{
			name: "completion authorization",
			got: []string{
				integrationCompletionLockName, completionProgressionLabelPrefix,
				integrationIncompleteCode, integrationStateChangedCode,
			},
			want: []string{
				"integration-completion", "progression ",
				"integration_incomplete", "integration_state_changed",
			},
		},
		{
			name: "role-pair fallbacks",
			got:  []string{legacyPlanningRolePair, baseCommitSnapshotRolePair},
			want: []string{"code-planning-pair", "coding-pair"},
		},
		{
			name: "breaker pattern",
			got:  []string{providerAuditDegradationPattern},
			want: []string{"provider_audit_degradation"},
		},
		{
			name: "rejection-RCA history keys",
			got: []string{
				rejectionRCABlockedClassKey, rejectionRCAGatedAtKey, rejectionRCAFirstRejectionAtKey,
				rejectionRCACausesKey, rejectionRCARecoveryPathKey,
			},
			want: []string{"blocked_class", "gated_at", "first_rejection_at", "causes", "recovery_path"},
		},
		{
			name: "obligation drift detail keys",
			got: []string{
				driftDetailPath, driftDetailHeading, driftDetailChange, driftDetailReviewedSection,
				driftDetailCurrentSection, driftDetailCarriers, driftDetailObligations, driftDetailReferenceIDs,
			},
			want: []string{
				"path", "heading", "change", "reviewed_section",
				"current_section", "carriers", "obligations", "reference_ids",
			},
		},
		{
			name: "transition history keys",
			got:  []string{transitionExtraKey, cycleMembersExtraKey, outputIndexExtraKey},
			want: []string{"transition", "cycle_members", "output_index"},
		},
		{
			name: "replacement history keys",
			got:  []string{replacementExtraSourceNewTransition, replacementExtraRetargetedConsumers},
			want: []string{"source_new_transition_id", "retargeted_consumers"},
		},
		{
			name: "alert categories",
			got:  []string{planHandoffFailedAlertCategory, planDispositionMissingAlertCategory},
			want: []string{"PLAN HANDOFF FAILED", "PLAN DISPOSITION MISSING"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if len(tt.got) != len(tt.want) {
				t.Fatalf("%d values, want %d", len(tt.got), len(tt.want))
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("value %d = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

// Numeric thresholds and permissions are pinned by value, written in decimal so
// the expectation does not share a literal notation with the declaration.
func TestA4NamedNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		got, want int
	}{
		{"suspiciousRateMinCount", suspiciousRateMinCount, 3},
		{"suspiciousRatePercent", suspiciousRatePercent, 95},
		{"amendReasonMaxBytes", amendReasonMaxBytes, 4096},
		{"sprintArchiveDirMode", int(sprintArchiveDirMode), 493},
		{"sprintArchiveFileMode", int(sprintArchiveFileMode), 420},
		{"sprintSummaryFileMode", int(sprintSummaryFileMode), 420},
		{"circuitBreakerReportFileMode", int(circuitBreakerReportFileMode), 420},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestA4DiscoveryDispositionSentinels(t *testing.T) {
	t.Parallel()

	for disposition, want := range map[string]bool{
		"deferred": true, "dismissed": true, "Deferred": false, "dismiss": false, "t1": false, "": false,
	} {
		if got := discoveryDisposition(disposition).isSentinel(); got != want {
			t.Errorf("%q isSentinel = %v, want %v", disposition, got, want)
		}
	}
}

// The suspicious-rate warning needs both enough samples and a rate strictly
// above the threshold, on each of its two metrics.
func TestA4SuspiciousRateBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		metrics   models.SprintMetrics
		wantCount int
	}{
		{"verdicts at the minimum count", models.SprintMetrics{ReviewVerdictCount: 3, ReviewVerdictApprovalRatePercent: 100}, 1},
		{"verdicts below the minimum count", models.SprintMetrics{ReviewVerdictCount: 2, ReviewVerdictApprovalRatePercent: 100}, 0},
		{"verdicts at the rate threshold", models.SprintMetrics{ReviewVerdictCount: 20, ReviewVerdictApprovalRatePercent: 95}, 0},
		{"verdicts above the rate threshold", models.SprintMetrics{ReviewVerdictCount: 20, ReviewVerdictApprovalRatePercent: 96}, 1},
		{"outcomes at the minimum count", models.SprintMetrics{TaskSubmittedForReviewCount: 3, TaskOutcomeApprovalRatePercent: 100}, 1},
		{"outcomes below the minimum count", models.SprintMetrics{TaskSubmittedForReviewCount: 2, TaskOutcomeApprovalRatePercent: 100}, 0},
		{"outcomes at the rate threshold", models.SprintMetrics{TaskSubmittedForReviewCount: 20, TaskOutcomeApprovalRatePercent: 95}, 0},
		{"outcomes above the rate threshold", models.SprintMetrics{TaskSubmittedForReviewCount: 20, TaskOutcomeApprovalRatePercent: 96}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := len(CheckSuspiciousRates(tt.metrics)); got != tt.wantCount {
				t.Errorf("%d warnings, want %d", got, tt.wantCount)
			}
		})
	}
}

// A task wakes the orchestrator as hypothesis-exhausted from the second
// distinct failure on, not the first.
func TestA4RepeatedFailureWakeThreshold(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name     string
		failedBy []string
		want     int
	}{
		{"one failure", []string{"coder-1"}, 0},
		{"two failures", []string{"coder-1", "coder-2"}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("target", models.TaskStatusReady, at)
			task.FailedBy = tt.failedBy
			task.History = append(task.History,
				models.TaskHistoryEntry{Time: at, Event: models.TaskEventOrchestratorAssessment},
				models.TaskHistoryEntry{Time: at.Add(time.Second), Event: models.TaskEventRejected})
			state.Tasks = []models.Task{task}
			if got := CountActionableHypothesisExhaustedTasks(state); got != tt.want {
				t.Errorf("actionable hypothesis-exhausted tasks = %d, want %d", got, tt.want)
			}
		})
	}
}

// The reason that begins an amendment is bounded in bytes, not runes.
func TestA4AmendReasonLimit(t *testing.T) {
	t.Parallel()

	request := func(reason string) error {
		_, err := checkAmendPlanRequest(nil, AmendPlanInput{TaskID: "plan-1", ChangedBy: "operator", Reason: reason})
		return err
	}
	if err := request(strings.Repeat("x", 4096)); err != nil {
		t.Errorf("4096-byte reason refused: %v", err)
	}
	err := request(strings.Repeat("x", 4097))
	if err == nil || !strings.HasSuffix(err.Error(), "amend-plan requires a nonempty reason of at most 4096 bytes") {
		t.Errorf("4097-byte reason error = %v, want the 4096-byte refusal", err)
	}
	if err := request("é" + strings.Repeat("x", 4094)); err != nil {
		t.Errorf("4096-byte multi-byte reason refused: %v", err)
	}
	if err := request(" \n"); err == nil {
		t.Error("blank reason accepted")
	}
}
