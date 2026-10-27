package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestC6InitStateKeepsFormerValues(t *testing.T) {
	tools := map[string]models.AgentToolConfig{"codex": {ValidationExecution: "local"}}
	in := InitialStateInputs{
		Description:     "goal text",
		SpecRef:         "specs/vision.md",
		EntryPoint:      "entry",
		Timestamp:       time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		Branch:          "feature-branch",
		DefaultCLI:      "codex",
		AutoResume:      true,
		NoFollowUp:      true,
		PostWorktreeCmd: "npm ci",
		AgentEnvDeny:    []string{"SECRET_ONE"},
		AgentTools:      tools,
	}

	state := composeInitialState(in)

	if state.Version != 1 || state.PipelineVersion != 3 {
		t.Fatalf("state versions changed: %d, %d", state.Version, state.PipelineVersion)
	}
	if want := "goal-1893553445"; state.Goal.ID != want {
		t.Fatalf("goal id = %q, want %q", state.Goal.ID, want)
	}
	if state.Goal.Description != "goal text" || state.Goal.SpecRef != "specs/vision.md" || state.Goal.EntryPoint != "entry" {
		t.Fatalf("goal fields changed")
	}
	if len(state.Goal.AlignmentHistory) != 1 || state.Goal.AlignmentHistory[0].Summary != "Initial goal. No tasks defined yet." ||
		state.Goal.AlignmentHistory[0].Event != string(models.TaskEventInitialization) {
		t.Fatalf("alignment history changed")
	}
	if state.Sprint.ID != "sprint-1" || state.Sprint.Number != 1 || state.Sprint.GoalRef != state.Goal.ID ||
		!state.Sprint.Timeline.Started.Equal(in.Timestamp) || state.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("sprint changed")
	}
	if state.CircuitBreaker.Status != "OK" || state.CircuitBreaker.CurrentTrigger != nil || len(state.CircuitBreaker.History) != 0 {
		t.Fatalf("circuit breaker changed")
	}

	postWorktreeCmd := "npm ci"
	want := models.Config{
		TerminalTaskArchival:            true,
		MaxCoderIterations:              10,
		MaxReviewCycles:                 5,
		MaxGlobalIntegrationGenerations: models.NormalizeGlobalIntegrationGenerationLimit(0),
		MaxInstances:                    models.EffectiveMaxInstances(0, 0),
		HeartbeatInterval:               60,
		LeaseDuration:                   1800,
		CoderPollInterval:               30,
		DoerMaxWait:                     600,
		OrchestratorPollInterval:        60,
		OrchestratorMaxWait:             18000,
		ReviewerPollInterval:            30,
		ReviewerMaxWait:                 600,
		AgentProgressTimeout:            1800,
		DefaultCLI:                      "codex",
		IntegrationBranch:               "feature-branch",
		Mode:                            models.SystemModeRunning,
		AutoResume:                      true,
		NoFollowUp:                      true,
		PostWorktreeCmd:                 &postWorktreeCmd,
		AgentEnvDeny:                    []string{"SECRET_ONE"},
		AgentTools:                      tools,
	}
	if !reflect.DeepEqual(state.Config, want) {
		t.Fatalf("initial config changed:\n got %+v\nwant %+v", state.Config, want)
	}
}

func TestC6InitStateEmptyPostWorktreeCommandIsNil(t *testing.T) {
	state := composeInitialState(InitialStateInputs{Timestamp: time.Unix(1, 0).UTC()})
	if state.Config.PostWorktreeCmd != nil {
		t.Fatalf("empty post-worktree command persisted as %q", *state.Config.PostWorktreeCmd)
	}
}

func TestC6InitStatePublishesWholeStateWithWrite(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	in := InitialStateInputs{
		Description: "goal text",
		Timestamp:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		Branch:      "integration",
	}

	if err := PublishInitialState(statePath, in); err != nil {
		t.Fatalf("PublishInitialState: %v", err)
	}

	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		t.Fatalf("read published state: %v", err)
	}
	if state.Goal.ID != "goal-1893553445" || state.Goal.Description != "goal text" || state.Config.IntegrationBranch != "integration" {
		t.Fatalf("published state differs from inputs: goal %q %q branch %q", state.Goal.ID, state.Goal.Description, state.Config.IntegrationBranch)
	}
	if len(state.Tasks) != 0 || len(state.Anomalies) != 0 || len(state.Goal.AlignmentHistory) != 1 {
		t.Fatalf("publication added records beyond the initial goal")
	}
}

func TestC6InitStateReturnsRawWriteError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := PublishInitialState(filepath.Join(blocker, "state.yaml"), InitialStateInputs{Timestamp: time.Unix(1, 0).UTC()})
	if err == nil {
		t.Fatal("expected a write error")
	}
	if strings.Contains(err.Error(), "failed to write state file") {
		t.Fatalf("ops added the command-owned prefix: %v", err)
	}
}
