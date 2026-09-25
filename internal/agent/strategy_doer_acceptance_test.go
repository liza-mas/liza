package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const acceptanceRefusalPlanRef = "specs/acceptance-plan.md#Task 1"

// acceptanceRefusalProject is a coding task whose plan_ref names a committed
// acceptance carrier on integration. The reviewed contract declares
// "sh boundary_test.sh"; mutate adjusts the task so its claim is refused.
type acceptanceRefusalProject struct {
	root     string
	bb       *db.Blackboard
	strategy RoleStrategy
	config   SupervisorConfig
	taskID   string
}

func setupAcceptanceRefusalProject(t *testing.T, mutate func(task *models.Task)) acceptanceRefusalProject {
	t.Helper()
	const (
		taskID  = "task-acceptance-refused"
		agentID = "coder-1"
	)
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)

	write := func(name, contents string) {
		t.Helper()
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("specs/acceptance-goal.md", "# Boundary\n\n## Identity\nReject malformed identity.\n\n## Replay\nExercise independent concurrent replay.\n")
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record boundary requirements")
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	write("specs/acceptance-plan.md", fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n- \"replay\": \"specs/acceptance-goal.md#Replay\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n- \"AC-replay\" -> \"replay\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\",\"AC-replay\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":10,\"approved_proofs\":[]}\n```\n", sourceCommit))
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: acceptance allocation carrier")
	testhelpers.MustGit(t, root, "branch", "-f", "integration", "HEAD")

	pr, err := ops.LoadResolverForModels(root)
	if err != nil {
		t.Fatalf("load resolver: %v", err)
	}
	initial, err := pr.InitialStatus("coding-pair")
	if err != nil {
		t.Fatalf("resolve initial status: %v", err)
	}
	state := testhelpers.CreateValidState()
	state.Agents[agentID] = testhelpers.RegisteredTestAgent(models.RoleCoder)
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC())
	task.Status = initial
	task.PlanRef = acceptanceRefusalPlanRef
	task.SpecRef = "specs/acceptance-goal.md"
	task.Validation = []string{"sh boundary_test.sh"}
	mutate(&task)
	state.Tasks = []models.Task{task}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	strategy, err := NewRoleStrategy(models.RoleCoder, testResolver(t))
	if err != nil {
		t.Fatalf("NewRoleStrategy() error = %v", err)
	}
	return acceptanceRefusalProject{
		root:     root,
		bb:       bb,
		strategy: strategy,
		taskID:   taskID,
		config: SupervisorConfig{
			AgentID:     agentID,
			Role:        models.RoleCoder,
			ProjectRoot: root,
			StatePath:   statePath,
			Authority:   testSupervisorAuthority(t, bb, agentID),
		},
	}
}

func (p acceptanceRefusalProject) claim(t *testing.T) error {
	t.Helper()
	taskID, _, err := p.strategy.ClaimTask(p.config, p.bb)
	if err == nil {
		t.Fatalf("ClaimTask() claimed %q, want the acceptance refusal", taskID)
	}
	return err
}

func (p acceptanceRefusalProject) task(t *testing.T) (*models.State, *models.Task) {
	t.Helper()
	state, err := p.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask(p.taskID)
	if task == nil {
		t.Fatalf("task %s missing", p.taskID)
	}
	return state, task
}

