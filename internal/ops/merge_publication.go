package ops

import (
	"errors"
	"fmt"
	"log"

	"github.com/liza-mas/liza/internal/git"
)

// maxMergeRetries is the maximum number of CAS retry attempts for the merge loop.
const maxMergeRetries = 3

// mergeCommitMessageFormat renders the true-merge commit message from the task
// ID, which names both the merged work and its task branch.
const mergeCommitMessageFormat = "Merge %s (task/%s)"

// mergeCASRetryTestHook is a test-only hook invoked after reading integration HEAD
// in each CAS attempt and before merge/ref-update logic runs.
// Production code leaves this nil.
var mergeCASRetryTestHook func(attempt int, integrationRef, preMergeHEAD string) error

// casMergeOutcome holds the result of a compare-and-swap merge into an integration ref.
type casMergeOutcome struct {
	mergeCommit  string // SHA of the resulting commit (merge commit or fast-forwarded task commit)
	preMergeHEAD string // integration HEAD before the merge (needed for working tree sync and rollback)
	fastForward  bool   // true when the merge was a fast-forward or the commit was already merged
	conflict     bool   // true when merge-tree found conflicts; caller handles state transition
}

// performCASMerge merges expectedCommit into integrationRef using a compare-and-swap
// retry loop to handle concurrent merges. Returns conflict=true when merge-tree
// detects conflicts (caller is responsible for the INTEGRATION_FAILED transition).
func performCASMerge(gw *git.Git, integrationRef, expectedCommit, taskID string, preUpdateHook func(candidateTreeish string) error) (*casMergeOutcome, error) {
	for attempt := 0; attempt < maxMergeRetries; attempt++ {
		if attempt > 0 {
			log.Printf("wt-merge %s: CAS retry attempt %d/%d", taskID, attempt+1, maxMergeRetries)
		}
		outcome, retry, err := attemptCASMerge(gw, integrationRef, expectedCommit, taskID, attempt, preUpdateHook)
		if err != nil {
			return nil, err
		}
		if !retry {
			return outcome, nil
		}
	}
	return nil, fmt.Errorf("merge CAS failed after %d attempts — high contention on %s", maxMergeRetries, integrationRef)
}

// attemptCASMerge runs one compare-and-swap attempt against the integration HEAD
// it reads. retry reports that the attempt lost the race (or its guard saw a
// stale HEAD) and a fresh attempt may still succeed.
func attemptCASMerge(gw *git.Git, integrationRef, expectedCommit, taskID string, attempt int, preUpdateHook func(candidateTreeish string) error) (outcome *casMergeOutcome, retry bool, err error) {
	preMergeHEAD, err := gw.GetCommitSHA(integrationRef)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get integration HEAD: %w", err)
	}
	if mergeCASRetryTestHook != nil {
		if hookErr := mergeCASRetryTestHook(attempt, integrationRef, preMergeHEAD); hookErr != nil {
			return nil, false, fmt.Errorf("merge CAS retry hook failed: %w", hookErr)
		}
	}

	// Already merged — expectedCommit is ancestor of integration HEAD.
	isAncestor, err := gw.IsAncestor(expectedCommit, preMergeHEAD)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check ancestry: %w", err)
	}
	if isAncestor {
		return &casMergeOutcome{mergeCommit: preMergeHEAD, preMergeHEAD: preMergeHEAD, fastForward: true}, false, nil
	}

	// Fast-forward: integration HEAD is ancestor of expected commit.
	isFF, err := gw.IsAncestor(preMergeHEAD, expectedCommit)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check fast-forward: %w", err)
	}
	if isFF {
		retry, err := publishGuardedRef(gw, integrationRef, expectedCommit, preMergeHEAD, preUpdateHook, "failed to fast-forward integration branch")
		if err != nil || retry {
			return nil, retry, err
		}
		return &casMergeOutcome{mergeCommit: expectedCommit, preMergeHEAD: preMergeHEAD, fastForward: true}, false, nil
	}

	// True merge — use merge-tree (no working tree modification).
	treeSHA, clean, err := gw.MergeTree(preMergeHEAD, expectedCommit)
	if err != nil {
		return nil, false, fmt.Errorf("merge-tree computation failed: %w", err)
	}
	if !clean {
		return &casMergeOutcome{preMergeHEAD: preMergeHEAD, conflict: true}, false, nil
	}

	mergeCommit, err := gw.CreateCommitFromTree(treeSHA, []string{preMergeHEAD, expectedCommit}, fmt.Sprintf(mergeCommitMessageFormat, taskID, taskID))
	if err != nil {
		return nil, false, fmt.Errorf("failed to create merge commit: %w", err)
	}
	retry, err = publishGuardedRef(gw, integrationRef, mergeCommit, preMergeHEAD, preUpdateHook, "failed to update integration branch")
	if err != nil || retry {
		return nil, retry, err
	}
	return &casMergeOutcome{mergeCommit: mergeCommit, preMergeHEAD: preMergeHEAD}, false, nil
}

// publishGuardedRef advances integrationRef from preMergeHEAD to candidate with a
// compare-and-swap, after the pre-update guard accepted candidate. It is the one
// publication step shared by the fast-forward and true-merge branches. retry
// reports a lost race: either the guard failed while the HEAD it read had moved,
// or update-ref found the ref no longer at preMergeHEAD. Any other failure is an
// error; updateFailure prefixes an update-ref error.
func publishGuardedRef(gw *git.Git, integrationRef, candidate, preMergeHEAD string, preUpdateHook func(candidateTreeish string) error, updateFailure string) (retry bool, err error) {
	if preUpdateHook != nil {
		if hookErr := preUpdateHook(candidate); hookErr != nil {
			return handlePreUpdateHookFailure(gw, integrationRef, preMergeHEAD, hookErr)
		}
	}
	if updateErr := gw.UpdateRef(integrationRef, candidate, preMergeHEAD); updateErr != nil {
		var casErr *git.RefConflictError
		if errors.As(updateErr, &casErr) {
			return true, nil
		}
		return false, fmt.Errorf("%s: %w", updateFailure, updateErr)
	}
	return false, nil
}

func handlePreUpdateHookFailure(gw *git.Git, integrationRef, preMergeHEAD string, hookErr error) (bool, error) {
	currentHEAD, err := gw.GetCommitSHA(integrationRef)
	if err != nil {
		stalenessErr := fmt.Errorf("failed to re-read integration HEAD: %w", err)
		return false, fmt.Errorf("pre-update hook failed and staleness could not be verified: %w", errors.Join(hookErr, stalenessErr))
	}
	if currentHEAD != preMergeHEAD {
		return true, nil
	}
	return false, hookErr
}
