package commands

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// WriteLifecycleOutcome presents the server's policy without inferring success
// or retryability from the legacy operation message.
func WriteLifecycleOutcome(w io.Writer, outcome models.LifecycleOutcome) {
	if outcome.Outcome == "" {
		return
	}
	fmt.Fprintf(w, "outcome: %s\nsafe_action: %s\ntask_status: %s\neffects: %s\n", outcome.Outcome, outcome.SafeAction, outcome.TaskStatus, outcome.Effects)
	for _, field := range [][2]string{
		{"task_id", outcome.TaskID}, {"transition_id", outcome.TransitionID},
		{"completed_transition_id", outcome.CompletedTransitionID}, {"request_id", outcome.RequestID},
		{"current_assignee", outcome.CurrentAssignee}, {"current_reviewer", outcome.CurrentReviewer},
	} {
		if field[1] != "" {
			fmt.Fprintf(w, "%s: %s\n", field[0], field[1])
		}
	}
}

// A replay is historical completion: do not print legacy messages implying
// this invocation ran setup, tests, alerts, cleanup or another fresh mutation.
func printLifecycleResult(outcome models.LifecycleOutcome) bool {
	WriteLifecycleOutcome(os.Stdout, outcome)
	return outcome.Outcome != "" && outcome.Outcome != models.LifecycleCompleted
}

func formatLifecycleMetrics(metrics *models.LifecycleOutcomeMetrics) string {
	var text strings.Builder
	if metrics == nil || !metrics.Available {
		text.WriteString("Lifecycle outcomes: unavailable\n")
		if metrics != nil && metrics.Warning != "" {
			fmt.Fprintf(&text, "  Warning: %s\n", metrics.Warning)
		}
		return text.String()
	}
	text.WriteString("Lifecycle outcomes: available (best-effort observations)\n")
	if metrics.ObservedSince != nil {
		fmt.Fprintf(&text, "  Observed since: %s\n", metrics.ObservedSince.Format(time.RFC3339Nano))
	}
	if metrics.LastUpdated != nil {
		fmt.Fprintf(&text, "  Last updated: %s\n", metrics.LastUpdated.Format(time.RFC3339Nano))
	}
	operations := make([]string, 0, len(metrics.Counts))
	for operation := range metrics.Counts {
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	observed := false
	for _, operation := range operations {
		outcomes := make([]string, 0, len(metrics.Counts[operation]))
		for outcome := range metrics.Counts[operation] {
			outcomes = append(outcomes, outcome)
		}
		sort.Strings(outcomes)
		for _, outcome := range outcomes {
			if count := metrics.Counts[operation][outcome]; count != 0 {
				fmt.Fprintf(&text, "  %s %s: %d\n", operation, outcome, count)
				observed = true
			}
		}
	}
	if !observed {
		text.WriteString("  No lifecycle invocations observed in this window.\n")
	}
	if metrics.Warning != "" {
		fmt.Fprintf(&text, "  Warning: %s\n", metrics.Warning)
	}
	return text.String()
}
