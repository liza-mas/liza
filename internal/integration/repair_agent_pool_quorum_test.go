package integration

import (
	"io"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	quorumTaskID       = "review-deadlock"
	quorumRolePair     = "quorum-pair"
	quorumDoerRole     = "quorum-coder"
	quorumReviewerRole = "quorum-reviewer"
	providerA          = "provider-a"
	providerB          = "provider-b"
)

func TestRepairAgentPoolQuorumClaimEligibility(t *testing.T) {
	t.Run("reports missing reviewer capacity for the deadlocked roster", func(t *testing.T) {
		projectRoot := writeQuorumRepairProject(t, false)

		result, err := commands.RepairAgentPool(commands.RepairAgentPoolOptions{
			ProjectRoot: projectRoot,
			CLI:         "codex",
			DryRun:      true,
		})
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		if len(result.Missing) != 1 {
			t.Fatalf("missing = %+v, want one reviewer role", result.Missing)
		}
		missing := result.Missing[0]
		if missing.Role != quorumReviewerRole || !slices.Equal(missing.TaskIDs, []string{quorumTaskID}) || missing.TaskCount != 1 {
			t.Fatalf("missing = %+v, want %s for %s", missing, quorumReviewerRole, quorumTaskID)
		}
		// Reviewers -1 (the prior approver) and -2 hold leases, so the start
		// takes the first free ID the claim filters accept.
		wantCommand := brand.Command("agent", quorumReviewerRole) + " --cli codex --agent-id " + quorumReviewerRole + "-3"
		if !slices.Equal(result.Commands, []string{wantCommand}) {
			t.Fatalf("commands = %v, want [%s]", result.Commands, wantCommand)
		}
		if len(result.Spawned) != 0 {
			t.Fatalf("dry run spawned agents: %+v", result.Spawned)
		}
	})

	t.Run("claim-eligible provider B reviewer suppresses repair", func(t *testing.T) {
		projectRoot := writeQuorumRepairProject(t, true)

		result, err := commands.RepairAgentPool(commands.RepairAgentPoolOptions{
			ProjectRoot: projectRoot,
			CLI:         "codex",
			DryRun:      true,
		})
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		if len(result.Missing) != 0 {
			t.Fatalf("missing = %+v, want no missing reviewer capacity", result.Missing)
		}
		if len(result.Commands) != 0 {
			t.Fatalf("commands = %v, want none", result.Commands)
		}
	})
}

func writeQuorumRepairProject(t *testing.T, addEligibleReviewer bool) string {
	t.Helper()

	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfigBytes(t, projectRoot, []byte(quorumRepairPipeline))
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	doerID := quorumDoerRole + "-1"
	priorApproverID := quorumReviewerRole + "-1"
	task := testhelpers.BuildTaskByStatus(quorumTaskID, models.TaskStatusPartiallyApproved, now)
	task.RolePair = quorumRolePair
	task.AssignedTo = testhelpers.StringPtr(doerID)
	task.ReviewCommit = testhelpers.StringPtr("review123")
	task.SpecRef = "specs/vision.md"
	task.Approvals = []models.Approval{{
		Agent:     priorApproverID,
		Provider:  providerB,
		Timestamp: now,
	}}
	task.HandoffEvents[0].Agent = doerID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{task.ID}
	state.Agents = map[string]models.Agent{
		doerID:                    quorumRepairAgent(quorumDoerRole, providerA),
		priorApproverID:           quorumRepairAgent(quorumReviewerRole, providerB),
		quorumReviewerRole + "-2": quorumRepairAgent(quorumReviewerRole, providerA),
	}
	if addEligibleReviewer {
		state.Agents[quorumReviewerRole+"-3"] = quorumRepairAgent(quorumReviewerRole, providerB)
	}

	testhelpers.WriteInitialState(t, statePath, state)
	if err := statevalidate.ValidateStateFile(statePath, false, io.Discard); err != nil {
		t.Fatalf("fixture state validation failed: %v", err)
	}
	return projectRoot
}

func quorumRepairAgent(role, provider string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Provider = provider
	return agent
}

const quorumRepairPipeline = `pipeline:
  roles:
    quorum-coder:
      type: doer
      display-name: "Quorum Coder"
    quorum-reviewer:
      type: reviewer
      display-name: "Quorum Reviewer"
  role-pairs:
    quorum-pair:
      doer: quorum-coder
      reviewer: quorum-reviewer
      review-policy:
        quorum: 2
        provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    quorum-subpipeline:
      steps:
        - quorum-pair
  entry-points:
    default: quorum-subpipeline.quorum-pair
`

