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
			"approved-proof reference drift", "acme-cli reaffirm-proof --help", "orchestrator-only remedy", "one transition only", "no status change or approval",
			"Blocked only on existing unfinished work", "then unblock without --assign-to", "hold with assess-blocked --awaits", "break the cycle first", "add --awaits <id>[,<id>]", "--clear-awaits drops it", "repeat the same --awaits",
			"RCA owner cannot establish the cause from reads", "hold the owner on it as for unfinished work", "Never re-assess it unchanged"},
		"HYPOTHESIS_EXHAUSTED": {noDefault, lineage, "after the owning specialist has confirmed"},
		"IMMEDIATE_DISCOVERY":  {noDefault, lineage},
		"PLANNING_COMPLETE":    {report, "planning-only run against its intended deliverable", "invent no planning-to-coding ratio"},
		"MANY_TO_ONE_READY":    {report},
		"CODING_COMPLETE":      {report, "coding complete is not delivery"},
		"SPRINT_COMPLETE":      {report},
	}
	for trigger, required := range wakes {
		rendered, err := RenderWakeInstructions(WakeTrigger(trigger), "orchestrator-1", testMatcherText())
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

// D-111: the blocked-task wake offers the in-place scope-exception route with
// its narrow eligibility, and only a coder's blocking protocol asks for the
// in-place candidate.
func TestBlockedTaskInPlaceScopeExceptionRoute(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName = "acme-cli"
		brand.ProjectDirName = ".acme-run"
	})
	rendered, err := RenderWakeInstructions("BLOCKED_TASKS", "orchestrator-1", testMatcherText())
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{
		"In-task-fixable gap", "only test support, fixtures or its own constants",
		"production code, merged contracts, acceptance and provider obligations unchanged",
		"no other unmerged task writes those files", "no unsettled human-owned decision",
		"unblock-task --scope-exception-file", "acme-cli unblock-task --help",
		"Prefer it to a correction plan or replacement", "in-place scope exception, owner correction",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Errorf("BLOCKED_TASKS missing %q", phrase)
		}
	}
	const candidate = "the orchestrator may grant it as a scope exception"
	for role, want := range map[string]bool{"coder": true, "integration-analyst": false} {
		protocol, err := BuildRoleContext(role, []string{"blocking-protocol"}, &RoleContextData{Role: role, AgentID: role + "-1", RoleType: "doer", TaskID: "task-1"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(protocol, candidate) != want {
			t.Errorf("%s blocking protocol in-place candidate = %v, want %v", role, !want, want)
		}
	}
}

// D-145: the blocked-task wake routes a timeout-only admission failure to a
// human-ruled budget exception instead of a correction plan.
func TestBlockedTaskBudgetExceptionRoute(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName = "acme-cli"
		brand.ProjectDirName = ".acme-run"
	})
	rendered, err := RenderWakeInstructions("BLOCKED_TASKS", "orchestrator-1", testMatcherText())
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{
		"Admission timed out, only timeout_seconds wrong",
		"human ruling: unblock-task --budget-exception <s> --budget-ruling <id>, no correction plan",
		"none: assess-blocked --human-action",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Errorf("BLOCKED_TASKS missing %q", phrase)
		}
	}
}
