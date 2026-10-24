package a4

import (
	"fmt"

	"github.com/liza-mas/liza/internal/ops"
)

func init() {
	helperOps["auto-transitions"] = func(root string) (string, error) {
		report, err := ops.ExecuteTransitionsReportWith(root, "auto", ops.AdmitReviewed)
		if err != nil {
			return "", err
		}
		if len(report.Failures) != 0 {
			return "", fmt.Errorf("auto-transition failures: %v", report.Failures)
		}
		var children []string
		for _, result := range report.Results {
			children = append(children, result.ChildTaskIDs...)
		}
		return fmt.Sprintf("children: %v", children), nil
	}
}
