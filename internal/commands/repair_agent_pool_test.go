package commands

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

type spawnedAgentCall struct {
	projectRoot string
	role        string
	cli         string
}

func withFakeRepairSpawner(t *testing.T, calls *[]spawnedAgentCall, err error) {
	t.Helper()

	withFakeRepairSpawnerByRole(t, calls, func(role string) error {
		return err
	})
}

func withFakeRepairSpawnerByRole(t *testing.T, calls *[]spawnedAgentCall, errForRole func(role string) error) {
	t.Helper()

	original := repairAgentPoolSpawn
	repairAgentPoolSpawn = func(projectRoot, role, cli string) (int, error) {
		*calls = append(*calls, spawnedAgentCall{
			projectRoot: projectRoot,
			role:        role,
			cli:         cli,
		})
		err := errForRole(role)
		if err != nil {
			return 0, err
		}
		return 12345, nil
	}
	t.Cleanup(func() {
		repairAgentPoolSpawn = original
	})
}

// writeRepairAgentPoolState writes state with an orchestrator holding a fresh
// lease unless the fixture declares one. These fixtures model claimable-work
// repair in a running pool; a running goal without an orchestrator is its own
// repair demand, covered by the findMissingOrchestrator tests.
func writeRepairAgentPoolState(t *testing.T, state *models.State) string {
	t.Helper()

	tmpDir := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	written := *state
	written.Agents = withLiveOrchestrator(state.Agents, time.Now().UTC())
	testhelpers.WriteInitialState(t, statePath, &written)
	return tmpDir
}

func withLiveOrchestrator(agents map[string]models.Agent, now time.Time) map[string]models.Agent {
	for _, agent := range agents {
		if agent.Role == models.RoleOrchestrator {
			return agents
		}
	}
	withOrchestrator := make(map[string]models.Agent, len(agents)+1)
	maps.Copy(withOrchestrator, agents)
	withOrchestrator["orchestrator-1"] = occupiedAgent(models.RoleOrchestrator, now, 987654321)
	return withOrchestrator
}

type repairReviewerPolicyResolver struct {
	models.PipelineResolver
	diversity    string
	diversityErr error
	reviewerRole string
}

func (r repairReviewerPolicyResolver) ProviderDiversity(string, string) (string, error) {
	return r.diversity, r.diversityErr
}

func (r repairReviewerPolicyResolver) ReviewerRole(rolePair string) (string, error) {
	if r.reviewerRole != "" {
		return r.reviewerRole, nil
	}
	return r.PipelineResolver.ReviewerRole(rolePair)
}

