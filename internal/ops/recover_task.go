package ops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// RecoverTaskResult contains the outcome of recovering a task.
type RecoverTaskResult struct {
	models.LifecycleOutcome
	TaskID            string
	InState           bool   // true if task was found in state
	AgentID           string // primary agent that held the claim, if any
	AgentRole         string
	ClaimReleased     bool
	WorktreeRemoved   bool
	BranchRemoved     bool
	WorktreeRecovered bool
	WorktreeCreated   bool
	FreshReset        bool
	PreservedWorktree bool
	AgentRecovered    bool
	Warnings          []string
}

// RecoverTaskOptions configures task recovery.
type RecoverTaskOptions struct {
	RequestOptions LifecycleRequestOptions
	// Force bypasses live-PID checks. For tasks absent from state, it also enables
	// git-only artifact cleanup.
	Force bool
	// Fresh explicitly discards the task branch/worktree and resets an in-state
	// non-terminal task to its role-pair initial status.
	Fresh bool
}

type recoverTaskArtifactSnapshot struct {
	WorktreePath string
	WorktreeRel  string
	BranchName   string
	Worktree     bool
	Branch       bool
	BaseCommit   string
}

type recoverTaskStatusSet struct {
	Initial           models.TaskStatus
	Executing         models.TaskStatus
	Submitted         models.TaskStatus
	Reviewing         models.TaskStatus
	Rejected          models.TaskStatus
	Approved          models.TaskStatus
	PartiallyApproved models.TaskStatus
	Reviewing2        models.TaskStatus
}

type recoverTaskFreshOutcome struct {
	TargetStatus models.TaskStatus
	Cleanup      attemptStateCleanupProfile
	ClearBlocked bool
}

type recoverTaskSnapshot struct {
	status                models.TaskStatus
	assignedTo            string
	hasAssignedTo         bool
	leaseExpires          time.Time
	hasLeaseExpires       bool
	reviewingBy           string
	hasReviewingBy        bool
	reviewLeaseExpires    time.Time
	hasReviewLeaseExpires bool
	worktree              string
	hasWorktree           bool
	baseCommit            string
	hasBaseCommit         bool
	reviewCommit          string
	hasReviewCommit       bool
}

type recoverTaskTestHooks struct {
	beforePreserveModify func()
	beforeFreshModify    func()
	cleanupGitArtifacts  func() error
	beforeFreshCreate    func()
}

var testRecoverTaskHooks *recoverTaskTestHooks

// RecoverTask performs task recovery. In-state tasks preserve recoverable work by
// default. Destructive branch/worktree reset requires RecoverTaskOptions.Fresh.
//
// Without force: requires the task to exist in state, and refuses if any claiming
// agent's PID is still alive.
//
// With force: cleans up git artifacts (worktree + branch) even if the task is not
// in state. For in-state tasks, force only bypasses PID liveness checks.
//
// Idempotent: safe to run multiple times. No terminal I/O.
func RecoverTask(projectRoot, taskID string, force bool, reason string) (*RecoverTaskResult, error) {
	return RecoverTaskWithOptions(projectRoot, taskID, reason, RecoverTaskOptions{Force: force})
}

// RecoverTaskWithOptions performs task recovery with explicit reset options.
func RecoverTaskWithOptions(projectRoot, taskID string, reason string, opts RecoverTaskOptions) (result *RecoverTaskResult, err error) {
	invocation := &ownershipInvocation{operation: "recover-task", opts: opts.RequestOptions, metrics: NewLifecycleInvocation(projectRoot)}
	defer func() { err = invocation.finish(projectRoot, err) }()
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID required"}
	}
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid task ID: %v", err)}
	}
	if err := ValidateLifecycleRequestOptions(opts.RequestOptions); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	err = withOwnershipTaskLock(projectRoot, taskID, "task-recover-worktree", func() error {
		var recoverErr error
		result, recoverErr = recoverTaskWithOptions(projectRoot, taskID, reason, opts, invocation)
		return recoverErr
	})
	return result, err
}

