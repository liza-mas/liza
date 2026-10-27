package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C2 P2a/P2d: reviewer diversity compares canonical provider keys, derived from
// the config of the state being evaluated; recorded providers stay tool names.

// c2PlanSubmitted is the submitted status of code-planning-main-pair, whose
// review policy asks for preferred provider diversity.
const c2PlanSubmitted = models.TaskStatus("CODING_PLAN_MAIN_TO_REVIEW")

func c2BuiltInKeys(config models.Config) agentlaunch.ProviderKeys {
	return agentlaunch.NewProviderKeys(agentlaunch.BuiltInAgentTools(), config)
}

func c2SharedKeyConfig() models.Config {
	return models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-one": {Executable: "one", ProviderKey: "shared"},
		"zz-two": {Executable: "two", ProviderKey: "shared"},
	}}
}

// c2ClaimFixture writes a disposable project holding one code-planning task
// (preferred provider diversity) submitted by doerProvider, with the claimer and
// one further reviewer registered.
func c2ClaimFixture(t *testing.T, config models.Config, doerProvider, claimerProvider string, otherReviewers ...string) (projectRoot, statePath string) {
	t.Helper()
	projectRoot = t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	statePath, _ = testhelpers.SetupLizaDir(t, projectRoot)

	state := testhelpers.CreateValidState()
	state.Config.AgentTools = config.AgentTools
	doer := testhelpers.RegisteredTestAgent(models.RoleCodePlanner)
	doer.Provider = doerProvider
	state.Agents["code-planner-1"] = doer
	state.Agents["code-plan-reviewer-1"] = reviewerCapacityTestAgent(models.RoleCodePlanReviewer, claimerProvider)
	for i, provider := range otherReviewers {
		id := "code-plan-reviewer-" + string(rune('2'+i))
		state.Agents[id] = reviewerCapacityTestAgent(models.RoleCodePlanReviewer, provider)
	}
	reviewCommit := "abc123"
	assignedTo := "code-planner-1"
	state.Tasks = []models.Task{{
		ID: "plan-task-1", Status: c2PlanSubmitted, RolePair: "code-planning-main-pair",
		Priority: 1, ReviewCommit: &reviewCommit, AssignedTo: &assignedTo,
		History: []models.TaskHistoryEntry{}, Created: time.Now().UTC(),
	}}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath
}

func c2Claim(t *testing.T, projectRoot string) error {
	t.Helper()
	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{
		ProjectRoot: projectRoot, AgentID: "code-plan-reviewer-1", Role: models.RoleCodePlanReviewer, LeaseDuration: 1800,
	})
	return err
}

func c2ReadTask(t *testing.T, statePath string) *models.Task {
	t.Helper()
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	task := state.FindTask("plan-task-1")
	if task == nil {
		t.Fatal("task plan-task-1 missing")
	}
	return task
}

func TestC2ClaimDoerDiversityComparesCanonicalKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    models.Config
		doer      string
		claimer   string
		others    []string
		wantClaim bool
	}{
		{"claude alternative blocks codex-acp claiming a codex task", models.Config{}, "codex", "codex-acp", []string{"claude"}, false},
		{"claude alternative blocks codex claiming a codex-acp task", models.Config{}, "codex-acp", "codex", []string{"claude"}, false},
		{"custom tools sharing a provider_key block each other", c2SharedKeyConfig(), "zz-one", "zz-two", []string{"claude"}, false},
		{"a truly different backend still claims", models.Config{}, "codex", "claude", []string{"codex-acp"}, true},
		{"an alias-only pool falls back and claims", models.Config{}, "codex", "codex-acp", []string{"codex"}, true},
		{"an alias of the doer is not a different-provider alternative", models.Config{}, "codex", "codex", []string{"codex-acp"}, true},
		{"keyless custom tools are distinct providers and claim", models.Config{AgentTools: map[string]models.AgentToolConfig{
			"zz-one": {Executable: "one"}, "zz-two": {Executable: "two"},
		}}, "zz-one", "zz-two", []string{"claude"}, true},
		{"tools that share a key only in another config stay distinct", models.Config{}, "zz-one", "zz-two", []string{"claude"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			projectRoot, statePath := c2ClaimFixture(t, tt.config, tt.doer, tt.claimer, tt.others...)

			err := c2Claim(t, projectRoot)

			task := c2ReadTask(t, statePath)
			if tt.wantClaim {
				if err != nil {
					t.Fatalf("ClaimReviewerTask() = %v, want the claim to proceed", err)
				}
				if task.ReviewingBy == nil || *task.ReviewingBy != "code-plan-reviewer-1" {
					t.Fatalf("ReviewingBy = %v, want the claimer", task.ReviewingBy)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "no reviewable tasks found") {
				t.Fatalf("ClaimReviewerTask() = %v, want the doer-diversity refusal", err)
			}
			if task.ReviewingBy != nil || task.Status != c2PlanSubmitted {
				t.Fatalf("a refused claim changed the task: status=%s reviewing_by=%v", task.Status, task.ReviewingBy)
			}
		})
	}
}

