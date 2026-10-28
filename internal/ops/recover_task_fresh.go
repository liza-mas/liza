package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
)

type recoverTaskFreshOutcome struct {
	TargetStatus models.TaskStatus
	Cleanup      attemptStateCleanupProfile
	ClearBlocked bool
}

func recoverTaskFreshReset(bb *db.Blackboard, gitWrapper *git.Git, taskID, reason string, statuses recoverTaskStatusSet, artifact recoverTaskArtifactSnapshot, snapshot recoverTaskSnapshot, result *RecoverTaskResult, invocation *ownershipInvocation) (*RecoverTaskResult, error) {
	outcome, err := recoverTaskFreshOutcomeForStatus(snapshot.status, statuses)
	if err != nil {
		return nil, err
	}
	if testRecoverTaskHooks != nil && testRecoverTaskHooks.beforeFreshModify != nil {
		testRecoverTaskHooks.beforeFreshModify()
	}

	baseCommit := ""
	var freshCreateErr error
	now := time.Now().UTC()
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task %s not found in state", taskID)
		}
		invocation.observe(task)
		if err := snapshot.validate(taskID, task); err != nil {
			return err
		}
		if err := ValidateLifecyclePreparation(task, invocation.request); err != nil {
			return err
		}
		if _, err := gitWrapper.GetCommitSHA(state.Config.IntegrationBranch); err != nil {
			return fmt.Errorf("fresh reset preflight failed for integration branch %s: %w", state.Config.IntegrationBranch, err)
		}

		if err := recoverTaskCleanupGitArtifacts(gitWrapper, taskID, artifact, result); err != nil {
			return fmt.Errorf("fresh reset cleanup failed; task state unchanged: %w", err)
		}
		if testRecoverTaskHooks != nil && testRecoverTaskHooks.beforeFreshCreate != nil {
			testRecoverTaskHooks.beforeFreshCreate()
		}
		createdBaseCommit, err := gitWrapper.CreateWorktree(taskID, state.Config.IntegrationBranch)
		if err != nil {
			freshCreateErr = err
			recoverTaskMarkFreshCreationFailure(state, task, snapshot, outcome, reason, now, err, result)
			return nil
		}
		baseCommit = createdBaseCommit
		result.WorktreeCreated = true
		result.FreshReset = true

		agentsToRecover := snapshot.agentsToRecover()
		releaseRecoverTaskSnapshotAgents(state, agentsToRecover, result)

		task.Status = outcome.TargetStatus
		task.AssignedTo = nil
		task.LeaseExpires = nil
		task.ReviewingBy = nil
		task.ReviewLeaseExpires = nil
		task.RejectionReason = nil
		if outcome.ClearBlocked {
			task.BlockedReason = nil
			task.BlockedQuestions = nil
		}
		task.RepairRequest = nil
		task.Worktree = &artifact.WorktreeRel
		task.BaseCommit = &baseCommit
		task.IntegrationFix = false
		clearAttemptState(task, outcome.Cleanup)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventRecoveredFresh,
			Reason: &reason,
			Extra: map[string]any{
				"target_status": string(outcome.TargetStatus),
				"base_commit":   baseCommit,
			},
		})
		state.HumanNotes = append(state.HumanNotes, recoverTaskHumanNote(taskID, reason, agentsToRecover, now))
		if testRecoverTaskHooks != nil && testRecoverTaskHooks.beforeFreshComplete != nil {
			testRecoverTaskHooks.beforeFreshComplete(task)
		}
		var completeErr error
		invocation.outcome, completeErr = CompleteLifecycleRequest(task, invocation.request, recoverTaskProjection(task, snapshot), state.Agents)
		result.LifecycleOutcome = invocation.outcome
		return completeErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to record fresh recovery: %w", err)
	}
	if freshCreateErr != nil {
		return nil, fmt.Errorf("fresh worktree creation failed after git cleanup; task marked BLOCKED for repair: %w", freshCreateErr)
	}
	return result, nil
}

func releaseRecoverTaskSnapshotAgents(state *models.State, agentsToRecover map[string]bool, result *RecoverTaskResult) {
	if len(agentsToRecover) > 0 {
		result.ClaimReleased = true
	}
	for agentID := range agentsToRecover {
		if _, exists := state.Agents[agentID]; exists {
			state.ReleaseAgent(agentID)
			delete(state.Agents, agentID)
			result.AgentRecovered = true
		}
	}
}

func recoverTaskMarkFreshCreationFailure(state *models.State, task *models.Task, snapshot recoverTaskSnapshot, outcome recoverTaskFreshOutcome, reason string, now time.Time, createErr error, result *RecoverTaskResult) {
	models.AdvanceLifecycle(task)
	agentsToRecover := snapshot.agentsToRecover()
	releaseRecoverTaskSnapshotAgents(state, agentsToRecover, result)

	previousBlockedQuestions := append([]string(nil), task.BlockedQuestions...)
	previousBlockedReason := ""
	if task.BlockedReason != nil {
		previousBlockedReason = *task.BlockedReason
	}

	task.Status = models.TaskStatusBlocked
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	task.RejectionReason = nil
	task.RepairRequest = nil
	task.Worktree = nil
	task.BaseCommit = nil
	task.IntegrationFix = false
	clearAttemptState(task, outcome.Cleanup)

	blockedReason := fmt.Sprintf("recover-task --fresh failed after deleting git artifacts: %v", createErr)
	if previousBlockedReason != "" {
		blockedReason = fmt.Sprintf("%s; previous blocked_reason: %s", blockedReason, previousBlockedReason)
	}
	task.BlockedReason = &blockedReason
	task.BlockedQuestions = append([]string{
		fmt.Sprintf("Repair task %s git artifacts manually or supersede it; original task branch/worktree were removed during failed fresh recovery.", task.ID),
	}, previousBlockedQuestions...)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventRecoveryFreshFailed,
		Reason: &reason,
		Extra: map[string]any{
			"error":            createErr.Error(),
			"attempted_status": string(outcome.TargetStatus),
		},
	})
	state.HumanNotes = append(state.HumanNotes, recoverTaskHumanNote(task.ID, reason, agentsToRecover, now))
}

func recoverTaskFreshOutcomeForStatus(status models.TaskStatus, statuses recoverTaskStatusSet) (recoverTaskFreshOutcome, error) {
	if status == models.TaskStatusBlocked {
		return recoverTaskFreshOutcome{
			TargetStatus: models.TaskStatusBlocked,
			Cleanup:      attemptStateRetire,
			ClearBlocked: false,
		}, nil
	}
	if isRecoverTaskFreshResetToInitialStatus(status, statuses) {
		return recoverTaskFreshOutcome{
			TargetStatus: statuses.Initial,
			Cleanup:      attemptStateInitialReset,
			ClearBlocked: true,
		}, nil
	}
	return recoverTaskFreshOutcome{}, fmt.Errorf("task status %s is not supported by --fresh recover-task", status)
}

func isRecoverTaskFreshResetToInitialStatus(status models.TaskStatus, statuses recoverTaskStatusSet) bool {
	return status == statuses.Initial ||
		status == statuses.Executing ||
		status == statuses.Submitted ||
		status == statuses.Reviewing ||
		status == statuses.Rejected ||
		status == statuses.Approved ||
		status == models.TaskStatusIntegrationFailed ||
		(statuses.PartiallyApproved != "" && status == statuses.PartiallyApproved) ||
		(statuses.Reviewing2 != "" && status == statuses.Reviewing2)
}
