package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ReconcileMergedResult contains the outcome of externally merged task repair.
type ReconcileMergedResult struct {
	TaskID         string            `json:"task_id"`
	OriginalStatus models.TaskStatus `json:"original_status"`
	MergeCommit    string            `json:"merge_commit"`
	PRURL          string            `json:"pr_url,omitempty"`
	Warnings       []string          `json:"warnings"`
}

// ReconcileMerged marks an INTEGRATION_FAILED task as MERGED after an operator
// verifies the task was completed externally, such as by a GitHub PR merge.
// The merge commit must resolve locally so state records a concrete commit.
func ReconcileMerged(projectRoot, taskID, mergeCommit, prURL, reason, agentID string) (*ReconcileMergedResult, error) {
	return reconcileMergedWithOptionalAuthority(projectRoot, taskID, mergeCommit, prURL, reason, agentID, nil)
}

// ReconcileMergedWithAuthority fences reconciliation and its metrics follow-up
// with the orchestrator's registration generation.
func ReconcileMergedWithAuthority(projectRoot, taskID, mergeCommit, prURL, reason string, authority models.AgentAuthority) (*ReconcileMergedResult, error) {
	return reconcileMergedWithOptionalAuthority(projectRoot, taskID, mergeCommit, prURL, reason, authority.ID, &authority)
}

func reconcileMergedWithOptionalAuthority(projectRoot, taskID, mergeCommit, prURL, reason, agentID string, authority *models.AgentAuthority) (*ReconcileMergedResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if mergeCommit == "" {
		return nil, &PreconditionError{Reason: "merge commit is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reconciliation reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	gw := git.New(projectRoot)

	resolvedCommit, err := gw.GetCommitSHA(mergeCommit)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("merge commit %q could not be resolved locally; fetch the merged PR first", mergeCommit)}
	}

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	_, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	originalStatus := task.Status
	if originalStatus != models.TaskStatusIntegrationFailed {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot reconcile task %s in status %s (must be INTEGRATION_FAILED)", taskID, originalStatus)}
	}

	now := time.Now().UTC()
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		currentTask := state.FindTask(taskID)
		if currentTask == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if currentTask.Status != originalStatus {
			return &PreconditionError{Reason: fmt.Sprintf("cannot reconcile task %s: status changed from %s to %s", taskID, originalStatus, currentTask.Status)}
		}
		if err := currentTask.TransitionWith(models.TaskStatusMerged, pb.transitions); err != nil {
			return err
		}

		releaseAgentsForTask(state, taskID)
		currentTask.Worktree = nil
		currentTask.AssignedTo = nil
		currentTask.LeaseExpires = nil
		currentTask.ReviewingBy = nil
		currentTask.ReviewLeaseExpires = nil
		currentTask.MergeCommit = &resolvedCommit
		currentTask.IntegrationFailure = nil
		models.AdvanceLifecycle(currentTask)

		currentTask.HandoffEvents = append(currentTask.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     agentID,
			Trigger:   models.HandoffTriggerCompletion,
			Succeeded: []string{"external merge reconciled"},
			NextStep:  "task is complete",
		})

		extra := map[string]any{
			"external_reconciliation": true,
		}
		if prURL != "" {
			extra["pr_url"] = prURL
		}
		currentTask.History = append(currentTask.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventMerged,
			Agent:  &agentID,
			Reason: &reason,
			Commit: &resolvedCommit,
			Extra:  extra,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile task as MERGED: %w", err)
	}

	var warnings []string
	if rmErr := gw.RemoveWorktreeDir(taskID); rmErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
	}
	taskBranch := paths.TaskBranchPrefix + taskID
	if exists, err := gw.BranchExists(taskBranch); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", taskBranch, err))
	} else if exists {
		if err := gw.DeleteBranch(taskBranch); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, err))
		}
	}
	warnings = append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)
	var metricsErr error
	if authority == nil {
		_, metricsErr = UpdateSprintMetrics(projectRoot)
	} else {
		_, metricsErr = UpdateSprintMetricsWithAuthority(projectRoot, *authority)
	}
	if metricsErr != nil {
		warnings = append(warnings, fmt.Sprintf("failed to update sprint metrics: %v", metricsErr))
	}

	return &ReconcileMergedResult{
		TaskID:         taskID,
		OriginalStatus: originalStatus,
		MergeCommit:    resolvedCommit,
		PRURL:          prURL,
		Warnings:       warnings,
	}, nil
}
