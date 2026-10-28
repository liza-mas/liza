package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The tests below pin the boundary between the claim's external worktree phase
// and its fenced assignment: the hook fires after the worktree exists and
// before the assignment transaction, so each refusal is proven against the
// physical artifacts, not only the persisted task.

type freshClaimPhaseFixture struct {
	projectRoot string
	stateFile   string
	bb          *db.Blackboard
	worktreeDir string
	taskID      string
	agentID     string
}

func newFreshClaimPhaseFixture(t *testing.T) *freshClaimPhaseFixture {
	t.Helper()
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	stateFile, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)

	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	agent := state.Agents["coder-1"]
	agent.Generation = lifecycleGenerationA
	state.Agents["coder-1"] = agent
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC())}
	bb := testhelpers.WriteInitialState(t, stateFile, state)
	return &freshClaimPhaseFixture{
		projectRoot: projectRoot, stateFile: stateFile, bb: bb, taskID: "task-1", agentID: "coder-1",
		worktreeDir: filepath.Join(projectRoot, paths.WorktreesDirName, "task-1"),
	}
}

func (f *freshClaimPhaseFixture) hookBeforeAssignment(t *testing.T, hook func()) {
	t.Helper()
	previous := testClaimTaskHooks
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: hook}
	t.Cleanup(func() { testClaimTaskHooks = previous })
}

func (f *freshClaimPhaseFixture) claim(t *testing.T) error {
	t.Helper()
	_, err := ClaimTaskWithAuthority(f.projectRoot, f.taskID, models.AgentAuthority{ID: f.agentID, Generation: lifecycleGenerationA})
	return err
}

func (f *freshClaimPhaseFixture) branchExists(t *testing.T) bool {
	t.Helper()
	return strings.TrimSpace(testhelpers.MustGit(t, f.projectRoot, "branch", "--list", paths.TaskBranchPrefix+f.taskID)) != ""
}

func (f *freshClaimPhaseFixture) worktreeExists() bool {
	_, err := os.Stat(f.worktreeDir)
	return err == nil
}

func TestClaimTask_GenerationReplacedBeforeAssignmentRemovesCreatedWorktree(t *testing.T) {
	f := newFreshClaimPhaseFixture(t)
	f.hookBeforeAssignment(t, func() {
		if !f.worktreeExists() {
			t.Error("hook fired before the claim created its worktree")
		}
		setLifecycleAgentGeneration(t, f.bb, f.agentID, lifecycleGenerationB)
	})

	assertLifecycleAuthorityError(t, f.claim(t), f.agentID)

	if f.worktreeExists() || f.branchExists(t) {
		t.Fatalf("losing claim kept resources it created: worktree=%v branch=%v", f.worktreeExists(), f.branchExists(t))
	}
	task := readClaimStateForTest(t, f.stateFile).FindTask(f.taskID)
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.Worktree != nil || task.BaseCommit != nil {
		t.Fatalf("stale claim published task state: %+v", task)
	}
}

func TestClaimTask_BoundaryChangedBeforeAssignmentRefusesAndRemovesCreatedWorktree(t *testing.T) {
	f := newFreshClaimPhaseFixture(t)
	f.hookBeforeAssignment(t, func() {
		if err := f.bb.Modify(func(state *models.State) error {
			models.AdvanceLifecycle(state.FindTask(f.taskID)) // retires the claim's preparation
			return nil
		}); err != nil {
			t.Fatalf("advance task boundary: %v", err)
		}
	})

	err := f.claim(t)
	if err == nil || !strings.Contains(err.Error(), "failed to commit claim") {
		t.Fatalf("claim error = %v, want failed to commit claim", err)
	}

	if f.worktreeExists() || f.branchExists(t) {
		t.Fatalf("refused claim kept resources it created: worktree=%v branch=%v", f.worktreeExists(), f.branchExists(t))
	}
	task := readClaimStateForTest(t, f.stateFile).FindTask(f.taskID)
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.Worktree != nil {
		t.Fatalf("claim assigned despite a changed boundary: %+v", task)
	}
}

func TestClaimTask_WinningClaimantKeepsWorktreeWhenLoserFails(t *testing.T) {
	f := newFreshClaimPhaseFixture(t)
	f.hookBeforeAssignment(t, func() {
		// A concurrent claimer won: it owns the task and the worktree this
		// invocation created for the same deterministic path.
		if err := f.bb.Modify(func(state *models.State) error {
			task := state.FindTask(f.taskID)
			winner, worktree := "coder-2", paths.WorktreesDirName+"/"+f.taskID
			lease := time.Now().UTC().Add(30 * time.Minute)
			task.Status, task.AssignedTo, task.LeaseExpires, task.Worktree = models.TaskStatusImplementing, &winner, &lease, &worktree
			agent := state.Agents[winner]
			agent.Status, agent.CurrentTask, agent.LeaseExpires = models.AgentStatusWorking, &f.taskID, &lease
			state.Agents[winner] = agent
			return nil
		}); err != nil {
			t.Fatalf("simulate winning claimer: %v", err)
		}
	})

	err := f.claim(t)
	if err == nil || !strings.Contains(err.Error(), "race condition") {
		t.Fatalf("claim error = %v, want race condition", err)
	}

	if !f.worktreeExists() || !f.branchExists(t) {
		t.Fatalf("loser removed the winner's resources: worktree=%v branch=%v", f.worktreeExists(), f.branchExists(t))
	}
	task := readClaimStateForTest(t, f.stateFile).FindTask(f.taskID)
	if task.AssignedTo == nil || *task.AssignedTo != "coder-2" || task.Status != models.TaskStatusImplementing {
		t.Fatalf("loser changed the winner's assignment: %+v", task)
	}
}

func TestClaimTask_PreservedClaimRefusedBeforeAssignmentKeepsPreexistingWork(t *testing.T) {
	fixture := newPreservedInitialClaimFixture(t)
	bb := db.New(fixture.stateFile)
	if err := bb.Modify(func(state *models.State) error {
		agent := state.Agents[fixture.agentID]
		agent.Generation = lifecycleGenerationA
		state.Agents[fixture.agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("set claimant generation: %v", err)
	}
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		setLifecycleAgentGeneration(t, bb, fixture.agentID, lifecycleGenerationB)
	}}
	t.Cleanup(func() { testClaimTaskHooks = nil })

	_, err := ClaimTaskWithAuthority(fixture.projectRoot, fixture.taskID, models.AgentAuthority{ID: fixture.agentID, Generation: lifecycleGenerationA})
	assertLifecycleAuthorityError(t, err, fixture.agentID)

	// The claim created nothing, so its failure path must not delete the
	// preserved worktree, branch or the task commit on it.
	if got, readErr := os.ReadFile(filepath.Join(fixture.worktreeDir, "task.txt")); readErr != nil || string(got) != fixture.taskContent {
		t.Fatalf("preserved work = %q, %v", got, readErr)
	}
	if head := testhelpers.MustGit(t, fixture.worktreeDir, "rev-parse", "HEAD"); head != fixture.preservedHead {
		t.Fatalf("preserved HEAD = %s, want %s", head, fixture.preservedHead)
	}
	task := readClaimStateForTest(t, fixture.stateFile).FindTask(fixture.taskID)
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.BaseCommit == nil || *task.BaseCommit != fixture.originalBase {
		t.Fatalf("stale preserved claim changed task ownership or base: %+v", task)
	}
}
