package ops

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// SupersedeResult contains the outcome of superseding a task.
type SupersedeResult struct {
	models.LifecycleOutcome
	TaskID         string            `json:"task_id"`
	OriginalStatus models.TaskStatus `json:"original_status"`
	ReplacementIDs []string          `json:"replacement_ids"`
	Warnings       []string          `json:"warnings"`
}

// SupersedeTaskOptions configures supersession behavior.
type SupersedeTaskOptions struct {
	Request LifecycleRequestOptions
	// RecoverabilityCommand records the operator-provided audit command for
	// unreplaced supersession. Liza records the command but does not execute it.
	RecoverabilityCommand string
}

// SupersedeTask transitions an initial, rejected, or BLOCKED task to SUPERSEDED
// with replacement task IDs. No-replacement supersession requires
// SupersedeTaskWithOptions with RecoverabilityCommand because the task's branch
// is deleted immediately (no successors to trigger cleanup). No terminal I/O.
func SupersedeTask(projectRoot, taskID string, replacementIDs []string, reason, agentID string) (*SupersedeResult, error) {
	return SupersedeTaskWithOptions(projectRoot, taskID, replacementIDs, reason, agentID, SupersedeTaskOptions{})
}

// SupersedeTaskWithOptions transitions an initial, rejected, or BLOCKED task to
// SUPERSEDED with explicit options for destructive no-replacement cleanup.
func SupersedeTaskWithOptions(projectRoot, taskID string, replacementIDs []string, reason, agentID string, opts SupersedeTaskOptions) (*SupersedeResult, error) {
	return supersedeTaskWithOptionalAuthority(projectRoot, taskID, replacementIDs, reason, agentID, opts, nil)
}

// SupersedeTaskWithAuthority fences the supersession transaction with the
// orchestrator's registration generation.
func SupersedeTaskWithAuthority(projectRoot, taskID string, replacementIDs []string, reason string, authority models.AgentAuthority, opts SupersedeTaskOptions) (*SupersedeResult, error) {
	return supersedeTaskWithOptionalAuthority(projectRoot, taskID, replacementIDs, reason, authority.ID, opts, &authority)
}