func recoverTaskWithOptions(projectRoot, taskID string, reason string, opts RecoverTaskOptions, invocation *ownershipInvocation) (*RecoverTaskResult, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID required")
	}
	if err := paths.ValidateTaskID(taskID); err != nil {
		return nil, fmt.Errorf("invalid task ID: %w", err)
	}
	if reason == "" {
		reason = "task recovery"
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	gitWrapper := git.New(projectRoot)
	result := &RecoverTaskResult{TaskID: taskID}

	state, err := bb.Read()
	if err != nil && !opts.Force {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var task *models.Task
	if err == nil && state != nil {
		task = state.FindTask(taskID)
	}
	if task != nil {
		result.InState = true
		invocation.observe(task)
		request, err := NewLifecycleRequest("recover-task", task, "human", nil, opts.RequestOptions, struct {
			Force, Fresh bool
			Reason       string
		}{opts.Force, opts.Fresh, reason})
		if err != nil {
			return nil, err
		}
		invocation.request = request
		receipt, err := checkOwnerEndingRequest(task, request)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			invocation.outcome = LifecycleReplayOutcome(task, receipt, "human")
			result.LifecycleOutcome = invocation.outcome
			result.ClaimReleased = receipt.Projection.ReleasedDoer || receipt.Projection.ReleasedReviewer
			result.FreshReset = opts.Fresh
			result.PreservedWorktree = !opts.Fresh && receipt.Projection.BaseCommit != ""
			return result, nil
		}
		if err := recoverTaskCheckLiveClaims(taskID, task, state, opts.Force, opts.Fresh, result); err != nil {
			return nil, err
		}
	} else if !opts.Force {
		return nil, fmt.Errorf("task %s not found in state, use --force to clean up git artifacts anyway", taskID)
	}

	artifact := inspectRecoverTaskArtifacts(gitWrapper, taskID, result)
	if !result.InState {
		if opts.RequestOptions.RequestID != "" {
			return nil, &PreconditionError{Reason: "cannot bind a recovery request to an absent task"}
		}
		invocation.effects = true
		if err := recoverTaskCleanupGitArtifacts(gitWrapper, taskID, artifact, result); err != nil {
			return result, fmt.Errorf("cleanup git artifacts: %w", err)
		}
		invocation.outcome = NewLifecycleOutcome("recover-task", nil, models.LifecycleCompleted, "continue", "committed")
		invocation.outcome.TaskID = taskID
		result.LifecycleOutcome = invocation.outcome
		return result, nil
	}
	if task.Status.IsTerminal() {
		return nil, fmt.Errorf("task %s is terminal (%s); recover-task only operates on non-terminal in-state tasks", taskID, task.Status)
	}

	resolver, _, resolverErr := loadResolver(projectRoot)
	if resolverErr != nil {
		return nil, fmt.Errorf("pipeline config: %w", resolverErr)
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)
	statuses := recoverTaskStatuses(task, resolver)
	if statuses.Initial == "" {
		return nil, fmt.Errorf("task %s role_pair %q has no initial status", taskID, task.RolePair)
	}
	snapshot := newRecoverTaskSnapshot(task)
	var preparation models.LifecyclePreparation
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task disappeared before recovery preparation")
		}
		if err := snapshot.validate(taskID, task); err != nil {
			return err
		}
		if err := recoverTaskCheckLiveClaims(taskID, task, state, opts.Force, opts.Fresh, result); err != nil {
			return err
		}
		if err := prepareOwnerEndingRequest(task, invocation.request); err != nil {
			return err
		}
		preparation = *task.Lifecycle.Preparation
		return nil
	}); err != nil {
		return nil, err
	}
	invocation.effects = true

	if opts.Fresh {
		return recoverTaskFreshReset(bb, gitWrapper, taskID, reason, statuses, artifact, snapshot, result, invocation)
	}

	// Without an attach, every preserve refusal is a read-only inspection, so the
	// refused request retires its own reservation instead of leaving a marker no
	// agent can retire (D60). An attach, even a failed one, may have changed Git
	// state and keeps the fence.
	attachAttempted := !artifact.Worktree && artifact.Branch
	preserve, err := recoverTaskPreserveArtifacts(gitWrapper, task, statuses, &artifact, result)
	if err != nil {
		if attachAttempted {
			return nil, err
		}
		invocation.effects = false
		return nil, retireFailedLifecyclePreparation(bb.Patient(), taskID, nil, &preparation, err, "none")
	}
	if testRecoverTaskHooks != nil && testRecoverTaskHooks.beforePreserveModify != nil {
		testRecoverTaskHooks.beforePreserveModify()
	}

	now := time.Now().UTC()
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return fmt.Errorf("task disappeared before recovery completion")
		}
		invocation.observe(task)
		if err := snapshot.validate(taskID, task); err != nil {
			return err
		}
		if err := ValidateLifecyclePreparation(task, invocation.request); err != nil {
			return err
		}

		agentsToRecover := snapshot.agentsToRecover()
		claimReleaseFailed := recoverTaskReleaseClaims(state, task, resolver, pipelineTransitions, statuses, reason, now, agentsToRecover, result)

		if preserve {
			task.Worktree = &artifact.WorktreeRel
			if artifact.BaseCommit != "" {
				task.BaseCommit = &artifact.BaseCommit
			}
			result.PreservedWorktree = true
		} else if !claimReleaseFailed {
			task.Worktree = nil
			task.BaseCommit = nil
		}

		if recoverTaskNeedsReviewCommitReset(task, statuses) {
			task.Status = statuses.Initial
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			clearAttemptState(task, attemptStateInitialReset)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("reset %s to %s: missing review_commit", taskID, statuses.Initial))
		}

		for agentID := range agentsToRecover {
			if _, exists := state.Agents[agentID]; exists {
				delete(state.Agents, agentID)
				result.AgentRecovered = true
			}
		}
		state.HumanNotes = append(state.HumanNotes, recoverTaskHumanNote(taskID, reason, agentsToRecover, now))
		var completeErr error
		invocation.outcome, completeErr = CompleteLifecycleRequest(task, invocation.request, recoverTaskProjection(task, snapshot), state.Agents)
		result.LifecycleOutcome = invocation.outcome
		return completeErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to recover task: %w", err)
	}

	return result, nil
}

