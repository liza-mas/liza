package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ErrSprintAlreadyCheckpoint indicates the sprint is already at CHECKPOINT.
var ErrSprintAlreadyCheckpoint = errors.New("sprint is already at CHECKPOINT")

// SprintCheckpointResult contains the outcome of creating a sprint checkpoint.
type SprintCheckpointResult struct {
	CheckpointAt time.Time `json:"checkpoint_at"`
	ReportPath   string    `json:"report_path"`
	Trigger      string    `json:"trigger,omitempty"`
}

// ErrNoPassedPlanCheckpoint reports that the locked state admits no automatic
// passed-plan checkpoint; nothing was written.
var ErrNoPassedPlanCheckpoint = errors.New("no passed plan awaits a checkpoint")

// sprintCheckpointBeforeModifyTestHook runs between the preliminary read and
// the locked write, so tests can commit a competing write there.
var sprintCheckpointBeforeModifyTestHook func()

// CheckpointPassedPlans creates the PLANNING_COMPLETE checkpoint for plans an
// orchestrator turn left passed and unattempted, whichever trigger woke it
// (D-219). Being automatic, it is admitted only for a RUNNING system and an
// IN_PROGRESS sprint holding such a plan, judged again under the lock that
// writes the checkpoint: a concurrent hard checkpoint, terminal sprint, pause,
// hold or attempt wins. It returns the plans the checkpoint was made for.
func CheckpointPassedPlans(projectRoot string) ([]string, error) {
	domain, err := LoadPlanHandoffDomain(projectRoot)
	if err != nil {
		return nil, err
	}
	var ids []string
	admit := func(s *models.State) error {
		ids = nil
		if s.Sprint.Status != models.SprintStatusInProgress ||
			s.Config.Mode != "" && s.Config.Mode != models.SystemModeRunning {
			return ErrNoPassedPlanCheckpoint
		}
		if ids = PassedPlansAwaitingCheckpoint(s, domain); len(ids) == 0 {
			return ErrNoPassedPlanCheckpoint
		}
		return nil
	}
	if _, err := sprintCheckpoint(projectRoot, models.CheckpointTriggerPlanningComplete, admit); err != nil {
		return nil, err
	}
	return ids, nil
}

// SprintCheckpoint transitions sprint status to CHECKPOINT and writes a sprint summary report.
// The trigger parameter records why the checkpoint was created (e.g. "PLANNING_COMPLETE",
// "MANY_TO_ONE_READY", "SPRINT_COMPLETE", or "" for manual). Transition checkpoints gate
// downstream transition creation; hard checkpoints pause all agents. No terminal I/O.
func SprintCheckpoint(projectRoot string, trigger string) (*SprintCheckpointResult, error) {
	return sprintCheckpoint(projectRoot, trigger, nil)
}