func TestFindMissingRolesWithClaimableWork_ReviewerClaimEligibility(t *testing.T) {
	now := time.Now().UTC()
	baseState := func() *models.State {
		state := testhelpers.CreateValidState()
		doerID := "coder-1"
		task := testhelpers.BuildTaskByStatus("review-deadlock", models.TaskStatusPartiallyApproved, now)
		task.AssignedTo = &doerID
		task.ReviewCommit = testhelpers.StringPtr("review123")
		task.Approvals = []models.Approval{{Agent: "quorum-reviewer-2", Provider: "google", Timestamp: now}}
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			"coder-1":           {Role: "coder", Provider: "anthropic"},
			"quorum-reviewer-1": repairReviewerAgent("quorum-reviewer", "anthropic"),
			"quorum-reviewer-2": repairReviewerAgent("quorum-reviewer", "google"),
		}
		return state
	}

	state := baseState()
	projectRoot := writeRepairAgentPoolState(t, state)
	baseResolver, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}

	t.Run("reports reviewer role and task for deadlocked roster", func(t *testing.T) {
		resolver := repairReviewerPolicyResolver{
			PipelineResolver: baseResolver,
			diversity:        "preferred",
			reviewerRole:     "quorum-reviewer",
		}

		missing := FindMissingRolesWithClaimableWork(baseState(), resolver)
		if len(missing) != 1 {
			t.Fatalf("missing = %+v, want one reviewer role", missing)
		}
		if missing[0].Role != "quorum-reviewer" || !slices.Equal(missing[0].TaskIDs, []string{"review-deadlock"}) {
			t.Fatalf("missing = %+v, want resolver-provided quorum-reviewer for review-deadlock", missing)
		}
	})

	t.Run("live claim-eligible reviewer suppresses repair", func(t *testing.T) {
		state := baseState()
		state.Agents["quorum-reviewer-3"] = repairReviewerAgent("quorum-reviewer", "google")
		resolver := repairReviewerPolicyResolver{
			PipelineResolver: baseResolver,
			diversity:        "preferred",
			reviewerRole:     "quorum-reviewer",
		}

		if missing := FindMissingRolesWithClaimableWork(state, resolver); len(missing) != 0 {
			t.Fatalf("missing = %+v, want none with a claim-eligible reviewer", missing)
		}
	})

	t.Run("resolver error preserves fail-open capacity", func(t *testing.T) {
		resolver := repairReviewerPolicyResolver{
			PipelineResolver: baseResolver,
			diversityErr:     errors.New("resolver unavailable"),
			reviewerRole:     "quorum-reviewer",
		}

		if missing := FindMissingRolesWithClaimableWork(baseState(), resolver); len(missing) != 0 {
			t.Fatalf("missing = %+v, want none when resolver error leaves reviewer claim-eligible", missing)
		}
	})

	t.Run("doer capacity remains role-presence based", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("doer-work", models.TaskStatusReady, now)}
		leaseExpires := now.Add(30 * time.Minute)
		state.Agents["coder-1"] = models.Agent{
			Role:         "coder",
			Status:       models.AgentStatusIdle,
			Heartbeat:    now,
			LeaseExpires: &leaseExpires,
		}
		resolver := repairReviewerPolicyResolver{PipelineResolver: baseResolver, diversity: "preferred"}

		if missing := FindMissingRolesWithClaimableWork(state, resolver); len(missing) != 0 {
			t.Fatalf("missing = %+v, want doer capacity semantics unchanged", missing)
		}
	})
}

func repairReviewerAgent(role, provider string) models.Agent {
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Provider = provider
	return agent
}

func TestRepairAgentPoolStoppedReviewerValidationFailure(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Run and reap a real child so the dead registration cannot accidentally
	// depend on an arbitrary PID being unused on the host.
	child := exec.Command(binary, "-test.run=^$")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	deadPID := child.Process.Pid
	for _, tt := range []struct {
		name        string
		pid         int
		failed      bool
		wantMissing int
	}{
		{"stopped without validation failure", deadPID, false, 1},
		{"stopped with current validation failure", deadPID, true, 1},
		{"live with current validation failure", os.Getpid(), true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("review-work", models.TaskStatusReadyForReview, now)
			task.ReviewCommit = testhelpers.StringPtr("review-head")
			task.Validation = []string{"canonical check"}
			task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "canonical check", Env: []string{"REQUIRED"}}}
			state.Tasks = []models.Task{task}
			agentID := "code-reviewer-1"
			agent := repairReviewerAgent(models.RoleCodeReviewer, "anthropic")
			agent.PID = tt.pid
			agent.Status = models.AgentStatusIdle
			agent.Generation = "current-registration"
			agent.Heartbeat = now
			agent.LeaseExpires = testhelpers.TimePtr(now.Add(30 * time.Minute))
			state.Agents = map[string]models.Agent{agentID: agent}
			if tt.pid == deadPID {
				if status := ops.AgentProcessStatus(agentID, agent).DisplayStatus(); status != "stopped" {
					t.Fatalf("reaped child process status = %q, want stopped", status)
				}
			} else if !ops.IsProcessAlive(agent.PID) {
				t.Fatal("live control PID is not alive")
			}
			if tt.failed {
				state.ValidationReadiness = map[string]map[string]models.ValidationReadiness{agentID: {task.ID: {
					TaskID: task.ID, Generation: agent.Generation, ReviewCommit: *task.ReviewCommit,
					Digest:    models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites),
					CheckedAt: now, Result: "failed", Code: "missing_env",
				}}}
			}
			if got := models.ValidationTaskKnownFailed(state, &state.Tasks[0], agentID, now); got != tt.failed {
				t.Fatalf("validation failure fixture = %v, want %v", got, tt.failed)
			}
			root := writeRepairAgentPoolState(t, state)
			var calls []spawnedAgentCall
			withFakeRepairSpawner(t, &calls, nil)
			result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, CLI: "claude"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Missing) != tt.wantMissing || len(calls) != tt.wantMissing {
				t.Fatalf("missing = %+v, spawns = %+v, want %d reviewer replacement", result.Missing, calls, tt.wantMissing)
			}
			if tt.wantMissing != 0 && (result.Missing[0].Role != models.RoleCodeReviewer || !slices.Equal(result.Missing[0].TaskIDs, []string{task.ID}) || calls[0].role != models.RoleCodeReviewer) {
				t.Fatalf("replacement must target the stopped reviewer's work: missing=%+v spawns=%+v", result.Missing, calls)
			}
		})
	}
}

