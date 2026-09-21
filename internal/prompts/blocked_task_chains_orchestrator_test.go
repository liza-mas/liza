package prompts

import (
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

// The orchestrator's wake instructions prescribe cause-based recovery and the
// delivery report at every final wake; the retired ambiguity-to-supersession
// instruction is gone; the doer blocking protocol no longer prescribes an
// edge for a correction owed by another stage.
func TestBlockedTaskChainsOrchestratorAndBlockingProtocol(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName = "acme-cli"
		brand.ProjectDirName = ".acme-run"
	})
	rawDefaultBrand := regexp.MustCompile(`(?i)(^|[^A-Za-z])liza($|[^A-Za-z0-9])|\{\{binaryName\}\}`)
	const report = "DELIVERY REPORT (final response, from state and evidence"
	const noDefault = "Supersession is not the default for ambiguity"
	const lineage = "review the logical lineage"
	wakes := map[string][]string{
		"BLOCKED_TASKS": {report, noDefault, lineage, "never a backward edge", "not supersession by default", "cancel-task for removed-only scope", "name the closure evidence",
			"approved-proof reference drift", "acme-cli reaffirm-proof --help", "orchestrator-only remedy", "one transition only", "no status change or approval"},
		"HYPOTHESIS_EXHAUSTED": {noDefault, lineage, "after the owning specialist has confirmed"},
		"IMMEDIATE_DISCOVERY":  {noDefault, lineage},
		"PLANNING_COMPLETE":    {report, "planning-only run against its intended deliverable", "invent no planning-to-coding ratio"},
		"MANY_TO_ONE_READY":    {report},
		"CODING_COMPLETE":      {report, "coding complete is not delivery"},
		"SPRINT_COMPLETE":      {report},
	}
	for trigger, required := range wakes {
		rendered, err := RenderWakeInstructions(trigger, "orchestrator-1")
		if err != nil {
			t.Fatalf("%s: %v", trigger, err)
		}
		for _, phrase := range required {
			if !strings.Contains(rendered, phrase) {
				t.Errorf("%s missing %q", trigger, phrase)
			}
		}
		for _, retired := range []string{"Spec ambiguity → clarify spec, then supersede", "Wrong approach → supersede task"} {
			if strings.Contains(rendered, retired) {
				t.Errorf("%s still carries retired instruction %q", trigger, retired)
			}
		}
		if m := rawDefaultBrand.FindString(rendered); m != "" {
			t.Errorf("%s leaks %q", trigger, m)
		}
	}

	protocol, err := BuildRoleContext("coder", []string{"blocking-protocol"}, &RoleContextData{Role: "coder", AgentID: "coder-1", RoleType: "doer", TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"compact diagnosis", "pipeline direction allows as a prerequisite", "is a hold, not an edge", "the engine rejects backward dependencies"} {
		if !strings.Contains(protocol, phrase) {
			t.Errorf("blocking protocol missing %q", phrase)
		}
	}
	if strings.Contains(protocol, "If another concrete task is blocking this task") {
		t.Error("blocking protocol still carries the unqualified add-edge direction")
	}
}