func newRecoverTaskSnapshot(task *models.Task) recoverTaskSnapshot {
	snapshot := recoverTaskSnapshot{status: task.Status}
	snapshot.assignedTo, snapshot.hasAssignedTo = optionalString(task.AssignedTo)
	snapshot.leaseExpires, snapshot.hasLeaseExpires = optionalTime(task.LeaseExpires)
	snapshot.reviewingBy, snapshot.hasReviewingBy = optionalString(task.ReviewingBy)
	snapshot.reviewLeaseExpires, snapshot.hasReviewLeaseExpires = optionalTime(task.ReviewLeaseExpires)
	snapshot.worktree, snapshot.hasWorktree = optionalString(task.Worktree)
	snapshot.baseCommit, snapshot.hasBaseCommit = optionalString(task.BaseCommit)
	snapshot.reviewCommit, snapshot.hasReviewCommit = optionalString(task.ReviewCommit)
	return snapshot
}

func optionalString(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	return *value, true
}

func optionalTime(value *time.Time) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	return value.UTC(), true
}

func (s recoverTaskSnapshot) validate(taskID string, task *models.Task) error {
	if task == nil {
		return fmt.Errorf("task %s disappeared during recover-task", taskID)
	}
	if task.Status != s.status {
		return fmt.Errorf("race condition: task %s status changed from %s to %s during recover-task", taskID, s.status, task.Status)
	}
	if err := validateSnapshotString(taskID, "assigned_to", s.assignedTo, s.hasAssignedTo, task.AssignedTo); err != nil {
		return err
	}
	if err := validateSnapshotTime(taskID, "lease_expires", s.leaseExpires, s.hasLeaseExpires, task.LeaseExpires); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "reviewing_by", s.reviewingBy, s.hasReviewingBy, task.ReviewingBy); err != nil {
		return err
	}
	if err := validateSnapshotTime(taskID, "review_lease_expires", s.reviewLeaseExpires, s.hasReviewLeaseExpires, task.ReviewLeaseExpires); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "worktree", s.worktree, s.hasWorktree, task.Worktree); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "base_commit", s.baseCommit, s.hasBaseCommit, task.BaseCommit); err != nil {
		return err
	}
	if err := validateSnapshotString(taskID, "review_commit", s.reviewCommit, s.hasReviewCommit, task.ReviewCommit); err != nil {
		return err
	}
	return nil
}

