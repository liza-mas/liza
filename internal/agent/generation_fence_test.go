package agent

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	generationA = "generation-a"
	generationB = "generation-b"
)

func TestSupervisorDispatchGenerationFence(t *testing.T) {
	t.Run("watchdog block", func(t *testing.T) {
		const agentID = "coder-1"
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		state := testhelpers.CreateValidState()
		state.Agents[agentID] = models.Agent{
			Role: models.RoleCoder, Status: models.AgentStatusWorking, Generation: generationB,
		}
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("task-watchdog-fence", models.TaskStatusImplementing, time.Now().UTC()),
		}
		bb := testhelpers.WriteInitialState(t, statePath, state)
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state before watchdog block: %v", err)
		}

		err = blockTaskFromSupervisor(bb, projectRoot, "task-watchdog-fence", models.AgentAuthority{
			ID: agentID, Generation: generationA,
		}, "watchdog stalled")
		assertSupervisorAuthorityError(t, err, agentID)
		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state after watchdog block: %v", err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("stale watchdog block changed generation-B state")
		}
	})

	t.Run("approved merge", func(t *testing.T) {
		projectRoot, statePath, _ := setupAgentMergeRepo(t)
		bb := db.New(statePath)
		if err := bb.Modify(func(state *models.State) error {
			agent := state.Agents["code-reviewer-2"]
			agent.Generation = generationB
			state.Agents["code-reviewer-2"] = agent
			return nil
		}); err != nil {
			t.Fatalf("install current reviewer generation: %v", err)
		}
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state before merge dispatch: %v", err)
		}
		pr, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			t.Fatalf("load resolver: %v", err)
		}

		err = handleApprovedMergesWithAuthority(projectRoot, models.AgentAuthority{
			ID: "code-reviewer-2", Generation: generationA,
		}, bb, pr)
		assertSupervisorAuthorityError(t, err, "code-reviewer-2")
		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state after merge dispatch: %v", err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("stale approved-merge dispatch changed generation-B state")
		}
	})

	t.Run("concurrent doer retries", func(t *testing.T) {
		const (
			taskID  = "task-doer-dispatch-fence"
			agentID = "coder-1"
		)
		projectRoot := t.TempDir()
		testhelpers.SetupTestGitRepo(t, projectRoot)
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		pr, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			t.Fatalf("load resolver: %v", err)
		}
		initial, err := pr.InitialStatus("coding-pair")
		if err != nil {
			t.Fatalf("resolve initial status: %v", err)
		}
		state := testhelpers.CreateValidState()
		agent := testhelpers.RegisteredTestAgent(models.RoleCoder)
		agent.Generation = generationB
		state.Agents[agentID] = agent
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, time.Now().UTC())
		task.Status = initial
		state.Tasks = []models.Task{task}
		bb := testhelpers.WriteInitialState(t, statePath, state)

		type outcome struct {
			generation string
			taskID     string
			err        error
		}
		start := make(chan struct{})
		outcomes := make(chan outcome, 2)
		for _, authority := range []models.AgentAuthority{
			{ID: agentID, Generation: generationA},
			{ID: agentID, Generation: generationB},
		} {
			go func(authority models.AgentAuthority) {
				<-start
				claimed, _, err := claimDoerTaskWithAuthority(projectRoot, authority, models.RoleCoder, bb)
				outcomes <- outcome{generation: authority.Generation, taskID: claimed, err: err}
			}(authority)
		}
		close(start)

		for range 2 {
			result := <-outcomes
			if result.generation == generationA {
				assertSupervisorAuthorityError(t, result.err, agentID)
				continue
			}
			if result.err != nil || result.taskID != taskID {
				t.Fatalf("generation-B doer result = (%q, %v), want task %q", result.taskID, result.err, taskID)
			}
		}
	})

	t.Run("concurrent reviewer retries", func(t *testing.T) {
		const (
			taskID  = "task-reviewer-dispatch-fence"
			agentID = "code-reviewer-1"
		)
		projectRoot := t.TempDir()
		testhelpers.SetupTestGitRepo(t, projectRoot)
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.SetupPipelineConfig(t, projectRoot)
		pr, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			t.Fatalf("load resolver: %v", err)
		}
		submitted, err := pr.SubmittedStatus("coding-pair")
		if err != nil {
			t.Fatalf("resolve submitted status: %v", err)
		}
		state := testhelpers.CreateValidState()
		agent := testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
		agent.Generation = generationB
		state.Agents[agentID] = agent
		reviewCommit := "abc123"
		task := models.Task{
			ID: taskID, Status: submitted, RolePair: "coding-pair", Priority: 1,
			Description: "reviewer dispatch generation fence", DoneWhen: "only B claims",
			Scope: "test", SpecRef: "README.md", ReviewCommit: &reviewCommit,
			Created: time.Now().UTC(),
		}
		state.Tasks = []models.Task{task}
		bb := testhelpers.WriteInitialState(t, statePath, state)

		type outcome struct {
			generation string
			taskID     string
			err        error
		}
		start := make(chan struct{})
		outcomes := make(chan outcome, 2)
		for _, authority := range []models.AgentAuthority{
			{ID: agentID, Generation: generationA},
			{ID: agentID, Generation: generationB},
		} {
			go func(authority models.AgentAuthority) {
				<-start
				claimed, _, _, err := claimReviewerTaskForRoleWithAuthority(
					projectRoot, authority, models.RoleCodeReviewer, taskID, 1800, bb,
				)
				outcomes <- outcome{generation: authority.Generation, taskID: claimed, err: err}
			}(authority)
		}
		close(start)

		for range 2 {
			result := <-outcomes
			if result.generation == generationA {
				assertSupervisorAuthorityError(t, result.err, agentID)
				continue
			}
			if result.err != nil || result.taskID != taskID {
				t.Fatalf("generation-B reviewer result = (%q, %v), want task %q", result.taskID, result.err, taskID)
			}
		}
	})
}