// TestRepairAgentPoolSpawnedReviewerMustBeClaimEligible checks each reviewer
// start against the claim filters a new reviewer on the spawn CLI would meet.
func TestRepairAgentPoolSpawnedReviewerMustBeClaimEligible(t *testing.T) {
	const doerProvider = "codex"
	priorApproverID := quorumReviewerRole + "-1"

	t.Run("busy different-provider reviewer makes a same-provider start unservable", func(t *testing.T) {
		// GIVEN the prior approver shares the doer's provider and a claude
		// reviewer is registered but busy
		busy := quorumRepairAgent(quorumReviewerRole, "claude")
		busy.Status = models.AgentStatusReviewing
		busy.CurrentTask = testhelpers.StringPtr("other-review")
		projectRoot := writeQuorumPoolProject(t, doerProvider, map[string]models.Agent{
			priorApproverID:           quorumRepairAgent(quorumReviewerRole, doerProvider),
			quorumReviewerRole + "-2": busy,
		})

		// WHEN repair would start a reviewer on the doer's provider
		result, err := commands.RepairAgentPool(commands.RepairAgentPoolOptions{ProjectRoot: projectRoot, CLI: doerProvider, DryRun: true})

		// THEN it reports the task instead of starting a useless reviewer
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		if len(result.Missing) != 0 || len(result.Commands) != 0 {
			t.Fatalf("missing = %+v commands = %v, want no start", result.Missing, result.Commands)
		}
		if len(result.Unservable) != 1 || result.Unservable[0].Role != quorumReviewerRole ||
			result.Unservable[0].CLI != doerProvider || !slices.Equal(result.Unservable[0].TaskIDs, []string{quorumTaskID}) {
			t.Fatalf("unservable = %+v, want %s on %s for %s", result.Unservable, quorumReviewerRole, doerProvider, quorumTaskID)
		}
	})

	t.Run("same-provider start is allowed when no other provider is registered", func(t *testing.T) {
		// GIVEN only the prior approver, which cannot give the second approval
		projectRoot := writeQuorumPoolProject(t, doerProvider, map[string]models.Agent{
			priorApproverID: quorumRepairAgent(quorumReviewerRole, doerProvider),
		})

		// WHEN
		result, err := commands.RepairAgentPool(commands.RepairAgentPoolOptions{ProjectRoot: projectRoot, CLI: doerProvider, DryRun: true})

		// THEN a fresh reviewer identity is started for the quorum
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		if len(result.Unservable) != 0 {
			t.Fatalf("unservable = %+v, want none", result.Unservable)
		}
		if len(result.Missing) != 1 || result.Missing[0].Role != quorumReviewerRole || result.Missing[0].SpawnCount != 1 {
			t.Fatalf("missing = %+v, want one %s start", result.Missing, quorumReviewerRole)
		}
	})
}

// TestRepairAgentPoolStartsReviewerUnderClaimableID covers identity reuse:
// an agent started without an ID takes the first free <role>-N, which is the
// prior approver's once it unregistered, and could never give the second
// approval.
func TestRepairAgentPoolStartsReviewerUnderClaimableID(t *testing.T) {
	approverID := quorumReviewerRole + "-1"

	t.Run("departed approver's ID is not reused", func(t *testing.T) {
		// GIVEN the only approver has exited, so its ID is the first free one
		projectRoot := writeQuorumPoolProject(t, "codex", nil)
		if got := identity.NextAvailableID(quorumReviewerRole, nil); got != approverID {
			t.Fatalf("auto-assignment would pick %s, want the approver's %s for this regression", got, approverID)
		}
		var started []string
		restore := commands.SetRepairAgentPoolSpawnForTest(func(_, role, _, agentID string) (int, error) {
			started = append(started, role+"/"+agentID)
			return 0, nil
		})
		t.Cleanup(restore)

		// WHEN repair starts the quorum reviewer
		result, err := commands.RepairAgentPool(commands.RepairAgentPoolOptions{ProjectRoot: projectRoot, CLI: "claude"})

		// THEN it is started under the next ID the claim filters accept
		if err != nil {
			t.Fatalf("RepairAgentPool() error = %v", err)
		}
		want := quorumReviewerRole + "/" + quorumReviewerRole + "-2"
		if !slices.Equal(started, []string{want}) {
			t.Fatalf("started = %v, want [%s]; missing = %+v", started, want, result.Missing)
		}
	})

	t.Run("IDs of pending starts are reserved", func(t *testing.T) {
		// GIVEN a started reviewer still registering under -2
		projectRoot := writeQuorumPoolProject(t, "codex", nil)
		state, err := db.For(paths.New(projectRoot).StatePath()).Read()
		if err != nil {
			t.Fatal(err)
		}
		pr, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			t.Fatal(err)
		}

		// WHEN the deficit is planned
		missing, _ := commands.FindRoleCapacityDeficits(state, pr, "claude", map[string]bool{quorumReviewerRole + "-2": true}, time.Now().UTC())

		// THEN the next start skips both the approver and the pending ID
		if len(missing) != 1 || !slices.Equal(missing[0].AgentIDs, []string{quorumReviewerRole + "-3"}) {
			t.Fatalf("missing = %+v, want one start under %s-3", missing, quorumReviewerRole)
		}
	})
}

func writeQuorumPoolProject(t *testing.T, doerProvider string, reviewers map[string]models.Agent) string {
	t.Helper()

	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfigBytes(t, projectRoot, []byte(quorumRepairPipeline))
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

	now := time.Now().UTC()
	doerID := quorumDoerRole + "-1"
	task := testhelpers.BuildTaskByStatus(quorumTaskID, models.TaskStatusPartiallyApproved, now)
	task.RolePair = quorumRolePair
	task.AssignedTo = testhelpers.StringPtr(doerID)
	task.ReviewCommit = testhelpers.StringPtr("review123")
	task.SpecRef = "specs/vision.md"
	task.Approvals = []models.Approval{{Agent: quorumReviewerRole + "-1", Provider: doerProvider, Timestamp: now}}
	task.HandoffEvents[0].Agent = doerID

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{task.ID}
	state.Agents = map[string]models.Agent{doerID: quorumRepairAgent(quorumDoerRole, doerProvider)}
	for id, agent := range reviewers {
		state.Agents[id] = agent
	}

	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot
}
