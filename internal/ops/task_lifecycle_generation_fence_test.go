package ops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	taskLifecycleGenerationA = "generation-a"
	taskLifecycleGenerationB = "generation-b"
)

type lifecycleGenerationFixture struct {
	projectRoot string
	statePath   string
	logPath     string
	authorityA  models.AgentAuthority
	authorityB  models.AgentAuthority
}

type lifecycleMutationInterleaving struct {
	fixture     lifecycleGenerationFixture
	writeNumber int
	writesSeen  int
	before      []byte
}

func TestTaskLifecycleMutationGenerationFence(t *testing.T) {
	t.Run("claim-task", func(t *testing.T) {
		fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", true, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
		})
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := ClaimTaskWithAuthority(fixture.projectRoot, "task-1", fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		_, err = ClaimTaskWithAuthority(fixture.projectRoot, "missing-task", fixture.authorityB)
		requireExistingValidation(t, err)
	})

	t.Run("claim-task-attempt-transition-writes", func(t *testing.T) {
		for _, writeNumber := range []int{1, 2} {
			t.Run(fmt.Sprintf("write-%d", writeNumber), func(t *testing.T) {
				fixture := newClaimAttemptTransitionFixture(t)
				interleaving := fixture.replaceAtLifecycleWrite(t, writeNumber)

				_, err := ClaimTaskWithAuthority(fixture.projectRoot, "task-1", fixture.authorityA)
				interleaving.requireStaleAndUnchanged(t, err)
			})
		}
	})

	t.Run("claim-task-degradation-follow-up", func(t *testing.T) {
		fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", true, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, time.Now().UTC()),
		})
		setPostWorktreeCommand(t, fixture, "exit 1")
		// The per-blackboard hook observes preparation and degradation.
		// Retirement uses modifyLifecycleState's separate hook between them.
		interleaving := fixture.replaceAtLifecycleWrite(t, 2)

		_, err := ClaimTaskWithAuthority(fixture.projectRoot, "task-1", fixture.authorityA)
		if !errors.Is(err, ErrAgentDegraded) {
			t.Fatalf("claim error = %v, want ErrAgentDegraded", err)
		}
		interleaving.requireReplacedAndUnchanged(t)
		task, readErr := db.For(fixture.statePath).GetTask("task-1")
		if readErr != nil {
			t.Fatal(readErr)
		}
		if task.Lifecycle == nil || task.Lifecycle.Preparation != nil || task.Lifecycle.Revision == 0 {
			t.Fatal("generation replacement did not reach degradation after setup-failure retirement")
		}
	})

	t.Run("claim-task-blocked-escalation-follow-up", func(t *testing.T) {
		fixture := newClaimBlockedEscalationFixture(t)
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := ClaimTaskWithAuthority(fixture.projectRoot, "task-1", fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)
	})

	t.Run("mark-blocked", func(t *testing.T) {
		fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
		})
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := MarkBlockedWithAuthority(fixture.projectRoot, "task-1", "blocked", []string{"what now?"}, fixture.authorityA, MarkBlockedOptions{})
		interleaving.requireStaleAndUnchanged(t, err)

		if _, err := MarkBlockedWithAuthority(fixture.projectRoot, "task-1", "blocked", []string{"what now?"}, fixture.authorityB, MarkBlockedOptions{}); err != nil {
			t.Fatalf("current generation mark-blocked: %v", err)
		}
	})

	t.Run("write-checkpoint", func(t *testing.T) {
		fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
		})
		input := &WriteCheckpointInput{
			TaskID:         "task-1",
			AgentID:        "coder-1",
			Intent:         "exercise the generation fence",
			ValidationPlan: "run focused tests",
		}
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		err := WriteCheckpointWithAuthority(fixture.projectRoot, input, fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		if err := WriteCheckpointWithAuthority(fixture.projectRoot, input, fixture.authorityB); err != nil {
			t.Fatalf("current generation write-checkpoint: %v", err)
		}

		mismatched := *input
		mismatched.AgentID = "coder-2"
		before := fixture.stateBytes(t)
		err = WriteCheckpointWithAuthority(fixture.projectRoot, &mismatched, fixture.authorityB)
		requireAuthorityActorMismatch(t, before, fixture, err)
	})

	t.Run("set-task-output", func(t *testing.T) {
		fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{
			testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC()),
		})
		input := &SetTaskOutputInput{
			TaskID:  "task-1",
			AgentID: "coder-1",
			Output: []models.OutputEntry{{
				Desc:     "follow-up",
				DoneWhen: "follow-up is complete",
				Scope:    "internal/ops",
			}},
		}
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		err := SetTaskOutputWithAuthority(fixture.projectRoot, input, fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		if err := SetTaskOutputWithAuthority(fixture.projectRoot, input, fixture.authorityB); err != nil {
			t.Fatalf("current generation set-task-output: %v", err)
		}

		mismatched := *input
		mismatched.AgentID = "coder-2"
		before := fixture.stateBytes(t)
		err = SetTaskOutputWithAuthority(fixture.projectRoot, &mismatched, fixture.authorityB)
		requireAuthorityActorMismatch(t, before, fixture, err)
	})
}