// sprintCheckpoint is SprintCheckpoint with an optional admission, checked on
// the preliminary read and again under the lock before anything is written.
func sprintCheckpoint(projectRoot string, trigger string, admit func(*models.State) error) (*SprintCheckpointResult, error) {
	lizaPaths := paths.New(projectRoot)
	blackboard := db.For(lizaPaths.StatePath())

	state, err := admitSprintCheckpoint(blackboard, admit)
	if err != nil {
		return nil, err
	}
	trigger = resolveCheckpointTrigger(projectRoot, state, trigger)

	run := sprintCheckpointRun{
		blackboard: blackboard,
		reportPath: lizaPaths.SprintSummaryPath(),
		timestamp:  time.Now(),
		trigger:    trigger,
		admit:      admit,
	}
	if sprintCheckpointBeforeModifyTestHook != nil {
		sprintCheckpointBeforeModifyTestHook()
	}
	if trigger == models.CheckpointTriggerSprintComplete {
		err = withEffectiveIntegrationCompletionAuthorization(projectRoot, "sprint checkpoint", true, func(authorization *effectiveIntegrationCompletionAuthorization) error {
			return run.checkpoint(state, authorization)
		})
	} else {
		err = run.checkpoint(state, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to update sprint status: %w", err)
	}

	return &SprintCheckpointResult{
		CheckpointAt: run.timestamp,
		ReportPath:   run.reportPath,
		Trigger:      trigger,
	}, nil
}

// admitSprintCheckpoint reads the state and refuses a sprint that cannot take
// a checkpoint, then applies the caller's admission. It returns the state read.
func admitSprintCheckpoint(blackboard *db.Blackboard, admit func(*models.State) error) (*models.State, error) {
	state, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	switch state.Sprint.Status {
	case models.SprintStatusCheckpoint:
		return nil, ErrSprintAlreadyCheckpoint
	case models.SprintStatusCompleted:
		return nil, &PreconditionError{Reason: "cannot checkpoint: sprint is COMPLETED"}
	case models.SprintStatusAborted:
		return nil, &PreconditionError{Reason: "cannot checkpoint: sprint is ABORTED"}
	}
	if admit != nil {
		if err := admit(state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

// resolveCheckpointTrigger returns the given trigger, or detects a transition
// checkpoint when it is empty. This makes the system resilient to LLM omission
// of the trigger parameter.
func resolveCheckpointTrigger(projectRoot string, state *models.State, trigger string) string {
	if trigger != "" {
		return trigger
	}
	// If LoadDetectionContext fails (no pipeline config), the trigger stays
	// empty. This is fine for legacy projects without pipelines.
	detCtx, err := LoadDetectionContext(projectRoot)
	if err != nil {
		return ""
	}
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if detCtx.PlanHandoff.HasUnfailedHandoff(state, task) {
			return models.CheckpointTriggerPlanningComplete
		}
	}
	if CountReadyManyToOneCohorts(state, detCtx.ManyToOneTransitions) > 0 {
		return models.CheckpointTriggerManyToOneReady
	}
	return ""
}

// sprintCheckpointRun is one checkpoint attempt: where the report goes, when
// and why the checkpoint is made, and the admission to judge again under the lock.
type sprintCheckpointRun struct {
	blackboard *db.Blackboard
	reportPath string
	timestamp  time.Time
	trigger    string
	admit      func(*models.State) error
}

// checkpoint writes the report and the sprint update. Under an authorization
// it first reads the state again and validates it, so the report describes the
// state the authorization judged; state is the preliminary read otherwise.
func (r sprintCheckpointRun) checkpoint(state *models.State, authorization *effectiveIntegrationCompletionAuthorization) error {
	if authorization != nil {
		var err error
		state, err = r.blackboard.Read()
		if err != nil {
			return fmt.Errorf("failed to re-read state after integration completion check: %w", err)
		}
		if err := authorization.validateState(state, true); err != nil {
			return err
		}
	}

	stagedReportPath, err := stageSprintSummary(filepath.Dir(r.reportPath), generateSprintSummary(state, r.timestamp))
	if err != nil {
		return err
	}
	defer os.Remove(stagedReportPath)

	return r.blackboard.Modify(func(s *models.State) error {
		return r.apply(s, stagedReportPath, authorization)
	})
}

// stageSprintSummary writes the report to a temporary file in dir and returns
// its path, which the caller removes once the report is published or abandoned.
func stageSprintSummary(dir, report string) (string, error) {
	stagedReport, err := os.CreateTemp(dir, ".sprint-summary-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to stage sprint summary: %w", err)
	}
	stagedReportPath := stagedReport.Name()
	if _, err := stagedReport.WriteString(report); err != nil {
		_ = stagedReport.Close()
		_ = os.Remove(stagedReportPath)
		return "", fmt.Errorf("failed to stage sprint summary: %w", err)
	}
	if err := stagedReport.Chmod(0644); err != nil {
		_ = stagedReport.Close()
		_ = os.Remove(stagedReportPath)
		return "", fmt.Errorf("failed to stage sprint summary: %w", err)
	}
	if err := stagedReport.Close(); err != nil {
		_ = os.Remove(stagedReportPath)
		return "", fmt.Errorf("failed to stage sprint summary: %w", err)
	}
	return stagedReportPath, nil
}

// apply is the locked update: it judges the admission and the authorization
// again on the locked state, publishes the staged report and moves the sprint
// to CHECKPOINT with its pending steering-report obligation.
func (r sprintCheckpointRun) apply(s *models.State, stagedReportPath string, authorization *effectiveIntegrationCompletionAuthorization) error {
	if r.admit != nil {
		if err := r.admit(s); err != nil {
			return err
		}
	}
	if authorization != nil {
		if err := authorization.validateState(s, true); err != nil {
			return err
		}
	}
	if err := os.Rename(stagedReportPath, r.reportPath); err != nil {
		return fmt.Errorf("failed to write sprint summary: %w", err)
	}
	since := s.Sprint.Timeline.CheckpointAt
	s.Sprint.Status = models.SprintStatusCheckpoint
	s.Sprint.Timeline.CheckpointAt = &r.timestamp
	s.Sprint.CheckpointTrigger = r.trigger
	// Record the steering-report obligation in the same transaction
	// that creates the checkpoint, so it cannot be lost to whatever
	// happens to the sprint before the orchestrator next looks.
	s.PendingCheckpointSummary = &models.PendingCheckpointSummary{At: r.timestamp, Trigger: r.trigger, Since: since}
	return nil
}
