package agent

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func newOrchestratorScipTestBlackboard(t *testing.T, projectRoot string, mutate func(*models.State)) *db.Blackboard {
	t.Helper()

	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{
		Role:       "orchestrator",
		Status:     models.AgentStatusIdle,
		Terminal:   "terminal-1",
		Heartbeat:  time.Now().UTC(),
		Generation: "test-generation",
	}
	if mutate != nil {
		mutate(state)
	}
	return testhelpers.WriteInitialState(t, statePath, state)
}

func orchestratorScipConfig(projectRoot string) SupervisorConfig {
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "test-generation"}
	return SupervisorConfig{AgentID: authority.ID, Authority: authority, ProjectRoot: projectRoot}
}

func mustReadState(t *testing.T, bb *db.Blackboard) *models.State {
	t.Helper()

	state, err := bb.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return state
}

// replaceOrchestratorPreExecutionHookForTest installs a hook that runs inside
// PreExecution, before the supervisor re-reads state for RevalidateWake. Use it
// to make a condition appear after wake selection; seeding it in the initial
// state instead is intercepted by the pre-loop quota/pause/checkpoint gates.
func replaceOrchestratorPreExecutionHookForTest(t *testing.T, hook func() error) func() {
	t.Helper()

	previous := orchestratorPreExecutionHook
	orchestratorPreExecutionHook = hook
	return func() {
		orchestratorPreExecutionHook = previous
	}
}
