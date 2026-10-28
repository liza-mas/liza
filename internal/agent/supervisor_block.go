package agent

import (
	"context"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
)

// supervisorBlockBeforeModifyTestHook runs just before a supervisor block's
// BLOCKED transaction; tests use it to move the task in between.
var supervisorBlockBeforeModifyTestHook func()

// blockTaskFromSupervisor marks a task this agent owns as BLOCKED and releases
// its owners. A doer's uncommitted work is first saved as a WIP commit on the
// task branch; a reviewer never commits on the doer's behalf. The worktree and
// branch are kept for unblock-task or repair (ADR-0203): deleting them lost a
// doer's work (D-101). The save runs outside the state lock, so the block
// applies only while the task still holds the lifecycle and owner observed
// before it; otherwise the task is left to whoever moved it.
func blockTaskFromSupervisor(bb *db.Blackboard, projectRoot, taskID string, authority models.AgentAuthority, reason string) error {
	pipelineTransitions, ptErr := ops.LoadPipelineTransitions(projectRoot)
	if ptErr != nil {
		return fmt.Errorf("load pipeline transitions for supervisor block: %w", ptErr)
	}
	// Locked read: this is the fence in front of the save and the transition.
	state, err := bb.Read()
	if err != nil {
		return err
	}
	if err := ops.RequireAgentAuthority(state, authority); err != nil {
		return err
	}
	task := state.FindTask(taskID)
	if task == nil {
		return nil
	}
	slot := ops.SupervisorTaskSlot(task, authority.ID)
	if slot == "" {
		GetLogger().Warn("Supervisor block skipped: task is not owned by this agent", "task_id", taskID, "agent_id", authority.ID)
		return nil
	}
	expected := models.TaskTransitionID(task)
	if slot == roles.ClaimDoer {
		commitBlockedDoerWIP(projectRoot, authority, taskID, reason)
	}
	owner := ops.SupervisorBlockOwnerDoer
	if slot == roles.ClaimReviewer {
		owner = ops.SupervisorBlockOwnerReviewer
	}
	_, err = applySupervisorBlock(bb, pipelineTransitions, taskID, authority, reason, ops.SupervisorBlockExpectation{Owner: owner, TransitionID: expected})
	return err
}

// blockExecutingTaskFromSupervisor is blockTaskFromSupervisor for a task that
// must still be executing under this agent's doer claim: an admitted, blocked,
// moved, released or reassigned task is left alone. blocked reports whether
// the BLOCKED transition committed.
func blockExecutingTaskFromSupervisor(bb *db.Blackboard, projectRoot, taskID string, authority models.AgentAuthority, reason string) (blocked bool, err error) {
	pipelineTransitions, err := ops.LoadPipelineTransitions(projectRoot)
	if err != nil {
		return false, fmt.Errorf("load pipeline transitions for supervisor block: %w", err)
	}
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		return false, fmt.Errorf("load pipeline resolver for supervisor block: %w", err)
	}
	// Locked read: this is the fence in front of the save and the transition.
	state, err := bb.Read()
	if err != nil {
		return false, err
	}
	if err := ops.RequireAgentAuthority(state, authority); err != nil {
		return false, err
	}
	task, executing := findExecutingTaskForAgent(state, taskID, authority.ID, pr)
	if !executing {
		return false, nil
	}
	// The same transition ID inside the transaction proves the task is still
	// in the executing state observed here.
	expected := models.TaskTransitionID(task)
	commitBlockedDoerWIP(projectRoot, authority, taskID, reason)
	outcome, err := applySupervisorBlock(bb, pipelineTransitions, taskID, authority, reason, ops.SupervisorBlockExpectation{Owner: ops.SupervisorBlockOwnerDoer, TransitionID: expected})
	return outcome == ops.SupervisorBlockApplied && err == nil, err
}

// blockReleasedTaskFromSupervisor blocks the task whose claim this
// supervisor's exit just released: the crash and abandoned-job loops decide
// after resetAgentAfterExit. It acts only on that exact unowned continuation,
// named by released, never on another unowned task. Once released the task is
// no longer executing, so no WIP commit is possible; the work stays in the
// kept worktree and the next claim adopts it.
func blockReleasedTaskFromSupervisor(bb *db.Blackboard, projectRoot, taskID string, authority models.AgentAuthority, released ops.ReleasedTaskClaim, reason string) error {
	if released.TaskID != taskID || released.TransitionID == "" {
		GetLogger().Warn("Supervisor block skipped: this exit released no claim on the task", "task_id", taskID, "agent_id", authority.ID)
		return nil
	}
	pipelineTransitions, ptErr := ops.LoadPipelineTransitions(projectRoot)
	if ptErr != nil {
		return fmt.Errorf("load pipeline transitions for supervisor block: %w", ptErr)
	}
	_, err := applySupervisorBlock(bb, pipelineTransitions, taskID, authority, reason, ops.SupervisorBlockExpectation{Owner: ops.SupervisorBlockOwnerReleased, TransitionID: released.TransitionID})
	return err
}

// commitBlockedDoerWIP saves a blocked doer's uncommitted work. A refusal or
// failure only logs: the block proceeds and the kept worktree still holds the
// work, which the next claim adopts.
func commitBlockedDoerWIP(projectRoot string, authority models.AgentAuthority, taskID, reason string) {
	message := fmt.Sprintf("WIP: supervisor block, auto-committed by supervisor, hooks skipped\n\nTask: %s\nAgent: %s\nReason: %s\n",
		taskID, authority.ID, reason)
	// No caller context: the locks taken carry their own acquisition timeouts,
	// and a supervisor that is shutting down must still save the work.
	result, err := ops.CommitTerminatedDoerWIP(context.Background(), projectRoot, authority, taskID, message)
	if err != nil {
		GetLogger().Warn("Failed to commit blocked task work in progress", "error", err, "task_id", taskID, "agent_id", authority.ID)
		return
	}
	if !result.Committed {
		GetLogger().Info("Blocked task work in progress not committed", "reason", result.SkipReason, "task_id", taskID, "agent_id", authority.ID)
		return
	}
	GetLogger().Info("Committed blocked task work in progress", "commit", result.SHA, "task_id", taskID, "agent_id", authority.ID)
}

// applySupervisorBlock asks ops to move the task to BLOCKED while the observed
// owner and lifecycle still hold, and reports a stale observation.
func applySupervisorBlock(bb *db.Blackboard, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, taskID string, authority models.AgentAuthority, reason string, expected ops.SupervisorBlockExpectation) (ops.SupervisorBlockOutcome, error) {
	if supervisorBlockBeforeModifyTestHook != nil {
		supervisorBlockBeforeModifyTestHook()
	}
	outcome, err := ops.BlockSupervisorTask(bb, pipelineTransitions, authority, taskID, reason, expected)
	if err == nil && outcome == ops.SupervisorBlockStale {
		GetLogger().Warn("Supervisor block skipped: task lifecycle moved before the block; left to its current owner", "task_id", taskID, "agent_id", authority.ID)
	}
	return outcome, err
}
