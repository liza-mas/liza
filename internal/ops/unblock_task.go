package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

const integrationOperationUnblockTask = "unblock-task"

// UnblockTaskResult contains the outcome of restoring a repaired BLOCKED task.
type UnblockTaskResult struct {
	TaskID       string                   `json:"task_id"`
	FromStatus   models.TaskStatus        `json:"from_status"`
	ToStatus     models.TaskStatus        `json:"to_status"`
	AssignedTo   string                   `json:"assigned_to,omitempty"`
	Claimable    bool                     `json:"claimable"`
	LeaseExpires *time.Time               `json:"lease_expires,omitempty"`
	Rebase       *UnblockTaskRebaseResult `json:"rebase,omitempty"`
}

// UnblockTaskOptions configures unblock-task behavior.
type UnblockTaskOptions struct {
	AssignTo   string
	RebaseOn   string
	AllowDirty bool
}

// UnblockTaskRebaseResult contains the outcome of a successful unblock rebase.
type UnblockTaskRebaseResult struct {
	OldHead          string   `json:"old_head"`
	NewHead          string   `json:"new_head"`
	TargetRef        string   `json:"target_ref"`
	TargetSHA        string   `json:"target_sha"`
	Autostash        bool     `json:"autostash"`
	StatusShort      string   `json:"status_short,omitempty"`
	UntrackedBlocked []string `json:"untracked_blocked,omitempty"`
}

// UnblockRebaseConflictError indicates unblock-task could not rebase a
// preserved task worktree. The task remains BLOCKED with a fresh repair_request.
type UnblockRebaseConflictError struct {
	TaskID    string
	TargetRef string
	TargetSHA string
	Cause     error
}

func (e *UnblockRebaseConflictError) Error() string {
	return fmt.Sprintf("unblock rebase conflict: task %s remains BLOCKED with repair_request", e.TaskID)
}

func (e *UnblockRebaseConflictError) Unwrap() error {
	return e.Cause
}

func (e *UnblockRebaseConflictError) SafeDetails() map[string]any {
	return map[string]any{
		"task_id":        e.TaskID,
		"target_ref":     e.TargetRef,
		"target_sha":     e.TargetSHA,
		"task_status":    string(models.TaskStatusBlocked),
		"repair_request": true,
	}
}

type unblockRebaseSnapshot struct {
	TaskID       string
	WorktreeRel  string
	WorktreePath string
	Branch       string
	OldHead      string
	TargetRef    string
	TargetSHA    string
	StatusShort  string
	Autostash    bool
}

// UnblockTask restores a repaired BLOCKED task to its role-pair executing state
// and assigns it back to an existing doer so normal submit-for-review flow can
// continue. The orchestrator must run any repair_request.validation commands
// before calling this operation; this op records the prior repair request in
// history for audit but intentionally does not execute arbitrary command text.
func UnblockTask(projectRoot, taskID, assignTo, reason, agentID string) (*UnblockTaskResult, error) {
	return UnblockTaskWithOptions(projectRoot, taskID, reason, agentID, UnblockTaskOptions{AssignTo: assignTo})
}

// UnblockTaskWithOptions restores a repaired BLOCKED task to either its initial
// state or directly to its executing state when AssignTo is provided.
func UnblockTaskWithOptions(projectRoot, taskID, reason, agentID string, opts UnblockTaskOptions) (*UnblockTaskResult, error) {
	return unblockTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID, opts, nil)
}

// UnblockTaskWithAuthority fences the unblock transaction and conflict
// follow-up write with the orchestrator's registration generation.
func UnblockTaskWithAuthority(projectRoot, taskID, reason string, authority models.AgentAuthority, opts UnblockTaskOptions) (*UnblockTaskResult, error) {
	return unblockTaskWithOptionalAuthority(projectRoot, taskID, reason, authority.ID, opts, &authority)
}

func unblockTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID string, opts UnblockTaskOptions, authority *models.AgentAuthority) (*UnblockTaskResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if err := identity.ValidateRole(agentID, roles.Orchestrator); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can unblock tasks: %v", err)}
	}

	var assignRole string
	if opts.AssignTo != "" {
		var err error
		assignRole, err = identity.ExtractRole(opts.AssignTo)
		if err != nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("invalid assign-to agent ID %s: %v", opts.AssignTo, err)}
		}
	}

	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	if opts.AssignTo != "" {
		_, task, err := readTaskState(bb, taskID)
		if err != nil {
			return nil, err
		}
		if len(task.ValidationPrerequisites) > 0 {
			return nil, &PreconditionError{Reason: "validation preflight requires the target session; unblock without --assign-to and let its supervisor claim the task"}
		}
	}

	rebaseResult, err := maybeRebaseTaskBeforeUnblock(bb, lp.ProjectRoot(), taskID, agentID, opts, authority)
	if err != nil {
		return nil, err
	}

	var result UnblockTaskResult
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if opts.AssignTo != "" && len(task.ValidationPrerequisites) > 0 {
			return validationError("target_session_required")
		}
		if task.Status != models.TaskStatusBlocked {
			return &PreconditionError{Reason: fmt.Sprintf("task must be BLOCKED to unblock, current status: %s", task.Status)}
		}
		if task.RolePair == "" {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", taskID)}
		}
		expectedDoer, err := resolver.DoerRole(task.RolePair)
		if err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
		}
		if opts.AssignTo != "" && assignRole != expectedDoer {
			return &PreconditionError{Reason: fmt.Sprintf("assign-to agent role %q does not match task doer role %q", assignRole, expectedDoer)}
		}
		targetStatus, err := resolver.ExecutingStatus(task.RolePair)
		if err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
		}
		if err := validateUnblockDirectDependencies(state, resolver, task); err != nil {
			return err
		}
		if err := validateDependencyDirection(state, resolver, task.ID, task.RolePair, task.DependsOn); err != nil {
			return err
		}
		var unmet []string
		for _, dep := range unmetDependencies(task, state) {
			if dep.Invalid() {
				return &PreconditionError{Reason: fmt.Sprintf("task %s has invalid dependency: %s", taskID, dep.Summary())}
			}
			if opts.AssignTo == "" && dep.Kind == models.DependencyUnsatisfiedPending {
				continue
			}
			unmet = append(unmet, dep.Summary())
		}
		if len(unmet) > 0 {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has unmet dependencies: %s", taskID, strings.Join(unmet, ", "))}
		}

		fromStatus := task.Status
		historyExtra := map[string]any{
			"target_status": string(targetStatus),
		}
		if rebaseResult != nil {
			if err := revalidateUnblockRebase(projectRoot, task, rebaseResult); err != nil {
				return err
			}
			historyExtra["rebase_old_head"] = rebaseResult.OldHead
			historyExtra["rebase_new_head"] = rebaseResult.NewHead
			historyExtra["rebase_target_ref"] = rebaseResult.TargetRef
			historyExtra["rebase_target_sha"] = rebaseResult.TargetSHA
			historyExtra["rebase_autostash"] = rebaseResult.Autostash
			task.BaseCommit = &rebaseResult.TargetSHA
		}
		if task.RepairRequest != nil {
			historyExtra["repair_operation"] = task.RepairRequest.Operation
			historyExtra["repair_target"] = task.RepairRequest.Target
			historyExtra["repair_command"] = task.RepairRequest.Command
			historyExtra["repair_evidence"] = append([]string(nil), task.RepairRequest.Evidence...)
			historyExtra["repair_validation"] = append([]string(nil), task.RepairRequest.Validation...)
		}

		var leaseExpires time.Time
		if opts.AssignTo != "" {
			if task.Worktree == nil {
				return &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree to resume", taskID)}
			}
			agent, exists := state.Agents[opts.AssignTo]
			if !exists {
				return &PreconditionError{Reason: fmt.Sprintf("assign-to agent %s is not registered", opts.AssignTo)}
			}
			if agent.Role != expectedDoer {
				return &PreconditionError{Reason: fmt.Sprintf("assign-to agent %s has role %q, want %q", opts.AssignTo, agent.Role, expectedDoer)}
			}
			if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != taskID {
				return &PreconditionError{Reason: fmt.Sprintf("agent %s is already working on task %s", opts.AssignTo, *agent.CurrentTask)}
			}
			leaseDuration := state.Config.LeaseDuration
			if leaseDuration == 0 {
				leaseDuration = models.DefaultLeaseDurationSeconds
			}
			leaseExpires = now.Add(time.Duration(leaseDuration) * time.Second)
			if err := task.TransitionWith(targetStatus, pipelineTransitions); err != nil {
				return err
			}
			historyExtra["assigned_to"] = opts.AssignTo
			task.AssignedTo = &opts.AssignTo
			task.LeaseExpires = &leaseExpires
			agent.Status = models.AgentStatusWorking
			agent.CurrentTask = &taskID
			agent.LeaseExpires = &leaseExpires
			agent.Heartbeat = now
			state.Agents[opts.AssignTo] = agent
		} else {
			initialStatus, err := resolver.InitialStatus(task.RolePair)
			if err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
			}
			if task.Worktree != nil && task.BaseCommit == nil {
				return &PreconditionError{Reason: fmt.Sprintf("task %s has preserved worktree but no base_commit", taskID)}
			}
			targetStatus = initialStatus
			historyExtra["target_status"] = string(targetStatus)
			if task.AssignedTo != nil {
				previous := *task.AssignedTo
				if agent, ok := state.Agents[previous]; ok && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
					state.ReleaseAgent(previous)
				}
			}
			task.Status = targetStatus
			task.AssignedTo = nil
			task.LeaseExpires = nil
		}
		task.BlockedReason = nil
		task.BlockedQuestions = nil
		task.RepairRequest = nil
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventUnblocked,
			Agent:  &agentID,
			Reason: &reason,
			Extra:  historyExtra,
		})

		result = UnblockTaskResult{
			TaskID:     taskID,
			FromStatus: fromStatus,
			ToStatus:   targetStatus,
			AssignedTo: opts.AssignTo,
			Claimable:  task.IsClaimable(expectedDoer, state.Tasks, resolver),
			Rebase:     rebaseResult,
		}
		if opts.AssignTo != "" {
			result.LeaseExpires = &leaseExpires
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to unblock task: %w", err)
	}

	return &result, nil
}