func TestOrchestratorLifecycleMutationGenerationFence(t *testing.T) {
	validAdd := func() AddTaskInput {
		return AddTaskInput{
			ID:          "task-1",
			RolePair:    "coding-pair",
			Description: "duplicate task",
			SpecRef:     "specs/vision.md",
			DoneWhen:    "done",
			Scope:       "internal/ops",
			Priority:    1,
		}
	}

	t.Run("add-task", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
		input := validAdd()
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := AddTaskWithAuthority(fixture.statePath, fixture.logPath, &input, fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		_, err = AddTaskWithAuthority(fixture.statePath, fixture.logPath, &input, fixture.authorityB)
		requireExistingValidation(t, err)
	})

	t.Run("add-tasks", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
		first := validAdd()
		first.ID = "task-2"
		second := validAdd()
		second.ID = "task-3"
		input := &AddTasksInput{Tasks: []AddTaskInput{first, second}, OrchestratorID: fixture.authorityA.ID}
		interleaving := fixture.replaceAtLifecycleWrite(t, 2)

		_, err := AddTasksWithAuthority(fixture.statePath, fixture.logPath, input, fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)
	})

	t.Run("supersede-task", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := SupersedeTaskWithAuthority(fixture.projectRoot, "task-1", []string{"task-1"}, "rescope", fixture.authorityA, SupersedeTaskOptions{})
		interleaving.requireStaleAndUnchanged(t, err)

		if _, err := SupersedeTaskWithAuthority(fixture.projectRoot, "task-1", []string{"task-1"}, "rescope", fixture.authorityB, SupersedeTaskOptions{}); err != nil {
			t.Fatalf("current generation supersede-task: %v", err)
		}
	})

	t.Run("retarget-dependency", func(t *testing.T) {
		testMissingTaskOrchestratorFence(t, func(f lifecycleGenerationFixture, authority models.AgentAuthority) error {
			_, err := RetargetDependencyWithAuthority(f.projectRoot, "missing-task", "old", []string{"new"}, "repair", authority)
			return err
		})
	})

	t.Run("apply-dependency-repair", func(t *testing.T) {
		testMissingTaskOrchestratorFence(t, func(f lifecycleGenerationFixture, authority models.AgentAuthority) error {
			_, err := ApplyDependencyRepairWithAuthority(f.projectRoot, "missing-task", "repair", authority)
			return err
		})
	})

	t.Run("repair-superseded-dependencies", func(t *testing.T) {
		testMissingTaskOrchestratorFence(t, func(f lifecycleGenerationFixture, authority models.AgentAuthority) error {
			_, err := RepairSupersededDependenciesWithAuthority(f.projectRoot, "missing-task", "repair", authority)
			return err
		})
	})

	t.Run("unblock-task", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusBlocked)
		if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
			// A task without preserved work can resume without a base commit.
			state.FindTask("task-1").Worktree = nil
			return nil
		}); err != nil {
			t.Fatalf("prepare valid unblock fixture: %v", err)
		}
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair", fixture.authorityA, UnblockTaskOptions{})
		interleaving.requireStaleAndUnchanged(t, err)

		result, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair", fixture.authorityB, UnblockTaskOptions{})
		if err != nil {
			t.Fatalf("current generation unblock-task: %v", err)
		}
		if result.Outcome != models.LifecycleCompleted || result.ToStatus == models.TaskStatusBlocked {
			t.Fatalf("current generation did not unblock task: %+v", result)
		}
	})

	t.Run("unblock-task-missing-task-stale-authority", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
		fixture.replaceGeneration(t)
		before := fixture.stateBytes(t)

		_, err := UnblockTaskWithAuthority(fixture.projectRoot, "missing-task", "repair", fixture.authorityA, UnblockTaskOptions{})
		var authorityErr *AgentAuthorityError
		if !errors.As(err, &authorityErr) {
			t.Fatalf("error = %T %v, want *AgentAuthorityError before missing-task validation", err, err)
		}
		if !bytes.Equal(before, fixture.stateBytes(t)) {
			t.Fatal("stale missing-task request changed state")
		}

		_, err = UnblockTaskWithAuthority(fixture.projectRoot, "missing-task", "repair", fixture.authorityB, UnblockTaskOptions{})
		requireValidationErrorContaining(t, err, "missing-task")
		if !bytes.Equal(before, fixture.stateBytes(t)) {
			t.Fatal("missing-task validation changed state")
		}
	})

	t.Run("assess-blocked", func(t *testing.T) {
		testMissingTaskOrchestratorFence(t, func(f lifecycleGenerationFixture, authority models.AgentAuthority) error {
			_, err := AssessBlockedWithAuthority(f.projectRoot, "missing-task", "assessed", authority, AssessBlockedOptions{})
			return err
		})
	})

	t.Run("assess-hypothesis-exhausted", func(t *testing.T) {
		testMissingTaskOrchestratorFence(t, func(f lifecycleGenerationFixture, authority models.AgentAuthority) error {
			_, err := AssessHypothesisExhaustedWithAuthority(f.projectRoot, "missing-task", "assessed", authority)
			return err
		})
	})

	t.Run("cancel-task", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusReady)
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := CancelTaskWithAuthority(fixture.projectRoot, "task-1", "cancel", fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		if _, err := CancelTaskWithAuthority(fixture.projectRoot, "task-1", "cancel", fixture.authorityB); err != nil {
			t.Fatalf("current generation cancel-task: %v", err)
		}
	})

	t.Run("reconcile-merged", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusIntegrationFailed)
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := ReconcileMergedWithAuthority(fixture.projectRoot, "task-1", "integration", "", "reconcile", fixture.authorityA)
		interleaving.requireStaleAndUnchanged(t, err)

		if _, err := ReconcileMergedWithAuthority(fixture.projectRoot, "task-1", "integration", "", "reconcile", fixture.authorityB); err != nil {
			t.Fatalf("current generation reconcile-merged: %v", err)
		}
	})

	t.Run("reconcile-merged-metrics-follow-up", func(t *testing.T) {
		fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusIntegrationFailed)
		interleaving := fixture.replaceAtLifecycleWrite(t, 2)

		result, err := ReconcileMergedWithAuthority(fixture.projectRoot, "task-1", "integration", "", "reconcile", fixture.authorityA)
		if err != nil {
			t.Fatalf("reconcile merged: %v", err)
		}
		interleaving.requireReplacedAndUnchanged(t)
		if len(result.Warnings) != 1 {
			t.Fatalf("warnings = %v, want one stale-generation metrics warning", result.Warnings)
		}
		for _, want := range []string{fixture.authorityA.ID, "stop"} {
			if !strings.Contains(result.Warnings[0], want) {
				t.Errorf("warning = %q, want %q", result.Warnings[0], want)
			}
		}
		for _, forbidden := range []string{taskLifecycleGenerationA, taskLifecycleGenerationB, generationFingerprint(taskLifecycleGenerationA), generationFingerprint(taskLifecycleGenerationB)} {
			if strings.Contains(result.Warnings[0], forbidden) {
				t.Error("metrics warning exposes a registration generation or fingerprint")
			}
		}
	})

	t.Run("unblock-task-conflict-follow-up", func(t *testing.T) {
		fixture := newUnblockConflictLifecycleFixture(t)
		interleaving := fixture.replaceAtLifecycleWrite(t, 1)

		_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair verified", fixture.authorityA, UnblockTaskOptions{RebaseOn: "integration"})
		interleaving.requireStaleAndUnchanged(t, err)
	})

	t.Run("current-generation-validation", func(t *testing.T) {
		t.Run("role", func(t *testing.T) {
			fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC()),
			})
			fixture.replaceGeneration(t)

			_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair", fixture.authorityB, UnblockTaskOptions{})
			requireValidationErrorContaining(t, err, "only orchestrator agents")
		})

		t.Run("status", func(t *testing.T) {
			fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
			fixture.replaceGeneration(t)

			_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair", fixture.authorityB, UnblockTaskOptions{})
			requireValidationErrorContaining(t, err, "must be BLOCKED")
		})

		t.Run("dependency", func(t *testing.T) {
			fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusBlocked)
			fixture.replaceGeneration(t)
			if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
				state.FindTask("task-1").DependsOn = []string{"missing-dependency"}
				return nil
			}); err != nil {
				t.Fatalf("install invalid dependency fixture: %v", err)
			}

			_, err := UnblockTaskWithAuthority(fixture.projectRoot, "task-1", "repair", fixture.authorityB, UnblockTaskOptions{})
			requireValidationErrorContaining(t, err, "invalid dependency")
		})
	})
}

