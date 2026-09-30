package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func slotTestAgent(provider, model string) models.Agent {
	agent := reviewerCapacityTestAgent("code-reviewer", provider)
	agent.Model = model
	return agent
}

func writeSlotModelsFile(t *testing.T, projectRoot, content string) {
	t.Helper()
	path := paths.New(projectRoot).ModelsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A reviewer list binds review slot len(approvals) to its item, the last item
// serving every slot past the end; a single mapping binds nothing.
func TestReviewerClaimEligibilityHonorsReviewSlots(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := &models.State{Agents: map[string]models.Agent{
		"code-reviewer-1": slotTestAgent("claude", "m1"),
		"code-reviewer-2": slotTestAgent("codex", "m2"),
		"code-reviewer-3": slotTestAgent("codex", ""),
	}}
	resolver := &doerDiversityResolver{diversity: "", reviewerRole: "code-reviewer"}
	two := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude", Model: "m1"}, {CLI: "codex", Model: "m2"}}, List: true}
	one := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude", Model: "m1"}}, List: true}
	single := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude", Model: "m1"}}}
	fresh := &models.Task{ID: "fresh", RolePair: "coding-pair"}
	halfApproved := &models.Task{ID: "half", RolePair: "coding-pair", Approvals: []models.Approval{{Agent: "code-reviewer-9", Provider: "claude"}}}

	tests := []struct {
		name  string
		slots rolemodels.Selection
		task  *models.Task
		agent string
		want  bool
	}{
		{"slot 1 entry takes a fresh task", two, fresh, "code-reviewer-1", true},
		{"slot 2 entry refused on a fresh task", two, fresh, "code-reviewer-2", false},
		{"slot 2 entry takes the second review", two, halfApproved, "code-reviewer-2", true},
		{"slot 1 entry refused on the second review", two, halfApproved, "code-reviewer-1", false},
		{"model must match exactly", two, halfApproved, "code-reviewer-3", false},
		{"one-item list serves every slot", one, halfApproved, "code-reviewer-1", true},
		{"single mapping binds nothing", single, fresh, "code-reviewer-2", true},
		{"no selection binds nothing", rolemodels.Selection{}, fresh, "code-reviewer-2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReviewerClaimEligible(ReviewerClaimEligibilityInput{
				State: state, Task: tt.task, AgentID: tt.agent,
				ReviewerRole: "code-reviewer", Now: now, Resolver: resolver, Slots: tt.slots,
			})
			if got != tt.want {
				t.Fatalf("ReviewerClaimEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Under provider-diversity: preferred, a different-provider reviewer that the
// slot refuses is no alternative, so the bound same-provider reviewer is not
// blocked waiting for it.
func TestDoerDiversityYieldsToBoundSlot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	doerID := "coder-1"
	task := &models.Task{ID: "task-1", RolePair: "coding-pair", AssignedTo: &doerID}
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1":         {Role: "coder", Provider: "claude"},
		"code-reviewer-1": slotTestAgent("claude", ""),
		"code-reviewer-2": slotTestAgent("codex", ""),
	}}
	resolver := &doerDiversityResolver{diversity: "preferred", reviewerRole: "code-reviewer"}
	bound := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude"}}, List: true}

	if !isBlockedByDoerDiversityAt(task, "claude", "code-reviewer-1", state, resolver, rolemodels.Selection{}, now) {
		t.Fatal("unbound: same-provider reviewer must yield to the registered codex reviewer")
	}
	if isBlockedByDoerDiversityAt(task, "claude", "code-reviewer-1", state, resolver, bound, now) {
		t.Fatal("bound to claude: the codex reviewer cannot take the slot, so claude must not be blocked")
	}
}

func claimSlotFixture(t *testing.T, modelsYAML string) string {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()
	reviewCommit := "abc123"
	state := testhelpers.CreateValidState()
	state.Agents["code-reviewer-1"] = slotTestAgent("claude", "m1")
	state.Agents["code-reviewer-2"] = slotTestAgent("codex", "m2")
	state.Tasks = []models.Task{{
		ID:           "task-pa",
		Status:       models.TaskStatusPartiallyApproved,
		RolePair:     "coding-pair",
		Priority:     1,
		ReviewCommit: &reviewCommit,
		History:      []models.TaskHistoryEntry{},
		Created:      now,
		Approvals:    []models.Approval{{Agent: "code-reviewer-9", Provider: "claude", Model: "m1", Timestamp: now}},
	}}
	testhelpers.WriteInitialState(t, stateFile, state)
	if modelsYAML != "" {
		writeSlotModelsFile(t, tmpDir, modelsYAML)
	}
	return tmpDir
}

const slotModelsList = "roles:\n  code-reviewer:\n    - {cli: claude, model: m1}\n    - {cli: codex, model: m2}\n"

func TestClaimReviewerTask_ReviewSlotRefusesOtherEntry(t *testing.T) {
	t.Parallel()

	root := claimSlotFixture(t, slotModelsList)
	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", LeaseDuration: 1800})
	if err == nil || !strings.Contains(err.Error(), "no reviewable tasks match this reviewer's models.yaml slot") {
		t.Fatalf("slot-1 reviewer on the second review: error = %v, want slot refusal", err)
	}
	result, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-2", LeaseDuration: 1800})
	if err != nil || result.TaskID != "task-pa" {
		t.Fatalf("slot-2 reviewer: result = %+v, error = %v; want task-pa claimed", result, err)
	}
}

func TestClaimReviewerTask_SingleEntryDoesNotBind(t *testing.T) {
	t.Parallel()

	root := claimSlotFixture(t, "roles:\n  code-reviewer: {cli: codex, model: m2}\n")
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", LeaseDuration: 1800}); err != nil {
		t.Fatalf("single mapping must not bind claims: error = %v", err)
	}
}

