package ops

import (
	"errors"
	"fmt"
	"log"
	"slices"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// Test seams restore their previous values with t.Cleanup. Production keeps
// the hook nil and uses the shared transition validator directly.
var integrationMutationReceiptPersistTestHook func(models.IntegrationMutationReceipt)
var validateIntegrationLifecycleTransition = statevalidate.ValidateIntegrationLifecycleTransition

type integrationRefMutation struct {
	taskID       string
	beforeCommit string
	afterCommit  string
}

func (mutation *integrationRefMutation) receipt() models.IntegrationMutationReceipt {
	return models.IntegrationMutationReceipt{
		TaskID:       mutation.taskID,
		BeforeCommit: mutation.beforeCommit,
		AfterCommit:  mutation.afterCommit,
	}
}

func persistIntegrationMutationReceipt(bb *db.Blackboard, mutation *integrationRefMutation, authority *models.AgentAuthority) error {
	if mutation == nil || mutation.beforeCommit == mutation.afterCommit {
		return nil
	}
	receipt := mutation.receipt()
	if integrationMutationReceiptPersistTestHook != nil {
		integrationMutationReceiptPersistTestHook(receipt)
	}
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		previous := *state
		previous.Goal = state.Goal
		if lifecycle := state.Goal.Integration; lifecycle != nil {
			previousLifecycle := *lifecycle
			previousLifecycle.MutationReceipts = slices.Clone(lifecycle.MutationReceipts)
			previous.Goal.Integration = &previousLifecycle
		} else {
			state.Goal.Integration = &models.IntegrationLifecycle{}
		}

		state.Goal.Integration.MutationReceipts = append(state.Goal.Integration.MutationReceipts, receipt)
		if err := invalidateGoalCompleteStopForMutation(state, receipt); err != nil {
			return err
		}
		return validateIntegrationLifecycleTransition(&previous, state)
	})
}

// provenMergeEffect returns the integration-ref movement this task already
// made, or nil when no live proof of it exists.
//
// Between the CAS merge and the MERGED write the integration ref has moved and
// persistIntegrationMutationReceipt has durably recorded that this task moved
// it. Two independent conditions make that record proof rather than a hint: the
// recorded commit must contain the approved review commit, which excludes the
// reverse receipt written by a rollback, and it must still be an ancestor of the
// integration ref, which excludes a merge that was later rolled back or lost.
func provenMergeEffect(state *models.State, gw *git.Git, integrationRef, taskID, expectedCommit string) (*integrationRefMutation, error) {
	if state.Goal.Integration == nil {
		return nil, nil
	}
	var candidate *models.IntegrationMutationReceipt
	for i := range state.Goal.Integration.MutationReceipts {
		if state.Goal.Integration.MutationReceipts[i].TaskID == taskID {
			candidate = &state.Goal.Integration.MutationReceipts[i]
		}
	}
	if candidate == nil || candidate.AfterCommit == "" {
		return nil, nil
	}
	carriesApproved, err := gw.IsAncestor(expectedCommit, candidate.AfterCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to check recorded merge ancestry: %w", err)
	}
	if !carriesApproved {
		return nil, nil
	}
	head, err := gw.GetCommitSHA(integrationRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get integration HEAD: %w", err)
	}
	stillLive, err := gw.IsAncestor(candidate.AfterCommit, head)
	if err != nil {
		return nil, fmt.Errorf("failed to check recorded merge liveness: %w", err)
	}
	if !stillLive {
		return nil, nil
	}
	return &integrationRefMutation{
		taskID:       taskID,
		beforeCommit: candidate.BeforeCommit,
		afterCommit:  candidate.AfterCommit,
	}, nil
}

func rollbackMergedCommit(projectRoot string, gitWrapper *git.Git, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID string) (*integrationRefMutation, error) {
	var mutation *integrationRefMutation
	err := withIntegrationMutationLock(projectRoot, integrationLockRollback+taskID, func() error {
		if err := gitWrapper.UpdateRef(integrationRef, preMergeHEAD, mergeCommit); err != nil {
			var casErr *git.RefConflictError
			if errors.As(err, &casErr) {
				log.Printf("wt-merge %s: skipping rollback — another merge landed on top of %s", taskID, shortSHA(mergeCommit))
				return nil
			}
			return err
		}
		mutation = &integrationRefMutation{taskID: taskID, beforeCommit: mergeCommit, afterCommit: preMergeHEAD}

		// Ref rolled back — sync working tree to match pre-merge state. This
		// reverse sync undoes the forward SyncMergedFiles step.
		if syncErr := gitWrapper.SyncMergedFiles(mergeCommit, preMergeHEAD); syncErr != nil {
			log.Printf("wt-merge %s: WARNING — failed to sync working tree after rollback: %v", taskID, syncErr)
		}
		if restoreRef != "" {
			if restoreErr := gitWrapper.RestoreSyncedFiles(preMergeHEAD, mergeCommit, restoreRef); restoreErr != nil {
				return fmt.Errorf("failed to restore working tree after rollback: %w", restoreErr)
			}
		}
		return nil
	})
	return mutation, err
}

// rollbackMergedCommitAndPersist reports whether the integration ref actually
// moved back. It does not when another merge landed on top, and it cannot when
// the baseline equals the merge commit — a resumed merge whose own pre-merge
// HEAD is no longer known from this invocation. Callers must not describe those
// outcomes as a rollback: the task's commit is still in the integration branch.
func rollbackMergedCommitAndPersist(bb *db.Blackboard, projectRoot string, gitWrapper *git.Git, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID string, authority *models.AgentAuthority) (bool, error) {
	rewound := false
	err := withEffectiveIntegrationCompletionLinearization(projectRoot, integrationLockRollback+taskID, func() error {
		if preMergeHEAD == "" || preMergeHEAD == mergeCommit {
			return nil
		}
		mutation, rollbackErr := rollbackMergedCommit(projectRoot, gitWrapper, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID)
		rewound = mutation != nil
		if receiptErr := persistIntegrationMutationReceipt(bb, mutation, authority); receiptErr != nil {
			receiptErr = fmt.Errorf("failed to persist rollback integration mutation receipt: %w", receiptErr)
			if rollbackErr != nil {
				return errors.Join(rollbackErr, receiptErr)
			}
			return receiptErr
		}
		return rollbackErr
	})
	return rewound, err
}

// integrationRetentionDetail names the state Git is actually left in when a
// failed merge was not rolled back, so the recorded diagnostic cannot imply a
// rewind that never happened.
func integrationRetentionDetail(rewound bool, rollbackErr error, mergeCommit string) string {
	if rewound || rollbackErr != nil {
		return ""
	}
	return fmt.Sprintf("integration branch retains %s: it was not rolled back", shortSHA(mergeCommit))
}