func newOrchestratorLifecycleFixture(t *testing.T, withGit bool, status models.TaskStatus) lifecycleGenerationFixture {
	t.Helper()
	return newLifecycleGenerationFixture(t, "orchestrator-1", "orchestrator", withGit, []models.Task{
		testhelpers.BuildTaskByStatus("task-1", status, time.Now().UTC()),
	})
}

func newClaimAttemptTransitionFixture(t *testing.T) lifecycleGenerationFixture {
	t.Helper()
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3
	task.Attempt = 1
	fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{task})
	if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
		state.Config.MaxCoderIterations = 3
		taskID := "task-1"
		agent := state.Agents[fixture.authorityA.ID]
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		agent.Heartbeat = now
		state.Agents[fixture.authorityA.ID] = agent
		return nil
	}); err != nil {
		t.Fatalf("prepare attempt-transition fixture: %v", err)
	}
	return fixture
}

func newClaimBlockedEscalationFixture(t *testing.T) lifecycleGenerationFixture {
	t.Helper()
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3
	task.Attempt = 2
	fixture := newLifecycleGenerationFixture(t, "coder-1", "coder", false, []models.Task{task})
	if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
		state.Config.MaxCoderIterations = 3
		taskID := "task-1"
		agent := state.Agents[fixture.authorityA.ID]
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = &taskID
		agent.Heartbeat = now
		state.Agents[fixture.authorityA.ID] = agent
		return nil
	}); err != nil {
		t.Fatalf("prepare blocked-escalation fixture: %v", err)
	}
	return fixture
}

