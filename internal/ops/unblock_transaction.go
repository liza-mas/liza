package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// unblockCall carries one unblock across its phases: input validation, the
// preflight snapshot, the optional rebase and the locked restore. mutate is the
// single restore body, run on the preflight snapshot as a dry run and then in the
// final transaction; every state-dependent check lives in it.
type unblockCall struct {
	projectRoot, taskID, reason, agentID string
	opts                                 UnblockTaskOptions
	authority                            *models.AgentAuthority
	// effects reports to the caller that a lifecycle write has been recorded.
	effects *bool

	assignRole          string
	resolver            *pipeline.Resolver
	pipelineTransitions map[models.TaskStatus][]models.TaskStatus
	lp                  paths.LizaPaths
	bb                  *db.Blackboard
	now                 time.Time
	request             LifecycleRequest

	rebaseResult *UnblockTaskRebaseResult
	prepared     bool
	dryRun       bool
	result       UnblockTaskResult
}

// unblockRestore is what the restore resolves for one task and then records.
type unblockRestore struct {
	doer         string
	fromStatus   models.TaskStatus
	targetStatus models.TaskStatus
	leaseExpires time.Time
	extra        map[string]any
}

func (c *unblockCall) validateInput() error {
	if c.taskID == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	if c.reason == "" {
		return &PreconditionError{Reason: "reason is required"}
	}
	if c.agentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	if c.opts.ScopeException != nil {
		if err := c.opts.ScopeException.Validate(); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
	}
	if c.opts.BudgetException != nil {
		if err := c.opts.BudgetException.Validate(); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
	}
	if err := identity.ValidateRole(c.agentID, roles.Orchestrator); err != nil && !c.opts.operator {
		return &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can unblock tasks: %v", err)}
	}

	if c.opts.AssignTo != "" {
		var err error
		c.assignRole, err = identity.ExtractRole(c.opts.AssignTo)
		if err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("invalid assign-to agent ID %s: %v", c.opts.AssignTo, err)}
		}
	}
	return nil
}

func (c *unblockCall) loadPipeline() error {
	resolver, _, err := loadResolver(c.projectRoot)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}
	c.resolver = resolver
	c.pipelineTransitions = BuildPipelineTransitions(resolver)
	return nil
}

// openRequest reads the preflight snapshot, admits the caller's authority and
// derives the lifecycle request identity from the task as observed there.
func (c *unblockCall) openRequest() (*models.State, error) {
	c.lp = paths.New(c.projectRoot)
	c.bb = RequestBlackboard(c.lp.StatePath(), c.authority, c.opts.Request)
	c.now = time.Now().UTC()
	preflight, err := c.bb.Read()
	if err != nil {
		return nil, err
	}
	if c.authority != nil {
		if err := RequireAgentAuthority(preflight, *c.authority); err != nil {
			return nil, err
		}
	}
	observed := preflight.FindTask(c.taskID)
	if observed == nil {
		return nil, &errors.NotFoundError{Entity: "task", ID: c.taskID}
	}
	c.request, err = NewLifecycleRequest("unblock-task", observed, c.agentID, c.authority, c.opts.Request, struct {
		Reason, AssignTo, RebaseOn string
		AllowDirty                 bool
		// omitempty keeps the identity of requests made without the flags.
		NewIteration    bool             `json:",omitempty"`
		ScopeException  *ScopeException  `json:",omitempty"`
		BudgetException *BudgetException `json:",omitempty"`
	}{c.reason, c.opts.AssignTo, c.opts.RebaseOn, c.opts.AllowDirty, c.opts.NewIteration, c.opts.ScopeException, c.opts.BudgetException})
	return preflight, err
}

// rebase prepares the owner-ending request in its own transaction and rebases the
// preserved worktree; the final restore revalidates the Git state it left.
func (c *unblockCall) rebase() error {
	var err error
	c.rebaseResult, err = maybeRebaseTaskBeforeUnblock(c.bb, c.lp.ProjectRoot(), c.taskID, c.agentID, c.opts, c.authority, c.request, c.prepare)
	return err
}

func (c *unblockCall) prepare() error {
	err := lifecycleMutation(c.bb, c.authority)(func(state *models.State) error {
		task := state.FindTask(c.taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: c.taskID}
		}
		return prepareOwnerEndingRequest(task, c.request)
	})
	if err == nil {
		c.prepared, *c.effects = true, true
	}
	return err
}

// mutate is the restore body: it admits the request, runs every gate on the
// task and the state, then records the restore and, outside a dry run, completes
// the lifecycle request.
func (c *unblockCall) mutate(state *models.State) error {
	task := state.FindTask(c.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: "task", ID: c.taskID}
	}
	if err := c.admit(task); err != nil {
		return err
	}
	if err := c.checkUnblockable(state, task); err != nil {
		return err
	}
	restore, err := c.resolveRestore(state, task)
	if err != nil {
		return err
	}
	if err := c.recordAudit(task, restore); err != nil {
		return err
	}
	if c.opts.AssignTo != "" {
		err = c.assignDoer(state, task, restore)
	} else {
		err = c.continueUnassigned(state, task, restore)
	}
	if err != nil {
		return err
	}
	return c.complete(state, task, restore)
}