func validateUnblockDirectDependencies(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	seen := make(map[string]bool, len(task.DependsOn))
	for _, depID := range task.DependsOn {
		if depID == "" || strings.TrimSpace(depID) != depID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has invalid depends_on entry %q", task.ID, depID)}
		}
		if depID == task.ID {
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
		}
		if seen[depID] {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has duplicate depends_on entry %q", task.ID, depID)}
		}
		seen[depID] = true

		depTask := state.FindTask(depID)
		if depTask == nil {
			continue
		}
		if depTask.Status != models.TaskStatusMerged &&
			depTask.Status != models.TaskStatusSuperseded &&
			depTask.Status.IsPipelineSprintTerminal(resolver.SprintTerminalStates()) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has terminal non-MERGED dependency %s (%s)", task.ID, depID, depTask.Status)}
		}
		if dependencyReachesTask(state, depID, task.ID, map[string]bool{}) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s dependency %s creates a dependency cycle", task.ID, depID)}
		}
	}
	return nil
}

func maybeRebaseTaskBeforeUnblock(bb *db.Blackboard, projectRoot, taskID, agentID string, opts UnblockTaskOptions, authority *models.AgentAuthority) (*UnblockTaskRebaseResult, error) {
	if strings.TrimSpace(opts.RebaseOn) == "" {
		return nil, nil
	}

	gitWrapper := gitpkg.New(projectRoot)
	snapshot, err := snapshotUnblockRebase(bb, gitWrapper, taskID, opts.RebaseOn)
	if err != nil {
		return nil, err
	}
	if tracked := trackedStatusLines(snapshot.StatusShort); len(tracked) > 0 && !opts.AllowDirty {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s worktree has tracked changes; pass --allow-dirty to rebase with git --autostash: %s", taskID, strings.Join(tracked, "; "))}
	}
	overwritten, err := gitWrapper.UntrackedFilesOverwrittenBy(snapshot.WorktreePath, snapshot.TargetSHA)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("failed to check untracked files before rebase: %v", err)}
	}
	if len(overwritten) > 0 {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has untracked files that exist on %s and could be overwritten: %s", taskID, snapshot.TargetRef, strings.Join(overwritten, ", "))}
	}

	autostash := opts.AllowDirty && len(trackedStatusLines(snapshot.StatusShort)) > 0
	if err := gitWrapper.RebaseOntoWithOptions(snapshot.WorktreePath, snapshot.TargetSHA, gitpkg.RebaseOptions{Autostash: autostash}); err != nil {
		if abortErr := gitWrapper.AbortRebase(snapshot.WorktreePath); abortErr != nil {
			return nil, &OperationalError{
				Code:    "git_operation",
				Phase:   "abort-unblock-rebase",
				Message: "failed to abort unblock-task rebase after failure",
				Details: rebaseFailureDetails(err, snapshot.TargetRef, snapshot.TargetSHA, snapshot.OldHead),
				Err:     abortErr,
			}
		}
		var conflict *gitpkg.RebaseConflictError
		if !stderrors.As(err, &conflict) {
			return nil, &OperationalError{
				Code:    "git_operation",
				Phase:   "unblock-rebase",
				Message: "unblock-task rebase failed (not a merge conflict)",
				Details: rebaseFailureDetails(err, snapshot.TargetRef, snapshot.TargetSHA, snapshot.OldHead),
				Err:     err,
			}
		}
		if markErr := markUnblockRebaseConflict(bb, taskID, agentID, snapshot, err, authority); markErr != nil {
			return nil, markErr
		}
		return nil, &UnblockRebaseConflictError{
			TaskID:    taskID,
			TargetRef: snapshot.TargetRef,
			TargetSHA: snapshot.TargetSHA,
			Cause:     err,
		}
	}

	newHead, err := gitWrapper.GetWorktreeHEAD(taskID)
	if err != nil {
		return nil, &OperationalError{
			Code:    "git_operation",
			Phase:   "post-unblock-rebase-head",
			Message: "failed to read worktree HEAD after unblock-task rebase",
			Err:     err,
		}
	}
	return &UnblockTaskRebaseResult{
		OldHead:     snapshot.OldHead,
		NewHead:     newHead,
		TargetRef:   snapshot.TargetRef,
		TargetSHA:   snapshot.TargetSHA,
		Autostash:   autostash,
		StatusShort: snapshot.StatusShort,
	}, nil
}