func newUnblockConflictLifecycleFixture(t *testing.T) lifecycleGenerationFixture {
	t.Helper()
	fixture := newOrchestratorLifecycleFixture(t, true, models.TaskStatusBlocked)
	writeAndCommit(t, fixture.projectRoot, "conflict.txt", "base\n", "Add base conflict file")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", "HEAD")
	testhelpers.CreateTestWorktree(t, fixture.projectRoot, "task-1")

	baseCommit := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "integration")
	worktreeDir := filepath.Join(fixture.projectRoot, ".worktrees", "task-1")
	writeAndCommit(t, worktreeDir, "conflict.txt", "task\n", "Task conflict edit")
	writeAndCommit(t, fixture.projectRoot, "conflict.txt", "integration\n", "Integration conflict edit")
	targetSHA := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "HEAD")
	testhelpers.MustGit(t, fixture.projectRoot, "branch", "-f", "integration", targetSHA)

	if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		task.RolePair = "code-planning-pair"
		task.BaseCommit = &baseCommit
		worktree := ".worktrees/task-1"
		task.Worktree = &worktree
		return nil
	}); err != nil {
		t.Fatalf("prepare unblock conflict fixture: %v", err)
	}
	return fixture
}

func newLifecycleGenerationFixture(t *testing.T, agentID, role string, withGit bool, tasks []models.Task) lifecycleGenerationFixture {
	t.Helper()
	projectRoot := t.TempDir()
	if withGit {
		testhelpers.SetupTestGitRepo(t, projectRoot)
	}
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	for i := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, tasks[i].ID)
	}
	agent := testhelpers.RegisteredTestAgent(role)
	agent.Generation = taskLifecycleGenerationA
	state.Agents[agentID] = agent
	testhelpers.WriteInitialState(t, statePath, state)

	return lifecycleGenerationFixture{
		projectRoot: projectRoot,
		statePath:   statePath,
		logPath:     filepath.Join(projectRoot, paths.ProjectDirName(), "log.jsonl"),
		authorityA:  models.AgentAuthority{ID: agentID, Generation: taskLifecycleGenerationA},
		authorityB:  models.AgentAuthority{ID: agentID, Generation: taskLifecycleGenerationB},
	}
}

