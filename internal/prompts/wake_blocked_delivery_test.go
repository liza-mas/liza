package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

// D-33: a prerequisite that can only move on through a many-to-one transition
// (a supplemental story) is commissioned with its delivery task in one request.
func TestBlockedWakeCommissionsDeadEndPrerequisiteWithDeliveryTask(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	output, err := buildInstructionsForWakeTrigger("BLOCKED_TASKS", "orchestrator-1",
		wakeTemplateData{BlockedScope: &blockedScopeData{TaskIDs: []string{"task-1"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"only onward transition is many-to-one",
		"same request as its delivery task",
		"refuses it alone",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("blocked wake missing %q", want)
		}
	}
	for _, leaked := range []string{"Liza", "LIZA", "liza", "{{binaryName}}"} {
		if strings.Contains(output, leaked) {
			t.Errorf("nondefault blocked wake leaks %q", leaked)
		}
	}
}