// admit validates the recorded preparation of a prepared request, or resolves
// the request against the task's receipts, answering an exact replay.
func (c *unblockCall) admit(task *models.Task) error {
	if c.prepared {
		return ValidateLifecyclePreparation(task, c.request)
	}
	receipt, err := checkOwnerEndingRequest(task, c.request)
	if err != nil {
		return err
	}
	if receipt != nil {
		c.result = UnblockTaskResult{LifecycleOutcome: LifecycleReplayOutcome(task, receipt, c.agentID), TaskID: c.taskID, FromStatus: receipt.Projection.SourceStatus, ToStatus: task.Status, AssignedTo: c.opts.AssignTo, NewIteration: c.opts.NewIteration}
		return errLifecycleReplay
	}
	return nil
}

// checkUnblockable refuses a task that is not a BLOCKED one this call may
// restore: protected validation, status, the automatic hold gate and the
// rejection-RCA gate.
func (c *unblockCall) checkUnblockable(state *models.State, task *models.Task) error {
	// Exact replay has no new assignment; fresh protected work must be
	// claimed by its own validated provider session.
	if c.opts.AssignTo != "" && len(task.ValidationPrerequisites) > 0 {
		return &PreconditionError{Reason: "validation preflight requires the target session; unblock without --assign-to and let its supervisor claim the task"}
	}
	if task.Status != models.TaskStatusBlocked {
		return WrapLifecycleError("unblock-task", task, &PreconditionError{Reason: fmt.Sprintf("task must be BLOCKED to unblock, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}
	if c.opts.stateLockHold != nil {
		if c.opts.AssignTo != "" || c.opts.RebaseOn != "" || c.opts.NewIteration || c.authority == nil {
			return &PreconditionError{Reason: "automatic state lock continuation cannot assign, rebase or consume an iteration"}
		}
		if reason := automaticStateLockHoldGate(state, task, c.projectRoot, c.resolver, c.opts.stateLockHold); reason != "" {
			return &PreconditionError{Reason: "state lock hold recovery refused: " + reason}
		}
	}
	return checkUnblockRejectionRCAGate(task, c.opts.NewIteration)
}

// resolveRestore resolves the doer role and executing status the task restores
// to, after its role pair, the requested exceptions and assignment, and its
// dependencies all admit the restore.
func (c *unblockCall) resolveRestore(state *models.State, task *models.Task) (*unblockRestore, error) {
	if task.RolePair == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", c.taskID)}
	}
	expectedDoer, err := c.resolver.DoerRole(task.RolePair)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
	}
	if c.opts.ScopeException != nil && expectedDoer != roles.Coder {
		return nil, &PreconditionError{Reason: fmt.Sprintf("scope exceptions apply to coding tasks only; task %s has doer role %q", c.taskID, expectedDoer)}
	}
	if c.opts.BudgetException != nil && expectedDoer != roles.Coder {
		return nil, &PreconditionError{Reason: fmt.Sprintf("budget exceptions apply to coding tasks only; task %s has doer role %q", c.taskID, expectedDoer)}
	}
	if c.opts.AssignTo != "" && c.assignRole != expectedDoer {
		return nil, &PreconditionError{Reason: fmt.Sprintf("assign-to agent role %q does not match task doer role %q", c.assignRole, expectedDoer)}
	}
	targetStatus, err := c.resolver.ExecutingStatus(task.RolePair)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
	}
	if err := c.checkDependencies(state, task); err != nil {
		return nil, err
	}
	return &unblockRestore{doer: expectedDoer, fromStatus: task.Status, targetStatus: targetStatus}, nil
}

// checkDependencies refuses direct, directional and unmet dependencies. A
// pending dependency only blocks a restore that assigns the doer directly.
func (c *unblockCall) checkDependencies(state *models.State, task *models.Task) error {
	if err := validateUnblockDirectDependencies(state, c.resolver, task); err != nil {
		return err
	}
	if err := validateDependencyDirection(state, c.resolver, task.ID, task.RolePair, task.DependsOn); err != nil {
		return err
	}
	var unmet []string
	for _, dep := range models.UnmetClaimDependencies(task, state, c.resolver) {
		if dep.Invalid() {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has invalid dependency: %s", c.taskID, dep.Summary())}
		}
		if c.opts.AssignTo == "" && dep.Kind == models.DependencyUnsatisfiedPending {
			continue
		}
		unmet = append(unmet, dep.Summary())
	}
	if len(unmet) > 0 {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has unmet dependencies: %s", c.taskID, strings.Join(unmet, ", "))}
	}
	return nil
}

// recordAudit builds the audit extra of the unblocked entry: the target status,
// the revalidated rebase and the repair request it clears.
func (c *unblockCall) recordAudit(task *models.Task, restore *unblockRestore) error {
	restore.extra = map[string]any{
		"target_status": string(restore.targetStatus),
	}
	if c.rebaseResult != nil {
		if err := revalidateUnblockRebase(c.projectRoot, task, c.rebaseResult); err != nil {
			return err
		}
		restore.extra["rebase_old_head"] = c.rebaseResult.OldHead
		restore.extra["rebase_new_head"] = c.rebaseResult.NewHead
		restore.extra["rebase_target_ref"] = c.rebaseResult.TargetRef
		restore.extra["rebase_target_sha"] = c.rebaseResult.TargetSHA
		restore.extra["rebase_autostash"] = c.rebaseResult.Autostash
		task.BaseCommit = &c.rebaseResult.TargetSHA
	}
	if task.RepairRequest != nil {
		restore.extra["repair_operation"] = task.RepairRequest.Operation
		restore.extra["repair_target"] = task.RepairRequest.Target
		restore.extra["repair_command"] = task.RepairRequest.Command
		restore.extra["repair_evidence"] = append([]string(nil), task.RepairRequest.Evidence...)
		restore.extra["repair_validation"] = append([]string(nil), task.RepairRequest.Validation...)
	}
	return nil
}

// assignDoer resumes the preserved worktree directly under the named doer.
func (c *unblockCall) assignDoer(state *models.State, task *models.Task, restore *unblockRestore) error {
	if task.Worktree == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree to resume", c.taskID)}
	}
	agent, exists := state.Agents[c.opts.AssignTo]
	if !exists {
		return &PreconditionError{Reason: fmt.Sprintf("assign-to agent %s is not registered", c.opts.AssignTo)}
	}
	if agent.Role != restore.doer {
		return &PreconditionError{Reason: fmt.Sprintf("assign-to agent %s has role %q, want %q", c.opts.AssignTo, agent.Role, restore.doer)}
	}
	if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != c.taskID {
		return &PreconditionError{Reason: fmt.Sprintf("agent %s is already working on task %s", c.opts.AssignTo, *agent.CurrentTask)}
	}
	leaseDuration := state.Config.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = models.DefaultLeaseDurationSeconds
	}
	restore.leaseExpires = c.now.Add(time.Duration(leaseDuration) * time.Second)
	if err := task.TransitionWith(restore.targetStatus, c.pipelineTransitions); err != nil {
		return err
	}
	restore.extra["assigned_to"] = c.opts.AssignTo
	task.AssignedTo = &c.opts.AssignTo
	task.LeaseExpires = &restore.leaseExpires
	// Direct assignment skips the claim, so it does the claim's accounting.
	task.Continuation = false
	if c.opts.NewIteration {
		task.Iteration++
	}
	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &c.taskID
	agent.LeaseExpires = &restore.leaseExpires
	agent.Heartbeat = c.now
	state.Agents[c.opts.AssignTo] = agent
	return nil
}