func snapshotUnblockRebase(bb *db.Blackboard, gitWrapper *gitpkg.Git, taskID, targetRef string) (unblockRebaseSnapshot, error) {
	var snapshot unblockRebaseSnapshot
	state, err := bb.Read()
	if err != nil {
		return snapshot, err
	}
	task := state.FindTask(taskID)
	if task == nil {
		return snapshot, &errors.NotFoundError{Entity: "task", ID: taskID}
	}
	if task.Status != models.TaskStatusBlocked {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("task must be BLOCKED to unblock, current status: %s", task.Status)}
	}
	if task.Worktree == nil || *task.Worktree == "" {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree to rebase", taskID)}
	}
	expectedRel := gitWrapper.GetWorktreeRelPath(taskID)
	if *task.Worktree != expectedRel {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("task %s worktree = %q, want %q", taskID, *task.Worktree, expectedRel)}
	}
	worktreePath := gitWrapper.GetWorktreePath(taskID)
	if err := gitWrapper.ValidateWorktreeHealth(taskID); err != nil {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("worktree not healthy: %v", err)}
	}
	branch, err := gitWrapper.GetWorktreeBranch(worktreePath)
	if err != nil {
		return snapshot, err
	}
	expectedBranch := paths.TaskBranchPrefix + taskID
	if branch != expectedBranch {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("task %s worktree branch = %q, want %q", taskID, branch, expectedBranch)}
	}
	oldHead, err := gitWrapper.GetWorktreeHEAD(taskID)
	if err != nil {
		return snapshot, err
	}
	targetSHA, err := gitWrapper.GetCommitSHA(targetRef)
	if err != nil {
		return snapshot, &PreconditionError{Reason: fmt.Sprintf("failed to resolve rebase target %q: %v", targetRef, err)}
	}
	statusShort, err := gitWrapper.WorktreeStatusShort(worktreePath)
	if err != nil {
		return snapshot, err
	}
	return unblockRebaseSnapshot{
		TaskID:       taskID,
		WorktreeRel:  expectedRel,
		WorktreePath: worktreePath,
		Branch:       branch,
		OldHead:      oldHead,
		TargetRef:    targetRef,
		TargetSHA:    targetSHA,
		StatusShort:  statusShort,
	}, nil
}

