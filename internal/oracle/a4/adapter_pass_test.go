package a4

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/ops"
)

func init() {
	helperOps["pass-operator"] = func(root string) (string, error) { return passReport(root, ops.AdmitOperator) }
	helperOps["pass-reviewed"] = func(root string) (string, error) { return passReport(root, ops.AdmitReviewed) }
}

// passReport runs one manual transition pass and renders its whole report, so
// executions, failures and warnings are all pinned. Unlike auto-transitions, a
// failure is output, not an error: the pass is meant to report it.
func passReport(root string, admission ops.TransitionAdmission) (string, error) {
	report, err := ops.ExecuteTransitionsReportWith(root, "manual", admission)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, result := range report.Results {
		lines = append(lines, fmt.Sprintf("result %s %s children=%v cohort=%v retired=%v",
			result.SourceTaskID, result.TransitionName, result.ChildTaskIDs, result.CohortTaskIDs, result.RetiredTaskIDs))
	}
	for _, failure := range report.Failures {
		lines = append(lines, "failure "+failure.String())
	}
	for _, warning := range report.Warnings {
		lines = append(lines, "warning "+warning)
	}
	return fmt.Sprintf("results: %d failures: %d warnings: %d\n%s", len(report.Results), len(report.Failures), len(report.Warnings), strings.Join(lines, "\n")), nil
}
