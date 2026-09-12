package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/sessionvalidation"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSessionPreflightNewContractBeforeStartBlocksLegacyGate(t *testing.T) {
	backend := preflightBackendCase{name: "cli", tool: "gemini"}
	fixture, config, runtimeConfig, worktree := backendPreflightFixture(t, backend, false)
	marker, bin := filepath.Join(t.TempDir(), "children"), t.TempDir()
	writeBackendPreflightStubs(t, bin, marker, "selected")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BACKEND_PREFLIGHT_VALUE", "")
	// A legacy preparation produces nil. The gate must still bind the task ID.
	gate := newTaskProviderLaunchGate(config, "task-backend", nil)
	request := LLMAgentRunRequest{
		BackendName: backend.tool, AgentID: fixture.agentID, Generation: fixture.authorityA.Generation,
		TaskID: "task-backend", ProjectRoot: fixture.projectRoot, AdditionalDirs: []string{worktree},
		RuntimeConfig: runtimeConfig, Prompt: "test prompt", LaunchGate: gate,
	}
	adapter := backend.adapter()
	if result, err := adapter.Run(context.Background(), request); err != nil || result.ExitCode != 0 {
		t.Fatalf("legacy positive control failed: exit=%d error=%v", result.ExitCode, err)
	}
	before, err := os.ReadFile(marker)
	if err != nil || len(before) == 0 {
		t.Fatalf("positive control did not start provider: %v", err)
	}
	if err := fixture.bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-backend")
		task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "project-validation", Env: []string{"BACKEND_PREFLIGHT_VALUE"}}}
		state.Config.AgentTools = map[string]models.AgentToolConfig{backend.tool: {ValidationExecution: "local"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Run(context.Background(), request)
	if !errors.Is(err, sessionvalidation.ErrPreflight) {
		t.Fatalf("newly protected task bypassed preparation: %v", err)
	}
	after, readErr := os.ReadFile(marker)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("provider started after contract changed: before=%q after=%q error=%v", before, after, readErr)
	}
}

func TestSessionPreflightDoerCooldownDoesNotStarveLowerPriorityTask(t *testing.T) {
	backend := preflightBackendCase{name: "cli", tool: "gemini"}
	fixture, config, _, worktree := backendPreflightFixture(t, backend, true)
	t.Setenv("BACKEND_PREFLIGHT_VALUE", "")
	if err := fixture.bb.Modify(func(state *models.State) error {
		high := state.FindTask("task-backend")
		high.Status, high.Priority, high.AssignedTo, high.LeaseExpires = models.TaskStatusReady, 1, nil, nil
		low := testhelpers.BuildTaskByStatus("task-low", models.TaskStatusReady, time.Now().UTC())
		low.Priority = 2
		state.Tasks = append(state.Tasks, low)
		agent := state.Agents[fixture.agentID]
		agent.Status, agent.CurrentTask = models.AgentStatusIdle, nil
		state.Agents[fixture.agentID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	session, err := prepareClaimSession(config, fixture.bb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ops.PrepareValidationPreflight(fixture.projectRoot, "task-backend", fixture.agentID, worktree, session)
	var failure *sessionvalidation.Error
	if !errors.As(err, &failure) || failure.Code != "environment_missing" {
		t.Fatalf("could not establish real prerequisite failure: %v", err)
	}
	state, err := fixture.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !ops.ValidationRetryPending(fixture.projectRoot, state, state.FindTask("task-backend"), fixture.agentID, session) {
		t.Fatal("failed context did not enter process-local cooldown")
	}
	if !models.IsDoerClaimableByAgent(state, state.FindTask("task-backend"), models.RoleCoder, fixture.agentID, loadResolver(fixture.projectRoot), time.Now().UTC()) {
		t.Fatal("high-priority task is not otherwise claimable; fixture does not exercise candidate filtering")
	}
	taskID, _, err := claimDoerTaskWithAuthority(fixture.projectRoot, fixture.authorityA, models.RoleCoder, fixture.bb, session)
	if err != nil || taskID != "task-low" {
		t.Fatalf("cooldown starved eligible lower priority task: task=%q error=%v", taskID, err)
	}
	state, err = fixture.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	high, low := state.FindTask("task-backend"), state.FindTask("task-low")
	if high.AssignedTo != nil || high.Status != models.TaskStatusReady {
		t.Fatal("cooled-down task acquired executable ownership")
	}
	if low.AssignedTo == nil || *low.AssignedTo != fixture.agentID || low.Status != models.TaskStatusImplementing {
		t.Fatal("lower priority claim was not durably assigned")
	}
}
