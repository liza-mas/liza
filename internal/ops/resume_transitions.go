package ops

import (
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// resumeTransitionReport is the post-commit outcome of a resume's transition pass.
type resumeTransitionReport struct {
	executed int    // transitions fired
	failure  string // non-empty if the pass or its checkpoint cleanup failed
}

// reportResumeTransitions executes available transitions after the resume
// transaction committed, so child tasks are created in the new sprint. This
// handles merged planning tasks with unconsumed output[] (e.g., epic → US
// writing, code plan → coding). Mid-sprint transition checkpoints use the same
// path so ready planning or many-to-one output can hand off without waiting for
// a separate orchestrator PreWork cycle. The human already reviewed by resuming
// from the checkpoint/COMPLETED state; transitions are idempotent via
// TransitionsExecuted. Failures are reported, never returned: the committed
// resume stands.
func reportResumeTransitions(projectRoot string, origin resumeOrigin, resumedCheckpoint models.Sprint) resumeTransitionReport {
	// An automatic resume carries no human review, so the reviewed plan
	// hand-off needs the orchestrator's pass; an operator resume is that review.
	admission := AdmitOperator
	if origin == resumeOriginAutomatic {
		admission = AdmitReviewed
	}
	report, err := ExecuteTransitionsReportWith(projectRoot, "", admission)
	if err != nil {
		return resumeTransitionReport{failure: err.Error()}
	}
	failures := make([]string, 0, len(report.Failures)+1)
	for _, failure := range report.Failures {
		failures = append(failures, failure.String())
	}
	if err := clearTransitionCheckpointTrigger(projectRoot, resumedCheckpoint); err != nil {
		failures = append(failures, err.Error())
	}
	return resumeTransitionReport{executed: len(report.Results), failure: strings.Join(failures, "; ")}
}

// SameResumedTransitionCheckpoint protects a newer checkpoint or sprint from
// cleanup by an earlier resume/PreWork pass. Compare inside the write callback.
func SameResumedTransitionCheckpoint(current, expected models.Sprint) bool {
	if current.Status != models.SprintStatusInProgress || current.ID != expected.ID || current.Number != expected.Number ||
		!models.IsTransitionCheckpointTrigger(expected.CheckpointTrigger) || current.CheckpointTrigger != expected.CheckpointTrigger {
		return false
	}
	a, b := current.Timeline.CheckpointAt, expected.Timeline.CheckpointAt
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

func clearTransitionCheckpointTrigger(projectRoot string, expected models.Sprint) error {
	statePath := paths.New(projectRoot).StatePath()
	blackboard := db.For(statePath)
	return blackboard.Modify(func(s *models.State) error {
		if SameResumedTransitionCheckpoint(s.Sprint, expected) {
			s.Sprint.CheckpointTrigger = ""
		}
		return nil
	})
}
