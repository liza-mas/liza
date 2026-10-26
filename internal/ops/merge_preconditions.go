package ops

import (
	"fmt"
	"log"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

// checkPreconditions refuses a merge that is not approved, has no review
// commit, has unreconciled verdicts, or whose worktree no longer holds the
// approved commit. It resolves the commit and integration ref the later phases
// use. Only the HEAD mismatch writes state.
func (m *mergeInvocation) checkPreconditions() error {
	// Load pipeline bundle once for all pipeline-aware checks
	pb, pbErr := loadPipelineBundle(m.projectRoot)
	if pbErr != nil {
		return fmt.Errorf("failed to load pipeline config: %w", pbErr)
	}
	m.pb = pb

	if !models.IsApprovedForMerge(m.task, pb.pr) {
		return WrapLifecycleError(integrationOperationWTMerge, m.task, &PreconditionError{Reason: fmt.Sprintf("task must be in an approved state to merge (current status: %s)", m.task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}

	if m.task.ReviewCommit == nil {
		return &PreconditionError{Reason: "task has no review_commit"}
	}
	if err := requireReconciledVerdicts(m.state, m.task, *m.task.ReviewCommit); err != nil {
		return err
	}

	// Normalize review_commit to full SHA.
	m.reviewCommit = *m.task.ReviewCommit
	expectedCommit, err := m.git.GetCommitSHA(m.reviewCommit)
	if err != nil {
		return fmt.Errorf("review_commit (%s) not found in repository: %w", m.reviewCommit, err)
	}
	m.expectedCommit = expectedCommit

	if err := m.checkWorktreeHead(); err != nil {
		return err
	}

	m.integrationBranch = integrationBranchOf(m.state)
	m.integrationRef = integrationBranchRefPrefix + m.integrationBranch
	return nil
}

// checkWorktreeHead verifies the task worktree still holds the approved commit
// to detect tampering. A missing worktree (e.g. cleared by task recovery after
// Ctrl-C) skips the check — the approved commit is still in git and safe to merge.
func (m *mergeInvocation) checkWorktreeHead() error {
	if m.task.Worktree == nil {
		log.Printf("wt-merge %s: WARNING — worktree missing (cleared by recovery?), proceeding with review_commit %s", m.taskID, shortSHA(m.expectedCommit))
		return nil
	}
	wtHEAD, err := m.git.GetWorktreeHEAD(m.taskID)
	if err != nil {
		return fmt.Errorf("failed to get worktree HEAD: %w", err)
	}
	if wtHEAD != m.expectedCommit {
		// HEAD mismatch indicates state corruption — stops retry loops, preserves worktree
		return m.recordHeadMismatch(wtHEAD)
	}
	return nil
}

// prepare persists the request before the integration ref is touched. A
// concurrent or restarted invocation must requery while this preparation is
// unresolved. From here on the invocation's effects are unknown.
func (m *mergeInvocation) prepare() error {
	err := modifyLifecycleState(m.bb, m.authority, func(s *models.State) error {
		// Admission point: a preparation committed before a halt may finish;
		// none is started after it.
		if err := RequireWorkAdmitted(s, integrationOperationWTMerge); err != nil {
			return err
		}
		live := s.FindTask(m.taskID)
		if live == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: m.taskID}
		}
		if !models.IsApprovedForMerge(live, m.pb.pr) || live.ReviewCommit == nil || *live.ReviewCommit != m.reviewCommit {
			return WrapLifecycleError(integrationOperationWTMerge, live, fmt.Errorf("approved review boundary changed"), models.LifecycleStateChanged, "requery", "none")
		}
		if err := PrepareLifecycleRequest(live, m.request, m.state.Agents); err != nil {
			return err
		}
		m.preparation = *live.Lifecycle.Preparation
		return nil
	})
	if err != nil {
		return err
	}
	if mergePreparedTestHook != nil {
		mergePreparedTestHook(m.taskID)
	}
	m.effects = LifecycleEffectsUnknown
	return nil
}
