package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// mutate is the block transaction body: it resolves the request identity,
// checks the task may be blocked by this caller, then records the transition.
func (c *markBlockedCall) mutate(state *models.State) error {
	task := state.FindTask(c.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: "task", ID: c.taskID}
	}
	request, err := c.newRequest(task)
	if err != nil {
		return err
	}
	receipt, err := checkOwnerEndingRequest(task, request)
	if err != nil {
		return err
	}
	if receipt != nil {
		c.outcome, c.replay = LifecycleReplayOutcome(task, receipt, c.agentID), true
		return errLifecycleReplay
	}
	if err := c.checkBlockable(state, task); err != nil {
		return err
	}
	if err := c.block(state, task); err != nil {
		return err
	}
	c.outcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	return err
}

func (c *markBlockedCall) newRequest(task *models.Task) (LifecycleRequest, error) {
	return NewLifecycleRequest("mark-blocked", task, c.agentID, c.authority, c.opts.Request, struct {
		Reason       string
		Questions    []string
		Repair       *models.RepairRequest
		Dependencies []string
		// Omitted when empty so earlier requests keep their identity.
		HumanAction      string `json:",omitempty"`
		StateLockTimeout bool   `json:",omitempty"`
	}{c.reason, c.questions, c.repairRequest, c.dependsOn, c.opts.HumanAction, c.opts.StateLockTimeout})
}

// checkBlockable refuses a task that is not executing, is not owned by the
// caller, or whose added dependencies or state-lock wait are not acceptable.
func (c *markBlockedCall) checkBlockable(state *models.State, task *models.Task) error {
	if !isExecutingStatus(task.Status, c.pipelineExecuting) {
		return WrapLifecycleError("mark-blocked", task, &PreconditionError{Reason: fmt.Sprintf("task must be in an executing status to be marked blocked, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}

	if task.AssignedTo == nil || *task.AssignedTo != c.agentID {
		return WrapLifecycleError("mark-blocked", task, &PreconditionError{Reason: "only the assigned agent can mark task as blocked"}, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	if err := validateDependsOnForBlockedTask(state, task, c.dependsOn); err != nil {
		return err
	}
	// Validate added edges without preventing emergency blocking to repair existing metadata.
	if err := validateDependencyDirection(state, c.resolver, task.ID, task.RolePair, c.dependsOn); err != nil {
		return err
	}
	if c.opts.StateLockTimeout {
		candidate := *task
		candidate.DependsOn = append(append([]string(nil), task.DependsOn...), c.dependsOn...)
		candidate.RepairRequest = c.repairRequest
		if reason := stateLockHoldGate(state, &candidate, c.resolver); reason != "" {
			return &PreconditionError{Reason: "state lock hold requires infrastructure-only wait: " + reason}
		}
	}
	return nil
}

// block records the BLOCKED transition. Blocking ends the authorized owner's
// work and retires its unfinished preparation.
func (c *markBlockedCall) block(state *models.State, task *models.Task) error {
	c.mutationStarted = true
	models.AdvanceLifecycle(task)
	if err := task.TransitionWith(models.TaskStatusBlocked, c.pipelineTransitions); err != nil {
		return err
	}
	task.BlockedReason = &c.reason
	task.BlockedQuestions = c.questions
	task.RepairRequest = c.repairRequest
	if len(c.dependsOn) > 0 {
		task.DependsOn = append(task.DependsOn, c.dependsOn...)
	}
	c.resultDependsOn = append([]string(nil), task.DependsOn...)
	releaseAgentsForTask(state, c.taskID)
	task.AssignedTo = nil
	task.LeaseExpires = nil

	entry := models.TaskHistoryEntry{
		Time:   c.now,
		Event:  models.TaskEventBlocked,
		Agent:  &c.agentID,
		Reason: &c.reason,
	}
	if c.opts.HumanAction != "" {
		entry.Extra = map[string]any{models.AwaitingHumanExtraKey: c.opts.HumanAction}
	}
	task.History = append(task.History, entry)
	task.StateLockHold = nil
	if c.opts.StateLockTimeout {
		installStateLockHold(state, task)
	}
	return nil
}
