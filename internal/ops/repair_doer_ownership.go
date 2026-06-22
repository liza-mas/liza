package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
)

// RepairInvalidDoerOwnership clears active doer claims whose owner process is
// gone or whose task-side and agent-side owner state cannot both be true.
// Unlike review repair, live doer PIDs are never repaired automatically because
// the task worktree may contain unsubmitted edits.
func RepairInvalidDoerOwnership(statePath, projectRoot, logPath, reason string) (int, error) {
	if reason == "" {
		reason = "validate --repair"
	}

	bb := db.For(statePath)
	logger := log.New(logPath)

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return 0, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	repaired := 0
	var refusals []string
	now := time.Now().UTC()

	err = bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			task := &state.Tasks[i]

			revertStatus, err := detectExecutingRevertStatus(task, pb)
			if err != nil {
				return err
			}
			if revertStatus == "" {
				continue
			}
			if task.AssignedTo != nil && strings.HasPrefix(*task.AssignedTo, "$") {
				continue
			}

			repairReason := invalidOrDeadActiveDoerOwnershipReason(state, task, pb.pr)
			if repairReason == "" {
				continue
			}

			doerID := ""
			if task.AssignedTo != nil {
				doerID = *task.AssignedTo
			}
			if doerID != "" {
				if agent, ok := state.Agents[doerID]; ok && agent.PID > 0 && IsProcessAlive(agent.PID) {
					refusals = append(refusals, fmt.Sprintf(
						"cannot repair invalid active doer ownership for task %s: assigned agent %s has live PID %d; inspect the worktree, then use %s or %s",
						task.ID,
						doerID,
						agent.PID,
						brand.Command("recover-agent", doerID, "--force"),
						brand.Command("release-claim", task.ID, "--role", "doer", "--force"),
					))
					continue
				}
			}

			if err := task.TransitionWith(revertStatus, pb.transitions); err != nil {
				return err
			}
			clearDoerClaimFields(task)

			if doerID != "" {
				if agent, ok := state.Agents[doerID]; ok {
					if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
						state.ReleaseAgent(doerID)
					}
				}
			}

			detail := fmt.Sprintf("Invalid active doer ownership repaired (%s; reason: %s). Worktree left on disk for inspection; a later reclaim may remove stale worktree resources.", reason, repairReason)
			if err := logger.Append(log.Entry{
				Timestamp: now,
				Agent:     "system",
				Action:    "invalid_doer_ownership_repaired",
				Task:      &task.ID,
				Detail:    detail,
			}); err != nil {
				return fmt.Errorf("failed to log invalid doer ownership repair for %s: %w", task.ID, err)
			}

			repaired++
		}

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to repair invalid doer ownership: %w", err)
	}
	if len(refusals) > 0 {
		return repaired, &DoerRepairRefusedError{Messages: refusals}
	}

	return repaired, nil
}

// DoerRepairRefusedError reports invalid doer ownership rows that were unsafe
// to repair automatically because their assigned process was still live.
type DoerRepairRefusedError struct {
	Messages []string
}

func (e *DoerRepairRefusedError) Error() string {
	return strings.Join(e.Messages, "; ")
}

func detectExecutingRevertStatus(task *models.Task, pb *pipelineBundle) (models.TaskStatus, error) {
	if task.RolePair == "" {
		return "", nil
	}
	executing, err := pb.pr.ExecutingStatus(task.RolePair)
	if err != nil {
		return "", nil
	}
	if task.Status != executing {
		return "", nil
	}
	initial, err := pb.pr.InitialStatus(task.RolePair)
	if err != nil {
		return "", fmt.Errorf("task %s is in executing state %s but initial status resolution failed for role-pair %q: %w",
			task.ID, task.Status, task.RolePair, err)
	}
	return initial, nil
}

func invalidOrDeadActiveDoerOwnershipReason(state *models.State, task *models.Task, pr models.PipelineResolver) string {
	reason := invalidActiveDoerOwnershipReason(state, task, pr)
	if reason != "" {
		return reason
	}
	if task.AssignedTo == nil || *task.AssignedTo == "" {
		return ""
	}
	agent, exists := state.Agents[*task.AssignedTo]
	if !exists || agent.PID <= 0 {
		return ""
	}
	if !IsProcessAlive(agent.PID) {
		return fmt.Sprintf("assigned_to %s has no live process (pid %d)", *task.AssignedTo, agent.PID)
	}
	return ""
}

func invalidActiveDoerOwnershipReason(state *models.State, task *models.Task, pr models.PipelineResolver) string {
	doerID := ""
	if task.AssignedTo != nil {
		doerID = *task.AssignedTo
	}
	return models.ActiveDoerOwnershipReason(state, task, doerID, pr)
}

func clearDoerClaimFields(task *models.Task) {
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.Worktree = nil
	task.BaseCommit = nil
	task.Iteration = 0
}