func supersedeTaskWithOptionalAuthority(projectRoot, taskID string, replacementIDs []string, reason, agentID string, opts SupersedeTaskOptions, authority *models.AgentAuthority) (result *SupersedeResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var observed *models.Task
	defer func() {
		retErr = WrapLifecycleError("supersede-task", observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("supersede-task", outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts.Request); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "rescope reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	recoverabilityCommand := strings.TrimSpace(opts.RecoverabilityCommand)
	if len(replacementIDs) == 0 {
		if recoverabilityCommand == "" {
			return nil, &PreconditionError{Reason: "recoverability command is required when superseding without replacements"}
		}
		if strings.ContainsAny(recoverabilityCommand, "\r\n") {
			return nil, &PreconditionError{Reason: "recoverability command must be a single line"}
		}
		recoverabilityCommand = secretmask.New().MaskText(recoverabilityCommand)
	} else if recoverabilityCommand != "" {
		return nil, &PreconditionError{Reason: "recoverability command is only valid when superseding without replacements"}
	}
	retErr = withOwnershipTaskLock(projectRoot, taskID, "supersede-task", func() error {
		var err error
		result, err = supersedeTaskLifecycle(projectRoot, taskID, replacementIDs, reason, agentID, recoverabilityCommand, opts, authority, &observed)
		return err
	})
	return result, retErr
}

func supersedeTaskLifecycle(projectRoot, taskID string, replacementIDs []string, reason, agentID, recoverabilityCommand string, opts SupersedeTaskOptions, authority *models.AgentAuthority, observed **models.Task) (*SupersedeResult, error) {
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	gw := git.New(projectRoot)

	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	// Phase 1: Read and Validate (no lock held)
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
	}
	*observed = task
	request, err := NewLifecycleRequest("supersede-task", task, agentID, authority, opts.Request, struct {
		Replacements                  []string
		Reason, RecoverabilityCommand string
	}{replacementIDs, reason, recoverabilityCommand})
	if err != nil {
		return nil, err
	}
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		return &SupersedeResult{LifecycleOutcome: LifecycleReplayOutcome(task, receipt, agentID), TaskID: taskID, OriginalStatus: receipt.Projection.SourceStatus, ReplacementIDs: replacementIDs}, nil
	}

	originalStatus := task.Status
	var outcome models.LifecycleOutcome

	// Supersede is allowed from: any initial (DRAFT_*) state, any rejected
	// state, BLOCKED, or INTEGRATION_FAILED when external repair/replacement
	// made the failed task obsolete.
	allowed := originalStatus == models.TaskStatusBlocked || originalStatus == models.TaskStatusIntegrationFailed
	if !allowed && task.RolePair != "" {
		// Pipeline-aware path: resolve initial/rejected from the task's role-pair.
		initialStatus, err := pb.resolver.InitialStatus(task.RolePair)
		if err == nil && originalStatus == initialStatus {
			allowed = true
		}
		if !allowed {
			rejectedStatus, err := pb.resolver.RejectedStatus(task.RolePair)
			if err == nil && originalStatus == rejectedStatus {
				allowed = true
			}
		}
	}
	if !allowed && task.RolePair == "" {
		// Legacy fallback: tasks without a role-pair use hardcoded statuses.
		allowed = originalStatus == models.TaskStatusReady || originalStatus == models.TaskStatusRejected
	}
	if !allowed {
		return nil, WrapLifecycleError("supersede-task", task, &PreconditionError{Reason: fmt.Sprintf("cannot supersede task %s in status %s (must be initial, rejected, or BLOCKED)", taskID, originalStatus)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}

	var salvage map[string]any
	if len(replacementIDs) == 0 {
		salvage, err = collectSupersedeSalvageSnapshot(gw, task, originalStatus, recoverabilityCommand)
		if err != nil {
			return nil, err
		}
	}

	// Phase 2: Atomic State Update
	hadWorktree := task.Worktree != nil
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		currentTask := state.FindTask(taskID)
		if currentTask == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		*observed = currentTask
		receipt, err := checkOwnerEndingRequest(currentTask, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			outcome = LifecycleReplayOutcome(currentTask, receipt, agentID)
			originalStatus = receipt.Projection.SourceStatus
			return errLifecycleReplay
		}

		if currentTask.Status != originalStatus {
			return WrapLifecycleError("supersede-task", currentTask, fmt.Errorf("task status changed before supersession"), models.LifecycleStateChanged, "requery", "none")
		}

		if _, err := supersedeTaskInState(state, pb, currentTask, replacementIDs, reason, agentID, salvage, time.Now().UTC()); err != nil {
			return err
		}
		// Legacy tasks predate pipeline-wide validation. For pipeline tasks, validate
		// every structural invariant while skipping unchanged external artifact refs.
		if currentTask.RolePair != "" {
			if err := statevalidate.ValidateState(state, projectRoot, true, io.Discard); err != nil {
				return err
			}
		}

		outcome, err = CompleteLifecycleRequest(currentTask, request, models.LifecycleProjection{SourceStatus: originalStatus}, state.Agents)
		return err
	})
	if isLifecycleReplay(err) {
		return &SupersedeResult{LifecycleOutcome: outcome, TaskID: taskID, OriginalStatus: originalStatus, ReplacementIDs: replacementIDs}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to supersede task: %w", err)
	}

	// Best-effort worktree cleanup (after state commit — safe to lose worktree now).
	var warnings []string
	if hadWorktree {
		if rmErr := gw.RemoveWorktreeDir(taskID); rmErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
		}
	}

	// When there are no successors, delete the branch immediately — no successor
	// will ever trigger cleanup via cleanupPredecessorBranches.
	// When successors exist, branch is preserved for git show access.
	if len(replacementIDs) == 0 {
		branchName := paths.TaskBranchPrefix + taskID
		exists, err := gw.BranchExists(branchName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", branchName, err))
		} else if exists {
			if err := gw.DeleteBranch(branchName); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", branchName, err))
			}
		}
	}

	// This superseded task may itself be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	warnings = append(warnings, cleanupPredecessorBranches(bb, gw, taskID)...)

	return &SupersedeResult{
		LifecycleOutcome: outcome,
		TaskID:           taskID,
		OriginalStatus:   originalStatus,
		ReplacementIDs:   replacementIDs,
		Warnings:         warnings,
	}, nil
}