func revalidateUnblockRebase(projectRoot string, task *models.Task, rebase *UnblockTaskRebaseResult) error {
	gitWrapper := gitpkg.New(projectRoot)
	if task.Worktree == nil || *task.Worktree != gitWrapper.GetWorktreeRelPath(task.ID) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree changed during rebase", task.ID)}
	}
	if err := gitWrapper.ValidateWorktreeHealth(task.ID); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("worktree not healthy after rebase: %v", err)}
	}
	branch, err := gitWrapper.GetWorktreeBranch(gitWrapper.GetWorktreePath(task.ID))
	if err != nil {
		return err
	}
	if branch != paths.TaskBranchPrefix+task.ID {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree branch changed during rebase: %s", task.ID, branch)}
	}
	head, err := gitWrapper.GetWorktreeHEAD(task.ID)
	if err != nil {
		return err
	}
	if head != rebase.NewHead {
		return &PreconditionError{Reason: fmt.Sprintf("task %s worktree HEAD changed during unblock: got %s, want %s", task.ID, head, rebase.NewHead)}
	}
	return nil
}

func markUnblockRebaseConflict(bb *db.Blackboard, taskID, agentID string, snapshot unblockRebaseSnapshot, cause error, authority *models.AgentAuthority) error {
	now := time.Now().UTC()
	reason := fmt.Sprintf("unblock-task rebase conflict onto %s (%s)", snapshot.TargetRef, shortSHA(snapshot.TargetSHA))
	questions := []string{
		"Resolve the rebase conflict in the preserved task worktree, then retry unblock-task with --rebase-on.",
	}
	command := fmt.Sprintf("git -C %s rebase %s", snapshot.WorktreeRel, snapshot.TargetSHA)
	excerpt := truncateForDiagnostics(cause.Error(), 2000)
	diagnostic := map[string]any{
		"operation":             integrationOperationUnblockTask,
		"reason":                "unblock_rebase_conflict",
		"rebase_target_ref":     snapshot.TargetRef,
		"rebase_target_sha":     snapshot.TargetSHA,
		"pre_rebase_head":       snapshot.OldHead,
		"exit_code":             1,
		"stdout_stderr_excerpt": excerpt,
		"recovery_hint":         "resolve the unblock-time rebase conflict in the task worktree, then retry unblock-task",
	}
	return lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if task.Status != models.TaskStatusBlocked {
			return &PreconditionError{Reason: fmt.Sprintf("task %s status changed during unblock rebase conflict handling: %s", taskID, task.Status)}
		}
		if task.Worktree == nil || *task.Worktree != snapshot.WorktreeRel {
			return &PreconditionError{Reason: fmt.Sprintf("task %s worktree changed during unblock rebase conflict handling", taskID)}
		}
		task.BlockedReason = &reason
		task.BlockedQuestions = questions
		task.RepairRequest = &models.RepairRequest{
			Operation: "resolve_unblock_rebase_conflict",
			Target:    taskID,
			Command:   command,
			Evidence: []string{
				fmt.Sprintf("command=%s target_ref=%s target_sha=%s exit_code=1 stderr=%s", command, snapshot.TargetRef, snapshot.TargetSHA, excerpt),
			},
			Validation: []string{
				fmt.Sprintf("git -C %s status --short", snapshot.WorktreeRel),
				command,
			},
		}
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlocked,
			Agent:  &agentID,
			Reason: &reason,
			Extra: map[string]any{
				"diagnostic": cloneMapForTaskDiagnostic(diagnostic),
			},
		})
		return nil
	})
}

func trackedStatusLines(statusShort string) []string {
	var lines []string
	for _, line := range strings.Split(statusShort, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "??") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
