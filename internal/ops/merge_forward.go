package ops

import (
	"errors"
	"fmt"
)

// publish advances the integration ref with the approved commit. A conflict is
// recorded as INTEGRATION_FAILED; any other failure is classified by whether it
// proves nothing was published.
func (m *mergeInvocation) publish() error {
	if err := m.publishForward(); err != nil {
		return m.classifyPublicationFailure(err)
	}
	if m.outcome.conflict {
		return m.recordConflict()
	}
	return nil
}

// publishForward runs the guarded compare-and-swap and the main-index sync under
// the completion linearization and the integration mutation lock, then persists
// the mutation receipt once the mutation lock is released. A receipt that cannot
// be persisted rolls the merge back, so no unrecorded mutation survives.
func (m *mergeInvocation) publishForward() error {
	artifactGuardHook := buildArtifactGuardHook(m.bb, m.projectRoot, m.git, m.taskID)
	var forwardMutation *integrationRefMutation
	return withEffectiveIntegrationCompletionLinearization(m.projectRoot, integrationLockForward+m.taskID, func() error {
		mutationErr := withIntegrationMutationLock(m.projectRoot, integrationLockForward+m.taskID, func() error {
			var mergeErr error
			m.casStarted = true
			m.outcome, mergeErr = performCASMerge(m.git, m.integrationRef, m.expectedCommit, m.taskID, artifactGuardHook)
			if mergeErr != nil || m.outcome.conflict {
				return mergeErr
			}
			if m.outcome.preMergeHEAD != m.outcome.mergeCommit {
				forwardMutation = &integrationRefMutation{
					taskID:       m.taskID,
					beforeCommit: m.outcome.preMergeHEAD,
					afterCommit:  m.outcome.mergeCommit,
				}
			}
			if syncErr := m.git.SyncMergedFiles(m.outcome.preMergeHEAD, m.outcome.mergeCommit); syncErr != nil {
				return fmt.Errorf("failed to sync working tree after merge: %w", syncErr)
			}
			return nil
		})
		if receiptErr := persistIntegrationMutationReceipt(m.bb, forwardMutation, m.authority); receiptErr != nil {
			return m.rollbackAfterReceiptFailure(mutationErr, fmt.Errorf("failed to persist integration mutation receipt: %w", receiptErr))
		}
		return mutationErr
	})
}

// rollbackAfterReceiptFailure rewinds the ref this invocation moved, only if it
// still points at the invocation's own merge.
func (m *mergeInvocation) rollbackAfterReceiptFailure(mutationErr, receiptErr error) error {
	rollbackMutation, rollbackErr := rollbackMergedCommit(
		m.projectRoot,
		m.git,
		m.integrationRef,
		m.outcome.preMergeHEAD,
		m.outcome.mergeCommit,
		"HEAD",
		m.taskID,
	)
	if rollbackErr == nil && rollbackMutation == nil {
		rollbackErr = fmt.Errorf("failed to roll back integration ref after receipt persistence failure: ref changed from %s", shortSHA(m.outcome.mergeCommit))
	}
	m.forwardRolledBack = rollbackErr == nil && rollbackMutation != nil
	return errors.Join(mutationErr, receiptErr, rollbackErr)
}

// attribute fixes the commit this task is credited with and the commit a failed
// validation must rewind the ref to.
//
// CAS found the approved commit already merged when the outcome's merge commit
// equals its pre-merge HEAD, so outcome.mergeCommit is whatever the integration
// ref points at now — which is another task's merge commit if one landed since.
// Where a receipt records what this task published, that is the commit to
// attribute to it. The rollback baseline is the HEAD this invocation merged
// onto on the forward path; on a resume the ref already carries the merge, so
// only the recorded receipt knows what preceded it.
func (m *mergeInvocation) attribute() error {
	m.mergeCommit = m.outcome.mergeCommit
	m.rollbackBaseline = m.outcome.preMergeHEAD
	if m.outcome.preMergeHEAD != m.outcome.mergeCommit {
		return nil
	}
	recordedState, readErr := m.bb.Read()
	if readErr != nil {
		return fmt.Errorf("failed to read state for merge attribution: %w", readErr)
	}
	recorded, provenErr := provenMergeEffect(recordedState, m.git, m.integrationRef, m.taskID, m.expectedCommit)
	if provenErr != nil {
		return provenErr
	}
	if recorded != nil {
		m.mergeCommit = recorded.afterCommit
		m.rollbackBaseline = recorded.beforeCommit
	}
	return nil
}

// detectCheckout notes the checked-out branch, needed for the working tree
// restore on both the success and the rollback paths. A branch other than the
// integration branch is restored from HEAD.
func (m *mergeInvocation) detectCheckout() {
	currentBranch, branchErr := m.git.GetCurrentBranch()
	if branchErr != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("skipped working tree restore (branch detection failed: %v)", branchErr))
		return
	}
	if currentBranch != m.integrationBranch {
		m.restoreRef = "HEAD"
	}
}
