package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

// Values of the initial state MAS init writes.
const (
	initStateVersion     = 1
	initPipelineVersion  = 3 // always v3: the pipeline is mandatory
	initSprintID         = "sprint-1"
	initSprintNumber     = 1
	initAlignmentSummary = "Initial goal. No tasks defined yet."
	initGoalIDFormat     = "goal-%d"
)

// InitialStateInputs are the resolved initialization inputs of a new
// Blackboard. The caller resolves flags, prompts and environment; this
// operation only constructs and publishes the state.
type InitialStateInputs struct {
	Description string
	SpecRef     string
	EntryPoint  string
	Timestamp   time.Time

	Branch                          string
	DefaultCLI                      string
	AutoResume                      bool
	NoFollowUp                      bool
	CopyWorktreeEnvFiles            bool
	MaxGlobalIntegrationGenerations int
	MaxInstances                    int
	ScipSearch                      []string
	PostWorktreeCmd                 string
	AgentEnvDeny                    []string
	AgentTools                      map[string]models.AgentToolConfig
}

// PublishInitialState constructs the initial state of a goal and publishes it
// with a whole-state Write. It returns the unwrapped write error; the caller
// owns cleanup and the user-facing wrap.
func PublishInitialState(statePath string, in InitialStateInputs) error {
	return db.For(statePath).Write(composeInitialState(in))
}

func composeInitialState(in InitialStateInputs) *models.State {
	goalID := fmt.Sprintf(initGoalIDFormat, in.Timestamp.Unix())
	return &models.State{
		Version:         initStateVersion,
		PipelineVersion: initPipelineVersion,
		Goal: models.Goal{
			ID:          goalID,
			Description: in.Description,
			SpecRef:     in.SpecRef,
			EntryPoint:  in.EntryPoint,
			Created:     in.Timestamp,
			Status:      models.GoalStatusInProgress,
			AlignmentHistory: []models.AlignmentHistory{
				{
					Timestamp: in.Timestamp,
					Event:     string(models.TaskEventInitialization),
					Summary:   initAlignmentSummary,
				},
			},
		},
		Tasks:       []models.Task{},
		Agents:      make(map[string]models.Agent),
		Discovered:  []models.Discovery{},
		HumanNotes:  []models.HumanNote{},
		SpecChanges: []models.SpecChange{},
		Anomalies:   []models.Anomaly{},
		Sprint:      initialSprint(goalID, in.Timestamp),
		CircuitBreaker: models.CircuitBreaker{
			LastCheck:      time.Time{}, // zero value for null
			Status:         circuitBreakerStatusOK,
			CurrentTrigger: nil,
			History:        []models.CircuitBreakerHistory{},
		},
		Config: composeInitialConfig(in),
	}
}

func initialSprint(goalID string, timestamp time.Time) models.Sprint {
	return models.Sprint{
		ID:      initSprintID,
		Number:  initSprintNumber,
		GoalRef: goalID,
		Scope: models.SprintScope{
			Planned: []string{},
			Stretch: []string{},
		},
		Timeline: models.SprintTimeline{
			Started:      timestamp,
			Deadline:     time.Time{}, // zero value for null
			CheckpointAt: nil,
			Ended:        nil,
		},
		Status: models.SprintStatusInProgress,
		Metrics: models.SprintMetrics{
			TasksDone:         0,
			TasksInProgress:   0,
			TasksBlocked:      0,
			IterationsTotal:   0,
			ReviewCyclesTotal: 0,
		},
		Retrospective: nil,
	}
}

// composeInitialConfig builds the initial configuration from the resolved inputs.
func composeInitialConfig(in InitialStateInputs) models.Config {
	var postWorktreeCmd *string
	if in.PostWorktreeCmd != "" {
		postWorktreeCmd = &in.PostWorktreeCmd
	}
	return models.Config{
		TerminalTaskArchival:            true,
		MaxCoderIterations:              models.DefaultMaxCoderIterations,
		MaxReviewCycles:                 models.DefaultMaxReviewCycles,
		MaxGlobalIntegrationGenerations: models.NormalizeGlobalIntegrationGenerationLimit(in.MaxGlobalIntegrationGenerations),
		MaxInstances:                    models.EffectiveMaxInstances(0, in.MaxInstances),
		HeartbeatInterval:               models.DefaultHeartbeatIntervalSec,
		LeaseDuration:                   models.DefaultLeaseDurationSeconds,
		CoderPollInterval:               models.DefaultCoderPollInterval,
		DoerMaxWait:                     models.DefaultDoerMaxWait,
		OrchestratorPollInterval:        models.DefaultOrchestratorPollInterval,
		OrchestratorMaxWait:             models.DefaultOrchestratorMaxWait,
		ReviewerPollInterval:            models.DefaultReviewerPollInterval,
		ReviewerMaxWait:                 models.DefaultReviewerMaxWait,
		AgentProgressTimeout:            models.DefaultAgentProgressTimeoutSec,
		DefaultCLI:                      in.DefaultCLI,
		ScipSearch:                      in.ScipSearch,
		IntegrationBranch:               in.Branch,
		EscalationWebhook:               nil,
		Mode:                            models.SystemModeRunning,
		AutoResume:                      in.AutoResume,
		NoFollowUp:                      in.NoFollowUp,
		PostWorktreeCmd:                 postWorktreeCmd,
		CopyWorktreeEnvFiles:            in.CopyWorktreeEnvFiles,
		AgentEnvDeny:                    in.AgentEnvDeny,
		AgentTools:                      in.AgentTools,
	}
}