func validateSnapshotString(taskID, field, expected string, expectedSet bool, current *string) error {
	currentValue, currentSet := optionalString(current)
	if expectedSet == currentSet && expected == currentValue {
		return nil
	}
	return fmt.Errorf("race condition: task %s %s changed during recover-task", taskID, field)
}

func validateSnapshotTime(taskID, field string, expected time.Time, expectedSet bool, current *time.Time) error {
	currentValue, currentSet := optionalTime(current)
	if expectedSet == currentSet && (!expectedSet || expected.Equal(currentValue)) {
		return nil
	}
	return fmt.Errorf("race condition: task %s %s changed during recover-task", taskID, field)
}

func (s recoverTaskSnapshot) agentsToRecover() map[string]bool {
	agents := map[string]bool{}
	if s.hasAssignedTo {
		agents[s.assignedTo] = true
	}
	if s.hasReviewingBy {
		agents[s.reviewingBy] = true
	}
	return agents
}

func recoverTaskCheckLiveClaims(taskID string, task *models.Task, state *models.State, force bool, fresh bool, result *RecoverTaskResult) error {
	var coderAgentID string
	var reviewerAgentID string
	forceHint := "--force"
	if fresh {
		forceHint = "--fresh --force"
	}
	if task.AssignedTo != nil {
		coderAgentID = *task.AssignedTo
		if agent, exists := state.Agents[coderAgentID]; exists {
			if !force && agent.PID != 0 && IsProcessAlive(agent.PID) {
				return fmt.Errorf("task %s: coder agent %s (PID %d) still running, use %s to recover",
					taskID, coderAgentID, agent.PID, forceHint)
			}
		}
	}
	if task.ReviewingBy != nil {
		reviewerAgentID = *task.ReviewingBy
		if agent, exists := state.Agents[reviewerAgentID]; exists {
			if !force && agent.PID != 0 && IsProcessAlive(agent.PID) {
				return fmt.Errorf("task %s: reviewer agent %s (PID %d) still running, use %s to recover",
					taskID, reviewerAgentID, agent.PID, forceHint)
			}
		}
	}
	if coderAgentID != "" {
		result.AgentID = coderAgentID
		result.AgentRole = "coder"
	} else if reviewerAgentID != "" {
		result.AgentID = reviewerAgentID
		result.AgentRole = "code-reviewer"
	}
	return nil
}

func inspectRecoverTaskArtifacts(gitWrapper *git.Git, taskID string, result *RecoverTaskResult) recoverTaskArtifactSnapshot {
	wtPath := gitWrapper.GetWorktreePath(taskID)
	branchName := paths.TaskBranchPrefix + taskID
	artifact := recoverTaskArtifactSnapshot{
		WorktreePath: wtPath,
		WorktreeRel:  fmt.Sprintf("%s/%s", paths.WorktreesDirName, taskID),
		BranchName:   branchName,
	}
	if _, statErr := os.Stat(wtPath); statErr == nil {
		artifact.Worktree = true
	}
	branchExisted, branchErr := gitWrapper.BranchExists(branchName)
	if branchErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("branch existence check: %v", branchErr))
	}
	artifact.Branch = branchExisted
	return artifact
}

func recoverTaskCleanupGitArtifacts(gitWrapper *git.Git, taskID string, artifact recoverTaskArtifactSnapshot, result *RecoverTaskResult) error {
	if artifact.Worktree || artifact.Branch {
		if testRecoverTaskHooks != nil && testRecoverTaskHooks.cleanupGitArtifacts != nil {
			if err := testRecoverTaskHooks.cleanupGitArtifacts(); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal: %v", err))
				return err
			}
		}
		if err := gitWrapper.RemoveWorktree(taskID); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("worktree removal: %v", err))
			return err
		}
		result.WorktreeRemoved = artifact.Worktree
		result.BranchRemoved = artifact.Branch
	}
	return nil
}

