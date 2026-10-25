package ops

import (
	"fmt"
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
	// Changed states what differs from the blocked attempt. Required when
	// replacing a BLOCKED task (ADR-0171); recorded masked.
	Changed string
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

const supersedeTaskOperation = "supersede-task"

// supersedePass carries one supersede-task request through its phases:
// admission and salvage before the lock, the locked retirement, and the cleanup
// that follows persistence.
type supersedePass struct {
	projectRoot, taskID   string
	replacementIDs        []string
	reason, agentID       string
	recoverabilityCommand string
	opts                  SupersedeTaskOptions
	authority             *models.AgentAuthority

	// Set by admit, before the lock.
	bb             *db.Blackboard
	gw             *git.Git
	pb             *pipelineBundle
	request        LifecycleRequest
	originalStatus models.TaskStatus
	hadWorktree    bool
	salvage        map[string]any

	// Set where the task is observed, before and inside the lock.
	observed *models.Task
	outcome  models.LifecycleOutcome
}

func supersedeTaskWithOptionalAuthority(projectRoot, taskID string, replacementIDs []string, reason, agentID string, opts SupersedeTaskOptions, authority *models.AgentAuthority) (result *SupersedeResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	pass := &supersedePass{projectRoot: projectRoot, taskID: taskID, replacementIDs: replacementIDs, reason: reason, agentID: agentID, opts: opts, authority: authority}
	defer func() {
		retErr = WrapLifecycleError(supersedeTaskOperation, pass.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(supersedeTaskOperation, outcome, &retErr, warnings)
	}()
	if err := pass.checkRequest(); err != nil {
		return nil, err
	}
	retErr = withOwnershipTaskLock(projectRoot, taskID, supersedeTaskOperation, func() error {
		var err error
		result, err = pass.supersede()
		return err
	})
	return result, retErr
}

// checkRequest refuses a malformed request before any state is read and
// normalises the recoverability command and the stated change.
func (p *supersedePass) checkRequest() error {
	if err := ValidateLifecycleRequestOptions(p.opts.Request); err != nil {
		return err
	}
	if p.taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if p.reason == "" {
		return &PreconditionError{Reason: "rescope reason is required"}
	}
	if p.agentID == "" {
		return &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	p.recoverabilityCommand = strings.TrimSpace(p.opts.RecoverabilityCommand)
	if len(p.replacementIDs) == 0 {
		if p.recoverabilityCommand == "" {
			return &PreconditionError{Reason: "recoverability command is required when superseding without replacements"}
		}
		if strings.ContainsAny(p.recoverabilityCommand, "\r\n") {
			return &PreconditionError{Reason: "recoverability command must be a single line"}
		}
		p.recoverabilityCommand = secretmask.New().MaskText(p.recoverabilityCommand)
	} else if p.recoverabilityCommand != "" {
		return &PreconditionError{Reason: "recoverability command is only valid when superseding without replacements"}
	}
	p.opts.Changed = secretmask.New().MaskText(strings.TrimSpace(p.opts.Changed))
	return nil
}

// supersede runs the request under the ownership task lock.
func (p *supersedePass) supersede() (*SupersedeResult, error) {
	if replay, err := p.admit(); replay != nil || err != nil {
		return replay, err
	}
	if len(p.replacementIDs) == 0 {
		var err error
		if p.salvage, err = collectSupersedeSalvageSnapshot(p.gw, p.observed, p.originalStatus, p.recoverabilityCommand); err != nil {
			return nil, err
		}
	}

	err := lifecycleMutation(p.bb, p.authority)(p.commit)
	if isLifecycleReplay(err) {
		return p.result(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to supersede task: %w", err)
	}
	return p.result(p.cleanup()), nil
}

// admit reads and validates without the state lock: pipeline bundle, task,
// authority, request identity, its retained receipt (returned as the replay
// result) and the supersession eligibility of the task.
func (p *supersedePass) admit() (*SupersedeResult, error) {
	p.bb = RequestBlackboard(paths.New(p.projectRoot).StatePath(), p.authority, p.opts.Request)
	p.gw = git.New(p.projectRoot)

	var err error
	if p.pb, err = loadPipelineBundle(p.projectRoot); err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	state, task, err := readTaskState(p.bb, p.taskID)
	if err != nil {
		return nil, err
	}
	if p.authority != nil {
		if err := RequireAgentAuthority(state, *p.authority); err != nil {
			return nil, err
		}
	}
	p.observed = task
	if p.request, err = NewLifecycleRequest(supersedeTaskOperation, task, p.agentID, p.authority, p.opts.Request, p.requestPayload()); err != nil {
		return nil, err
	}
	receipt, err := checkOwnerEndingRequest(task, p.request)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		p.outcome = LifecycleReplayOutcome(task, receipt, p.agentID)
		p.originalStatus = receipt.Projection.SourceStatus
		return p.result(nil), nil
	}
	p.originalStatus = task.Status
	p.hadWorktree = task.Worktree != nil
	return nil, p.checkEligible(task)
}

func (p *supersedePass) requestPayload() any {
	return struct {
		Replacements                  []string
		Reason, RecoverabilityCommand string
		Changed                       string `json:",omitempty"`
	}{p.replacementIDs, p.reason, p.recoverabilityCommand, p.opts.Changed}
}

// checkEligible refuses a task outside the statuses supersession accepts.
func (p *supersedePass) checkEligible(task *models.Task) error {
	if p.supersedable(task) {
		return nil
	}
	return WrapLifecycleError(supersedeTaskOperation, task, &PreconditionError{Reason: fmt.Sprintf("cannot supersede task %s in status %s (must be initial, rejected, or BLOCKED)", p.taskID, task.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
}

// supersedable allows any initial (DRAFT_*) state, any rejected state, BLOCKED,
// or INTEGRATION_FAILED when external repair/replacement made the failed task
// obsolete.
func (p *supersedePass) supersedable(task *models.Task) bool {
	status := task.Status
	if status == models.TaskStatusBlocked || status == models.TaskStatusIntegrationFailed {
		return true
	}
	if task.RolePair == "" {
		// Legacy fallback: tasks without a role-pair use hardcoded statuses.
		return status == models.TaskStatusReady || status == models.TaskStatusRejected
	}
	// Pipeline-aware path: resolve initial/rejected from the task's role-pair.
	if initialStatus, err := p.pb.resolver.InitialStatus(task.RolePair); err == nil && status == initialStatus {
		return true
	}
	rejectedStatus, err := p.pb.resolver.RejectedStatus(task.RolePair)
	return err == nil && status == rejectedStatus
}

// commit is the locked transaction: lookup, receipt replay, status recheck,
// the retirement itself, candidate validation and the lifecycle completion.
func (p *supersedePass) commit(state *models.State) error {
	currentTask := state.FindTask(p.taskID)
	if currentTask == nil {
		return &errors.NotFoundError{Entity: "task", ID: p.taskID}
	}
	p.observed = currentTask
	receipt, err := checkOwnerEndingRequest(currentTask, p.request)
	if err != nil {
		return err
	}
	if receipt != nil {
		p.outcome = LifecycleReplayOutcome(currentTask, receipt, p.agentID)
		p.originalStatus = receipt.Projection.SourceStatus
		return errLifecycleReplay
	}

	if currentTask.Status != p.originalStatus {
		return WrapLifecycleError(supersedeTaskOperation, currentTask, fmt.Errorf("task status changed before supersession"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}

	if _, err := supersedeTaskInState(state, p.pb, currentTask, p.replacementIDs, p.reason, p.opts.Changed, p.agentID, p.salvage, time.Now().UTC()); err != nil {
		return err
	}
	// Legacy tasks predate pipeline-wide validation. For pipeline tasks, validate
	// every structural invariant while skipping unchanged external artifact refs.
	if currentTask.RolePair != "" {
		if err := statevalidate.ValidateCandidate(state, p.bb.ReadSnapshot, p.projectRoot, true, os.Stderr); err != nil {
			return err
		}
	}

	p.outcome, err = CompleteLifecycleRequest(currentTask, p.request, models.LifecycleProjection{SourceStatus: p.originalStatus}, state.Agents)
	return err
}

// result projects the recorded outcome; replays carry no warnings.
func (p *supersedePass) result(warnings []string) *SupersedeResult {
	return &SupersedeResult{
		LifecycleOutcome: p.outcome,
		TaskID:           p.taskID,
		OriginalStatus:   p.originalStatus,
		ReplacementIDs:   p.replacementIDs,
		Warnings:         warnings,
	}
}

// cleanup is best-effort and runs after the state commit, so losing the
// worktree is safe: the worktree directory, the task's own branch, and the
// branches of predecessors this task was the last live successor of.
func (p *supersedePass) cleanup() []string {
	var warnings []string
	if p.hadWorktree {
		if rmErr := p.gw.RemoveWorktreeDir(p.taskID); rmErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", rmErr))
		}
	}

	// When there are no successors, delete the branch immediately — no successor
	// will ever trigger cleanup via cleanupPredecessorBranches.
	// When successors exist, branch is preserved for git show access.
	if len(p.replacementIDs) == 0 {
		warnings = append(warnings, deleteOwnTaskBranch(p.gw, p.taskID)...)
	}

	// This superseded task may itself be a successor — check if its terminal
	// transition releases an older predecessor's branch.
	return append(warnings, cleanupPredecessorBranches(p.bb, p.gw, p.taskID)...)
}

// supersedeTaskInState retires one task inside an already-locked candidate
// state: dependency-direction validation, downstream pruning, the SUPERSEDED
// transition, ownership release, the audit entry and consumer rewriting. It
// returns the dependency edges pruned as illegal downstream links, so a
// composing transaction can record them. Full-state validation and the
// lifecycle receipt stay with the caller.
//
// Replacing a BLOCKED task is a blocked recovery (ADR-0171): it is refused at
// the agent cap, requires a stated change, and marks the audit entry so later
// recoveries in the lineage count it. changed must already be masked.
func supersedeTaskInState(state *models.State, pb *pipelineBundle, task *models.Task, replacementIDs []string, reason, changed, agentID string, salvage map[string]any, now time.Time) ([]string, error) {
	var blockedRecovery map[string]any
	if task.Status == models.TaskStatusBlocked && len(replacementIDs) > 0 {
		if err := checkBlockedRecovery(state, task, changed); err != nil {
			return nil, err
		}
		blockedRecovery = models.BlockedRecoveryRecord(task, changed)
	}
	if err := validateDependencyDirection(state, pb.resolver, task.ID, task.RolePair, replacementIDs); err != nil {
		return nil, err
	}
	if err := rejectDroppedReservations(state, pb.resolver, task, replacementIDs); err != nil {
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
	retireRuntimeInputBindings(state, task, replacementIDs, now)

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
	if blockedRecovery != nil {
		if historyEntry.Extra == nil {
			historyEntry.Extra = make(map[string]any)
		}
		historyEntry.Extra[models.BlockedRecoveryKey] = blockedRecovery
	}
	task.History = append(task.History, historyEntry)

	if err := rewriteActiveDependents(state, pb.resolver, task.ID, replacementIDs, agentID, now); err != nil {
		return nil, err
	}
	return removedDependencies, nil
}

// checkBlockedRecovery refuses replacing the BLOCKED task when its lineage is
// at the agent blocked-recovery cap, or when no change since the block is
// stated.
func checkBlockedRecovery(state *models.State, task *models.Task, changed string) error {
	if chain, capped := models.BlockedRecoveryCapped(state, task); capped {
		return &PreconditionError{Reason: fmt.Sprintf(
			"replacing BLOCKED task %s exceeds the blocked-recovery cap: lineage %s already had %d blocked recoveries (%s). "+
				"Hold it with assess-blocked for a human decision; a human release of this blocked episode (release-blocked-chain, or analyze then resume to resolve the %s circuit-breaker halt) authorizes one further replacement",
			task.ID, strings.Join(chain.Path, " -> "), len(chain.Recoveries), strings.Join(chain.Recoveries, ", "), models.BlockedReplacementChainPattern)}
	}
	if strings.TrimSpace(changed) == "" {
		blockedReason := ""
		if task.BlockedReason != nil {
			blockedReason = *task.BlockedReason
		}
		return &PreconditionError{Reason: fmt.Sprintf(
			"replacing BLOCKED task %s requires stating what changed since it blocked (supersede-task --changed, or field changed in a replace-task payload or plan output); blocked_reason: %q",
			task.ID, blockedReason)}
	}
	return nil
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
