package ops

import (
	"errors"
	"fmt"
	"log"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// admit reads the task and decides whether this invocation may proceed: caller
// authority, work admission, request identity and receipt replay, including the
// resume of a merge an earlier invocation left prepared. A non-nil result is
// the replay of a completed request; the merge does nothing further.
func (m *mergeInvocation) admit() (*MergeResult, error) {
	m.bb = RequestBlackboard(paths.New(m.projectRoot).StatePath(), m.authority, m.opts)
	var err error
	if m.state, m.task, err = readTaskState(m.bb, m.taskID); err != nil {
		return nil, err
	}
	if m.authority != nil {
		if err := RequireAgentAuthority(m.state, *m.authority); err != nil {
			return nil, err
		}
	}
	// Fast refusal before interrupted-preparation recovery, so a refused merge
	// changes nothing; the preparation write below is the authoritative check.
	if err := RequireWorkAdmitted(m.state, integrationOperationWTMerge); err != nil {
		return nil, err
	}
	if m.request, err = m.newRequest(); err != nil {
		return nil, err
	}
	receipt, err := CheckLifecycleRequest(m.task, m.request, m.state.Agents)
	if err != nil {
		if receipt, err = m.resumeRefused(err); err != nil {
			return nil, err
		}
	}
	if receipt == nil {
		return nil, nil
	}
	return &MergeResult{LifecycleOutcome: LifecycleReplayOutcome(m.task, receipt, m.agentID), TaskID: m.taskID, MergeCommit: receipt.Projection.MergeCommit}, nil
}

func (m *mergeInvocation) newRequest() (LifecycleRequest, error) {
	return NewLifecycleRequest(integrationOperationWTMerge, m.task, m.agentID, m.authority, m.opts, m.mergeExtra)
}

// resumeRefused handles CheckLifecycleRequest refusing the request: when the
// refusal is an interrupted wt-merge whose effect is settled, the preparation
// is retired and the request rebuilt against the state the merge finishes from.
// Any other refusal is returned unchanged.
func (m *mergeInvocation) resumeRefused(refusal error) (*models.LifecycleReceipt, error) {
	// A caller that pinned an expected transition asked to act on the exact
	// boundary it inspected. Resuming retires the preparation and advances
	// that boundary, so such a caller requeries instead — and must see no
	// state change from having asked.
	if m.opts.ExpectedTransition != "" {
		return nil, refusal
	}
	resumed, resumeErr := resumeInterruptedMerge(m.bb, m.projectRoot, m.state, m.task, m.request, m.authority)
	if resumeErr != nil {
		return nil, errors.Join(refusal, resumeErr)
	}
	if !resumed {
		return nil, refusal
	}
	// Retiring the preparation advanced the task boundary, so the request
	// built against the old one is stale. Rebuild and re-check against the
	// state the merge will actually finish from.
	var err error
	if m.state, m.task, err = readTaskState(m.bb, m.taskID); err != nil {
		return nil, err
	}
	if m.request, err = m.newRequest(); err != nil {
		return nil, err
	}
	return CheckLifecycleRequest(m.task, m.request, m.state.Agents)
}

// interruptedMergePreparation reports whether the refusal in the caller's way is
// an unresolved wt-merge preparation at this exact boundary, left by any actor.
// mergeWorktreeLifecycle is the only creator of wt-merge preparations and holds
// the task review lock for its whole call, so a preparation seen under that
// lock belongs to an invocation that has ended. Its Git effect is settled from
// Git and receipts, never from the preparer's identity or liveness: a merge
// owner handed over from a departed approver must be able to finish it.
func interruptedMergePreparation(task *models.Task, request LifecycleRequest) bool {
	if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
		return false
	}
	p := task.Lifecycle.Preparation
	return p.Operation == integrationOperationWTMerge && preparationStillCurrent(task, request, nil)
}

// retireProvenMergePreparation clears an interrupted wt-merge preparation whose
// external effect is settled — proven landed, or proven absent — so the merge
// can be finished or retried instead of requerying forever. The fence it lifts
// exists because an unresolved preparation means uncertain effects; the checks
// in resumeInterruptedMerge remove that uncertainty. Exactly-once does not rest
// on this fence — the locked approved-status recheck before publication refuses
// any duplicate.
func retireProvenMergePreparation(bb *db.Blackboard, taskID string, authority *models.AgentAuthority, prepared models.LifecyclePreparation, request LifecycleRequest) error {
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		// Retirement belongs to a retry, which is new work: a halt committed
		// after the retry's first read leaves the preparation for resume.
		if err := RequireWorkAdmitted(state, integrationOperationWTMerge); err != nil {
			return err
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &lizaerrors.NotFoundError{Entity: notFoundEntityTask, ID: taskID}
		}
		if !interruptedMergePreparation(task, request) ||
			!sameLifecycleRequest(task.Lifecycle.Preparation.LifecycleIdentity, prepared.LifecycleIdentity) ||
			task.Lifecycle.Preparation.Boundary != prepared.Boundary {
			return fmt.Errorf("interrupted merge preparation changed before it could be retired")
		}
		models.AdvanceLifecycle(task)
		return nil
	})
}

// resumeInterruptedMerge admits a retry that CheckLifecycleRequest refused,
// when the refusal is an interrupted wt-merge whose integration effect is
// settled: a receipt proves the merge landed, or integration does not carry the
// approved commit, so no effect exists to duplicate. When integration carries
// the approved commit without an attributable receipt the effect is unknown; it
// stays fenced for inspection. It reports false, with no state change, for
// every other refusal.
func resumeInterruptedMerge(bb *db.Blackboard, projectRoot string, state *models.State, task *models.Task, request LifecycleRequest, authority *models.AgentAuthority) (bool, error) {
	if !interruptedMergePreparation(task, request) || task.ReviewCommit == nil {
		return false, nil
	}
	gitWrapper := git.New(projectRoot)
	expectedCommit, err := gitWrapper.GetCommitSHA(*task.ReviewCommit)
	if err != nil {
		return false, nil // An unresolvable review commit is not proof of anything.
	}
	integrationRef := integrationBranchRefPrefix + integrationBranchOf(state)
	proven, err := provenMergeEffect(state, gitWrapper, integrationRef, task.ID, expectedCommit)
	if err != nil {
		return false, err
	}
	preparer := task.Lifecycle.Preparation.Actor
	if proven == nil {
		head, err := gitWrapper.GetCommitSHA(integrationRef)
		if err != nil {
			return false, fmt.Errorf("failed to get integration HEAD: %w", err)
		}
		carried, err := gitWrapper.IsAncestor(expectedCommit, head)
		if err != nil {
			return false, fmt.Errorf("failed to check approved commit reachability: %w", err)
		}
		if carried {
			return false, fmt.Errorf("wt-merge preparation by %s left approved commit %s on integration without an attributable receipt; inspect before recovery", preparer, shortSHA(expectedCommit))
		}
		log.Printf("wt-merge %s: retiring orphaned merge preparation by %s — integration does not carry %s", task.ID, preparer, shortSHA(expectedCommit))
		if err := retireProvenMergePreparation(bb, task.ID, authority, *task.Lifecycle.Preparation, request); err != nil {
			return false, err
		}
		return true, nil
	}
	log.Printf("wt-merge %s: resuming interrupted merge by %s — integration already carries %s", task.ID, preparer, shortSHA(proven.afterCommit))
	if err := retireProvenMergePreparation(bb, task.ID, authority, *task.Lifecycle.Preparation, request); err != nil {
		return false, err
	}
	return true, nil
}
