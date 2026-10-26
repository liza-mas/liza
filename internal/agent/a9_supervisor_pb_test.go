package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Characterization of the orchestrator post-turn verification (A9 supervisor
// PB-17). Written green on the unchanged tree before verifyOrchestratorWakeChanges
// was reduced to a per-trigger dispatch: each trigger's expectation and whether
// a missed expectation is a warning (nil) or a refusal (error) stay fixed.

func pbVerifyState(sprintStatus models.SprintStatus, checkpointAt *time.Time, tasks ...models.Task) *models.State {
	now := time.Now().UTC()
	return &models.State{
		Version: 1,
		Goal:    models.Goal{ID: "goal-1", Description: "pb", Status: models.GoalStatusInProgress, Created: now},
		Sprint: models.Sprint{
			Number:   1,
			Status:   sprintStatus,
			Timeline: models.SprintTimeline{CheckpointAt: checkpointAt},
		},
		Tasks:  tasks,
		Config: models.Config{IntegrationBranch: "main"},
	}
}

func pbDiscovery(id string, urgency models.DiscoveryUrgency, converted bool) models.Discovery {
	d := models.Discovery{ID: id, Urgency: urgency, Created: time.Now().UTC()}
	if converted {
		task := "task-converted"
		d.ConvertedToTask = &task
	}
	return d
}

func TestA9VerifyOrchestratorWakeTriggers(t *testing.T) {
	now := time.Now().UTC()
	task := func(status models.TaskStatus) models.Task {
		return testhelpers.BuildTaskByStatus("task-1", status, now)
	}

	tests := []struct {
		name    string
		result  OrchestratorWakeResult
		before  *models.State
		after   *models.State
		wantErr string // "" = verification passes (possibly with a warning)
	}{
		{
			name:    "initial planning without tasks is refused",
			result:  OrchestratorWakeResult{Trigger: WakeTriggerInitialPlanning},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusInProgress, nil),
			wantErr: "INITIAL_PLANNING trigger but no tasks were created",
		},
		{
			name:   "initial planning with tasks passes",
			result: OrchestratorWakeResult{Trigger: WakeTriggerInitialPlanning},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusInProgress, nil, task(models.TaskStatusReady)),
		},
		{
			name:   "unresolved blocked tasks only warn",
			result: OrchestratorWakeResult{Trigger: WakeTriggerBlocked},
			before: pbVerifyState(models.SprintStatusInProgress, nil, task(models.TaskStatusBlocked)),
			after:  pbVerifyState(models.SprintStatusInProgress, nil, task(models.TaskStatusBlocked)),
		},
		{
			name:   "human note turn has no expected change",
			result: OrchestratorWakeResult{Trigger: WakeTriggerHumanNote},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusInProgress, nil),
		},
		{
			name:   "unconverted immediate discovery is refused",
			result: OrchestratorWakeResult{Trigger: WakeTriggerImmediateDiscovery},
			before: func() *models.State {
				s := pbVerifyState(models.SprintStatusInProgress, nil)
				s.Discovered = []models.Discovery{pbDiscovery("d-1", models.DiscoveryUrgencyImmediate, false)}
				return s
			}(),
			after: func() *models.State {
				s := pbVerifyState(models.SprintStatusInProgress, nil)
				s.Discovered = []models.Discovery{pbDiscovery("d-1", models.DiscoveryUrgencyImmediate, false)}
				return s
			}(),
			wantErr: "IMMEDIATE_DISCOVERY trigger but unconverted count didn't decrease (before: 1, after: 1)",
		},
		{
			name:   "converted immediate discovery passes",
			result: OrchestratorWakeResult{Trigger: WakeTriggerImmediateDiscovery},
			before: func() *models.State {
				s := pbVerifyState(models.SprintStatusInProgress, nil)
				s.Discovered = []models.Discovery{pbDiscovery("d-1", models.DiscoveryUrgencyImmediate, false)}
				return s
			}(),
			after: func() *models.State {
				s := pbVerifyState(models.SprintStatusInProgress, nil)
				s.Discovered = []models.Discovery{pbDiscovery("d-1", models.DiscoveryUrgencyImmediate, true)}
				return s
			}(),
		},
		{
			name:    "many-to-one without a checkpoint is refused",
			result:  OrchestratorWakeResult{Trigger: WakeTriggerManyToOneReady},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusInProgress, nil),
			wantErr: "MANY_TO_ONE_READY trigger but sprint status is IN_PROGRESS (expected CHECKPOINT or COMPLETED)",
		},
		{
			name:    "many-to-one checkpoint without checkpoint_at is refused",
			result:  OrchestratorWakeResult{Trigger: WakeTriggerManyToOneReady},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusCheckpoint, nil),
			wantErr: "MANY_TO_ONE_READY trigger but checkpoint_at is not set",
		},
		{
			name:    "sprint complete without a checkpoint is refused",
			result:  OrchestratorWakeResult{Trigger: WakeTriggerSprintComplete},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusInProgress, nil),
			wantErr: "SPRINT_COMPLETE trigger but sprint status is IN_PROGRESS (expected CHECKPOINT or COMPLETED)",
		},
		{
			name:    "sprint complete checkpoint without checkpoint_at is refused",
			result:  OrchestratorWakeResult{Trigger: WakeTriggerSprintComplete},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusCompleted, nil),
			wantErr: "SPRINT_COMPLETE trigger but checkpoint_at is not set",
		},
		{
			name:   "sprint complete with a checkpoint passes",
			result: OrchestratorWakeResult{Trigger: WakeTriggerSprintComplete},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusCheckpoint, &now),
		},
		{
			name: "coding complete outside reconciliation is refused",
			result: OrchestratorWakeResult{
				Trigger:     WakeTriggerCodingComplete,
				Integration: prompts.EffectiveIntegrationCompletion{Status: prompts.IntegrationViewWaiting},
			},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusInProgress, nil),
			wantErr: `CODING_COMPLETE trigger but integration status is "waiting"`,
		},
		{
			name: "integration blocked outcome is accepted",
			result: OrchestratorWakeResult{
				Trigger:     WakeTriggerIntegrationBlocked,
				Integration: prompts.EffectiveIntegrationCompletion{Status: prompts.IntegrationViewBlocked},
			},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusInProgress, nil),
		},
		{
			name: "integration waiting with an unaccepted outcome is refused",
			result: OrchestratorWakeResult{
				Trigger:     WakeTriggerIntegrationWaiting,
				Integration: prompts.EffectiveIntegrationCompletion{Status: prompts.IntegrationViewWaiting, ReasonCode: "r"},
			},
			before:  pbVerifyState(models.SprintStatusInProgress, nil),
			after:   pbVerifyState(models.SprintStatusInProgress, nil),
			wantErr: `integration outcome "waiting" (r) is not an accepted post-run result`,
		},
		{
			name: "integration exhausted outcome is accepted",
			result: OrchestratorWakeResult{
				Trigger:     WakeTriggerIntegrationExhausted,
				Integration: prompts.EffectiveIntegrationCompletion{Status: prompts.IntegrationViewExhausted},
			},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusInProgress, nil),
		},
		{
			name:   "unlisted trigger verifies nothing",
			result: OrchestratorWakeResult{Trigger: WakeTriggerNone},
			before: pbVerifyState(models.SprintStatusInProgress, nil),
			after:  pbVerifyState(models.SprintStatusInProgress, nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.WriteInitialState(t, statePath, tt.after)
			err := verifyOrchestratorWakeChanges(db.New(statePath), tt.before, tt.result)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("verification refused: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("verification passed, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}
