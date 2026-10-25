package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// submissionRun is what the phases of one submit-for-review preparation share,
// extended in order: snapshot preconditions (this file), task-locked Git work
// and the preparation marker (submission_git_work.go), the batch admission plan
// (submission_admission.go), and the final transaction (submission_finalize.go).
// It owns no lock: each phase runs under the locks its caller holds.
type submissionRun struct {
	projectRoot, taskID, commitRef, agentID string
	authority                               *models.AgentAuthority
	opts                                    LifecycleRequestOptions
	invocation                              *submissionInvocation

	// Snapshot preconditions.
	bb             *db.Blackboard
	state          *models.State
	task           *models.Task
	request        LifecycleRequest
	runtimeRole    string
	resolver       *pipeline.Resolver
	expectedStatus models.TaskStatus
	targetStatus   models.TaskStatus
	transitions    map[models.TaskStatus][]models.TaskStatus

	// Task-locked Git work.
	git               *gitpkg.Git
	worktreePath      string
	expectedBranch    string
	preRebaseCommit   string
	integrationBranch string
	rebaseBase        string
	acceptance        *acceptanceInput

	// Batch admission.
	admissionConfig models.Config
	held            *admissionSlots
	retryPlan       *timeoutRetryPlan
	limited         bool

	// After the rebase: the candidate to publish and the boundary it is
	// published under (the state copy still holds the pre-submit claim base).
	postRebaseCommit string
	boundaryTask     models.Task
}

// readSnapshot validates the payload structurally before the boundary reads
// state, resolves a ref or touches the worktree, then reads one state snapshot.
// A retained receipt for the request replays: the returned submission is final.
func (s *submissionRun) readSnapshot() (*preparedSubmission, error) {
	if err := rejectInvalidLifecyclePayload(integrationOperationSubmitForReview, SubmitForReviewPayload(s.commitRef)); err != nil {
		return nil, err
	}

	lp := paths.New(s.projectRoot)
	s.bb = RequestBlackboard(lp.StatePath(), s.authority, s.opts)

	runtimeRole, err := identity.ExtractRole(s.agentID)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("invalid agent ID format: %s (%v)", s.agentID, err)}
	}
	s.runtimeRole = runtimeRole

	// Phase 1: Read state to get config and validate preconditions
	state, task, err := readTaskState(s.bb, s.taskID)
	if err != nil {
		return nil, err
	}
	if s.authority != nil {
		if err := RequireAgentAuthority(state, *s.authority); err != nil {
			return nil, err
		}
	}
	s.state, s.task = state, task
	s.invocation.task = task
	request, receipt, err := submissionRequest(task, s.commitRef, s.agentID, s.authority, s.opts, state.Agents)
	if err != nil {
		return nil, err
	}
	s.request = request
	if receipt != nil {
		return replaySubmission(task, receipt, s.agentID), nil
	}
	return nil, nil
}

// resolvePipeline resolves the executing and submitted statuses of the task's
// role pair from the pipeline configuration.
func (s *submissionRun) resolvePipeline() error {
	resolver, _, resolverErr := loadResolver(s.projectRoot)
	if resolverErr != nil {
		return resolverErr
	}
	s.resolver = resolver
	if s.task.RolePair == "" {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no role_pair set", s.taskID)}
	}
	expectedCurrentStatus, err := resolver.ExecutingStatus(s.task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", s.task.RolePair)}
	}
	targetSubmittedStatus, err := resolver.SubmittedStatus(s.task.RolePair)
	if err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("unrecognized role-pair %q — check pipeline.yaml config", s.task.RolePair)}
	}
	s.expectedStatus, s.targetStatus = expectedCurrentStatus, targetSubmittedStatus
	s.transitions = BuildPipelineTransitions(resolver)
	return nil
}

// checkOwnership refuses a snapshot that is not an executing task of this
// agent with a worktree, a pre-execution checkpoint and resolvable output.
func (s *submissionRun) checkOwnership() error {
	task := s.task
	if task.Status != s.expectedStatus {
		return &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, task, models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone),
			Err:     &PreconditionError{Reason: fmt.Sprintf("task %s is not %s (current status: %s)", s.taskID, s.expectedStatus, task.Status)},
		}
	}

	if task.AssignedTo == nil || *task.AssignedTo != s.agentID {
		return &LifecycleError{
			Outcome: NewLifecycleOutcome(integrationOperationSubmitForReview, task, models.LifecycleStaleCaller, LifecycleSafeActionStop, LifecycleEffectsNone),
			Err:     &PreconditionError{Reason: notAssignedReason(s.taskID, s.agentID, task)},
		}
	}

	if task.Worktree == nil {
		return &PreconditionError{Reason: fmt.Sprintf("task %s has no worktree", s.taskID)}
	}

	// Pre-execution checkpoint required before submission
	if !HasCheckpoint(task.History, s.agentID) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s: pre-execution checkpoint required before submission (use %q)", s.taskID, brand.Command("write-checkpoint", s.taskID))}
	}
	if err := validateOutputArtifactRefScalars(s.taskID, task.Output); err != nil {
		return err
	}
	return rejectStaleDraftOutput(s.state, s.resolver, task)
}

// notAssignedReason words the refusal of a task another agent, or none, holds.
func notAssignedReason(taskID, agentID string, task *models.Task) string {
	currentAgent := "none"
	if task.AssignedTo != nil {
		currentAgent = *task.AssignedTo
	}
	return fmt.Sprintf("task %s is not assigned to agent %s (currently assigned to: %s)", taskID, agentID, currentAgent)
}