func TestParseAutoRepairAgentPoolEnv(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		ok          bool
		wantEnabled bool
		wantWarning bool
	}{
		{name: "unset defaults enabled", ok: false, wantEnabled: true},
		{name: "explicit empty defaults enabled", value: "", ok: true, wantEnabled: true},
		{name: "one enables", value: "1", ok: true, wantEnabled: true},
		{name: "true enables", value: "TRUE", ok: true, wantEnabled: true},
		{name: "zero disables", value: "0", ok: true, wantEnabled: false},
		{name: "false disables", value: "False", ok: true, wantEnabled: false},
		{name: "no disables", value: "no", ok: true, wantEnabled: false},
		{name: "invalid enables with warning", value: "maybe", ok: true, wantEnabled: true, wantWarning: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEnabled, warning := parseAutoRepairAgentPoolEnv(tt.value, tt.ok)
			if gotEnabled != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", gotEnabled, tt.wantEnabled)
			}
			if (warning != "") != tt.wantWarning {
				t.Fatalf("warning = %q, wantWarning %v", warning, tt.wantWarning)
			}
		})
	}
}

func TestRepairAgentPool_DryRunUsesConfiguredDefaultCLI(t *testing.T) {
	t.Setenv(brand.EnvName("DEFAULT_CLI"), "")
	t.Setenv(brand.EnvName("DEFAULT_DOER_CLI"), "")
	t.Setenv(brand.EnvName("DEFAULT_REVIEWER_CLI"), "")

	state := testhelpers.CreateValidState()
	state.Config.DefaultCLI = "gemini"
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusDraftCodingPlan, time.Now().UTC()),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("dry run spawned agents: %+v", calls)
	}
	if result.CLI != "gemini" {
		t.Errorf("CLI = %q, want gemini", result.CLI)
	}
	if len(result.Missing) != 1 {
		t.Fatalf("missing roles = %+v, want one role", result.Missing)
	}
	if result.Missing[0].Role != "code-planner" {
		t.Errorf("missing role = %q, want code-planner", result.Missing[0].Role)
	}
	if result.Commands[0] != brand.BinaryName+" agent code-planner --cli gemini" {
		t.Errorf("command = %q", result.Commands[0])
	}
}

func TestRepairAgentPool_DryRunUsesRoleSpecificDefaultCLIs(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Config.DefaultDoerCLI = "codex"
	state.Config.DefaultReviewerCLI = "gemini"
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("review-1", models.TaskStatusReadyForReview, now),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("dry run spawned agents: %+v", calls)
	}
	if result.CLI != "" {
		t.Errorf("CLI = %q, want empty for heterogeneous role-specific defaults", result.CLI)
	}
	if result.RoleCLIs["coder"] != "codex" {
		t.Errorf("RoleCLIs[coder] = %q, want codex", result.RoleCLIs["coder"])
	}
	if result.RoleCLIs["code-reviewer"] != "gemini" {
		t.Errorf("RoleCLIs[code-reviewer] = %q, want gemini", result.RoleCLIs["code-reviewer"])
	}
	if !slices.Contains(result.Commands, brand.BinaryName+" agent coder --cli codex") {
		t.Errorf("commands = %v, want coder codex command", result.Commands)
	}
	if !slices.Contains(result.Commands, brand.BinaryName+" agent code-reviewer --cli gemini") {
		t.Errorf("commands = %v, want code-reviewer gemini command", result.Commands)
	}
}