// supersedeTaskInState retires one task inside an already-locked candidate
// state: dependency-direction validation, downstream pruning, the SUPERSEDED
// transition, ownership release, the audit entry and consumer rewriting. It
// returns the dependency edges pruned as illegal downstream links, so a
// composing transaction can record them. Full-state validation and the
// lifecycle receipt stay with the caller.
func supersedeTaskInState(state *models.State, pb *pipelineBundle, task *models.Task, replacementIDs []string, reason, agentID string, salvage map[string]any, now time.Time) ([]string, error) {
	if err := validateDependencyDirection(state, pb.resolver, task.ID, task.RolePair, replacementIDs); err != nil {
		return nil, err
	}
	retainedDependencies, removedDependencies, err := pruneDownstreamDependencies(state, pb.resolver, task)
	if err != nil {
		return nil, err
	}
	task.DependsOn = retainedDependencies
	models.AdvanceLifecycle(task)

	if err := task.TransitionWith(models.TaskStatusSuperseded, pb.transitions); err != nil {
		return nil, err
	}
	task.SupersededBy = replacementIDs
	task.RescopeReason = &reason

	releaseAgentsForTask(state, task.ID)
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
	task.Worktree = nil
	clearAttemptState(task, attemptStateRetire)

	var note string
	if len(replacementIDs) > 0 {
		note = fmt.Sprintf("replaced by: %s", strings.Join(replacementIDs, ", "))
	} else {
		note = "superseded without replacements"
	}
	historyEntry := models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventSuperseded,
		Agent:  &agentID,
		Reason: &reason,
		Note:   &note,
	}
	if salvage != nil {
		historyEntry.Extra = salvage
	}
	if len(removedDependencies) > 0 {
		if historyEntry.Extra == nil {
			historyEntry.Extra = make(map[string]any)
		}
		historyEntry.Extra["removed_dependencies"] = append([]string(nil), removedDependencies...)
	}
	task.History = append(task.History, historyEntry)

	if err := rewriteActiveDependents(state, pb.resolver, task.ID, replacementIDs, agentID, now); err != nil {
		return nil, err
	}
	return removedDependencies, nil
}

func collectSupersedeSalvageSnapshot(gw *git.Git, task *models.Task, originalStatus models.TaskStatus, recoverabilityCommand string) (map[string]any, error) {
	branchName := paths.TaskBranchPrefix + task.ID
	snapshot := map[string]any{
		"recoverability_command": recoverabilityCommand,
		"pre_supersession": map[string]any{
			"status":        string(originalStatus),
			"branch":        branchName,
			"worktree":      nil,
			"worktree_path": gw.GetWorktreePath(task.ID),
			"base_commit":   nil,
		},
	}
	pre := snapshot["pre_supersession"].(map[string]any)
	if task.Worktree != nil {
		pre["worktree"] = *task.Worktree
	}
	if task.BaseCommit != nil {
		pre["base_commit"] = *task.BaseCommit
	}

	branchExists, err := gw.BranchExists(branchName)
	if err != nil {
		return nil, fmt.Errorf("pre-supersession salvage branch check: %w", err)
	}
	pre["branch_exists"] = branchExists
	if branchExists {
		branchHead, err := gw.GetCommitSHA(branchName)
		if err != nil {
			return nil, fmt.Errorf("pre-supersession salvage branch HEAD: %w", err)
		}
		pre["branch_head"] = branchHead
	}

	worktreePath := gw.GetWorktreePath(task.ID)
	_, statErr := os.Stat(worktreePath)
	worktreeExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("pre-supersession salvage worktree check: %w", statErr)
	}
	pre["worktree_exists"] = worktreeExists
	if worktreeExists {
		worktreeHead, err := gw.GetWorktreeHEAD(task.ID)
		if err != nil {
			return nil, fmt.Errorf("pre-supersession salvage worktree HEAD: %w", err)
		}
		worktreeStatus, err := gw.WorktreeStatusShort(worktreePath)
		if err != nil {
			return nil, fmt.Errorf("pre-supersession salvage worktree status: %w", err)
		}
		pre["worktree_head"] = worktreeHead
		pre["worktree_status"] = worktreeStatus
	}

	return snapshot, nil
}
