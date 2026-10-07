package agent

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestD63AutoResumeReportsPartialTransitionFailure(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AutoResume = true
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("active", models.TaskStatusImplementing, time.Now().UTC())}
	state.Sprint.Scope.Planned = []string{"active"}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	previous := resumeCheckpoint
	t.Cleanup(func() { resumeCheckpoint = previous })
	resumeCheckpoint = func(string, string) (*ops.ResumeResult, error) {
		if err := bb.Modify(func(s *models.State) error {
			s.Sprint.Status = models.SprintStatusInProgress
			s.Sprint.CheckpointTrigger = ""
			return nil
		}); err != nil {
			return nil, err
		}
		return &ops.ResumeResult{TransitionsExecuted: 2, TransitionError: "task plan transition architecture-to-code-plan: live provider declaration"}, nil
	}
	logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitWhilePaused(ctx, root, "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if countLogLines(logs.String(), "level=WARN", "live provider declaration", "transitions_executed=2") != 1 {
		t.Fatalf("auto-resume lost partial report: %s", logs.String())
	}
	if strings.Contains(logs.String(), "liza") {
		t.Fatal("resume diagnostic contains a raw brand identifier")
	}
	if after, err := db.For(statePath).Read(); err != nil || after.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("resume did not complete: %+v, %v", after, err)
	}
}

func d63PausedProject(t *testing.T, status models.SprintStatus, trigger string) (string, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AutoResume = true
	state.Sprint.Status = status
	state.Sprint.CheckpointTrigger = trigger
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("active", models.TaskStatusImplementing, time.Now().UTC())}
	state.Sprint.Scope.Planned = []string{"active"}
	return root, testhelpers.WriteInitialState(t, statePath, state)
}

func TestD63TransitionCheckpointAutoResumeOwnership(t *testing.T) {
	for _, trigger := range []string{models.CheckpointTriggerPlanningComplete, models.CheckpointTriggerManyToOneReady} {
		for _, role := range []string{"doer", "reviewer", "orchestrator"} {
			t.Run(trigger+"/"+role, func(t *testing.T) {
				root, bb := d63PausedProject(t, models.SprintStatusCheckpoint, trigger)
				before, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				previous := resumeCheckpoint
				t.Cleanup(func() { resumeCheckpoint = previous })
				calls := 0
				resumeCheckpoint = func(string, string) (*ops.ResumeResult, error) {
					calls++
					if err := bb.Modify(func(s *models.State) error {
						s.Sprint.Status = models.SprintStatusInProgress
						s.Sprint.CheckpointTrigger = ""
						return nil
					}); err != nil {
						return nil, err
					}
					return &ops.ResumeResult{TransitionsExecuted: 3}, nil
				}
				logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := waitWhilePaused(ctx, root, role); err != nil {
					t.Fatal(err)
				}
				after, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				if role != "orchestrator" {
					if calls != 0 || !reflect.DeepEqual(before, after) {
						t.Fatalf("%s consumed transition checkpoint: calls=%d sprint=%+v", role, calls, after.Sprint)
					}
					return
				}
				if calls != 1 || after.Sprint.Status != models.SprintStatusInProgress || after.Sprint.CheckpointTrigger != "" {
					t.Fatalf("orchestrator did not resume once: calls=%d sprint=%+v", calls, after.Sprint)
				}
				if countLogLines(logs.String(), "level=INFO", "Auto-resume completed", "transitions_executed=3") != 1 {
					t.Fatalf("successful resume lost transition count: %s", logs.String())
				}
			})
		}
	}
}

func TestD63OrdinaryCheckpointStillAutoResumes(t *testing.T) {
	root, bb := d63PausedProject(t, models.SprintStatusCheckpoint, "")
	previous := resumeCheckpoint
	t.Cleanup(func() { resumeCheckpoint = previous })
	calls := 0
	resumeCheckpoint = func(string, string) (*ops.ResumeResult, error) {
		calls++
		if err := bb.Modify(func(s *models.State) error {
			s.Sprint.Status = models.SprintStatusInProgress
			return nil
		}); err != nil {
			return nil, err
		}
		return &ops.ResumeResult{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitWhilePaused(ctx, root, "doer"); err != nil {
		t.Fatal(err)
	}
	after, err := bb.Read()
	if err != nil || calls != 1 || after.Sprint.Status != models.SprintStatusInProgress {
		t.Fatalf("ordinary checkpoint did not resume: calls=%d state=%+v err=%v", calls, after, err)
	}
}

func TestD63CompletedSprintReportsTransitionResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		error string
	}{
		{name: "partial failure", count: 2, error: "live provider declaration"},
		{name: "failed handoff is not goal completion", error: "live provider declaration"},
		{name: "successful transitions", count: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, bb := d63PausedProject(t, models.SprintStatusCompleted, "")
			previousResume, previousStop := resumeCompletedSprint, stopCompletedGoal
			t.Cleanup(func() { resumeCompletedSprint, stopCompletedGoal = previousResume, previousStop })
			resumeCalls, stopCalls := 0, 0
			resumeCompletedSprint = func(string, string) (*ops.ResumeResult, error) {
				resumeCalls++
				if err := bb.Modify(func(s *models.State) error {
					s.Sprint.Status = models.SprintStatusInProgress
					return nil
				}); err != nil {
					return nil, err
				}
				return &ops.ResumeResult{SprintAdvanced: &ops.AdvanceSprintResult{}, TransitionsExecuted: tc.count, TransitionError: tc.error}, nil
			}
			stopCompletedGoal = func(string, string) (*ops.ModeChangeResult, error) {
				stopCalls++
				return &ops.ModeChangeResult{}, nil
			}
			logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := waitWhilePaused(ctx, root, "orchestrator"); err != nil {
				t.Fatal(err)
			}
			if resumeCalls != 1 || stopCalls != 0 {
				t.Fatalf("completion result mishandled: resume=%d stop=%d", resumeCalls, stopCalls)
			}
			level, message := "level=INFO", "Auto-resume completed"
			if tc.error != "" {
				level, message = "level=WARN", tc.error
			}
			if countLogLines(logs.String(), level, message, fmt.Sprintf("transitions_executed=%d", tc.count)) != 1 {
				t.Fatalf("completed-sprint resume lost report: %s", logs.String())
			}
		})
	}
}
