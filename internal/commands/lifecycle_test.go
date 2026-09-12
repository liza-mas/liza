package commands

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func TestLifecycleText_ReplayDoesNotClaimFreshEffects(t *testing.T) {
	replay := models.LifecycleOutcome{
		Operation: "wt-merge", TaskID: "target", Outcome: models.LifecycleAlreadyCompleted,
		SafeAction: "stop", TaskStatus: models.TaskStatusMerged, Effects: "committed",
		TransitionID: "current-boundary", CompletedTransitionID: "original-completion", RequestID: "same-request",
	}
	for _, tc := range []struct {
		name      string
		print     func()
		forbidden []string
	}{
		{"merge", func() { printMergeResult(&ops.MergeResult{LifecycleOutcome: replay}) }, []string{"merge successful", "Merge commit created", "tests passed"}},
		{"claim", func() { printClaimResult(&ops.ClaimResult{LifecycleOutcome: replay}) }, []string{"IMPLEMENTING:", "lease_expires:", "worktree:"}},
		{"verdict", func() { printVerdictResult(&ops.VerdictResult{LifecycleOutcome: replay}) }, []string{"APPROVED:", "REJECTED:"}},
		{"blocked", func() { _ = printMarkBlockedResult(&ops.MarkBlockedResult{LifecycleOutcome: replay}, nil) }, []string{"marked as BLOCKED"}},
		{"assessment", func() { _ = printAssessBlockedResult(&ops.AssessBlockedResult{LifecycleOutcome: replay}, nil) }, []string{"assessed by orchestrator"}},
		{"repair", func() {
			_ = printApplyDependencyRepairResult(&ops.ApplyDependencyRepairResult{LifecycleOutcome: replay}, nil)
		}, []string{"Applied dependency repair"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := captureStdout(t, tc.print)
			for _, want := range []string{"outcome: ALREADY_COMPLETED", "safe_action: stop", "task_status: MERGED", "transition_id: current-boundary", "completed_transition_id: original-completion"} {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q in %s", want, output)
				}
			}
			for _, forbidden := range tc.forbidden {
				if strings.Contains(output, forbidden) {
					t.Errorf("replay claims fresh effect %q", forbidden)
				}
			}
		})
	}
}

func TestLifecycleText_UsesServerSelectedAction(t *testing.T) {
	for _, action := range []string{"continue", "stop", "requery", "retry", "correct_input"} {
		var output bytes.Buffer
		WriteLifecycleOutcome(&output, models.LifecycleOutcome{Outcome: models.LifecycleStateChanged, SafeAction: action, TaskStatus: "UNKNOWN", Effects: "unknown"})
		if strings.Count(output.String(), "safe_action:") != 1 || !strings.Contains(output.String(), "safe_action: "+action+"\n") {
			t.Fatalf("presenter changed action: %s", output.String())
		}
	}
}

func TestLifecycleMetricsPresentation_AvailabilityWindowAndOutcomes(t *testing.T) {
	when := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		metrics       *models.LifecycleOutcomeMetrics
		wants, absent []string
	}{
		{"legacy", nil, []string{"Lifecycle outcomes: unavailable"}, []string{"No lifecycle invocations", "Observed since:"}},
		{"unavailable", &models.LifecycleOutcomeMetrics{Warning: "diagnostic file missing"}, []string{"unavailable", "diagnostic file missing"}, []string{"No lifecycle invocations"}},
		{"measured-zero", &models.LifecycleOutcomeMetrics{Available: true, ObservedSince: &when, LastUpdated: &when}, []string{"available (best-effort observations)", "Observed since: 2026-09-12T12:00:00Z", "No lifecycle invocations observed in this window."}, nil},
		{"measured-outcomes", &models.LifecycleOutcomeMetrics{Available: true, ObservedSince: &when, Counts: map[string]map[string]uint64{"submit-for-review": {"COMPLETED": 2, "ALREADY_COMPLETED": 1}}}, []string{"submit-for-review COMPLETED: 2", "submit-for-review ALREADY_COMPLETED: 1"}, []string{"No lifecycle invocations"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := buildMetricsInfo(models.SprintMetrics{LifecycleOutcomes: tc.metrics})
			text, err := formatMetricsValue(info)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wants {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in %s", want, text)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(text, absent) {
					t.Errorf("misleading %q in %s", absent, text)
				}
			}
			if tc.metrics != nil {
				for _, format := range []string{"json", "yaml"} {
					output, err := formatMetricsOutput(info, format)
					if err != nil || !strings.Contains(output, "lifecycle_outcomes") || !strings.Contains(output, "available") {
						t.Errorf("metrics projection dropped availability: %s %v", output, err)
					}
				}
			}
		})
	}
}

func TestLifecycleRecoverAgent_RespawnFailureKeepsCommittedRecovery(t *testing.T) {
	result := &ops.RecoverAgentResult{
		AgentID: "coder-1", Role: "coder", AgentDeleted: true,
		LifecycleOutcome: models.LifecycleOutcome{
			Operation: "recover-agent", TaskID: "target", Outcome: models.LifecycleCompleted,
			SafeAction: "continue", TaskStatus: models.TaskStatusReady, Effects: "committed",
			RequestID: "recovery-request", TransitionID: "current-boundary", CompletedTransitionID: "recovery-completion",
		},
	}
	calls := 0
	err := respawnRecoveredAgent(result, "codex", func(binary string, args, env []string) error {
		calls++
		if binary == "" || len(args) < 3 || args[2] != "coder" {
			t.Fatal("respawn lost executable or role")
		}
		return syscall.ENOEXEC
	})
	var failure *ops.LifecycleError
	if !errors.As(err, &failure) || !errors.Is(err, syscall.ENOEXEC) {
		t.Fatalf("lost typed outcome or exec cause: %v", err)
	}
	if calls != 1 || failure.Outcome.Outcome != models.LifecycleStateChanged || failure.Outcome.SafeAction != "requery" || failure.Outcome.Effects != "committed" {
		t.Fatalf("failed respawn must not invite recovery replay: %+v", failure.Outcome)
	}
	if failure.Outcome.CompletedTransitionID != "recovery-completion" || failure.Outcome.RequestID != "recovery-request" || failure.Outcome.TransitionID != "current-boundary" {
		t.Fatal("respawn failure discarded the committed recovery identity")
	}
	if result.Outcome != models.LifecycleCompleted || result.SafeAction != "continue" {
		t.Fatal("error projection modified original recovery receipt")
	}
}