func assertSupervisorAuthorityError(t *testing.T, err error, agentID string, generations ...string) {
	t.Helper()
	var authorityErr *ops.AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("error = %T %v, want *ops.AgentAuthorityError", err, err)
	}
	for _, want := range []string{
		agentID,
		"stop",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want %q", err, want)
		}
	}
	if len(generations) == 0 {
		generations = []string{generationA, generationB}
	}
	for _, generation := range generations {
		if strings.Contains(err.Error(), generation) || strings.Contains(err.Error(), fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))) {
			t.Error("authority error exposes a registration generation or fingerprint")
		}
	}
	if authorityErr.SafeDetails()["safe_action"] != "stop" {
		t.Error("authority rejection must direct the caller to stop")
	}
}

func TestSupervisorOwnedMutationGenerationFence(t *testing.T) {
	const (
		agentID     = "orchestrator-1"
		generationA = "generation-a"
		generationB = "generation-b"
	)
	stale := models.AgentAuthority{ID: agentID, Generation: generationA}

	tests := []struct {
		name   string
		mutate func(*db.Blackboard, string, string) error
	}{
		{
			name: "heartbeat",
			mutate: func(_ *db.Blackboard, _ string, statePath string) error {
				return NewHeartbeat(HeartbeatConfig{Authority: stale, StatePath: statePath}).beat()
			},
		},
		{
			name: "unregister",
			mutate: func(bb *db.Blackboard, projectRoot, _ string) error {
				return unregisterAgent(bb, stale, projectRoot)
			},
		},
		{
			name: "post-exit reset",
			mutate: func(bb *db.Blackboard, projectRoot, _ string) error {
				return resetAgentAfterExit(bb, stale, projectRoot)
			},
		},
		{
			name: "orchestrator status",
			mutate: func(bb *db.Blackboard, _, _ string) error {
				return setAgentToOrchestratingStatus(bb, stale)
			},
		},
		{
			name: "watchdog blocking",
			mutate: func(bb *db.Blackboard, projectRoot, _ string) error {
				return blockTaskFromSupervisor(bb, projectRoot, "task-1", stale, "watchdog stalled")
			},
		},
		{
			name: "reviewer worktree blocking",
			mutate: func(bb *db.Blackboard, _, _ string) error {
				return blockReviewerTask(bb, "task-1", stale, "worktree missing")
			},
		},
		{
			name: "provider audit health",
			mutate: func(bb *db.Blackboard, projectRoot, _ string) error {
				_, err := handleProviderAuditDegraded(bb, SupervisorConfig{
					AgentID:     agentID,
					Authority:   stale,
					ProjectRoot: projectRoot,
					CLIName:     "codex",
				}, "task-1", "failed to record rollout items: thread stale not found")
				return err
			},
		},
		{
			name: "exit-42 restart state",
			mutate: func(bb *db.Blackboard, projectRoot, _ string) error {
				_, err := newExit42RestartTracker().Handle(bb, projectRoot, "orchestrator", "task-1", stale)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			testhelpers.SetupPipelineConfig(t, projectRoot)
			now := time.Now().UTC()
			lease := now.Add(time.Hour)
			state := testhelpers.CreateValidState()
			state.Agents[agentID] = models.Agent{
				Role:         "orchestrator",
				Status:       models.AgentStatusIdle,
				Heartbeat:    now,
				LeaseExpires: &lease,
				Generation:   generationB,
			}
			state.Tasks = []models.Task{{
				ID:          "task-1",
				Status:      models.TaskStatusReady,
				RolePair:    "coding-pair",
				Priority:    1,
				Created:     now,
				Description: "generation fence fixture",
				DoneWhen:    "stale supervisor cannot mutate",
				Scope:       "test",
				SpecRef:     "README.md",
			}}
			bb := testhelpers.WriteInitialState(t, statePath, state)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read state before mutation: %v", err)
			}

			err = tt.mutate(bb, projectRoot, statePath)
			assertSupervisorAuthorityError(t, err, agentID)
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatalf("read state after mutation: %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("stale %s mutation changed generation-B state", tt.name)
			}
		})
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	for _, name := range []string{
		"heartbeat.go",
		"supervisor.go",
		"claiming.go",
		"strategy_orchestrator.go",
		"provider_audit.go",
		"worktree_check.go",
	} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if bytes.Contains(data, []byte(".Modify(")) {
			t.Errorf("%s retains a direct blackboard mutation outside the shared authority fence", name)
		}
		if !bytes.Contains(data, []byte("ModifyWithAgentAuthority")) {
			t.Errorf("%s does not route its supervisor-owned mutation through the shared authority fence", name)
		}
	}
}