// The claim evaluates the config of the state it locks: the same persisted
// reviewers and task flip outcome when only agent_tools differs, and the stored
// providers stay tool names either way.
func TestC2ClaimUsesLockedStateConfig(t *testing.T) {
	t.Parallel()

	projectRoot, statePath := c2ClaimFixture(t, c2SharedKeyConfig(), "zz-one", "zz-two", "claude")
	if err := c2Claim(t, projectRoot); err == nil {
		t.Fatal("zz-one and zz-two share a key under the locked config: the claim must be refused")
	}

	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	state.Config.AgentTools["zz-two"] = models.AgentToolConfig{Executable: "two", ProviderKey: "split"}
	testhelpers.WriteInitialState(t, statePath, state)

	if err := c2Claim(t, projectRoot); err != nil {
		t.Fatalf("after the config split the tools are distinct providers: %v", err)
	}
	got, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents["code-plan-reviewer-1"].Provider != "zz-two" || got.Agents["code-planner-1"].Provider != "zz-one" {
		t.Fatalf("stored providers were rewritten: reviewer=%q doer=%q", got.Agents["code-plan-reviewer-1"].Provider, got.Agents["code-planner-1"].Provider)
	}
}

func TestC2ReviewerClaimEligibilityProjectionAgreesWithClaim(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	doerID := "coder-1"
	task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1":         {Role: "coder", Provider: "codex"},
		"code-reviewer-1": reviewerCapacityTestAgent("code-reviewer", "codex-acp"),
		"code-reviewer-2": reviewerCapacityTestAgent("code-reviewer", "claude"),
	}}
	resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}
	eligible := func(keys agentlaunch.ProviderKeys) bool {
		return ReviewerClaimEligible(ReviewerClaimEligibilityInput{
			State: state, Task: task, AgentID: "code-reviewer-1",
			ReviewerRole: "code-reviewer", Now: now, Resolver: resolver, Keys: keys,
		})
	}

	if eligible(c2BuiltInKeys(models.Config{})) {
		t.Fatal("codex-acp shares codex's key and a claude reviewer is registered: not eligible")
	}
	if !eligible(agentlaunch.ProviderKeys{}) {
		t.Fatal("zero-value keys compare recorded tool names exactly, as before C2")
	}
	state.Agents["code-reviewer-2"] = reviewerCapacityTestAgent("code-reviewer", "codex")
	if !eligible(c2BuiltInKeys(models.Config{})) {
		t.Fatal("an alias-only pool has no different-provider alternative: eligible")
	}
}

type c2PartialResolver struct{ *diversityTestResolver }

func (c2PartialResolver) PartiallyApprovedStatus(string) (models.TaskStatus, error) {
	return "PARTIAL", nil
}

// Persisted approvals whose reviewers are no longer registered are evaluated by
// the current config: adding a provider_key override flips the preference
// without rewriting any approval.
func TestC2PartiallyApprovedPreferenceUsesCurrentConfig(t *testing.T) {
	t.Parallel()

	pr := c2PartialResolver{&diversityTestResolver{pairs: map[string]diversityPairDef{"pair": {reviewer: "rv", submitted: "SUBMITTED"}}}}
	aliasApproved := &models.Task{ID: "alias-approved", RolePair: "pair", Priority: 1, Status: "PARTIAL",
		Approvals: []models.Approval{{Agent: "gone-1", Provider: "codex-acp"}}}
	otherApproved := &models.Task{ID: "other-approved", RolePair: "pair", Priority: 1, Status: "PARTIAL",
		Approvals: []models.Approval{{Agent: "gone-2", Provider: "claude"}}}
	candidates := []*models.Task{aliasApproved, otherApproved}
	const draws = 64
	picked := func(keys agentlaunch.ProviderKeys) map[string]int {
		counts := map[string]int{}
		for range draws {
			counts[selectBestCandidate(candidates, pr, "codex", "claimer-1", &models.State{}, keys).ID]++
		}
		return counts
	}

	if got := picked(c2BuiltInKeys(models.Config{})); got["other-approved"] != draws {
		t.Fatalf("picks = %v, want only other-approved: codex-acp's approval is the claimer's own provider", got)
	}
	split := models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}}
	if got := picked(c2BuiltInKeys(split)); got["alias-approved"] == 0 || got["other-approved"] == 0 {
		t.Fatalf("picks = %v, want both tasks diverse once the override splits the alias", got)
	}
	if aliasApproved.Approvals[0].Provider != "codex-acp" {
		t.Fatal("the persisted approval provider was rewritten")
	}
}