func recoverTaskStatuses(task *models.Task, resolver models.PipelineResolver) recoverTaskStatusSet {
	statuses := recoverTaskStatusSet{}
	statuses.Initial, _ = resolver.InitialStatus(task.RolePair)
	statuses.Executing, _ = resolver.ExecutingStatus(task.RolePair)
	statuses.Submitted, _ = resolver.SubmittedStatus(task.RolePair)
	statuses.Reviewing, _ = resolver.ReviewingStatus(task.RolePair)
	statuses.Rejected, _ = resolver.RejectedStatus(task.RolePair)
	statuses.Approved, _ = resolver.ApprovedStatus(task.RolePair)
	statuses.PartiallyApproved, _ = resolver.PartiallyApprovedStatus(task.RolePair)
	statuses.Reviewing2, _ = resolver.Reviewing2Status(task.RolePair)
	return statuses
}

func recoverTaskPreserveArtifacts(gitWrapper *git.Git, task *models.Task, statuses recoverTaskStatusSet, artifact *recoverTaskArtifactSnapshot, result *RecoverTaskResult) (bool, error) {
	if artifact.Worktree && !artifact.Branch {
		return false, fmt.Errorf("task %s: worktree exists but branch %s is missing; preserve recovery fails closed (use --fresh to discard)", task.ID, artifact.BranchName)
	}
	if !artifact.Worktree && !artifact.Branch {
		if task.Status == statuses.Executing {
			return false, nil
		}
		if isRecoverTaskReviewCandidate(task.Status, statuses) {
			return false, fmt.Errorf("task %s: submitted candidate is unrecoverable because worktree and branch are both missing", task.ID)
		}
		if task.Status == models.TaskStatusIntegrationFailed {
			return false, fmt.Errorf("task %s: integration-failed repair substrate is unrecoverable because worktree and branch are both missing", task.ID)
		}
		return false, nil
	}
	if !artifact.Worktree && artifact.Branch {
		if err := gitWrapper.AttachWorktree(task.ID, artifact.BranchName); err != nil {
			return false, fmt.Errorf("reattach worktree from %s: %w", artifact.BranchName, err)
		}
		result.WorktreeRecovered = true
		artifact.Worktree = true
	}
	if err := gitWrapper.ValidateWorktreeHealth(task.ID); err != nil {
		return false, fmt.Errorf("preserved worktree not healthy: %w", err)
	}
	status, err := gitWrapper.WorktreeStatusShort(artifact.WorktreePath)
	if err != nil {
		return false, fmt.Errorf("preserved worktree status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return false, fmt.Errorf("preserved worktree is dirty; recover-task refuses to redispatch ambiguous work (use --fresh to discard)")
	}
	baseCommit := ""
	if task.BaseCommit != nil {
		baseCommit = *task.BaseCommit
	}
	if baseCommit == "" {
		head, err := gitWrapper.GetWorktreeHEAD(task.ID)
		if err != nil {
			return false, err
		}
		baseCommit = head
	}
	artifact.BaseCommit = baseCommit
	if isRecoverTaskReviewCandidate(task.Status, statuses) {
		if task.ReviewCommit == nil || *task.ReviewCommit == "" {
			return false, fmt.Errorf("task %s has no review_commit; cannot preserve submitted review boundary", task.ID)
		}
		head, err := gitWrapper.GetWorktreeHEAD(task.ID)
		if err != nil {
			return false, err
		}
		if head != *task.ReviewCommit {
			return false, fmt.Errorf("review_commit %s does not match worktree HEAD %s; recover-task refuses to review a different candidate", *task.ReviewCommit, head)
		}
	}
	return true, nil
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

func recoverTaskReleaseClaims(state *models.State, task *models.Task, resolver models.PipelineResolver, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, statuses recoverTaskStatusSet, reason string, now time.Time, agentsToRecover map[string]bool, result *RecoverTaskResult) bool {
	claimReleaseFailed := false
	if task.AssignedTo != nil {
		currentCoderID := *task.AssignedTo
		if task.Status == models.TaskStatusBlocked || isRecoverTaskReviewCandidate(task.Status, statuses) || task.Status == models.TaskStatusIntegrationFailed {
			releaseRecoverTaskAgent(state, task, currentCoderID)
			result.ClaimReleased = true
		} else {
			effectiveCoderRelease := resolveDoerClaimReleaseStatus(task, resolver)
			released, err := releaseOneClaim(state, task, effectiveCoderRelease, pipelineTransitions, true, currentCoderID, reason, now)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("coder claim release: %v", err))
				delete(agentsToRecover, currentCoderID)
				claimReleaseFailed = true
			}
			if released {
				result.ClaimReleased = true
			}
		}
	}
	if task.ReviewingBy != nil {
		currentReviewerID := *task.ReviewingBy
		if task.Status == models.TaskStatusBlocked {
			releaseRecoverTaskReviewerAgent(state, task, currentReviewerID)
			result.ClaimReleased = true
			return claimReleaseFailed
		}
		effectiveReviewerRelease, err := resolveReviewerClaimReleaseStatus(task, resolver)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release: %v", err))
			delete(agentsToRecover, currentReviewerID)
			claimReleaseFailed = true
		} else {
			released, err := releaseOneClaim(state, task, effectiveReviewerRelease, pipelineTransitions, true, currentReviewerID, reason, now)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("reviewer claim release: %v", err))
				delete(agentsToRecover, currentReviewerID)
				claimReleaseFailed = true
			}
			if released {
				result.ClaimReleased = true
			}
		}
	}
	return claimReleaseFailed
}