func setPostWorktreeCommand(t *testing.T, fixture lifecycleGenerationFixture, command string) {
	t.Helper()
	if err := db.For(fixture.statePath).Modify(func(state *models.State) error {
		state.Config.PostWorktreeCmd = &command
		return nil
	}); err != nil {
		t.Fatalf("set post-worktree command: %v", err)
	}
}

func (f lifecycleGenerationFixture) replaceAtLifecycleWrite(t *testing.T, writeNumber int) *lifecycleMutationInterleaving {
	t.Helper()
	bb := db.For(f.statePath)
	state, err := bb.Read()
	if err != nil {
		t.Fatalf("read state for admission: %v", err)
	}
	if err := RequireAgentAuthority(state, f.authorityA); err != nil {
		t.Fatalf("admit generation A: %v", err)
	}
	interleaving := &lifecycleMutationInterleaving{fixture: f, writeNumber: writeNumber}
	cleanup := setLifecycleMutationTestHook(bb, func() {
		interleaving.writesSeen++
		if interleaving.writesSeen != writeNumber {
			return
		}
		f.replaceGeneration(t)
		interleaving.before = f.stateBytes(t)
	})
	t.Cleanup(cleanup)
	return interleaving
}

func (f lifecycleGenerationFixture) replaceGeneration(t *testing.T) {
	t.Helper()
	bb := db.For(f.statePath)
	if err := bb.Modify(func(current *models.State) error {
		agent := current.Agents[f.authorityA.ID]
		agent.Generation = taskLifecycleGenerationB
		current.Agents[f.authorityA.ID] = agent
		return nil
	}); err != nil {
		t.Fatalf("replace generation A with B: %v", err)
	}
}

func (f lifecycleGenerationFixture) stateBytes(t *testing.T) []byte {
	t.Helper()
	stateBytes, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatalf("read serialized state: %v", err)
	}
	return stateBytes
}

func (i *lifecycleMutationInterleaving) requireStaleAndUnchanged(t *testing.T, err error) {
	t.Helper()
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("error = %T %v, want *AgentAuthorityError", err, err)
	}
	for _, want := range []string{i.fixture.authorityA.ID, "stop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
	for _, forbidden := range []string{taskLifecycleGenerationA, taskLifecycleGenerationB, generationFingerprint(taskLifecycleGenerationA), generationFingerprint(taskLifecycleGenerationB)} {
		if strings.Contains(err.Error(), forbidden) {
			t.Error("authority error exposes a registration generation or fingerprint")
		}
	}
	if authorityErr.SafeDetails()["safe_action"] != "stop" {
		t.Error("authority rejection must direct the caller to stop")
	}
	i.requireReplacedAndUnchanged(t)
}

func (i *lifecycleMutationInterleaving) requireReplacedAndUnchanged(t *testing.T) {
	t.Helper()
	if i.writesSeen < i.writeNumber || i.before == nil {
		t.Fatalf("lifecycle write barrier saw %d writes, want at least %d", i.writesSeen, i.writeNumber)
	}
	after := i.fixture.stateBytes(t)
	if !bytes.Equal(after, i.before) {
		t.Fatalf("stale mutation changed replacement state\nbefore:\n%s\nafter:\n%s", i.before, after)
	}
}

func testMissingTaskOrchestratorFence(t *testing.T, mutate func(lifecycleGenerationFixture, models.AgentAuthority) error) {
	t.Helper()
	fixture := newOrchestratorLifecycleFixture(t, false, models.TaskStatusReady)
	interleaving := fixture.replaceAtLifecycleWrite(t, 1)

	err := mutate(fixture, fixture.authorityA)
	interleaving.requireStaleAndUnchanged(t, err)
	requireExistingValidation(t, mutate(fixture, fixture.authorityB))
}

func requireAuthorityActorMismatch(t *testing.T, before []byte, fixture lifecycleGenerationFixture, err error) {
	t.Helper()
	requireValidationErrorContaining(t, err, "does not match authority agent")
	after := fixture.stateBytes(t)
	if !bytes.Equal(after, before) {
		t.Fatalf("actor mismatch changed state\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func requireValidationErrorContaining(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want validation containing %q", want)
	}
	if IsAgentAuthorityError(err) {
		t.Fatalf("current generation rejected by authority fence: %v", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func requireExistingValidation(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("current generation unexpectedly bypassed existing validation")
	}
	if IsAgentAuthorityError(err) {
		t.Fatalf("current generation rejected by authority fence: %v", err)
	}
}