func TestClaimReviewerTask_UnreadableModelsFileRefusesClaim(t *testing.T) {
	t.Parallel()

	root := claimSlotFixture(t, "roles:\n  code-reviewer: []\n")
	_, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: root, AgentID: "code-reviewer-1", LeaseDuration: 1800})
	if err == nil || !strings.Contains(err.Error(), "cannot read review slots") || !strings.Contains(err.Error(), "models.yaml") {
		t.Fatalf("error = %v, want the claim refused with the file error", err)
	}
}

// A rejection clears the task's approvals, so its re-review starts again at
// slot 1.
func TestReviewerClaimEligibilityRejectionRestartsAtSlotOne(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	state := &models.State{Agents: map[string]models.Agent{
		"code-reviewer-1": slotTestAgent("claude", "m1"),
		"code-reviewer-2": slotTestAgent("codex", "m2"),
	}}
	resolver := &doerDiversityResolver{reviewerRole: "code-reviewer"}
	slots := rolemodels.Selection{Items: []rolemodels.Entry{{CLI: "claude", Model: "m1"}, {CLI: "codex", Model: "m2"}}, List: true}
	task := &models.Task{ID: "task-1", RolePair: "coding-pair", Approvals: []models.Approval{{Agent: "code-reviewer-9", Provider: "claude"}}}
	eligible := func(agentID string) bool {
		return ReviewerClaimEligible(ReviewerClaimEligibilityInput{
			State: state, Task: task, AgentID: agentID,
			ReviewerRole: "code-reviewer", Now: now, Resolver: resolver, Slots: slots,
		})
	}
	if eligible("code-reviewer-1") || !eligible("code-reviewer-2") {
		t.Fatal("fixture: the half-approved task must bind slot 2")
	}

	clearAttemptState(task, attemptStateReviewRejection)

	if !eligible("code-reviewer-1") || eligible("code-reviewer-2") {
		t.Fatal("after rejection the re-review must bind slot 1 again")
	}
}