func TestRepairAgentPool_ExplicitCLISpawnsOneAgentPerMissingRole(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, time.Now().UTC()),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		CLI:         "codex",
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("spawn calls = %+v, want one", calls)
	}
	if calls[0].role != "coder" || calls[0].cli != "codex" {
		t.Errorf("spawn call = %+v, want coder/codex", calls[0])
	}
	if len(result.Spawned) != 1 {
		t.Fatalf("spawned = %+v, want one", result.Spawned)
	}
	if result.CLI != "codex" {
		t.Errorf("CLI = %q, want codex for explicit CLI", result.CLI)
	}
	if result.Missing[0].TaskCount != 2 {
		t.Errorf("TaskCount = %d, want 2", result.Missing[0].TaskCount)
	}
}

func TestRepairAgentPool_RolesFiltersMissingWork(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("review-1", models.TaskStatusReadyForReview, now),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		Roles:       []string{"code-reviewer"},
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("spawn calls = %+v, want one", calls)
	}
	if calls[0].role != "code-reviewer" {
		t.Fatalf("spawned role = %q, want code-reviewer", calls[0].role)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "code-reviewer" {
		t.Fatalf("missing = %+v, want only code-reviewer", result.Missing)
	}
}

func TestRepairAgentPool_RegisteredRoleIsNotMissing(t *testing.T) {
	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Heartbeat:    now,
		LeaseExpires: &leaseExpires,
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		CLI:         "claude",
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("missing roles = %+v, want none", result.Missing)
	}
	if len(calls) != 0 {
		t.Fatalf("spawn calls = %+v, want none", calls)
	}
}

func TestRepairAgentPool_DegradedRegisteredRoleIsMissing(t *testing.T) {
	now := time.Now().UTC()
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Provider:     "codex",
		PID:          1234,
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: &leaseExpires,
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {
			State:        models.AgentHealthDegraded,
			Role:         "coder",
			Provider:     "codex",
			PID:          1234,
			RegisteredAt: &now,
			DegradedAt:   now,
			Reason:       "claim_worktree_create_failed",
			LastError:    "cannot lock ref",
			RecoverHint:  "restart elsewhere",
		},
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "coder" {
		t.Fatalf("missing roles = %+v, want coder", result.Missing)
	}
	if len(result.Degraded) != 1 || result.Degraded[0].AgentID != "coder-1" {
		t.Fatalf("degraded = %+v, want coder-1", result.Degraded)
	}
}

func TestRepairAgentPool_OrphanedDegradedRoleRemainsVisible(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {
			State:       models.AgentHealthDegraded,
			Role:        "coder",
			Provider:    "codex",
			PID:         1234,
			DegradedAt:  now,
			Reason:      "claim_worktree_create_failed",
			LastError:   "cannot lock ref",
			RecoverHint: "restart outside sandbox",
		},
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "coder" {
		t.Fatalf("missing roles = %+v, want coder", result.Missing)
	}
	if len(result.Degraded) != 1 || result.Degraded[0].AgentID != "coder-1" {
		t.Fatalf("degraded = %+v, want orphaned coder-1", result.Degraded)
	}
}

