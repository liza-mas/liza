package commands

import (
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The K7 constants of pool repair name values that command lines, rendered
// output and the validation records already carry; their spellings are frozen.
func TestRepairAgentPoolVocabularyKeepsFormerValues(t *testing.T) {
	for name, tc := range map[string]struct{ got, want string }{
		"--cli flag":                    {agentFlagCLI, "--cli"},
		"--agent-id flag":               {agentFlagAgentID, "--agent-id"},
		"--models-item flag":            {agentFlagModelsItem, "--models-item"},
		"orchestrator demand reason":    {orchestratorDemandReason, "no live orchestrator while goal is IN_PROGRESS"},
		"merge owner demand reason":     {mergeOwnerDemandReason, "approved task(s) with no live, healthy merge owner"},
		"unverified validation status":  {validationCapacityUnverified, "unverified"},
		"failed validation readiness":   {validationReadinessFailed, "failed"},
		"started-with of a bare CLI":    {startedWith("codex", 0), "--cli codex"},
		"started-with of a list item":   {startedWith("codex", 2), "models.yaml item 2 (codex)"},
		"orchestrator demand in a plan": {orchestratorRoleWork(orchestratorPresence{Roles: []string{"orchestrator"}}).Reason, "no live orchestrator while goal is IN_PROGRESS"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", name, tc.got, tc.want)
		}
	}
}

// planRoleStarts builds the command of each start from the flag constants: the
// explicit CLI, then the agent ID planned for that start when there is one.
func TestPlanRoleStartsBuildsCommandsFromFlagConstants(t *testing.T) {
	state, pr := testhelpers.CreateValidState(), loadRepairResolver(t)
	plan, err := planRoleStarts(RepairCLI{Explicit: "codex"}, MissingRoleWork{
		Role: "code-reviewer", SpawnCount: 2, AgentIDs: []string{"code-reviewer-1"},
	}, state, pr)
	if err != nil {
		t.Fatalf("planRoleStarts() error = %v", err)
	}
	if plan.cli != "codex" || plan.spawnCLI != "codex" || len(plan.starts) != 2 {
		t.Fatalf("plan = %+v, want two starts on codex", plan)
	}
	want := []string{
		brand.BinaryName + " agent code-reviewer --cli codex --agent-id code-reviewer-1",
		brand.BinaryName + " agent code-reviewer --cli codex",
	}
	for i, start := range plan.starts {
		if start.Command != want[i] {
			t.Errorf("start %d command = %q, want %q", i+1, start.Command, want[i])
		}
	}
}

// Roles started on one CLI keep it common; a role with no single CLI, or a
// second role on another, leaves none.
func TestImplicitRoleCLIsObserve(t *testing.T) {
	tests := []struct {
		name          string
		observed      []string
		wantCommon    string
		wantMixedCLIs bool
	}{
		{name: "none observed"},
		{name: "one CLI", observed: []string{"codex"}, wantCommon: "codex"},
		{name: "same CLI twice", observed: []string{"codex", "codex"}, wantCommon: "codex"},
		{name: "two CLIs", observed: []string{"codex", "claude"}, wantMixedCLIs: true},
		{name: "mixed role first", observed: []string{"", "codex"}, wantMixedCLIs: true},
		{name: "mixed stays mixed", observed: []string{"codex", "claude", "claude"}, wantMixedCLIs: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var implicit implicitRoleCLIs
			for _, cli := range tc.observed {
				implicit.observe(cli)
			}
			if implicit.common != tc.wantCommon || implicit.heterogeneous != tc.wantMixedCLIs {
				t.Fatalf("implicit CLIs = %+v, want common %q, heterogeneous %t", implicit, tc.wantCommon, tc.wantMixedCLIs)
			}
		})
	}
}
