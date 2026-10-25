package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

const integrationOperationUnblockTask = "unblock-task"

// UnblockTaskResult contains the outcome of restoring a repaired BLOCKED task.
type UnblockTaskResult struct {
	models.LifecycleOutcome
	Warnings     []string                 `json:"warnings,omitempty"`
	TaskID       string                   `json:"task_id"`
	FromStatus   models.TaskStatus        `json:"from_status"`
	ToStatus     models.TaskStatus        `json:"to_status"`
	AssignedTo   string                   `json:"assigned_to,omitempty"`
	Claimable    bool                     `json:"claimable"`
	NewIteration bool                     `json:"new_iteration"`
	LeaseExpires *time.Time               `json:"lease_expires,omitempty"`
	Rebase       *UnblockTaskRebaseResult `json:"rebase,omitempty"`
}

// UnblockTaskOptions configures unblock-task behavior. By default the restore
// is a continuation: resuming the preserved work consumes no iteration.
// NewIteration makes the resume consume one; AssignTo only picks the doer.
type UnblockTaskOptions struct {
	Request      LifecycleRequestOptions
	AssignTo     string
	RebaseOn     string
	AllowDirty   bool
	NewIteration bool
	// ScopeException, when set, records a bounded in-task scope grant on
	// the unblocked history entry (ADR-0206). Coding tasks only.
	ScopeException *ScopeException
	// BudgetException, when set, records a human-ruled canonical admission
	// budget raise on the unblocked history entry (ADR-0207). Coding tasks only.
	BudgetException *BudgetException
	// Only mechanical contention recovery supplies this captured episode.
	stateLockHold *models.StateLockHold
	// Only UnblockTaskAsOperator sets this; it waives the orchestrator-ID check.
	operator bool
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

// UnblockTaskAsOperator restores a BLOCKED task for an operator, who holds no
// agent registration. changedBy is the audit actor; every other gate applies.
func UnblockTaskAsOperator(projectRoot, taskID, reason, changedBy string, opts UnblockTaskOptions) (*UnblockTaskResult, error) {
	opts.operator = true
	return unblockTaskWithOptionalAuthority(projectRoot, taskID, reason, changedBy, opts, nil)
}

func unblockTaskWithOptionalAuthority(projectRoot, taskID, reason, agentID string, opts UnblockTaskOptions, authority *models.AgentAuthority) (result *UnblockTaskResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	effects := false
	defer func() {
		outcome, action, effect := models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone
		if effects {
			outcome, action, effect = models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown
		}
		retErr = WrapLifecycleError("unblock-task", nil, retErr, outcome, action, effect)
		var value models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			value, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult("unblock-task", value, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts.Request); err != nil {
		return nil, err
	}
	retErr = withOwnershipTaskLock(projectRoot, taskID, "unblock-task", func() error {
		var err error
		result, err = unblockTaskLifecycle(projectRoot, taskID, reason, agentID, opts, authority, &effects)
		return err
	})
	return result, retErr
}

func unblockTaskLifecycle(projectRoot, taskID, reason, agentID string, opts UnblockTaskOptions, authority *models.AgentAuthority, effects *bool) (*UnblockTaskResult, error) {
	call := &unblockCall{projectRoot: projectRoot, taskID: taskID, reason: reason, agentID: agentID, opts: opts, authority: authority, effects: effects, dryRun: true}
	if err := call.validateInput(); err != nil {
		return nil, err
	}
	if err := call.loadPipeline(); err != nil {
		return nil, err
	}
	preflight, err := call.openRequest()
	if err != nil {
		return nil, err
	}
	// Validate every state-dependent input on an isolated snapshot before Git.
	if err := call.mutate(preflight); err != nil {
		if isLifecycleReplay(err) {
			return &call.result, nil
		}
		return nil, err
	}
	call.dryRun = false
	if strings.TrimSpace(opts.RebaseOn) != "" {
		if err := call.rebase(); err != nil {
			return nil, err
		}
	}
	err = lifecycleMutation(call.bb, authority)(call.mutate)
	if isLifecycleReplay(err) {
		return &call.result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to unblock task: %w", err)
	}

	return &call.result, nil
}

// checkUnblockRejectionRCAGate refuses a task whose rejection-RCA gate is still
// open and, once a disposition has closed it, allows only the restore form that
// disposition authorizes. unblock-task is the only operation that may restore a
// BLOCKED task, so this is where the gate's promise — no further normal claim or
// resubmission until a classified RCA and an authorized disposition exist — is
// kept. A task that never gated carries no record and is untouched. The restore
// mode fixes whether the resume consumes an iteration; who resumes is free.
func checkUnblockRejectionRCAGate(task *models.Task, newIteration bool) error {
	record := task.RejectionRCA
	if record == nil {
		return nil
	}
	if task.RejectionRCAGateOpen() {
		// A fingerprint is written only together with the caller fields, so it
		// is what distinguishes a seeded record from a recorded one.
		missing := "resume-rejection-rca"
		if record.Fingerprint == "" {
			missing = "record-rejection-rca"
		}
		// Closing the gate is a state change the caller must make through two
		// other operations, so requery — no payload correction can clear it.
		return WrapLifecycleError("unblock-task", task, &PreconditionError{Reason: fmt.Sprintf(
			"task %s has an open rejection-RCA gate: run %s next, then unblock-task (record-rejection-rca records the classified RCA, resume-rejection-rca records the authorized disposition)",
			task.ID, missing)}, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	switch mode := record.Disposition.RestoreMode; mode {
	// A wrong flag is a payload defect, so both refusals ask to correct it.
	case models.RestoreModeClaimable:
		// A product correction is a new iteration.
		if !newIteration {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s recovery path %s is a product correction that consumes an iteration: retry unblock-task with --new-iteration",
				task.ID, record.Disposition.RecoveryPath)}
		}
		return nil
	case models.RestoreModeAssign:
		// A capability or lifecycle cause continues the current iteration.
		if newIteration {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s recovery path %s continues the current iteration: retry unblock-task without --new-iteration",
				task.ID, record.Disposition.RecoveryPath)}
		}
		return nil
	default:
		// rescope routes to supersession; no form of unblock-task will ever
		// restore this task, so stop rather than invite a retry.
		return WrapLifecycleError("unblock-task", task, &PreconditionError{Reason: fmt.Sprintf(
			"task %s recovery path %s authorizes restore mode %q: it is not restored by unblock-task",
			task.ID, record.Disposition.RecoveryPath, mode)}, models.LifecycleStateChanged, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
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

func maybeRebaseTaskBeforeUnblock(bb *db.Blackboard, projectRoot, taskID, agentID string, opts UnblockTaskOptions, authority *models.AgentAuthority, request LifecycleRequest, prepare func() error) (*UnblockTaskRebaseResult, error) {
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
	if err := prepare(); err != nil {
		return nil, err
	}
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
		if markErr := markUnblockRebaseConflict(bb, taskID, agentID, snapshot, err, authority, request); markErr != nil {
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

func markUnblockRebaseConflict(bb *db.Blackboard, taskID, agentID string, snapshot unblockRebaseSnapshot, cause error, authority *models.AgentAuthority, request LifecycleRequest) error {
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
		if err := ValidateLifecyclePreparation(task, request); err != nil {
			return err
		}
		models.AdvanceLifecycle(task)
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