func TestRepairAgentPool_StaleDegradedEpochDoesNotSuppressCapacity(t *testing.T) {
	now := time.Now().UTC()
	oldRegisteredAt := now.Add(-time.Hour)
	leaseExpires := now.Add(30 * time.Minute)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Provider:     "codex",
		PID:          5678,
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: &leaseExpires,
	}
	state.AgentHealth = map[string]models.AgentHealth{
		"coder-1": {
			State:        models.AgentHealthDegraded,
			Role:         "coder",
			Provider:     "codex",
			PID:          1234,
			RegisteredAt: &oldRegisteredAt,
			DegradedAt:   oldRegisteredAt,
			Reason:       "claim_worktree_create_failed",
		},
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("missing roles = %+v, want none", result.Missing)
	}
	if len(result.Degraded) != 0 {
		t.Fatalf("degraded = %+v, want none for stale epoch", result.Degraded)
	}
}

func TestRepairAgentPool_NilLeaseWithRecentHeartbeatIsNotMissing(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:      "coder",
		Status:    models.AgentStatusIdle,
		Heartbeat: now.Add(-time.Minute),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("missing roles = %+v, want none", result.Missing)
	}
	if len(calls) != 0 {
		t.Fatalf("spawn calls = %+v, want none", calls)
	}
}

func TestRepairAgentPool_DefaultsToMissingRepair(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "coder" {
		t.Fatalf("missing roles = %+v, want coder", result.Missing)
	}
}

func TestRepairAgentPool_NilLeaseWithStaleHeartbeatIsMissing(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:      "coder",
		Status:    models.AgentStatusIdle,
		Heartbeat: now.Add(-(models.DefaultHeartbeatIntervalSec*time.Second + models.LeaseExpiryGracePeriod + time.Minute)),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "coder" {
		t.Fatalf("missing roles = %+v, want coder", result.Missing)
	}
}

func TestRepairAgentPool_ExpiredAgentLeaseIsMissing(t *testing.T) {
	now := time.Now().UTC()
	leaseExpires := now.Add(-time.Minute)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	state.Agents["coder-1"] = models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Heartbeat:    now.Add(-2 * time.Minute),
		LeaseExpires: &leaseExpires,
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		CLI:         "claude",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0].Role != "coder" {
		t.Fatalf("missing roles = %+v, want coder", result.Missing)
	}
}

func TestRepairAgentPoolRejectsInvalidCLI(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	_, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		CLI:         "unknown",
	})
	if err == nil {
		t.Fatal("RepairAgentPool() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid CLI") {
		t.Fatalf("error = %q, want invalid CLI", err)
	}
}

func TestRepairAgentPoolAttemptsAllMissingRolesOnSpawnFailure(t *testing.T) {
	state := testhelpers.CreateValidState()
	now := time.Now().UTC()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
		testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusDraftCodingPlan, now),
		testhelpers.BuildTaskByStatus("review-1", models.TaskStatusReadyForReview, now),
	}
	projectRoot := writeRepairAgentPoolState(t, state)

	var calls []spawnedAgentCall
	withFakeRepairSpawnerByRole(t, &calls, func(role string) error {
		if role == "code-reviewer" {
			return errors.New("boom")
		}
		return nil
	})

	result, err := RepairAgentPool(RepairAgentPoolOptions{
		ProjectRoot: projectRoot,
		Missing:     true,
		CLI:         "claude",
	})
	if err == nil {
		t.Fatal("RepairAgentPool() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to start 1 of 3 missing role agent") {
		t.Fatalf("error = %q, want summarized spawn failure", err)
	}
	if result == nil {
		t.Fatal("result = nil, want partial result")
	}
	if len(calls) != 3 {
		t.Fatalf("spawn calls = %+v, want all 3 missing roles attempted", calls)
	}
	if len(result.Commands) != 3 {
		t.Fatalf("commands = %+v, want command for each missing role", result.Commands)
	}
	if len(result.Spawned) != 2 {
		t.Fatalf("spawned = %+v, want 2 successful starts", result.Spawned)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("failed = %+v, want 1 failed start", result.Failed)
	}
	if result.Failed[0].Role != "code-reviewer" || result.Failed[0].Error != "boom" {
		t.Fatalf("failed = %+v, want code-reviewer boom", result.Failed)
	}
}