func releaseRecoverTaskAgent(state *models.State, task *models.Task, agentID string) {
	models.AdvanceLifecycle(task)
	if agent, ok := state.Agents[agentID]; ok {
		if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
			state.ReleaseAgent(agentID)
		}
	}
	task.AssignedTo = nil
	task.LeaseExpires = nil
}

func releaseRecoverTaskReviewerAgent(state *models.State, task *models.Task, agentID string) {
	models.AdvanceLifecycle(task)
	if agent, ok := state.Agents[agentID]; ok {
		if agent.CurrentTask != nil && *agent.CurrentTask == task.ID {
			state.ReleaseAgent(agentID)
		}
	}
	task.ReviewingBy = nil
	task.ReviewLeaseExpires = nil
}

func recoverTaskNeedsReviewCommitReset(task *models.Task, statuses recoverTaskStatusSet) bool {
	return task.ReviewCommit == nil && isRecoverTaskReviewCandidate(task.Status, statuses)
}

func recoverTaskProjection(task *models.Task, snapshot recoverTaskSnapshot) models.LifecycleProjection {
	projection := models.LifecycleProjection{SourceStatus: snapshot.status, ReleasedDoer: snapshot.hasAssignedTo && task.AssignedTo == nil, ReleasedReviewer: snapshot.hasReviewingBy && task.ReviewingBy == nil}
	if task.BaseCommit != nil {
		projection.BaseCommit = *task.BaseCommit
	}
	return projection
}

func isRecoverTaskReviewCandidate(status models.TaskStatus, statuses recoverTaskStatusSet) bool {
	return status == statuses.Submitted ||
		status == statuses.Reviewing ||
		status == statuses.Approved ||
		(statuses.PartiallyApproved != "" && status == statuses.PartiallyApproved) ||
		(statuses.Reviewing2 != "" && status == statuses.Reviewing2)
}

func recoverTaskHumanNote(taskID, reason string, agentsToRecover map[string]bool, now time.Time) models.HumanNote {
	var recoveredAgents []string
	for agentID := range agentsToRecover {
		recoveredAgents = append(recoveredAgents, agentID)
	}
	msg := fmt.Sprintf("Task %s recovered: %s", taskID, reason)
	if len(recoveredAgents) == 1 {
		msg = fmt.Sprintf("Task %s recovered (was held by %s): %s", taskID, recoveredAgents[0], reason)
	} else if len(recoveredAgents) > 1 {
		msg = fmt.Sprintf("Task %s recovered (was held by %v): %s", taskID, recoveredAgents, reason)
	}
	note := models.HumanNote{
		Timestamp: now,
		Message:   msg,
		For:       taskID,
	}
	note.MarkSeenByOrchestrator(now) // audit record, not a request
	return note
}
