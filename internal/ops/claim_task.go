package ops

import (
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ClaimResult contains the outcome of a successful task claim.
type ClaimResult struct {
	models.LifecycleOutcome
	TaskID            string            `json:"task_id"`
	AgentID           string            `json:"agent_id"`
	SourceStatus      models.TaskStatus `json:"source_status"`
	WorktreeRel       string            `json:"worktree_rel"`
	BaseCommit        string            `json:"base_commit"`
	LeaseExpires      time.Time         `json:"lease_expires"`
	IntegrationFix    bool              `json:"integration_fix"`
	PreviousAssignee  string            `json:"previous_assignee"`  // empty if none
	WorktreeRecreated bool              `json:"worktree_recreated"` // true if old worktree was deleted and new one created
	Warnings          []string          `json:"warnings"`
}

type claimTaskTestHooks struct {
	afterInitialAdmission                  func()
	beforePhase3Modify                     func()
	afterPreservedIntegrationEqualityCheck func()
}

var testClaimTaskHooks *claimTaskTestHooks

// ClaimTask implements the three-phase claim pattern to prevent TOCTOU races.
// Phase 1: Validate under lock (no mutation)
// Phase 2: Handle worktree outside lock
// Phase 3: Re-validate and commit under lock
//
// Returns a structured ClaimResult on success. No terminal I/O.
func ClaimTask(projectRoot, taskID, agentID string) (*ClaimResult, error) {
	return claimTaskWithOptionalAuthority(projectRoot, taskID, agentID, nil)
}

// ClaimTaskWithAuthority fences the phase-three claim write and every
// claim-triggered follow-up write with the caller's registration generation.
func ClaimTaskWithAuthority(projectRoot, taskID string, authority models.AgentAuthority, sessions ...*ValidationSession) (*ClaimResult, error) {
	return claimTaskWithOptionalAuthority(projectRoot, taskID, authority.ID, &authority, sessions...)
}

func claimTaskWithOptionalAuthority(projectRoot, taskID, agentID string, authority *models.AgentAuthority, sessions ...*ValidationSession) (*ClaimResult, error) {
	return ClaimTaskWithRequest(projectRoot, taskID, agentID, authority, LifecycleRequestOptions{}, sessions...)
}

// ClaimTaskWithRequest preserves the caller's original claim identity across
// retries, including after the task has moved beyond its claimed state.
func ClaimTaskWithRequest(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, sessions ...*ValidationSession) (result *ClaimResult, err error) {
	invocation := &ownershipInvocation{operation: "claim-task", opts: opts, authority: authority, metrics: NewLifecycleInvocation(projectRoot)}
	defer func() { err = invocation.finish(projectRoot, err) }()
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	err = WithProjectLifecycleSharedLock(projectRoot, "task-claim-worktree", func() error {
		drainDeferredLifecycleRetirements(projectRoot)
		var claimErr error
		result, claimErr = claimTask(projectRoot, taskID, agentID, authority, invocation, sessions...)
		return claimErr
	})
	if err != nil {
		// Degrade at this boundary rather than in one caller: every production
		// entry point (supervisor claim loop, await-verdict auto-reclaim, CLI)
		// goes through ClaimTask, and a setup failure must bound retries for all
		// of them. See ADR-0117.
		if degradedErr := degradeOnPostWorktreeSetupFailure(projectRoot, taskID, agentID, authority, err); degradedErr != nil {
			return nil, degradedErr
		}
	}
	return result, err
}

// ErrAgentDegraded marks a claim failure that already recorded degraded agent
// health. Callers must stop claiming rather than retry another candidate.
var ErrAgentDegraded = stderrors.New("agent degraded: infrastructure claim failure")

// degradeOnPostWorktreeSetupFailure records degraded health for a setup failure
// and returns the error wrapped with ErrAgentDegraded. Returns nil when the
// error is not a setup failure, or a later setup of its worktree passed,
// leaving the original error untouched.
func degradeOnPostWorktreeSetupFailure(projectRoot, taskID, agentID string, authority *models.AgentAuthority, err error) error {
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		return nil
	}
	classification := ClassifyInfraClaimError(err)
	role, roleErr := identity.ExtractRole(agentID)
	if roleErr != nil {
		log.Printf("WARNING: claim-task %s: cannot resolve role for degraded health: %v", taskID, roleErr)
		return fmt.Errorf("%w: %w", ErrAgentDegraded, err)
	}
	if markErr := MarkAgentDegraded(MarkAgentDegradedInput{
		ProjectRoot:    projectRoot,
		AgentID:        agentID,
		Role:           role,
		Reason:         classification.Reason,
		LastTask:       taskID,
		CandidateTasks: []string{taskID},
		LastError:      err.Error(),
		RecoverHint:    classification.RecoverHint,
		DegradedBy:     "claim_task",
		Authority:      authority,
		SetupOutcome:   setupErr,
	}); markErr != nil {
		if stderrors.Is(markErr, ErrSetupFailureSuperseded) {
			log.Printf("claim-task %s: not degrading %s: a later setup of %s passed", taskID, agentID, setupErr.Dir)
			return nil
		}
		log.Printf("WARNING: claim-task %s: failed to record degraded health: %v", taskID, markErr)
	}
	return fmt.Errorf("%w: %w", ErrAgentDegraded, err)
}