// continueUnassigned returns the task to its role pair's initial status, to be
// claimed again, releasing the agent that still holds it.
func (c *unblockCall) continueUnassigned(state *models.State, task *models.Task, restore *unblockRestore) error {
	initialStatus, err := c.resolver.InitialStatus(task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q: %v", task.RolePair, err)}
	}
	if task.Worktree != nil && task.BaseCommit == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has preserved worktree but no base_commit", c.taskID)}
	}
	restore.targetStatus = initialStatus
	restore.extra["target_status"] = string(initialStatus)
	if task.AssignedTo != nil {
		previous := *task.AssignedTo
		if agent, ok := state.Agents[previous]; ok && agent.CurrentTask != nil && *agent.CurrentTask == c.taskID {
			state.ReleaseAgent(previous)
		}
	}
	task.Status = initialStatus
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.Continuation = !c.opts.NewIteration
	return nil
}

// complete records the unblocked entry and the result; outside a dry run it
// also advances the lifecycle and completes the request.
func (c *unblockCall) complete(state *models.State, task *models.Task, restore *unblockRestore) error {
	restore.extra["new_iteration"] = c.opts.NewIteration
	if c.opts.ScopeException != nil {
		restore.extra[historyExtraScopeException] = c.opts.ScopeException.historyValue()
	}
	if c.opts.BudgetException != nil {
		restore.extra[historyExtraBudgetException] = c.opts.BudgetException.historyValue()
	}
	task.BlockedReason = nil
	task.BlockedQuestions = nil
	task.RepairRequest = nil
	task.StateLockHold = nil
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   c.now,
		Event:  models.TaskEventUnblocked,
		Agent:  &c.agentID,
		Reason: &c.reason,
		Extra:  restore.extra,
	})

	c.result = UnblockTaskResult{
		TaskID:       c.taskID,
		FromStatus:   restore.fromStatus,
		ToStatus:     restore.targetStatus,
		AssignedTo:   c.opts.AssignTo,
		Claimable:    task.IsClaimable(restore.doer, state.Tasks, c.resolver),
		NewIteration: c.opts.NewIteration,
		Rebase:       c.rebaseResult,
	}
	if c.opts.AssignTo != "" {
		c.result.LeaseExpires = &restore.leaseExpires
	}
	if c.dryRun {
		return nil
	}
	models.AdvanceLifecycle(task)
	var err error
	c.result.LifecycleOutcome, err = CompleteLifecycleRequest(task, c.request, models.LifecycleProjection{SourceStatus: restore.fromStatus}, state.Agents)
	return err
}
