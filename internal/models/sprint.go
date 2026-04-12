package models

import "time"

// SprintStatus represents the state of a sprint
type SprintStatus string

const (
	SprintStatusInProgress SprintStatus = "IN_PROGRESS"
	SprintStatusCheckpoint SprintStatus = "CHECKPOINT"
	SprintStatusCompleted  SprintStatus = "COMPLETED"
	SprintStatusAborted    SprintStatus = "ABORTED"
)

// CheckpointTrigger values record why a checkpoint was created.
const (
	CheckpointTriggerPlanningComplete = "PLANNING_COMPLETE"
	CheckpointTriggerSprintComplete   = "SPRINT_COMPLETE"
)

// IsValid checks if the sprint status is valid
func (ss SprintStatus) IsValid() bool {
	switch ss {
	case SprintStatusInProgress, SprintStatusCheckpoint, SprintStatusCompleted, SprintStatusAborted:
		return true
	}
	return false
}

// Sprint represents a sprint with scope, timeline, and metrics
type Sprint struct {
	ID                string         `yaml:"id"`
	Number            int            `yaml:"number"`
	GoalRef           string         `yaml:"goal_ref"`
	Scope             SprintScope    `yaml:"scope"`
	Timeline          SprintTimeline `yaml:"timeline"`
	Status            SprintStatus   `yaml:"status"`
	Metrics           SprintMetrics  `yaml:"metrics"`
	CheckpointTrigger string         `yaml:"checkpoint_trigger,omitempty"`
	Retrospective     *string        `yaml:"retrospective,omitempty"`
	Extra             map[string]any `yaml:",inline"`
}

// SprintSummary is a lightweight record of a completed sprint kept in state.yaml.
// Full sprint data (metrics, scope, retrospective) is archived to .liza/archive/sprint-N.yaml.
type SprintSummary struct {
	ID        string       `yaml:"id"`
	Number    int          `yaml:"number"`
	Status    SprintStatus `yaml:"status"`
	Started   time.Time    `yaml:"started"`
	Ended     time.Time    `yaml:"ended"`
	TasksDone int          `yaml:"tasks_done"`
}

// SprintScope defines planned and stretch tasks
type SprintScope struct {
	Planned []string       `yaml:"planned"`
	Stretch []string       `yaml:"stretch"`
	Extra   map[string]any `yaml:",inline"`
}

// SprintTimeline defines sprint timing
type SprintTimeline struct {
	Started      time.Time      `yaml:"started"`
	Deadline     time.Time      `yaml:"deadline"`
	CheckpointAt *time.Time     `yaml:"checkpoint_at,omitempty"`
	Ended        *time.Time     `yaml:"ended,omitempty"`
	Extra        map[string]any `yaml:",inline"`
}

// SprintMetrics tracks sprint progress and quality
type SprintMetrics struct {
	TasksDone                        int            `yaml:"tasks_done" json:"tasks_done"`
	TasksInProgress                  int            `yaml:"tasks_in_progress" json:"tasks_in_progress"`
	TasksBlocked                     int            `yaml:"tasks_blocked" json:"tasks_blocked"`
	IterationsTotal                  int            `yaml:"iterations_total" json:"iterations_total"`
	ReviewCyclesTotal                int            `yaml:"review_cycles_total" json:"review_cycles_total"`
	ReviewVerdictApprovals           int            `yaml:"review_verdict_approvals" json:"review_verdict_approvals"`
	ReviewVerdictRejections          int            `yaml:"review_verdict_rejections" json:"review_verdict_rejections"`
	ReviewVerdictCount               int            `yaml:"review_verdict_count" json:"review_verdict_count"`
	ReviewVerdictApprovalRatePercent int            `yaml:"review_verdict_approval_rate_percent" json:"review_verdict_approval_rate_percent"`
	TaskSubmittedForReviewCount      int            `yaml:"task_submitted_for_review_count" json:"task_submitted_for_review_count"`
	TaskOutcomeApprovalRatePercent   int            `yaml:"task_outcome_approval_rate_percent" json:"task_outcome_approval_rate_percent"`
	Extra                            map[string]any `yaml:",inline" json:"-"`
}

// AllPlannedTasksTerminal returns true if the sprint has planned tasks and all of
// them are in a sprint-terminal state. Returns false if the planned list is empty
// or any planned task is not found/not sprint-terminal.
// Equivalent to AllPlannedTasksTerminalWith(nil).
func (s *State) AllPlannedTasksTerminal() bool {
	return s.AllPlannedTasksTerminalWith(nil)
}

// AllPlannedTasksTerminalWith checks if all planned tasks are sprint-terminal.
// Uses pipeline-defined terminal states in addition to universal terminals
// (MERGED, ABANDONED, SUPERSEDED). When pipelineTerminals is nil, only universal
// terminal states are recognized.
func (s *State) AllPlannedTasksTerminalWith(pipelineTerminals []TaskStatus) bool {
	if len(s.Sprint.Scope.Planned) == 0 {
		return false
	}
	for _, taskID := range s.Sprint.Scope.Planned {
		task := s.FindTask(taskID)
		if task == nil {
			return false
		}
		if task.RolePair != "" {
			if !task.Status.IsPipelineSprintTerminal(pipelineTerminals) {
				return false
			}
		} else {
			if !task.Status.IsSprintTerminal() {
				return false
			}
		}
	}
	return true
}

// SprintStalled returns true if the sprint has planned tasks and every planned
// task is either sprint-terminal or BLOCKED, with at least one BLOCKED. This indicates
// no agent can make progress — the sprint is stuck and needs human intervention.
func (s *State) SprintStalled() bool {
	if len(s.Sprint.Scope.Planned) == 0 {
		return false
	}
	hasBlocked := false
	for _, taskID := range s.Sprint.Scope.Planned {
		task := s.FindTask(taskID)
		if task == nil {
			return false
		}
		if task.Status == TaskStatusBlocked {
			hasBlocked = true
		} else if !task.Status.IsSprintTerminal() {
			return false
		}
	}
	return hasBlocked
}