// claimTask is the claim entry orchestration: admission under a read of current
// state, then the preparation and assignment phases under the task worktree
// lock. Private concern files own each phase (claim_task_admission.go,
// claim_task_worktree.go, claim_task_assignment.go, claim_task_cleanup.go).
func claimTask(projectRoot, taskID, agentID string, authority *models.AgentAuthority, invocation *ownershipInvocation, sessions ...*ValidationSession) (*ClaimResult, error) {
	op, replay, err := admitClaimTask(projectRoot, taskID, agentID, authority, invocation, sessions)
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}
	var claimResult *ClaimResult
	err = filelock.New(claimTaskWorktreeLockPath(op.statePath, taskID)).WithLockOperation("claim-task-worktree", func() error {
		var lockedErr error
		claimResult, lockedErr = completeClaimTaskAfterValidation(op)
		return lockedErr
	})
	if err != nil {
		return nil, err
	}
	return claimResult, nil
}

func claimTaskWorktreeLockPath(statePath, taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return statePath + ".claim-" + hex.EncodeToString(sum[:8])
}

// completeClaimTaskAfterValidation runs phases two and three under the task
// worktree lock: replay and recheck, persisted preparation, the external
// worktree work outside the state lock, and the fenced assignment.
func completeClaimTaskAfterValidation(op *claimOperation) (result *ClaimResult, retErr error) {
	replay, err := op.replayUnderTaskLock()
	if err != nil || replay != nil {
		return replay, err
	}
	lockedTask, err := recheckClaimTaskBeforeWorktree(op.bb, op.taskID, op.agentID, op.runtimeRole, op.taskStatus, op.strategy, op.ctx.resolver)
	if err != nil {
		return nil, err
	}
	acceptance, err := op.captureEvidence(lockedTask)
	if err != nil {
		return nil, err
	}
	preparation, err := op.persistPreparation()
	if err != nil {
		return nil, err
	}
	// Arm retirement only after our preparation is persisted. Ordinary returns
	// unwind Git cleanup first; a panic never assigns retErr and stays fenced.
	// The failure is usually state-lock contention, so retirement waits the
	// patient budget and, failing that, is retried by this process's next claim.
	defer func() {
		if retErr != nil {
			retErr = retireOrDeferFailedLifecyclePreparation(op.projectRoot, op.taskID, op.authority, &preparation, retErr, "unknown")
		}
	}()
	op.invocation.effects = true

	prepared, err := op.prepareWorktree()
	if err != nil {
		return nil, err
	}
	assignment := op.newAssignment(acceptance, prepared.preflight)
	if testClaimTaskHooks != nil && testClaimTaskHooks.beforePhase3Modify != nil {
		testClaimTaskHooks.beforePhase3Modify()
	}
	if err := assignment.commit(); err != nil {
		op.cleanupFailedClaim(prepared.phase.created)
		return nil, fmt.Errorf("failed to commit claim: %w", err)
	}
	return op.result(prepared, assignment.leaseExpires), nil
}

func recheckClaimTaskBeforeWorktree(
	bb *db.Blackboard,
	taskID,
	agentID,
	runtimeRole string,
	expectedStatus models.TaskStatus,
	strategy claimStrategy,
	resolver models.PipelineResolver,
) (*models.Task, error) {
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != expectedStatus {
		return nil, fmt.Errorf("race condition: task status changed from %s to %s", expectedStatus, task.Status)
	}
	if reason := models.DoerClaimBlockedReason(state, task, runtimeRole, agentID, resolver, time.Now().UTC()); reason != "" {
		return nil, &PreconditionError{Reason: reason}
	}
	if strategy.requiresDependencyRecheck() || len(task.ProviderDependencies) > 0 || len(task.ProviderReservations) > 0 {
		if unmet := models.UnmetClaimDependencies(task, state, resolver); len(unmet) > 0 {
			return nil, fmt.Errorf("race condition: dependencies changed: %s", formatDependencyResults(unmet))
		}
	}
	agent, err := requireRegisteredClaimAgent(state, agentID, runtimeRole)
	if err != nil {
		return nil, err
	}
	if agent.CurrentTask != nil && *agent.CurrentTask != "" && *agent.CurrentTask != taskID {
		return nil, fmt.Errorf("race condition: agent %s became busy with %s", agentID, *agent.CurrentTask)
	}
	return task, nil
}

func replayClaimResult(task *models.Task, receipt *models.LifecycleReceipt, actor string) *ClaimResult {
	lease, _ := time.Parse(time.RFC3339Nano, receipt.Projection.LeaseExpires)
	return &ClaimResult{LifecycleOutcome: LifecycleReplayOutcome(task, receipt, actor), TaskID: task.ID, AgentID: actor,
		SourceStatus: receipt.Projection.SourceStatus, WorktreeRel: path.Join(paths.WorktreesDirName, task.ID), BaseCommit: receipt.Projection.BaseCommit,
		LeaseExpires: lease, IntegrationFix: receipt.Projection.SourceStatus == models.TaskStatusIntegrationFailed}
}

func formatDependencyResults(results []models.DependencySatisfaction) string {
	var summaries []string
	for _, result := range results {
		summaries = append(summaries, result.Summary())
	}
	return strings.Join(summaries, ", ")
}