func TestC2FreshSubmissionSatisfiabilityComparesCanonicalKeys(t *testing.T) {
	t.Parallel()

	pr := &diversityTestResolver{pairs: map[string]diversityPairDef{
		"pair-alias":     {reviewer: "rv-alias", submitted: "SUBMITTED_A"},
		"pair-different": {reviewer: "rv-different", submitted: "SUBMITTED_D"},
	}}
	state := &models.State{Agents: map[string]models.Agent{
		"rv-alias-2":     reviewerCapacityTestAgent("rv-alias", "codex-acp"),
		"rv-different-2": reviewerCapacityTestAgent("rv-different", "claude"),
	}}
	aliasPool := &models.Task{ID: "alias-pool", RolePair: "pair-alias", Priority: 1, Status: "SUBMITTED_A"}
	differentPool := &models.Task{ID: "different-pool", RolePair: "pair-different", Priority: 1, Status: "SUBMITTED_D"}
	keys := c2BuiltInKeys(models.Config{})

	if isDiversitySatisfiable(aliasPool, "codex", "claimer-1", pr, state, keys) {
		t.Fatal("a codex-acp alternative does not make diversity satisfiable for a codex claimer")
	}
	if !isDiversitySatisfiable(differentPool, "codex", "claimer-1", pr, state, keys) {
		t.Fatal("a claude alternative makes diversity satisfiable for a codex claimer")
	}
	for range 32 {
		if got := selectBestCandidate([]*models.Task{aliasPool, differentPool}, pr, "codex", "claimer-1", state, keys); got.ID != "different-pool" {
			t.Fatalf("picked %q, want the task whose pool has a different provider", got.ID)
		}
	}
}

func TestC2ApprovalsHaveProviderDiversity(t *testing.T) {
	t.Parallel()

	approvals := func(providers ...string) []models.Approval {
		out := make([]models.Approval, len(providers))
		for i, p := range providers {
			out[i] = models.Approval{Agent: "rv-" + p, Provider: p}
		}
		return out
	}
	keys := c2BuiltInKeys(c2SharedKeyConfig())
	tests := []struct {
		name      string
		approvals []models.Approval
		keys      agentlaunch.ProviderKeys
		want      bool
	}{
		{"codex and codex-acp are one provider", approvals("codex", "codex-acp"), keys, false},
		{"custom tools sharing a key are one provider", approvals("zz-one", "zz-two"), keys, false},
		{"codex and claude are two", approvals("codex", "claude"), keys, true},
		{"a later different provider counts", approvals("codex", "codex-acp", "claude"), keys, true},
		{"one approval cannot be diverse", approvals("codex"), keys, false},
		{"no approvals", nil, keys, false},
		{"zero-value keys compare recorded names", approvals("codex", "codex-acp"), agentlaunch.ProviderKeys{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApprovalsHaveProviderDiversity(tt.approvals, tt.keys); got != tt.want {
				t.Fatalf("ApprovalsHaveProviderDiversity() = %v, want %v", got, tt.want)
			}
		})
	}
}

// P4: review-slot admission binds the recorded tool name, never the key.
func TestC2ReviewSlotStaysToolKeyed(t *testing.T) {
	t.Parallel()

	slots := rolemodels.Selection{List: true, Items: []rolemodels.Entry{{CLI: "codex-acp", Model: "m"}}}
	task := &models.Task{ID: "task-1"}

	if !ReviewerSlotAdmits(slots, task, models.Agent{Provider: "codex-acp", Model: "m"}) {
		t.Fatal("an agent running the slot's tool is admitted")
	}
	if ReviewerSlotAdmits(slots, task, models.Agent{Provider: "codex", Model: "m"}) {
		t.Fatal("codex shares codex-acp's provider key but is not the slot's tool: not admitted")
	}
}

// The end-to-end claim prefers the partially-approved task whose approval was
// made by a different provider key: an approval from an alias of the claimer's
// tool does not count as a different provider.
func TestC2ClaimPrefersCanonicallyDiverseSecondReview(t *testing.T) {
	t.Parallel()

	const attempts = 12
	for range attempts {
		projectRoot, statePath := c2ClaimFixture(t, models.Config{}, "codex", "codex")
		state, err := db.New(statePath).Read()
		if err != nil {
			t.Fatal(err)
		}
		delete(state.Agents, "code-planner-1")
		reviewCommit := "def456"
		partial := func(id, approverProvider string) models.Task {
			return models.Task{
				ID: id, Status: models.TaskStatus("CODING_PLAN_MAIN_PARTIALLY_APPROVED"), RolePair: "code-planning-main-pair",
				Priority: 1, ReviewCommit: &reviewCommit, History: []models.TaskHistoryEntry{}, Created: time.Now().UTC(),
				Approvals: []models.Approval{{Agent: "code-plan-reviewer-9", Provider: approverProvider, Timestamp: time.Now().UTC()}},
			}
		}
		state.Tasks = []models.Task{partial("alias-approved", "codex-acp"), partial("other-approved", "claude")}
		testhelpers.WriteInitialState(t, statePath, state)

		result, err := ClaimReviewerTask(ClaimReviewerTaskInput{
			ProjectRoot: projectRoot, AgentID: "code-plan-reviewer-1", Role: models.RoleCodePlanReviewer, LeaseDuration: 1800,
		})
		if err != nil {
			t.Fatalf("ClaimReviewerTask() = %v", err)
		}
		if result.TaskID != "other-approved" {
			t.Fatalf("claimed %q, want other-approved: codex-acp's approval shares the codex claimer's provider key", result.TaskID)
		}
	}
}
