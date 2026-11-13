package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inputReadinessEstimateFragments pin the estimate mode and the guards that
// keep it apart from the rigorous readiness verdict.
var inputReadinessEstimateFragments = []string{
	"Two modes. **Rigorous** (the default) judges one document and gives a verdict.",
	"Open with: \"Estimate, not the readiness gate.\"",
	"An estimate is never a verdict: it never passes a gate and is never recorded as readiness.",
	"independence binds the rigorous mode only",
	"Measure against this run's scope (its in items).",
}

func TestInputReadinessHasAnEstimateMode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), "skills", "check-liza-input-readiness", "SKILL.md"))
	if err != nil {
		t.Fatalf("read check-liza-input-readiness: %v", err)
	}
	content := strings.Join(strings.Fields(string(raw)), " ")
	for _, fragment := range inputReadinessEstimateFragments {
		if !strings.Contains(content, fragment) {
			t.Errorf("check-liza-input-readiness: missing estimate rule %q", fragment)
		}
	}
}
