package a6

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/ops"
)

// integrationHelper executes the production reconciliation in a new process,
// whose local timezone and environment were established before Go initialized.
func integrationHelper(root string) int {
	result, err := ops.ReconcileIntegrationAnalyses(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
