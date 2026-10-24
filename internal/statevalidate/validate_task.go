package statevalidate

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateRequiredFields checks that the top-level state structure contains all
// mandatory fields (version, goal, tasks, agents, config, sprint). Prevents
// operating on a partially-initialised or corrupted state file.
func validateRequiredFields(v *violations, state *models.State, projectRoot string, skipSpecFileCheck bool) {
	if state.Version == 0 {
		v.add(fmt.Errorf("missing required field 'version'"))
	}

	if state.Goal.ID == "" {
		v.add(fmt.Errorf("missing required field 'goal'"))
	}

	if state.Tasks == nil {
		v.add(fmt.Errorf("missing required field 'tasks'"))
	}

	if state.Agents == nil {
		v.add(fmt.Errorf("missing required field 'agents'"))
	}

	if state.Config.IntegrationBranch == "" {
		v.add(fmt.Errorf("missing required field 'config'"))
	}

	if state.Sprint.ID == "" {
		v.add(fmt.Errorf("missing required field 'sprint'"))
	}

	if !skipSpecFileCheck && state.Goal.SpecRef != "" {
		if err := checkSpecFileExists(projectRoot, state.Goal.SpecRef, state.Config.IntegrationBranch); err != nil {
			v.add(fmt.Errorf("goal %w", err))
		}
	}
}

// validateTaskStates ensures every task has a valid status (either hardcoded or
// pipeline-declared), a valid task type, and — for pipeline-configured goals —
// a role_pair that maps to a known pipeline role pair. Prevents tasks from
// entering undefined lifecycle states.
func validateTaskStates(v *violations, state *models.State, resolver *pipeline.Resolver) {
	for _, task := range state.Tasks {
		statusValid := task.Status.IsValid()
		if !statusValid && resolver != nil {
			// Accept pipeline-declared states and cross-cutting meta-states
			statusValid = task.Status.IsPipelineValid(resolver.AllDeclaredStates())
		}
		if !statusValid {
			v.add(fmt.Errorf("unknown task status '%s' for task %s", task.Status, task.ID))
		}
		if !task.EffectiveType().IsValid() {
			v.add(fmt.Errorf("unknown task type '%s' for task %s", task.Type, task.ID))
		}

		// Pipeline-goal tasks: role_pair is required unconditionally
		if resolver != nil {
			if task.RolePair == "" {
				v.add(fmt.Errorf("task %s missing role_pair (required for pipeline-configured goals)", task.ID))
			} else if _, err := resolver.InitialStatus(task.RolePair); err != nil {
				v.add(fmt.Errorf("task %s has invalid role_pair %q: %w", task.ID, task.RolePair, err))
			}
		}
	}
}

// validateTaskInvariants enforces structural invariants across all tasks:
// status-specific required fields, single-assignment per agent, worktree
// existence for executing tasks, completion field presence, spec_ref validity,
// integration_fix history consistency, failed_by uniqueness, parent_task
// referential integrity, and output entry completeness. Prevents invalid task
// state combinations that would cause downstream agent or merge failures.
// Each constraint is checked on its own, so one defect of a task does not hide
// another.
func validateTaskInvariants(v *violations, state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) {
	assignments := make(agentAssignments)
	taskIDs := buildTaskIDSet(state.Tasks)
	sc := newStatusClassifier(resolver, cfg)

	for _, task := range state.Tasks {
		validateTaskLifecycle(v, &task)
		validateTerminalArchiveShape(v, &task)
		validateStateLockHoldShape(v, &task)
		validateStatusFields(v, &task, &sc)
		validateTaskValidationCommands(v, &task)
		assignments.collect(&task, resolver)
		validateExecutingWorktree(v, &task, &sc, projectRoot)
		validateCompletionFields(v, &task, resolver, cfg)
		validateTaskArtifactRefs(v, &task, state, projectRoot, skipSpecFileCheck)
		validateIntegrationFixHistory(v, &task)
		validateFailedByUnique(v, &task)
		validateParentTasks(v, &task, taskIDs)
		validateTaskOutput(v, &task, !artifactRefsRetired(task))
		validateAcceptanceState(v, &task)
		validatePlanCheck(v, &task, state, resolver)
		validatePlanAmendment(v, &task, state, resolver)
		validateValidationNotes(v, &task, state, resolver)
		validateAttempt(v, &task, &sc)
	}

	assignments.validate(v)
}

// validateStatusFields checks that each task status has the fields required by
// its lifecycle phase (e.g. executing tasks need assigned_to, worktree,
// base_commit, and lease_expires; reviewing tasks need reviewing_by and
// review_lease_expires). Prevents tasks from entering states without the
// metadata needed for agents to operate on them.
//
// Messages name the status for the operator. Identities name the phase and
// the field instead, so a task moving between statuses of the same phase keeps
// its violation rather than trading an old one for a new one.
func validateStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	validateInitialStatusFields(v, task, sc)
	validateExecutingStatusFields(v, task, sc)
	validateSubmittedStatusFields(v, task, sc)
	validateReviewingStatusFields(v, task, sc)
	validateApprovedStatusFields(v, task, sc)
	validateIntegrationFailureStatus(v, task, sc)
	validateMergedStatusFields(v, task)
	validateBlockedStatusFields(v, task)
	validateRejectedStatusFields(v, task, sc)
	validateSupersededStatusFields(v, task)
	validateTaskRejectionRCA(v, task)
}