// requireEscalatedToOrchestrator asserts the refused task left the claimable
// pool through BLOCKED, with a reason naming the allocation, and now counts as
// orchestrator work.
func (p acceptanceRefusalProject) requireEscalatedToOrchestrator(t *testing.T, planRef, reasonPrefix, faultClass string) {
	t.Helper()
	state, task := p.task(t)
	if task.Status != models.TaskStatusBlocked {
		t.Fatalf("status = %s, want BLOCKED: a refused claim left the task claimable for the next retry", task.Status)
	}
	if task.BlockedReason == nil || !strings.HasPrefix(*task.BlockedReason, reasonPrefix) || !strings.Contains(*task.BlockedReason, planRef) {
		t.Fatalf("blocked_reason = %v, want prefix %q naming %q", task.BlockedReason, reasonPrefix, planRef)
	}
	if len(task.BlockedQuestions) == 0 || !strings.Contains(strings.Join(task.BlockedQuestions, " "), "unblock-task") {
		t.Fatalf("blocked_questions = %v, want the orchestrator repair ending in unblock-task", task.BlockedQuestions)
	}
	if task.AssignedTo != nil || task.AcceptanceSource != nil {
		t.Fatalf("blocked task assigned_to=%v acceptance_source=%v, want both unset", task.AssignedTo, task.AcceptanceSource)
	}
	last := task.History[len(task.History)-1]
	if last.Event != models.TaskEventBlocked || last.Extra["claim_refusal"] != "acceptance_evidence" || last.Extra["fault_class"] != faultClass {
		t.Fatalf("last history = %+v, want a blocked entry with claim_refusal=acceptance_evidence fault_class=%s", last, faultClass)
	}
	if err := statevalidate.ValidateState(state, p.root, true, io.Discard); err != nil {
		t.Fatalf("blocked state is invalid: %v", err)
	}
	if got := ops.CountActionableBlockedTasks(state); got != 1 {
		t.Fatalf("CountActionableBlockedTasks = %d, want 1 so the orchestrator wakes", got)
	}
	if models.IsDoerClaimableByAgent(state, task, models.RoleCoder, p.config.AgentID, loadResolver(p.root), time.Now().UTC()) {
		t.Fatal("blocked task is still doer-claimable")
	}
}

// D63: a content fault in the allocation is deterministic, so the first
// refused claim escalates instead of being retried every few seconds.
func TestDoerClaim_ContentAcceptanceFaultBlocksOnFirstRefusal(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(task *models.Task)
	}{
		{"validation differs from the reviewed commands", func(task *models.Task) {
			task.Validation = []string{"sh boundary_test.sh", "ruff check ."}
		}},
		{"allocation heading is missing", func(task *models.Task) {
			task.PlanRef = "specs/acceptance-plan.md#task-1-slug"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a claimable coding task whose acceptance allocation cannot
			// match the reviewed carrier
			project := setupAcceptanceRefusalProject(t, tc.mutate)
			_, before := project.task(t)
			planRef := before.PlanRef

			// WHEN a doer supervisor attempts it once
			err := project.claim(t)

			// THEN the refusal names the allocation and the task is handed to
			// the orchestrator rather than left for the next retry
			if !strings.Contains(err.Error(), planRef) {
				t.Errorf("claim refusal %q does not name the allocation %q", err, planRef)
			}
			project.requireEscalatedToOrchestrator(t, planRef, "acceptance_evidence_invalid:", "content")

			// AND a further attempt finds nothing to claim instead of re-refusing
			if _, _, err := project.strategy.ClaimTask(project.config, project.bb); err == nil || !strings.Contains(err.Error(), "no claimable tasks") {
				t.Fatalf("second ClaimTask() error = %v, want no claimable tasks", err)
			}
		})
	}
}

// D63: an allocation refusal cannot prove determinism at its origin, so it is
// escalated only after three identical refusals, with a reason that does not
// assert the content is invalid.
func TestDoerClaim_RepeatedAllocationRefusalBlocksOnThirdObservation(t *testing.T) {
	// GIVEN a task whose carrier and validation match but no reviewed merged
	// planning parent allocates it
	project := setupAcceptanceRefusalProject(t, func(*models.Task) {})

	// WHEN the same refusal is observed twice
	for attempt := 1; attempt <= 2; attempt++ {
		err := project.claim(t)
		if !strings.Contains(err.Error(), "requires allocation") {
			t.Fatalf("attempt %d refusal = %v, want the allocation refusal", attempt, err)
		}
		// THEN the task stays claimable: repetition has not yet been established
		if _, task := project.task(t); task.Status == models.TaskStatusBlocked {
			t.Fatalf("attempt %d blocked the task before the retry-exhaustion threshold", attempt)
		}
	}

	// WHEN it is observed a third time
	project.claim(t)

	// THEN it escalates as retry exhaustion
	project.requireEscalatedToOrchestrator(t, acceptanceRefusalPlanRef, "acceptance_claim_refused_repeatedly:", "allocation")
	_, task := project.task(t)
	if !strings.Contains(*task.BlockedReason, "persistent repository read failure") {
		t.Fatalf("blocked_reason %q asserts invalid content instead of stating the uncertainty", *task.BlockedReason)
	}
}
