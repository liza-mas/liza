package commands

import (
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C2 P2b: pool repair projects reviewer eligibility with the same canonical
// provider keys the claim uses, built once from the projected state's config.

const c2PlanReviewerRole = "code-plan-reviewer"

// c2AliasPoolState is a second-review code-planning task (preferred provider
// diversity) whose doer ran provider doer, with one live idle reviewer on
// idleProvider and the prior approver, registered on approverProvider.
func c2AliasPoolState(doer, idleProvider, approverProvider string, config models.Config) *models.State {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = config.AgentTools
	doerAgent := testhelpers.RegisteredTestAgent(models.RoleCodePlanner)
	doerAgent.Provider = doer
	idle := testhelpers.RegisteredTestAgent(c2PlanReviewerRole)
	idle.Provider = idleProvider
	approver := testhelpers.RegisteredTestAgent(c2PlanReviewerRole)
	approver.Provider = approverProvider
	state.Agents = map[string]models.Agent{
		"code-planner-1":       doerAgent,
		"code-plan-reviewer-1": idle,
		"code-plan-reviewer-2": approver,
	}
	assignedTo, reviewCommit := "code-planner-1", "review123"
	state.Tasks = []models.Task{{
		ID: "plan-1", Status: "CODING_PLAN_MAIN_PARTIALLY_APPROVED", RolePair: "code-planning-main-pair",
		Priority: 1, AssignedTo: &assignedTo, ReviewCommit: &reviewCommit, Created: now,
		History:   []models.TaskHistoryEntry{},
		Approvals: []models.Approval{{Agent: "code-plan-reviewer-2", Provider: approverProvider, Timestamp: now}},
	}}
	return state
}

func c2MissingPlanReviewerTasks(t *testing.T, state *models.State) []string {
	t.Helper()
	resolver, err := ops.LoadResolverForModels(writeRepairAgentPoolState(t, testhelpers.CreateValidState()))
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	work, _ := roleWorkFor(FindMissingRolesWithClaimableWork(state, resolver, rolemodels.File{}), c2PlanReviewerRole)
	return work.TaskIDs
}

func TestC2PoolRepairProjectsCanonicalDoerDiversity(t *testing.T) {
	sharedKeys := models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-one": {Executable: "one", ProviderKey: "shared"},
		"zz-two": {Executable: "two", ProviderKey: "shared"},
	}}
	tests := []struct {
		name                 string
		config               models.Config
		doer, idle, approver string
		wantMissing          []string
	}{
		{"an idle alias of the doer is not eligible while a different provider is registered", models.Config{}, "codex", "codex-acp", "claude", []string{"plan-1"}},
		{"custom tools sharing a key are not eligible either", sharedKeys, "zz-one", "zz-two", "claude", []string{"plan-1"}},
		{"an idle reviewer of a different backend is eligible", models.Config{}, "codex", "gemini", "claude", nil},
		{"an alias-only pool has no alternative, so the alias is eligible", models.Config{}, "codex", "codex-acp", "codex", nil},
		{"keyless custom tools stay distinct and eligible", models.Config{}, "zz-one", "zz-two", "claude", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := c2AliasPoolState(tt.doer, tt.idle, tt.approver, tt.config)

			if got := c2MissingPlanReviewerTasks(t, state); !slices.Equal(got, tt.wantMissing) {
				t.Fatalf("tasks without an eligible reviewer = %v, want %v", got, tt.wantMissing)
			}
			if got := state.Agents["code-plan-reviewer-1"].Provider; got != tt.idle {
				t.Fatalf("projection rewrote the stored provider: %q", got)
			}
		})
	}
}

func TestC2ReviewerStartPlannerDerivesKeysFromProjectedConfig(t *testing.T) {
	now := time.Now().UTC()
	state := c2AliasPoolState("codex", "claude", "claude", models.Config{})
	pr, err := ops.LoadResolverForModels(writeRepairAgentPoolState(t, testhelpers.CreateValidState()))
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	policy, ok := pr.(ops.ReviewerClaimPolicyResolver)
	if !ok {
		t.Fatalf("%T does not expose the review policy", pr)
	}
	plan := func() (string, int, bool) {
		planner := newReviewerStartPlanner(state, pr, RepairCLI{Explicit: "codex-acp"}, c2PlanReviewerRole, nil, now)
		if planner == nil {
			t.Fatal("planner not built")
		}
		return planner.assign(&state.Tasks[0], policy)
	}

	if _, _, ok := plan(); ok {
		t.Fatal("a reviewer started on codex-acp shares the doer codex's key while claude is registered: no start can serve the task")
	}
	state.Config.AgentTools = map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}
	if _, _, ok := plan(); !ok {
		t.Fatal("after the config split codex-acp is a different provider: a start can serve the task")
	}
}

func TestC2CapacityDemandMatchesIdleReviewersByCanonicalKey(t *testing.T) {
	pr, err := ops.LoadResolverForModels(writeRepairAgentPoolState(t, testhelpers.CreateValidState()))
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	spawnFor := func(idleProvider string) int {
		state := c2AliasPoolState("codex", idleProvider, "claude", models.Config{})
		missing, _ := FindRoleCapacityDeficits(state, pr, RepairCLI{Explicit: "claude"}, nil, time.Now().UTC())
		work, _ := roleWorkFor(missing, c2PlanReviewerRole)
		return work.SpawnCount
	}

	if got := spawnFor("codex-acp"); got != 1 {
		t.Fatalf("idle codex-acp reviewer shares the codex doer's key and cannot cover the task: spawn = %d, want 1", got)
	}
	if got := spawnFor("gemini"); got != 0 {
		t.Fatalf("idle reviewer of a different backend covers the task: spawn = %d, want 0", got)
	}
}
